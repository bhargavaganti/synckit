// Command synckit-gui is the native desktop app. It embeds the tailnet receiver
// daemon (GUI + daemon in one) and drives every operation through
// internal/service, the same core the CLI uses.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bhargav/synckit/internal/bundle"
	"github.com/bhargav/synckit/internal/daemon"
	"github.com/bhargav/synckit/internal/service"
	"github.com/bhargav/synckit/internal/settings"
	"github.com/bhargav/synckit/internal/syncengine"
	ts "github.com/bhargav/synckit/internal/tailscale"
	"github.com/bhargav/synckit/internal/transport"
	"github.com/bhargav/synckit/internal/vault"
	"github.com/bhargav/synckit/internal/version"
)

type gui struct {
	svc *service.Service
	win fyne.Window

	machine    *widget.Label
	daemonLbl  *widget.Label
	status     *widget.Label
	updatesBox *fyne.Container
	appsBox    *fyne.Container
	snapChecks *fyne.Container
	bundlesBox *fyne.Container
	peersBox   *fyne.Container
	matrixBox  *fyne.Container

	tsPathEntry *widget.Entry
	diagText    *widget.Entry

	installed []string // installed app ids, for snapshot selection
}

func main() {
	spool := service.DefaultSpoolDir()
	if err := os.MkdirAll(spool, 0o755); err != nil {
		log.Fatal(err)
	}
	// Apply a saved Tailscale CLI path override, if any.
	if st := settings.Load(); st.TailscalePath != "" {
		ts.SetBinPath(st.TailscalePath)
	}
	// Enable encryption at rest/in transit if a shared key is configured.
	if v, err := vault.Load(vault.DefaultPath()); err == nil {
		bundle.UseVault(v)
	}

	svc := service.New(spool, transport.DefaultPort)

	a := fyneapp.NewWithID("dev.bhargav.synckit")
	a.Settings().SetTheme(theme.DefaultTheme())
	w := a.NewWindow("synckit")
	w.Resize(fyne.NewSize(760, 720))

	g := &gui{
		svc:        svc,
		win:        w,
		machine:    widget.NewLabel(""),
		daemonLbl:  widget.NewLabel("daemon: starting…"),
		status:     widget.NewLabel("Ready."),
		updatesBox: container.NewVBox(),
		appsBox:    container.NewVBox(),
		snapChecks: container.NewHBox(),
		bundlesBox: container.NewVBox(),
		peersBox:   container.NewVBox(),
		matrixBox:  container.NewVBox(),
	}
	w.SetContent(g.build())

	go g.startDaemon()
	go g.startEngine()
	g.refresh()

	w.ShowAndRun()
}

// build lays out the window: header, tabs, status bar.
func (g *gui) build() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("🔄  synckit "+version.String(), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), g.refresh)
	header := container.NewBorder(nil, nil,
		container.NewHBox(title, g.machine),
		refreshBtn,
	)

	snapBtn := widget.NewButtonWithIcon("Snapshot now", theme.MediaRecordIcon(), g.onSnapshot)
	snapBtn.Importance = widget.HighImportance
	snapBar := container.NewBorder(nil, nil,
		widget.NewLabel("Create snapshot:"), snapBtn, g.snapChecks)

	thisMachine := container.NewBorder(
		container.NewVBox(snapBar, widget.NewSeparator()), nil, nil, nil,
		container.NewScroll(g.appsBox),
	)

	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("This machine", theme.ComputerIcon(), thisMachine),
		container.NewTabItemWithIcon("Local bundles", theme.StorageIcon(), container.NewScroll(g.bundlesBox)),
		container.NewTabItemWithIcon("Tailnet", theme.RadioButtonIcon(), container.NewScroll(g.peersBox)),
		container.NewTabItemWithIcon("Sync map", theme.GridIcon(), container.NewScroll(g.matrixBox)),
		container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), g.buildSettings()),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	// The updates banner sits above the tabs; empty unless a peer has something
	// newer than we hold — then it shows one Apply button per app.
	top := container.NewVBox(header, widget.NewSeparator(), g.updatesBox)

	footer := container.NewVBox(widget.NewSeparator(),
		container.NewBorder(nil, nil, g.daemonLbl, nil, g.status))

	return container.NewBorder(top, footer, nil, nil, tabs)
}

