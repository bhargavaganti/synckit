package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/app"
)

func newAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Manage user-defined apps (sync more apps by giving their paths)",
		Long: `synckit syncs Chrome, Firefox and DBeaver out of the box. Declare any other
app in ~/.synckit/apps.json to sync it too — give its config directory per OS
and it flows through detect / snapshot / restore / sync like the built-ins.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := app.DefaultConfigPath()
			fmt.Println("config:", path)
			custom, err := app.LoadConfigs(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			if len(custom) == 0 {
				fmt.Println("no custom apps yet. Scaffold an example with:  synckit apps init")
				return nil
			}
			fmt.Printf("%d custom app(s):\n", len(custom))
			for _, ad := range custom {
				insts, _ := ad.Detect()
				state := "not found on this machine"
				if len(insts) > 0 {
					state = insts[0].Root
				}
				port := "secrets: cross-machine"
				if !ad.Portability().SecretsCrossMachine {
					port = "secrets: same-machine only"
				}
				fmt.Printf("  • %-14s [%s]\n      %s\n", ad.ID(), port, state)
			}
			return nil
		},
	}

	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write an example ~/.synckit/apps.json to edit",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := app.DefaultConfigPath()
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(app.ExampleConfigJSON), 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s\nEdit it, then run `synckit detect` to verify.\n", path)
			return nil
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite an existing apps.json")

	cmd.AddCommand(initCmd)
	return cmd
}
