package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/app"
	"github.com/bhargav/synckit/internal/bundle"
	"github.com/bhargav/synckit/internal/vault"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "synckit",
		Short:         "Sync Chrome, Firefox and DBeaver profiles across machines",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Load the shared encryption key once (if present) so every bundle
		// operation in any subcommand is encrypted/decrypted transparently.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if v, err := vault.Load(vault.DefaultPath()); err == nil {
				bundle.UseVault(v)
			}
		},
	}
	root.AddCommand(
		newDetectCmd(),
		newSnapshotCmd(),
		newRestoreCmd(),
		newPeersCmd(),
		newPushCmd(),
		newPullCmd(),
		newSyncCmd(),
		newServeCmd(),
		newAppsCmd(),
		newMatrixCmd(),
		newKeyCmd(),
	)
	return root
}

// selectionFromApps turns --apps chrome,firefox into a set; nil means "all".
func selectionFromApps(apps []string) map[string]bool {
	if len(apps) == 0 {
		return nil
	}
	sel := map[string]bool{}
	for _, a := range apps {
		sel[strings.TrimSpace(strings.ToLower(a))] = true
	}
	return sel
}

// originNow gathers this machine's identity and the current time. The core
// packages deliberately take these as inputs, so all wall-clock/host access
// lives here at the edge.
func originNow() (bundle.Origin, time.Time) {
	host, _ := os.Hostname()
	return bundle.Origin{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: host,
		User:     currentUser(),
	}, time.Now()
}

func currentUser() string {
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "unknown"
}

// bundleID builds a human-sortable id like host-20260726-153004.
func bundleID(o bundle.Origin, t time.Time) string {
	host := o.Hostname
	if host == "" {
		host = "host"
	}
	return fmt.Sprintf("%s-%s", host, t.Format("20060102-150405"))
}

// defaultSpoolDir is where the daemon stores received bundles and where
// snapshots land by default.
func defaultSpoolDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "synckit-bundles")
	}
	return filepath.Join(home, ".synckit", "bundles")
}

// adapters returns the registry, optionally filtered to a selection.
func adapters(sel map[string]bool) []app.Adapter {
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