// buildSettings is the Settings tab: the Tailscale CLI path override and a
// diagnostics console so you can see exactly what synckit detects.
func (g *gui) buildSettings() fyne.CanvasObject {
	st := settings.Load()
	g.tsPathEntry = widget.NewEntry()
	g.tsPathEntry.SetPlaceHolder("auto-detect (leave blank)")
	g.tsPathEntry.SetText(st.TailscalePath)

	browse := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			g.tsPathEntry.SetText(rc.URI().Path())
			rc.Close()
		}, g.win)
	})
	save := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		s := settings.Load()
		s.TailscalePath = strings.TrimSpace(g.tsPathEntry.Text)
		if err := settings.Save(s); err != nil {
			g.setStatus("Save failed: " + err.Error())
			return
		}
		ts.SetBinPath(s.TailscalePath)
		g.setStatus("Saved Tailscale path.")
		g.runDiagnostics()
		g.refresh()
	})
	save.Importance = widget.HighImportance

	g.diagText = widget.NewMultiLineEntry()
	g.diagText.Wrapping = fyne.TextWrapOff
	g.diagText.SetText("Click “Run diagnostics”.")
	g.diagText.Disable() // read-only console
	diagBtn := widget.NewButtonWithIcon("Run diagnostics", theme.SearchIcon(), g.runDiagnostics)

	info := widget.NewLabel(fmt.Sprintf("synckit %s\nspool: %s", version.String(), g.svc.SpoolDir))
	hint := widget.NewLabel("Set this if peers aren't found. On macOS the CLI is often at:\n" +
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale")
	hint.TextStyle = fyne.TextStyle{Italic: true}

	top := container.NewVBox(
		info,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Tailscale CLI path", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		hint,
		container.NewBorder(nil, nil, nil, container.NewHBox(browse, save), g.tsPathEntry),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, widget.NewLabelWithStyle("Diagnostics", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), diagBtn, nil),
	)
	return container.NewBorder(top, nil, nil, nil, container.NewScroll(g.diagText))
}

// runDiagnostics fills the console with what synckit sees for Tailscale.
func (g *gui) runDiagnostics() {
	g.setStatus("Running diagnostics…")
	go func() {
		out := "synckit " + version.String() + "\ntailscale bin: " + ts.BinPath() + "\n\n" + ts.Diagnose()
		fyne.Do(func() {
			g.diagText.SetText(out)
			g.status.SetText("Ready.")
		})
	}()
}

// startEngine runs the seamless sync loop: auto-snapshot on close + periodic,
// auto-share to serving peers, and surface (never auto-apply) newer peer profiles.
func (g *gui) startEngine() {
	eng := syncengine.New(g.svc, syncengine.Config{
		Poll:         20 * time.Second,
		Periodic:     15 * time.Minute,
		AutoSnapshot: true,
		AutoShare:    true,
	})
	eng.OnActivity = func(s string) {
		g.setStatus(s)
		fyne.Do(g.refreshBundles)
	}
	eng.OnUpdates = func(ups []syncengine.Update) {
		fyne.Do(func() { g.renderUpdates(ups) })
	}
	eng.Run(context.Background())
}

// renderUpdates paints the "a peer has something newer" banner.
func (g *gui) renderUpdates(ups []syncengine.Update) {
	g.updatesBox.RemoveAll()
	if len(ups) == 0 {
		g.updatesBox.Refresh()
		return
	}
	for _, u := range ups {
		u := u
		msg := fmt.Sprintf("⬇  %s has a newer %s profile (%s)", u.PeerHost, u.App, u.Age)
		apply := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
			dialog.ShowConfirm("Apply "+u.App+" from "+u.PeerHost+"?",
				"Fetch and restore "+u.BundleName+". Your current profile is backed up first; close "+u.App+" before applying.",
				func(ok bool) {
					if ok {
						g.onFetch(u.PeerIP, u.BundleName, true)
					}
				}, g.win)
		})
		apply.Importance = widget.HighImportance
		card := widget.NewCard("", "", container.NewBorder(nil, nil,
			widget.NewLabelWithStyle(msg, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			apply))
		g.updatesBox.Add(card)
	}
	g.updatesBox.Refresh()
}

