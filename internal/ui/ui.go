// Package ui serves the synckit control dashboard on localhost. It is the
// control plane — distinct from the daemon's tailnet data plane — so the API
// that can snapshot, restore and trigger fetches is never exposed to peers.
package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bhargav/synckit/internal/app"
	"github.com/bhargav/synckit/internal/bundle"
	"github.com/bhargav/synckit/internal/restore"
	"github.com/bhargav/synckit/internal/snapshot"
	ts "github.com/bhargav/synckit/internal/tailscale"
	"github.com/bhargav/synckit/internal/transport"
)

// Config configures the dashboard server.
type Config struct {
	SpoolDir string // where local bundles live / snapshots are written
	Port     int    // peer daemon port to probe on other machines
}

// Server hosts the dashboard and its JSON API.
type Server struct {
	cfg Config
}

func New(cfg Config) *Server { return &Server{cfg: cfg} }

// Handler returns the localhost mux (dashboard + /api/*).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/api/restore", s.handleRestore)
	mux.HandleFunc("/api/fetch", s.handleFetch)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

// ---- API payloads ----

type machineDTO struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	User     string `json:"user"`
}

type instanceDTO struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Version string `json:"version"`
	Running bool   `json:"running"`
	Root    string `json:"root"`
}

type appDTO struct {
	ID                  string        `json:"id"`
	Installed           bool          `json:"installed"`
	SecretsCrossMachine bool          `json:"secretsCrossMachine"`
	Note                string        `json:"note"`
	Instances           []instanceDTO `json:"instances"`
}

type bundleDTO struct {
	Name      string   `json:"name"`
	ID        string   `json:"id"`
	CreatedAt string   `json:"createdAt"`
	Apps      []string `json:"apps"`
	SizeMB    float64  `json:"sizeMB"`
	OriginOS  string   `json:"originOS"`
	OriginHost string  `json:"originHost"`
}

type peerDTO struct {
	Host    string      `json:"host"`
	IP      string      `json:"ip"`
	OS      string      `json:"os"`
	Online  bool        `json:"online"`
	Serving bool        `json:"serving"`
	Bundles []bundleDTO `json:"bundles"`
}

