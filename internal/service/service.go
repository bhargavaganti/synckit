// Package service is the single source of truth for synckit's operations,
// shared by every front-end (native GUI, web dashboard, CLI). It orchestrates
// the adapter/snapshot/restore/transport/tailscale layers and returns plain
// data structs — no HTTP, no widgets — so any UI can drive it in-process.
package service

import (
	"fmt"
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
	"github.com/bhargav/synckit/internal/settings"
	"github.com/bhargav/synckit/internal/snapshot"
	ts "github.com/bhargav/synckit/internal/tailscale"
	"github.com/bhargav/synckit/internal/transport"
)

// Outcome is one app instance's restore result, re-exported so front-ends can
// consume restore results without importing the restore package directly.
type Outcome = restore.AppOutcome

// Service carries the configuration every operation needs.
type Service struct {
	SpoolDir string // where local bundles live and snapshots are written
	Port     int    // daemon port to probe/reach on peers
}

// New builds a Service, defaulting the spool dir under the user's home.
func New(spoolDir string, port int) *Service {
	if spoolDir == "" {
		spoolDir = DefaultSpoolDir()
	}
	if port == 0 {
		port = transport.DefaultPort
	}
	return &Service{SpoolDir: spoolDir, Port: port}
}

// DefaultSpoolDir is ~/.synckit/bundles (or a local fallback).
func DefaultSpoolDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "synckit-bundles")
	}
	return filepath.Join(home, ".synckit", "bundles")
}

// ---- data types returned to UIs ----

type Machine struct {
	Hostname, OS, Arch, User string
}

type Instance struct {
	ID, Label, Version, Root string
	Running                  bool
}

type App struct {
	ID                  string
	Installed           bool
	SecretsCrossMachine bool
	Note                string
	Instances           []Instance
}

type Bundle struct {
	Name, ID, CreatedAt string
	CreatedTime         time.Time // precise timestamp for sync comparisons
	Apps                []string
	SizeMB              float64
	OriginOS, OriginHost string
}

type Peer struct {
	Host, IP, OS string
	Online       bool
	Serving      bool
	Bundles      []Bundle
}

type Overview struct {
	Machine      Machine
	Apps         []App
	LocalBundles []Bundle
	Peers        []Peer
	TailscaleUp  bool
}

// ---- reads ----

// Machine returns this host's identity.
func (s *Service) MachineInfo() Machine {
	host, _ := os.Hostname()
	return Machine{Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH, User: currentUser()}
}

// LocalApps enumerates every adapter's detected instances on this machine.
func (s *Service) LocalApps() []App {
	var out []App
	for _, ad := range app.Registry() {
		insts, _ := ad.Detect()
		a := App{
			ID:                  ad.ID(),
			Installed:           len(insts) > 0,
			SecretsCrossMachine: ad.Portability().SecretsCrossMachine,
			Note:                ad.Portability().Note,
		}
		for _, inst := range insts {
			running, _ := ad.Running(inst)
			ver, _ := ad.Version(inst)
			a.Instances = append(a.Instances, Instance{
				ID: inst.ID, Label: inst.Label, Version: ver, Running: running, Root: inst.Root,
			})
		}
		out = append(out, a)
	}
	return out
}

// LocalBundles lists bundles in the spool, newest first.
func (s *Service) LocalBundles() []Bundle {
	refs, _ := transport.NewFile(s.SpoolDir).List()
	var out []Bundle
	for _, ref := range refs {
		out = append(out, refToBundle(ref, filepath.Base(ref.Location)))
	}
	sortBundles(out)
	return out
}

// Peers lists tailnet machines and, for online ones, probes for a synckit
// daemon and lists the bundles it offers. Probes run concurrently.
func (s *Service) Peers() []Peer {
	if !ts.Available() {
		return nil
	}
	peers, err := ts.Peers(false)
	if err != nil {
		return nil
	}
	out := make([]Peer, len(peers))
	var wg sync.WaitGroup
	for i, p := range peers {
		out[i] = Peer{Host: p.Host, IP: p.IP, OS: p.OS, Online: p.Online}
		if !p.Online || p.IP == "" {
			continue
		}
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			tr := transport.NewTailscale(ip, s.Port)
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
		}(i, p.IP)
	}
	wg.Wait()
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Serving != out[b].Serving {
			return out[a].Serving
		}
		if out[a].Online != out[b].Online {
			return out[a].Online
		}
		return out[a].Host < out[b].Host
	})
	return out
}

// Overview is a single snapshot of everything a dashboard shows.
func (s *Service) Overview() Overview {
	return Overview{
		Machine:      s.MachineInfo(),
		Apps:         s.LocalApps(),
		LocalBundles: s.LocalBundles(),
		Peers:        s.Peers(),
		TailscaleUp:  ts.Available(),
	}
}