// startDaemon runs the in-process tailnet receiver so the app IS the sync node.
func (g *gui) startDaemon() {
	bindIP := ""
	msg := "daemon: bound to all interfaces"
	if ts.Available() {
		if ip, err := ts.SelfIP(); err == nil {
			bindIP = ip
			msg = "daemon: receiving on " + ip
		} else {
			msg = "daemon: tailscale up? binding all interfaces"
		}
	} else {
		msg = "daemon: no tailscale CLI; binding all interfaces"
	}

	srv, err := daemon.New(daemon.Config{
		SpoolDir:     g.svc.SpoolDir,
		BindIP:       bindIP,
		Port:         g.svc.Port,
		Capabilities: func() any { return g.svc.Capability() },
		OnReceive: func(path string) {
			g.setStatus("Received a bundle from a peer — check Local bundles.")
			fyne.Do(g.refreshBundles)
		},
	})
	if err != nil {
		fyne.Do(func() { g.daemonLbl.SetText("daemon: error — " + err.Error()) })
		return
	}
	fyne.Do(func() { g.daemonLbl.SetText(msg) })
	if err := srv.Serve(); err != nil {
		fyne.Do(func() { g.daemonLbl.SetText("daemon stopped: " + err.Error()) })
	}
}

func (g *gui) setStatus(s string) { fyne.Do(func() { g.status.SetText(s) }) }

// refresh reloads the full overview off the UI thread, then repaints.
func (g *gui) refresh() {
	g.setStatus("Refreshing…")
	go func() {
		ov := g.svc.Overview()
		fyne.Do(func() {
			g.machine.SetText("— " + ov.Machine.Hostname + " · " + ov.Machine.OS + "/" + ov.Machine.Arch)
			g.renderApps(ov.Apps)
			g.renderBundles(ov.LocalBundles)
			g.renderPeers(ov.Peers)
			g.status.SetText("Ready.")
		})
	}()
	// The matrix probes peers over the network — fetch it separately so the rest
	// of the UI doesn't wait on it.
	go func() {
		m := g.svc.TailnetMatrix()
		fyne.Do(func() { g.renderMatrix(m) })
	}()
}

