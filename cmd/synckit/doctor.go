package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/bundle"
	"github.com/bhargav/synckit/internal/settings"
	ts "github.com/bhargav/synckit/internal/tailscale"
	"github.com/bhargav/synckit/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the synckit version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("synckit %s\n", version.String())
			return nil
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose Tailscale detection, encryption and paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("synckit %s\n", version.String())
			fmt.Printf("spool:    %s\n", defaultSpoolDir())
			if bundle.Encrypted() {
				fmt.Println("encryption: ON")
			} else {
				fmt.Println("encryption: OFF (run `synckit key init`)")
			}
			if st := settings.Load(); st.TailscalePath != "" {
				fmt.Printf("tailscale path override: %s\n", st.TailscalePath)
			}
			fmt.Println("\n--- tailscale ---")
			fmt.Print(ts.Diagnose())
			return nil
		},
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or change synckit settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			st := settings.Load()
			fmt.Println("settings:", settings.Path())
			path := st.TailscalePath
			if path == "" {
				path = "(auto-detect)"
			}
			fmt.Printf("  tailscale path: %s\n", path)
			fmt.Printf("  resolved to:    %s\n", ts.BinPath())
			return nil
		},
	}

	tsCmd := &cobra.Command{
		Use:   "tailscale <path>",
		Short: "Set the Tailscale CLI path (empty to clear and auto-detect)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := settings.Load()
			if len(args) == 0 {
				st.TailscalePath = ""
			} else {
				st.TailscalePath = args[0]
			}
			if err := settings.Save(st); err != nil {
				return err
			}
			ts.SetBinPath(st.TailscalePath)
			if st.TailscalePath == "" {
				fmt.Println("cleared Tailscale path override (auto-detect).")
			} else {
				fmt.Printf("set Tailscale path: %s\n", st.TailscalePath)
			}
			// Immediately verify.
			if _, err := ts.SelfIP(); err != nil {
				return errors.New("saved, but Tailscale still not working: " + err.Error())
			}
			fmt.Println("verified: Tailscale responds.")
			return nil
		},
	}
	cmd.AddCommand(tsCmd)
	return cmd
}
