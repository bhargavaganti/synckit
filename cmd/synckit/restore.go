package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/restore"
)

func newRestoreCmd() *cobra.Command {
	var appsFlag []string
	var force, dryRun, forceClose bool
	cmd := &cobra.Command{
		Use:   "restore <bundle.zip>",
		Short: "Restore app profiles from a bundle onto this machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sel := selectionFromApps(appsFlag)
			res, err := restore.Run(adapters(nil), restore.Options{
				Src:        args[0],
				Selected:   sel,
				Force:      force,
				ForceClose: forceClose,
				DryRun:     dryRun,
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
				if o.Target != "" {
					fmt.Printf("    target: %s\n", o.Target)
				}
				if o.Backup != "" {
					fmt.Printf("    backup: %s\n", o.Backup)
				}
				for _, w := range o.Warnings {
					fmt.Printf("    ⚠ %s\n", w)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&appsFlag, "apps", nil, "limit to these apps (chrome,firefox,dbeaver)")
	cmd.Flags().BoolVar(&force, "force", false, "restore even if the target app is running (risks corruption)")
	cmd.Flags().BoolVar(&forceClose, "force-close", false, "terminate the target app before restoring (unsaved work is lost)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would happen without changing anything")
	return cmd
}