type overviewDTO struct {
	Machine      machineDTO  `json:"machine"`
	Apps         []appDTO    `json:"apps"`
	LocalBundles []bundleDTO `json:"localBundles"`
	Peers        []peerDTO   `json:"peers"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ov := overviewDTO{
		Machine:      s.machine(),
		Apps:         s.localApps(),
		LocalBundles: s.localBundles(),
		Peers:        s.discoverPeers(),
	}
	writeJSON(w, ov)
}

func (s *Server) machine() machineDTO {
	host, _ := os.Hostname()
	return machineDTO{Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH, User: currentUser()}
}

func (s *Server) localApps() []appDTO {
	var out []appDTO
	for _, ad := range app.Registry() {
		insts, _ := ad.Detect()
		d := appDTO{
			ID:                  ad.ID(),
			Installed:           len(insts) > 0,
			SecretsCrossMachine: ad.Portability().SecretsCrossMachine,
			Note:                ad.Portability().Note,
		}
		for _, inst := range insts {
			running, _ := ad.Running(inst)
			ver, _ := ad.Version(inst)
			d.Instances = append(d.Instances, instanceDTO{
				ID: inst.ID, Label: inst.Label, Version: ver, Running: running, Root: inst.Root,
			})
		}
		out = append(out, d)
	}
	return out
}

func (s *Server) localBundles() []bundleDTO {
	tr := transport.NewFile(s.cfg.SpoolDir)
	refs, _ := tr.List()
	var out []bundleDTO
	for _, ref := range refs {
		out = append(out, refToBundle(ref, filepath.Base(ref.Location)))
	}
	sortBundles(out)
	return out
}

// discoverPeers lists online tailnet machines and probes each for a daemon.
func (s *Server) discoverPeers() []peerDTO {
	if !ts.Available() {
		return nil
	}
	peers, err := ts.Peers(false)
	if err != nil {
		return nil
	}
	var online []ts.Peer
	for _, p := range peers {
		if p.IP != "" {
			online = append(online, p)
		}
	}
	out := make([]peerDTO, len(online))
	var wg sync.WaitGroup
	for i, p := range online {
		out[i] = peerDTO{Host: p.Host, IP: p.IP, OS: p.OS, Online: p.Online}
		if !p.Online {
			continue
		}
		wg.Add(1)
		go func(i int, p ts.Peer) {
			defer wg.Done()
			tr := transport.NewTailscale(p.IP, s.cfg.Port)
			if !tr.Reachable(2 * time.Second) {
				return
			}
			out[i].Serving = true
			refs, err := tr.List()
			if err != nil {
				return
			}
			for _, ref := range refs {
				name := ref.Location
				if idx := strings.LastIndexByte(name, ':'); idx >= 0 {
					name = name[idx+1:]
				}
				out[i].Bundles = append(out[i].Bundles, refToBundle(ref, name))
			}
			sortBundles(out[i].Bundles)
		}(i, p)
	}
	wg.Wait()
	// Serving peers first, then by name.
	sort.Slice(out, func(a, b int) bool {
		if out[a].Serving != out[b].Serving {
			return out[a].Serving
		}
		return out[a].Host < out[b].Host
	})
	return out
}

// ---- actions ----

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Apps  []string `json:"apps"`
		Force bool     `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	sel := selection(req.Apps)
	origin, now := originNow()
	id := bundleID(origin, now)
	dst := filepath.Join(s.cfg.SpoolDir, id+".zip")

	res, err := snapshot.Run(adaptersFor(sel), snapshot.Options{
		Dst: dst, Now: now, Origin: origin, ID: id, Force: req.Force, Selected: sel,
	})
	if err != nil {
		writeErr(w, err, res)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "bundle": id + ".zip", "apps": res.Apps, "skipped": res.Skipped})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bundle string   `json:"bundle"`
		Apps   []string `json:"apps"`
		DryRun bool     `json:"dryRun"`
		Force  bool     `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Bundle == "" {
		http.Error(w, "bundle required", http.StatusBadRequest)
		return
	}
	src := filepath.Join(s.cfg.SpoolDir, filepath.Base(req.Bundle))
	res, err := restore.Run(app.Registry(), restore.Options{
		Src: src, Selected: selection(req.Apps), DryRun: req.DryRun, Force: req.Force,
		BackupTag: time.Now().Format("20060102-150405"),
	})
	if err != nil {
		writeErr(w, err, nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "outcomes": res.Outcomes})
}

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Peer   string `json:"peer"`   // IP
		Name   string `json:"name"`   // bundle file name
		Apply  bool   `json:"apply"`  // restore after fetch
		DryRun bool   `json:"dryRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Peer == "" || req.Name == "" {
		http.Error(w, "peer and name required", http.StatusBadRequest)
		return
	}
	tr := transport.NewTailscale(req.Peer, s.cfg.Port)
	dst := filepath.Join(s.cfg.SpoolDir, req.Name)
	if err := tr.Get(transport.Ref{Location: req.Peer + ":" + req.Name}, dst); err != nil {
		writeErr(w, err, nil)
		return
	}
	resp := map[string]any{"ok": true, "fetched": req.Name}
	if req.Apply {
		res, err := restore.Run(app.Registry(), restore.Options{
			Src: dst, DryRun: req.DryRun, BackupTag: time.Now().Format("20060102-150405"),
		})
		if err != nil {
			writeErr(w, err, nil)
			return
		}
		resp["outcomes"] = res.Outcomes
	}
	writeJSON(w, resp)
}

// ---- helpers ----

func refToBundle(ref transport.Ref, name string) bundleDTO {
	m := ref.Meta
	apps := map[string]bool{}
	var names []string
	var bytes int64
	for _, a := range m.Apps {
		if !apps[a.App] {
			apps[a.App] = true
			names = append(names, a.App)
		}
		bytes += a.Bytes
	}
	sort.Strings(names)
	created := ""
	if !m.CreatedAt.IsZero() {
		created = m.CreatedAt.Format("2006-01-02 15:04")
	}
	return bundleDTO{
		Name: name, ID: m.ID, CreatedAt: created, Apps: names,
		SizeMB: float64(bytes) / (1 << 20), OriginOS: m.Origin.OS, OriginHost: m.Origin.Hostname,
	}
}

func sortBundles(b []bundleDTO) {
	sort.Slice(b, func(i, j int) bool { return b[i].CreatedAt > b[j].CreatedAt })
}

func selection(apps []string) map[string]bool {
	if len(apps) == 0 {
		return nil
	}
	m := map[string]bool{}
	for _, a := range apps {
		m[strings.ToLower(strings.TrimSpace(a))] = true
	}
	return m
}

func adaptersFor(sel map[string]bool) []app.Adapter {
	all := app.Registry()
	if sel == nil {
		return all
	}
	var out []app.Adapter
	for _, a := range all {
		if sel[a.ID()] {
			out = append(out, a)
		}
	}
	return out
}

func originNow() (bundle.Origin, time.Time) {
	host, _ := os.Hostname()
	return bundle.Origin{OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: host, User: currentUser()}, time.Now()
}

func bundleID(o bundle.Origin, t time.Time) string {
	host := o.Hostname
	if host == "" {
		host = "host"
	}
	return fmt.Sprintf("%s-%s", host, t.Format("20060102-150405"))
}

func currentUser() string {
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "unknown"
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error, extra any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // report as structured error, not HTTP failure
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error(), "detail": extra})
}
