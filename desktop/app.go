package main

import (
	"context"
	"fmt"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/bhargav/synckit/internal/app"
	"github.com/bhargav/synckit/internal/bundle"
	"github.com/bhargav/synckit/internal/daemon"
	"github.com/bhargav/synckit/internal/service"
	"github.com/bhargav/synckit/internal/settings"
	ts "github.com/bhargav/synckit/internal/tailscale"
	"github.com/bhargav/synckit/internal/transport"
	"github.com/bhargav/synckit/internal/vault"
	"github.com/bhargav/synckit/internal/version"
)

// App is the Wails backend: a thin, JSON-friendly binding over service.Service.
// Every exported method becomes callable from the frontend as a Promise.
type App struct {
	ctx context.Context
	svc *service.Service
}

func NewApp() *App {
	// Apply persisted Tailscale override + encryption key, like the CLI does.
	if st := settings.Load(); st.TailscalePath != "" {
		ts.SetBinPath(st.TailscalePath)
	}
	if v, err := vault.Load(vault.DefaultPath()); err == nil {
		bundle.UseVault(v)
	}
	return &App{svc: service.New("", transport.DefaultPort)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.startDaemon()
}

// startDaemon runs the in-process tailnet receiver so the app IS a sync node.
func (a *App) startDaemon() {
	bindIP := ""
	if ts.Available() {
		if ip, err := ts.SelfIP(); err == nil {
			bindIP = ip
		}
	}
	srv, err := daemon.New(daemon.Config{
		SpoolDir:     a.svc.SpoolDir,
		BindIP:       bindIP,
		Port:         a.svc.Port,
		Capabilities: func() any { return a.svc.Capability() },
		OnReceive: func(path string) {
			a.emit("bundle-received", path)
		},
	})
	if err != nil {
		return
	}
	_ = srv.Serve()
}

func (a *App) emit(event string, data ...interface{}) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, event, data...)
	}
}

// ---- reads ----

func (a *App) Version() string           { return version.String() }
func (a *App) Overview() service.Overview { return a.svc.Overview() }
func (a *App) Matrix() service.Matrix     { return a.svc.TailnetMatrix() }
func (a *App) Diagnose() string           { return ts.Diagnose() }
func (a *App) SpoolDir() string           { return a.svc.SpoolDir }

// ---- actions ----

// SnapshotResult is a compact result for the UI (no huge checksum maps).
type SnapshotResult struct {
	Bundle    string   `json:"bundle"`
	Instances int      `json:"instances"`
	Skipped   []string `json:"skipped"`
	Encrypted bool     `json:"encrypted"`
}

func (a *App) Snapshot(apps []string) (*SnapshotResult, error) {
	res, err := a.svc.Snapshot(apps, false)
	if err != nil {
		if res != nil && len(res.Skipped) > 0 {
			return nil, fmt.Errorf("%v; skipped: %v", err, res.Skipped)
		}
		return nil, err
	}
	return &SnapshotResult{
		Bundle: res.Bundle, Instances: len(res.Apps),
		Skipped: res.Skipped, Encrypted: bundle.Encrypted(),
	}, nil
}

func (a *App) Restore(bundleName string, apps []string, dryRun, forceClose bool) ([]service.Outcome, error) {
	label := "Restoring " + bundleName
	if dryRun {
		return a.svc.RestoreOpts(bundleName, apps, dryRun, false, forceClose, nil)
	}
	a.emit("transfer", map[string]any{"name": label, "done": 0, "total": -1, "active": true})
	out, err := a.svc.RestoreOpts(bundleName, apps, dryRun, false, forceClose, func(done, total int64) {
		a.emit("transfer", map[string]any{"name": label, "done": done, "total": total, "active": true})
	})
	a.emit("transfer", map[string]any{"name": label, "done": 1, "total": 1, "active": false})
	return out, err
}

