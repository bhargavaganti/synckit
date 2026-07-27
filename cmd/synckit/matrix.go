package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/service"
	"github.com/bhargav/synckit/internal/transport"
)

func newMatrixCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "matrix",
		Short: "Show what can sync across the tailnet (per app, per machine)",
		Long: `Queries every peer running the synckit daemon for its capabilities and prints
a matrix: which apps/profiles each machine has, and what can flow between them.
Firefox profiles are matched by role (e.g. default-release) across machines,
and secret portability is applied per app (Chrome secrets stay machine-bound).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := service.New(defaultSpoolDir(), port)
			m := svc.TailnetMatrix()

			if len(m.Machines) <= 1 {
				fmt.Println("Only this machine is reachable. Start `synckit serve` on your")
				fmt.Println("other machines so they advertise capabilities over the tailnet.")
			}

			// Column widths.
			appW := len("APP / PROFILE")
			for _, r := range m.Rows {
				if w := len(r.App + " / " + r.Role); w > appW {
					appW = w
				}
			}
			colW := make([]int, len(m.Machines))
			for i, mc := range m.Machines {
				colW[i] = len(mc)
				if colW[i] < 8 {
					colW[i] = 8
				}
			}

			// Header.
			fmt.Printf("%-*s", appW+2, "APP / PROFILE")
			for i, mc := range m.Machines {
				fmt.Printf("%-*s", colW[i]+2, mc)
			}
			fmt.Println("SYNCABLE")

			for _, r := range m.Rows {
				fmt.Printf("%-*s", appW+2, r.App+" / "+r.Role)
				for i, mc := range m.Machines {
					cell := "  -"
					if c, ok := r.Cells[mc]; ok && c.Present {
						cell = "  ✓"
						if c.Version != "" {
							cell = "✓ " + c.Version
						}
					}
					fmt.Printf("%-*s", colW[i]+2, cell)
				}
				fmt.Printf("%s  (%s)\n", verdictLabel(r.Verdict), r.Note)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", transport.DefaultPort, "daemon port on peers")
	return cmd
}

func verdictLabel(v service.Verdict) string {
	switch v {
	case service.VerdictFull:
		return "✅ full"
	case service.VerdictSettings:
		return "⚠ settings-only"
	case service.VerdictSeed:
		return "→ seed"
	default:
		return strings.ToUpper(string(v))
	}
}