// renderMatrix paints the tailnet syncability grid: apps by role down the side,
// machines across the top, with a per-row syncable verdict + in-sync/differs.
func (g *gui) renderMatrix(m service.Matrix) {
	g.matrixBox.RemoveAll()
	if len(m.Rows) == 0 {
		g.matrixBox.Add(widget.NewLabel("No apps detected yet. Start `synckit serve` on your\nother machines so they advertise over the tailnet."))
		g.matrixBox.Refresh()
		return
	}
	cols := len(m.Machines) + 2
	grid := container.NewGridWithColumns(cols)
	bold := func(s string) fyne.CanvasObject {
		return widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	grid.Add(bold("App / profile"))
	for _, mc := range m.Machines {
		grid.Add(bold(mc))
	}
	grid.Add(bold("Syncable"))

	for _, r := range m.Rows {
		grid.Add(widget.NewLabel(r.App + " / " + r.Role))
		for _, mc := range m.Machines {
			c := r.Cells[mc]
			txt := "—"
			if c.Present {
				txt = "✓"
				if c.Version != "" {
					txt = "✓ " + c.Version
				}
			}
			grid.Add(widget.NewLabel(txt))
		}
		grid.Add(widget.NewLabel(matrixStatus(r)))
	}
	g.matrixBox.Add(grid)
	g.matrixBox.Refresh()
}

func matrixStatus(r service.MatrixRow) string {
	verdict := map[service.Verdict]string{
		service.VerdictFull:     "✅ full",
		service.VerdictSettings: "⚠ settings",
		service.VerdictSeed:     "→ seed",
	}[r.Verdict]
	switch r.Sync {
	case service.SyncInSync:
		return verdict + " · ✓ in sync"
	case service.SyncDiffers:
		if r.NewestHost != "" {
			return verdict + " · ≠ " + r.NewestHost + " newest"
		}
		return verdict + " · ≠ differs"
	default:
		return verdict
	}
}

func (g *gui) refreshBundles() {
	go func() {
		bs := g.svc.LocalBundles()
		fyne.Do(func() { g.renderBundles(bs) })
	}()
}

// ---- rendering ----

func (g *gui) renderApps(apps []service.App) {
	g.appsBox.RemoveAll()
	g.snapChecks.RemoveAll()
	g.installed = g.installed[:0]
	for _, a := range apps {
		g.appsBox.Add(g.appCard(a))
		if a.Installed {
			g.installed = append(g.installed, a.ID)
			ck := widget.NewCheck(a.ID, nil)
			ck.SetChecked(true)
			ck.OnChanged = nil
			g.snapChecks.Add(ck)
		}
	}
	g.appsBox.Refresh()
	g.snapChecks.Refresh()
}

func (g *gui) appCard(a service.App) fyne.CanvasObject {
	var badge string
	if !a.Installed {
		badge = "not installed"
	} else if a.SecretsCrossMachine {
		badge = "secrets: portable"
	} else {
		badge = "secrets: this machine only"
	}
	head := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle(a.ID, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel(badge))

	body := container.NewVBox()
	for _, inst := range a.Instances {
		state := "idle"
		if inst.Running {
			state = "⚠ running"
		}
		ver := ""
		if inst.Version != "" {
			ver = "  v" + inst.Version
		}
		line := container.NewBorder(nil, nil,
			widget.NewLabel("• "+inst.Label+ver),
			widget.NewLabel(state))
		path := widget.NewLabelWithStyle("   "+inst.Root, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
		body.Add(container.NewVBox(line, path))
	}
	if a.Installed && len(a.Instances) == 0 {
		body.Add(widget.NewLabel("  (no profiles found)"))
	}
	return widget.NewCard("", "", container.NewVBox(head, body))
}

func (g *gui) renderBundles(bundles []service.Bundle) {
	g.bundlesBox.RemoveAll()
	if len(bundles) == 0 {
		g.bundlesBox.Add(widget.NewLabel("No local bundles yet. Use “Snapshot now”."))
		g.bundlesBox.Refresh()
		return
	}
	for _, b := range bundles {
		b := b
		dry := widget.NewButton("Dry-run", func() { g.onRestore(b.Name, true) })
		res := widget.NewButtonWithIcon("Restore", theme.ContentUndoIcon(), func() { g.confirmRestore(b) })
		res.Importance = widget.HighImportance
		row := container.NewBorder(nil, nil, nil, container.NewHBox(dry, res),
			g.bundleInfo(b))
		g.bundlesBox.Add(widget.NewCard("", "", row))
	}
	g.bundlesBox.Refresh()
}

func (g *gui) bundleInfo(b service.Bundle) fyne.CanvasObject {
	name := b.ID
	if name == "" {
		name = b.Name
	}
	sub := fmt.Sprintf("%s · %.1f MB", joinApps(b.Apps), b.SizeMB)
	if b.CreatedAt != "" {
		sub += " · " + b.CreatedAt
	}
	if b.OriginHost != "" {
		sub += " · from " + b.OriginHost + " (" + b.OriginOS + ")"
	}
	return container.NewVBox(
		widget.NewLabelWithStyle(name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(sub, fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)
}

func (g *gui) renderPeers(peers []service.Peer) {
	g.peersBox.RemoveAll()
	if len(peers) == 0 {
		g.peersBox.Add(widget.NewLabel("No tailnet peers found (is Tailscale up?)."))
		g.peersBox.Refresh()
		return
	}
	for _, p := range peers {
		p := p
		status := "offline"
		if p.Online && p.Serving {
			status = "● serving synckit"
		} else if p.Online {
			status = "○ online, not serving"
		}
		head := container.NewBorder(nil, nil,
			widget.NewLabelWithStyle(p.Host, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewLabel(status),
			widget.NewLabel(p.IP+" · "+p.OS))

		body := container.NewVBox(head)
		for _, b := range p.Bundles {
			b := b
			fetch := widget.NewButton("Fetch", func() { g.onFetch(p.IP, b.Name, false) })
			imp := widget.NewButtonWithIcon("Fetch & import", theme.DownloadIcon(),
				func() { g.confirmFetch(p, b) })
			imp.Importance = widget.HighImportance
			row := container.NewBorder(nil, nil, nil, container.NewHBox(fetch, imp), g.bundleInfo(b))
			body.Add(row)
		}
		if p.Online && p.Serving && len(p.Bundles) == 0 {
			body.Add(widget.NewLabel("  (no bundles offered)"))
		}
		g.peersBox.Add(widget.NewCard("", "", body))
	}
	g.peersBox.Refresh()
}

// ---- actions ----

func (g *gui) selectedApps() []string {
	var out []string
	for _, obj := range g.snapChecks.Objects {
		if ck, ok := obj.(*widget.Check); ok && ck.Checked {
			out = append(out, ck.Text)
		}
	}
	return out
}

func (g *gui) onSnapshot() {
	apps := g.selectedApps()
	g.setStatus("Creating snapshot…")
	go func() {
		res, err := g.svc.Snapshot(apps, false)
		if err != nil {
			g.setStatus("Snapshot failed: " + err.Error())
			return
		}
		msg := fmt.Sprintf("Snapshot created: %s (%d instance(s))", res.Bundle, len(res.Apps))
		if len(res.Skipped) > 0 {
			msg += " — skipped " + fmt.Sprint(len(res.Skipped)) + " running"
		}
		g.setStatus(msg)
		fyne.Do(g.refreshBundles)
	}()
}

func (g *gui) confirmRestore(b service.Bundle) {
	dialog.ShowConfirm("Restore "+b.Name+"?",
		"Existing profiles are backed up first. Close the target apps before restoring.",
		func(ok bool) {
			if ok {
				g.onRestore(b.Name, false)
			}
		}, g.win)
}

func (g *gui) onRestore(name string, dry bool) {
	g.setStatus(map[bool]string{true: "Dry-run…", false: "Restoring…"}[dry])
	go func() {
		outcomes, err := g.svc.Restore(name, nil, dry, false)
		if err != nil {
			g.setStatus("Restore failed: " + err.Error())
			return
		}
		g.setStatus(summarize(dry, outcomes))
	}()
}

func (g *gui) confirmFetch(p service.Peer, b service.Bundle) {
	dialog.ShowConfirm("Fetch & import from "+p.Host+"?",
		"Download "+b.Name+" and restore it here. Existing profiles are backed up first.",
		func(ok bool) {
			if ok {
				g.onFetch(p.IP, b.Name, true)
			}
		}, g.win)
}

func (g *gui) onFetch(ip, name string, apply bool) {
	g.setStatus("Fetching " + name + "…")
	go func() {
		outcomes, err := g.svc.Fetch(ip, name, apply, false)
		if err != nil {
			g.setStatus("Fetch failed: " + err.Error())
			return
		}
		if apply {
			g.setStatus("Fetched + " + summarize(false, outcomes))
		} else {
			g.setStatus("Fetched " + name + " → Local bundles")
		}
		fyne.Do(g.refreshBundles)
	}()
}

// ---- small helpers ----

func joinApps(a []string) string {
	if len(a) == 0 {
		return "?"
	}
	s := a[0]
	for _, n := range a[1:] {
		s += " + " + n
	}
	return s
}

func summarize(dry bool, outcomes []service.Outcome) string {
	verb := "restored"
	if dry {
		verb = "would restore"
	}
	ok, skipped := 0, 0
	for _, o := range outcomes {
		if o.Skipped != "" {
			skipped++
		} else {
			ok++
		}
	}
	return fmt.Sprintf("%s %d instance(s), %d skipped", verb, ok, skipped)
}
