package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/restore"
	ts "github.com/bhargav/synckit/internal/tailscale"
	"github.com/bhargav/synckit/internal/transport"
)

// serving is a peer discovered to be running a synckit daemon, plus its bundles.
type serving struct {
	peer    ts.Peer
	bundles []transport.Ref
	err     error
}

func newSyncCmd() *cobra.Command {
	var port int
	var from, name string
	var apply, dryRun, latest bool
	var appsFlag []string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Discover tailnet machines serving synckit and pull profiles into sync",
		Long: `Without --from, sync BROWSES: it probes every online tailnet machine for a
running synckit daemon and lists the bundles each one offers.

With --from <peer>, it FETCHES the newest bundle (or --name <bundle>) from that
peer into the local spool. Add --apply to also restore it (profiles are backed
up first); use --dry-run to preview the restore.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ts.Available() {
				return fmt.Errorf("tailscale CLI not found; install Tailscale and run `tailscale up`")
			}
			if from == "" {
				return browse(port)
			}
			return fetchFrom(from, name, latest, apply, dryRun, appsFlag, port)
		},
	}
	cmd.Flags().IntVar(&port, "port", transport.DefaultPort, "daemon port on peers")
	cmd.Flags().StringVar(&from, "from", "", "peer to fetch from (omit to browse all peers)")
	cmd.Flags().StringVar(&name, "name", "", "specific bundle name to fetch (default: newest)")
	cmd.Flags().BoolVar(&latest, "latest", true, "when no --name, pick the newest bundle")
	cmd.Flags().BoolVar(&apply, "apply", false, "restore the fetched bundle (backs up existing profiles)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "with --apply, preview the restore without writing")
	cmd.Flags().StringSliceVar(&appsFlag, "apps", nil, "limit restore to these apps")
	return cmd
}

// browse probes all online peers concurrently and prints which serve synckit.
func browse(port int) error {
	peers, err := ts.Peers(false)
	if err != nil {
		return err
	}
	var online []ts.Peer
	for _, p := range peers {
		if p.Online && p.IP != "" {
			online = append(online, p)
		}
	}
	if len(online) == 0 {
		fmt.Println("no online tailnet peers")
		return nil
	}

	results := make([]serving, len(online))
	var wg sync.WaitGroup
	for i, p := range online {
		wg.Add(1)
		go func(i int, p ts.Peer) {
			defer wg.Done()
			tr := transport.NewTailscale(p.IP, port)
			if !tr.Reachable(2 * time.Second) {
				results[i] = serving{peer: p, err: fmt.Errorf("not serving")}
				return
			}
			refs, err := tr.List()
			results[i] = serving{peer: p, bundles: refs, err: err}
		}(i, p)
	}
	wg.Wait()

	any := false
	for _, s := range results {
		if s.err != nil {
			continue // silent for non-serving peers; they're just not running synckit
		}
		any = true
		fmt.Printf("● %s (%s, %s) — %d bundle(s)\n", s.peer.Host, s.peer.IP, s.peer.OS, len(s.bundles))
		for _, b := range sortRefs(s.bundles) {
			fmt.Printf("    %-32s %s  [%s]\n", b.ID, appsSummary(b), b.Meta.CreatedAt.Format("2006-01-02 15:04"))
		}
	}
	if !any {
		fmt.Printf("%d peer(s) online, but none are serving synckit.\n", len(online))
		fmt.Println("Run `synckit serve` on those machines to make their bundles available.")
	}
	return nil
}

// fetchFrom pulls a bundle from one peer and optionally restores it.
func fetchFrom(peer, name string, latest, apply, dryRun bool, appsFlag []string, port int) error {
	host, err := ts.Resolve(peer)
	if err != nil {
		return err
	}
	tr := transport.NewTailscale(host, port)
	if !tr.Reachable(3 * time.Second) {
		return fmt.Errorf("%s is not reachable or not running `synckit serve`", peer)
	}

	refs, err := tr.List()
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return fmt.Errorf("%s has no bundles", peer)
	}

	var chosen transport.Ref
	if name != "" {
		for _, r := range refs {
			if bundleName(r) == name || r.ID == name {
				chosen = r
				break
			}
		}
		if chosen.Location == "" {
			return fmt.Errorf("bundle %q not found on %s", name, peer)
		}
	} else {
		chosen = sortRefs(refs)[0] // newest first
	}

	dst := filepath.Join(defaultSpoolDir(), bundleName(chosen))
	if err := tr.Get(chosen, dst); err != nil {
		return err
	}
	fmt.Printf("fetched %s from %s → %s\n", chosen.ID, peer, dst)

	if !apply {
		fmt.Printf("Not restored. Run:  synckit restore %q\n", dst)
		return nil
	}

	sel := selectionFromApps(appsFlag)
	res, err := restore.Run(adapters(nil), restore.Options{
		Src:       dst,
		Selected:  sel,
		DryRun:    dryRun,
		BackupTag: time.Now().Format("20060102-150405"),
	})
	if err != nil {
		return err
	}
	for _, o := range res.Outcomes {
		status := "restored"
		switch {
		case o.Skipped != "":
			status = "SKIPPED: " + o.Skipped
		case dryRun:
			status = "would restore"
		}
		fmt.Printf("%-8s %-20s → %s\n", o.App, o.Instance, status)
		if o.Backup != "" {
			fmt.Printf("    backup: %s\n", o.Backup)
		}
		for _, w := range o.Warnings {
			fmt.Printf("    ⚠ %s\n", w)
		}
	}
	return nil
}

// sortRefs returns bundles newest-first by CreatedAt.
func sortRefs(refs []transport.Ref) []transport.Ref {
	out := make([]transport.Ref, len(refs))
	copy(out, refs)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Meta.CreatedAt.After(out[j].Meta.CreatedAt)
	})
	return out
}

func appsSummary(r transport.Ref) string {
	if len(r.Meta.Apps) == 0 {
		return "?"
	}
	seen := map[string]bool{}
	var names []string
	for _, a := range r.Meta.Apps {
		if !seen[a.App] {
			seen[a.App] = true
			names = append(names, a.App)
		}
	}
	sort.Strings(names)
	s := names[0]
	for _, n := range names[1:] {
		s += "+" + n
	}
	return s
}

func bundleName(r transport.Ref) string {
	if i := lastIndexByte(r.Location, ':'); i >= 0 {
		return r.Location[i+1:]
	}
	return filepath.Base(r.Location)
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