// ---- actions ----

// SnapshotResult reports a created bundle.
type SnapshotResult struct {
	Bundle  string
	Apps    []bundle.AppEntry
	Skipped []string
}

// Snapshot captures the selected apps (nil = all) into a new bundle.
func (s *Service) Snapshot(apps []string, force bool) (*SnapshotResult, error) {
	sel := selection(apps)
	origin, now := s.originNow()
	id := BundleID(origin, now)
	dst := filepath.Join(s.SpoolDir, id+".zip")
	res, err := snapshot.Run(adaptersFor(sel), snapshot.Options{
		Dst: dst, Now: now, Origin: origin, ID: id, Force: force, Selected: sel,
		Ignore: settings.Load().Ignore,
	})
	if err != nil {
		if res != nil {
			return &SnapshotResult{Skipped: res.Skipped}, err
		}
		return nil, err
	}
	return &SnapshotResult{Bundle: id + ".zip", Apps: res.Apps, Skipped: res.Skipped}, nil
}

// Restore applies a local bundle (by file name) onto this machine.
func (s *Service) Restore(bundleName string, apps []string, dryRun, force bool) ([]restore.AppOutcome, error) {
	src := filepath.Join(s.SpoolDir, filepath.Base(bundleName))
	res, err := restore.Run(app.Registry(), restore.Options{
		Src: src, Selected: selection(apps), DryRun: dryRun, Force: force,
		BackupTag: time.Now().Format("20060102-150405"),
	})
	if err != nil {
		return nil, err
	}
	return res.Outcomes, nil
}

// Fetch pulls a bundle from a peer into the spool, optionally restoring it.
func (s *Service) Fetch(peerIP, name string, apply, dryRun bool) ([]restore.AppOutcome, error) {
	tr := transport.NewTailscale(peerIP, s.Port)
	dst := filepath.Join(s.SpoolDir, name)
	if err := tr.Get(transport.Ref{Location: peerIP + ":" + name}, dst); err != nil {
		return nil, err
	}
	if !apply {
		return nil, nil
	}
	return s.Restore(name, nil, dryRun, false)
}

// PruneOwnSnapshots keeps the newest `keep` snapshots that originated on THIS
// machine and deletes older ones, returning how many were removed. It never
// touches bundles received from peers, so nothing inbound is lost.
func (s *Service) PruneOwnSnapshots(keep int) int {
	host, _ := os.Hostname()
	var mine []Bundle
	for _, b := range s.LocalBundles() { // already newest-first
		if b.OriginHost == host {
			mine = append(mine, b)
		}
	}
	removed := 0
	for i, b := range mine {
		if i < keep {
			continue
		}
		if err := os.Remove(filepath.Join(s.SpoolDir, b.Name)); err == nil {
			removed++
		}
	}
	return removed
}

// Push sends a local bundle to a peer's daemon.
func (s *Service) Push(bundleName, peerIP string) error {
	src := filepath.Join(s.SpoolDir, filepath.Base(bundleName))
	_, err := transport.NewTailscale(peerIP, s.Port).Put(src)
	return err
}

// ---- helpers ----

func (s *Service) originNow() (bundle.Origin, time.Time) {
	host, _ := os.Hostname()
	return bundle.Origin{OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: host, User: currentUser()}, time.Now()
}

// BundleID builds a sortable id like host-20260726-153004.
func BundleID(o bundle.Origin, t time.Time) string {
	host := o.Hostname
	if host == "" {
		host = "host"
	}
	return fmt.Sprintf("%s-%s", host, t.Format("20060102-150405"))
}

func refToBundle(ref transport.Ref, name string) Bundle {
	m := ref.Meta
	seen := map[string]bool{}
	var names []string
	var bytes int64
	for _, a := range m.Apps {
		if !seen[a.App] {
			seen[a.App] = true
			names = append(names, a.App)
		}
		bytes += a.Bytes
	}
	sort.Strings(names)
	created := ""
	if !m.CreatedAt.IsZero() {
		created = m.CreatedAt.Format("2006-01-02 15:04")
	}
	return Bundle{
		Name: name, ID: m.ID, CreatedAt: created, CreatedTime: m.CreatedAt, Apps: names,
		SizeMB: float64(bytes) / (1 << 20), OriginOS: m.Origin.OS, OriginHost: m.Origin.Hostname,
	}
}

func sortBundles(b []Bundle) {
	sort.Slice(b, func(i, j int) bool { return b[i].CreatedAt > b[j].CreatedAt })
}

func selection(apps []string) map[string]bool {
	if len(apps) == 0 {
		return nil
	}
	m := map[string]bool{}
	for _, a := range apps {
		if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
			m[a] = true
		}
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

func currentUser() string {
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "unknown"
}
