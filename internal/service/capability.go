package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Capability is what one machine advertises to the tailnet: its identity plus,
// per app, the profiles it holds and whether that app's secrets can travel to
// other machines. Peers exchange these so synckit can reason about what is
// syncable across the whole tailnet instead of each machine guessing alone.
type Capability struct {
	Machine Machine  `json:"machine"`
	Apps    []AppCap `json:"apps"`
}

type AppCap struct {
	ID                  string       `json:"id"`
	Installed           bool         `json:"installed"`
	SecretsCrossMachine bool         `json:"secretsCrossMachine"`
	Profiles            []ProfileCap `json:"profiles"`
}

type ProfileCap struct {
	ID      string `json:"id"`      // native profile id / dir name
	Role    string `json:"role"`    // cross-machine role key (see roleOf)
	Label   string `json:"label"`
	Version string `json:"version"`
	Running bool   `json:"running"`
}

// roleOf derives a stable, cross-machine identity for a profile. Chrome and
// DBeaver ids are already stable ("Default", "Profile 1", "workspace6"), but
// Firefox profile directories carry a random prefix ("v7sfxn9k.default-release")
// that differs on every machine — so for Firefox we key on the suffix after the
// dot, which IS the stable role ("default-release", "default", "dev-edition-default").
func roleOf(app, profileID string) string {
	if app == "firefox" {
		if i := strings.IndexByte(profileID, '.'); i >= 0 && i+1 < len(profileID) {
			return profileID[i+1:]
		}
	}
	return profileID
}

// Capability builds this machine's advertisement from live detection.
func (s *Service) Capability() Capability {
	var apps []AppCap
	for _, a := range s.LocalApps() {
		ac := AppCap{ID: a.ID, Installed: a.Installed, SecretsCrossMachine: a.SecretsCrossMachine}
		for _, inst := range a.Instances {
			ac.Profiles = append(ac.Profiles, ProfileCap{
				ID:      inst.ID,
				Role:    roleOf(a.ID, inst.ID),
				Label:   inst.Label,
				Version: inst.Version,
				Running: inst.Running,
			})
		}
		apps = append(apps, ac)
	}
	return Capability{Machine: s.MachineInfo(), Apps: apps}
}

// PeerCapability fetches a peer's advertisement from its /capabilities endpoint.
func (s *Service) PeerCapability(ip string) (Capability, error) {
	c := &http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%d/capabilities", ip, s.Port)
	resp, err := c.Get(url)
	if err != nil {
		return Capability{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Capability{}, fmt.Errorf("capabilities: %s", resp.Status)
	}
	var cap Capability
	if err := json.NewDecoder(resp.Body).Decode(&cap); err != nil {
		return Capability{}, err
	}
	return cap, nil
}

// ---- the syncability matrix ----

// Cell is one machine's state for a given app/role.
type Cell struct {
	Present   bool   `json:"present"`
	Version   string `json:"version"`
	ProfileID string `json:"profileId"`
	Running   bool   `json:"running"`
}

// Verdict classifies what can flow for a role across the tailnet.
type Verdict string

const (
	// VerdictFull: present on 2+ machines and secrets travel — full clone.
	VerdictFull Verdict = "full"
	// VerdictSettings: present on 2+ machines but secrets are machine-bound
	// (Chrome) — settings/bookmarks/extensions sync, passwords/cookies do not.
	VerdictSettings Verdict = "settings-only"
	// VerdictSeed: present on only one machine — nothing to reconcile yet, but
	// it can be seeded to the others.
	VerdictSeed Verdict = "seed"
)

type MatrixRow struct {
	App                 string          `json:"app"`
	Role                string          `json:"role"`
	SecretsCrossMachine bool            `json:"secretsCrossMachine"`
	Cells               map[string]Cell `json:"cells"` // hostname -> state
	Verdict             Verdict         `json:"verdict"`
	Note                string          `json:"note"`
}

type Matrix struct {
	Machines []string    `json:"machines"` // hostnames, local first
	Rows     []MatrixRow `json:"rows"`
}

// BuildMatrix reconciles the local + peer capabilities into a per-role view of
// which machines hold what, and what can sync between them.
func BuildMatrix(local Capability, peers []Capability) Matrix {
	all := append([]Capability{local}, peers...)

	machines := make([]string, 0, len(all))
	seenHost := map[string]bool{}
	for _, c := range all {
		if !seenHost[c.Machine.Hostname] {
			seenHost[c.Machine.Hostname] = true
			machines = append(machines, c.Machine.Hostname)
		}
	}

	type key struct{ app, role string }
	rows := map[key]*MatrixRow{}
	var order []key
	for _, c := range all {
		for _, a := range c.Apps {
			for _, p := range a.Profiles {
				k := key{a.ID, p.Role}
				r, ok := rows[k]
				if !ok {
					r = &MatrixRow{
						App:                 a.ID,
						Role:                p.Role,
						SecretsCrossMachine: a.SecretsCrossMachine,
						Cells:               map[string]Cell{},
					}
					rows[k] = r
					order = append(order, k)
				}
				r.Cells[c.Machine.Hostname] = Cell{
					Present: true, Version: p.Version, ProfileID: p.ID, Running: p.Running,
				}
			}
		}
	}

	for _, k := range order {
		r := rows[k]
		present := 0
		for _, m := range machines {
			if r.Cells[m].Present {
				present++
			}
		}
		switch {
		case present < 2:
			r.Verdict = VerdictSeed
			r.Note = "on one machine only — can seed to the others"
		case r.SecretsCrossMachine:
			r.Verdict = VerdictFull
			r.Note = "full clone incl. saved logins"
		default:
			r.Verdict = VerdictSettings
			r.Note = "settings, bookmarks & extensions only — passwords/cookies are bound to their origin machine"
		}
	}

	// Stable order: app, then role.
	sort.Slice(order, func(i, j int) bool {
		if order[i].app != order[j].app {
			return order[i].app < order[j].app
		}
		return order[i].role < order[j].role
	})
	out := Matrix{Machines: machines}
	for _, k := range order {
		out.Rows = append(out.Rows, *rows[k])
	}
	return out
}

// TailnetMatrix gathers capabilities from every serving peer and builds the
// syncability matrix for the whole tailnet.
func (s *Service) TailnetMatrix() Matrix {
	local := s.Capability()
	var peers []Capability
	for _, p := range s.Peers() {
		if !p.Serving {
			continue
		}
		if cap, err := s.PeerCapability(p.IP); err == nil {
			peers = append(peers, cap)
		}
	}
	return BuildMatrix(local, peers)
}
