// Package syncengine turns synckit from a manual tool into a seamless one.
//
// Policy (chosen deliberately, not silent-overwrite):
//   - Auto-snapshot idle apps when they close, plus a periodic fallback.
//   - Auto-share the newest snapshot to every peer that is serving synckit.
//   - Detect when a peer offers a profile NEWER than anything we hold, and
//     surface it as an Update for the UI — the actual apply stays one click
//     away, so another machine's profile never overwrites yours silently.
//
// "Newer" uses each bundle's origin timestamp. Our own local bundles form the
// watermark per app, so once we've snapshotted (or fetched) something at least
// as new, the notification clears without any extra bookkeeping.
package syncengine

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/bhargav/synckit/internal/service"
)

// Update is a peer bundle newer than our watermark for a given app.
type Update struct {
	App        string
	PeerHost   string
	PeerIP     string
	BundleName string
	CreatedAt  time.Time
	Age        string // e.g. "2h newer than yours"
}

// Config tunes the engine. Zero values fall back to sane defaults in New.
type Config struct {
	Poll         time.Duration // how often to check peers + app state
	Periodic     time.Duration // max time between auto-snapshots while idle
	AutoSnapshot bool          // snapshot on close + periodically
	AutoShare    bool          // push newest snapshot to serving peers
	KeepOwn      int           // retain this many of our own snapshots (prune older)
}

// Engine runs the seamless loop. Callbacks are invoked from the loop goroutine;
// a GUI should marshal them onto its UI thread.
type Engine struct {
	svc *service.Service
	cfg Config

	myHost string

	mu          sync.Mutex
	prevRunning map[string]bool // app id -> was any instance running last tick
	lastSnap    time.Time

	// OnActivity reports a human-readable line of what the engine just did.
	OnActivity func(string)
	// OnUpdates reports the current set of available (not-yet-applied) updates.
	OnUpdates func([]Update)
}

func New(svc *service.Service, cfg Config) *Engine {
	if cfg.Poll <= 0 {
		cfg.Poll = 20 * time.Second
	}
	if cfg.Periodic <= 0 {
		cfg.Periodic = 15 * time.Minute
	}
	if cfg.KeepOwn <= 0 {
		cfg.KeepOwn = 15
	}
	host, _ := os.Hostname()
	return &Engine{
		svc:         svc,
		cfg:         cfg,
		myHost:      host,
		prevRunning: map[string]bool{},
	}
}

// Run drives the loop until ctx is cancelled. It ticks immediately, then every
// cfg.Poll interval.
func (e *Engine) Run(ctx context.Context) {
	e.tick()
	t := time.NewTicker(e.cfg.Poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.tick()
		}
	}
}

func (e *Engine) tick() {
	ov := e.svc.Overview()

	if e.cfg.AutoSnapshot {
		if reason := e.snapshotReason(ov); reason != "" {
			e.doSnapshot(reason, ov)
			ov = e.svc.Overview() // refresh local bundles for watermark
		}
	}

	updates := computeUpdates(e.myHost, ov)
	if e.OnUpdates != nil {
		e.OnUpdates(updates)
	}
}

// snapshotReason returns a non-empty reason when a snapshot should be taken:
// an app just closed (freshest, safe) or the periodic interval elapsed.
func (e *Engine) snapshotReason(ov service.Overview) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	closed := ""
	anyInstalled := false
	for _, a := range ov.Apps {
		if !a.Installed {
			continue
		}
		anyInstalled = true
		running := false
		for _, inst := range a.Instances {
			if inst.Running {
				running = true
				break
			}
		}
		if e.prevRunning[a.ID] && !running {
			closed = a.ID
		}
		e.prevRunning[a.ID] = running
	}
	if closed != "" {
		return "after " + closed + " closed"
	}
	if anyInstalled && time.Since(e.lastSnap) >= e.cfg.Periodic {
		return "periodic"
	}
	return ""
}

func (e *Engine) doSnapshot(reason string, ov service.Overview) {
	res, err := e.svc.Snapshot(nil, false) // all apps; running ones auto-skipped
	e.mu.Lock()
	e.lastSnap = time.Now()
	e.mu.Unlock()
	if err != nil {
		e.activity("Auto-snapshot skipped (" + reason + "): " + err.Error())
		return
	}
	e.activity(fmt.Sprintf("Auto-snapshot %s: %s (%d instance(s))", reason, res.Bundle, len(res.Apps)))

	if e.cfg.AutoShare {
		e.shareNewest(res.Bundle, ov)
	}
	if n := e.svc.PruneOwnSnapshots(e.cfg.KeepOwn); n > 0 {
		e.activity(fmt.Sprintf("Pruned %d old local snapshot(s)", n))
	}
}

// shareNewest pushes the given bundle to every peer currently serving synckit.
func (e *Engine) shareNewest(bundleName string, ov service.Overview) {
	shared := 0
	for _, p := range ov.Peers {
		if !p.Serving {
			continue
		}
		if err := e.svc.Push(bundleName, p.IP); err != nil {
			e.activity("Share to " + p.Host + " failed: " + err.Error())
			continue
		}
		shared++
	}
	if shared > 0 {
		e.activity(fmt.Sprintf("Shared %s to %d peer(s)", bundleName, shared))
	}
}

func (e *Engine) activity(s string) {
	if e.OnActivity != nil {
		e.OnActivity(s)
	}
}

// computeUpdates finds, per app, the newest peer bundle (from another host)
// that beats our local watermark for that app.
func computeUpdates(myHost string, ov service.Overview) []Update {
	// Watermark: newest local bundle time per app, across all origins. Once we
	// hold something at least this new, a peer's older bundle won't notify.
	watermark := map[string]time.Time{}
	for _, b := range ov.LocalBundles {
		for _, app := range b.Apps {
			if b.CreatedTime.After(watermark[app]) {
				watermark[app] = b.CreatedTime
			}
		}
	}

	best := map[string]Update{} // app -> newest candidate
	for _, p := range ov.Peers {
		if !p.Serving {
			continue
		}
		for _, b := range p.Bundles {
			if b.OriginHost == "" || b.OriginHost == myHost {
				continue // ours (or unknown) — not an inbound update
			}
			for _, app := range b.Apps {
				if !b.CreatedTime.After(watermark[app]) {
					continue
				}
				cur, ok := best[app]
				if !ok || b.CreatedTime.After(cur.CreatedAt) {
					best[app] = Update{
						App:        app,
						PeerHost:   p.Host,
						PeerIP:     p.IP,
						BundleName: b.Name,
						CreatedAt:  b.CreatedTime,
						Age:        humanNewer(b.CreatedTime, watermark[app]),
					}
				}
			}
		}
	}

	out := make([]Update, 0, len(best))
	for _, u := range best {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out
}

func humanNewer(peer, local time.Time) string {
	if local.IsZero() {
		return "new (you have none)"
	}
	d := peer.Sub(local)
	switch {
	case d < time.Minute:
		return "seconds newer"
	case d < time.Hour:
		return fmt.Sprintf("%dm newer than yours", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh newer than yours", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd newer than yours", int(d.Hours()/24))
	}
}