func (a *App) Fetch(peerIP, name string, apply, dryRun bool) ([]service.Outcome, error) {
	a.emit("transfer", map[string]any{"name": name, "done": 0, "total": -1, "active": true})
	outcomes, err := a.svc.FetchProgress(peerIP, name, apply, dryRun, func(done, total int64) {
		a.emit("transfer", map[string]any{"name": name, "done": done, "total": total, "active": true})
	})
	a.emit("transfer", map[string]any{"name": name, "done": 1, "total": 1, "active": false})
	return outcomes, err
}

func (a *App) Push(bundleName, peerIP string) error {
	return a.svc.Push(bundleName, peerIP)
}

func (a *App) DeleteBundle(name string) error { return a.svc.DeleteBundle(name) }

// ---- settings / ignore ----

func (a *App) GetSettings() settings.Settings { return settings.Load() }

func (a *App) SetTailscalePath(path string) error {
	st := settings.Load()
	st.TailscalePath = path
	if err := settings.Save(st); err != nil {
		return err
	}
	ts.SetBinPath(path)
	return nil
}

func (a *App) AddIgnore(appID, pattern string) error {
	st := settings.Load()
	if st.Ignore == nil {
		st.Ignore = map[string][]string{}
	}
	for _, g := range st.Ignore[appID] {
		if g == pattern {
			return nil
		}
	}
	st.Ignore[appID] = append(st.Ignore[appID], pattern)
	return settings.Save(st)
}

func (a *App) RemoveIgnore(appID, pattern string) error {
	st := settings.Load()
	kept := st.Ignore[appID][:0]
	for _, g := range st.Ignore[appID] {
		if g != pattern {
			kept = append(kept, g)
		}
	}
	if len(kept) == 0 {
		delete(st.Ignore, appID)
	} else {
		st.Ignore[appID] = kept
	}
	return settings.Save(st)
}

// ---- encryption key ----

type KeyStatus struct {
	Enabled   bool   `json:"enabled"`
	Recipient string `json:"recipient"`
	Path      string `json:"path"`
}

func (a *App) KeyStatus() KeyStatus {
	v, err := vault.Load(vault.DefaultPath())
	if err != nil {
		return KeyStatus{Enabled: false, Path: vault.DefaultPath()}
	}
	return KeyStatus{Enabled: true, Recipient: v.Recipient(), Path: vault.DefaultPath()}
}

func (a *App) KeyInit() (string, error) {
	rec, err := vault.Init(vault.DefaultPath())
	if err != nil {
		return "", err
	}
	if v, err := vault.Load(vault.DefaultPath()); err == nil {
		bundle.UseVault(v)
	}
	return rec, nil
}

// chromeSafePreset is the whitelist for Chrome "bookmarks-only" safe mode — the
// files that actually survive a cross-machine copy (bookmarks; NOT the HMAC-
// signed Preferences or OS-encrypted Login Data).
var chromeSafePreset = []string{"Bookmarks", "Bookmarks.bak"}

func (a *App) ChromeSafeMode() bool {
	return len(settings.Load().IncludeOnly["chrome"]) > 0
}

func (a *App) SetChromeSafeMode(on bool) error {
	st := settings.Load()
	if st.IncludeOnly == nil {
		st.IncludeOnly = map[string][]string{}
	}
	if on {
		st.IncludeOnly["chrome"] = chromeSafePreset
	} else {
		delete(st.IncludeOnly, "chrome")
	}
	return settings.Save(st)
}

func (a *App) ChromeWholeUserData() bool { return settings.Load().ChromeWholeUserData }

func (a *App) SetChromeWholeUserData(on bool) error {
	st := settings.Load()
	st.ChromeWholeUserData = on
	return settings.Save(st)
}

// AppIDs returns the built-in app ids, for the ignore editor.
func (a *App) AppIDs() []string {
	var ids []string
	for _, ad := range app.Registry() {
		ids = append(ids, ad.ID())
	}
	return ids
}
