package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/bundle"
	"github.com/bhargav/synckit/internal/snapshot"
)

func newSnapshotCmd() *cobra.Command {
	var appsFlag []string
	var out string
	var force bool
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Capture app profiles into a portable bundle (.zip)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !bundle.Encrypted() {
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ bundles are UNENCRYPTED — run `synckit key init` to encrypt secrets at rest.")
			}
			sel := selectionFromApps(appsFlag)
			origin, now := originNow()
			id := bundleID(origin, now)
			dst := out
			if dst == "" {
				dst = filepath.Join(defaultSpoolDir(), id+".zip")
			}

			res, err := snapshot.Run(adapters(sel), snapshot.Options{
				Dst:      dst,
				Now:      now,
				Origin:   origin,
				ID:       id,
				Force:    force,
				Selected: sel,
			})
			if err != nil {
				// Still surface any skips that explain an empty capture.
				if res != nil {
					for _, s := range res.Skipped {
						fmt.Println("skipped:", s)
					}
				}
				return err
			}

			fmt.Printf("bundle: %s\n", res.Bundle)
			var totalFiles int
			var totalBytes int64
			for _, a := range res.Apps {
				fmt.Printf("  %-8s %-24s %5d files  %8.1f MB\n",
					a.App, a.Instance, a.Files, float64(a.Bytes)/(1<<20))
				totalFiles += a.Files
				totalBytes += a.Bytes
			}
			for _, s := range res.Skipped {
				fmt.Println("  skipped:", s)
			}
			fmt.Printf("total: %d files, %.1f MB\n", totalFiles, float64(totalBytes)/(1<<20))
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&appsFlag, "apps", nil, "limit to these apps (chrome,firefox,dbeaver)")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output bundle path (default ~/.synckit/bundles/<id>.zip)")
	cmd.Flags().BoolVar(&force, "force", false, "snapshot even if an app is running (risks corruption)")
	return cmd
}
