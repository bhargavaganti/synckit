package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDetectCmd() *cobra.Command {
	var appsFlag []string
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "List the app profiles/workspaces found on this machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			sel := selectionFromApps(appsFlag)
			for _, ad := range adapters(sel) {
				insts, err := ad.Detect()
				if err != nil {
					fmt.Printf("%-8s error: %v\n", ad.ID(), err)
					continue
				}
				if len(insts) == 0 {
					fmt.Printf("%-8s (not installed)\n", ad.ID())
					continue
				}
				port := "secrets: cross-machine"
				if !ad.Portability().SecretsCrossMachine {
					port = "secrets: same-machine only"
				}
				fmt.Printf("%-8s [%s]\n", ad.ID(), port)
				for _, inst := range insts {
					running, _ := ad.Running(inst)
					state := ""
					if running {
						state = "  ⚠ RUNNING (close before snapshot)"
					}
					ver, _ := ad.Version(inst)
					if ver != "" {
						ver = " v" + ver
					}
					fmt.Printf("    • %s%s%s\n      %s\n", inst.Label, ver, state, inst.Root)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&appsFlag, "apps", nil, "limit to these apps (chrome,firefox,dbeaver)")
	return cmd
}
