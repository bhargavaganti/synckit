package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/settings"
)

func newIgnoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ignore",
		Short: "Exclude bulky/unwanted paths from snapshots (keeps bundles small)",
		Long: `Ignore rules are extra exclude globs on top of each app's built-in ones,
stored in ~/.synckit/settings.json. Use "*" as the app to apply to every app.
Patterns are relative to the profile root; "dir/**" excludes a whole tree.

  synckit ignore add chrome "IndexedDB/**"
  synckit ignore add "*" "*.log"
  synckit ignore list`,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newIgnoreListCmd(), newIgnoreAddCmd(), newIgnoreRemoveCmd())
	return cmd
}

func newIgnoreListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show current ignore rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			st := settings.Load()
			if len(st.Ignore) == 0 {
				fmt.Println("no ignore rules. Add one:  synckit ignore add chrome \"IndexedDB/**\"")
				return nil
			}
			apps := make([]string, 0, len(st.Ignore))
			for a := range st.Ignore {
				apps = append(apps, a)
			}
			sort.Strings(apps)
			for _, a := range apps {
				fmt.Printf("%s:\n", a)
				for _, g := range st.Ignore[a] {
					fmt.Printf("  %s\n", g)
				}
			}
			return nil
		},
	}
}

func newIgnoreAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <app|*> <pattern>",
		Short: "Add an ignore pattern for an app (or * for all)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, pat := args[0], args[1]
			st := settings.Load()
			if st.Ignore == nil {
				st.Ignore = map[string][]string{}
			}
			for _, g := range st.Ignore[app] {
				if g == pat {
					fmt.Println("already present.")
					return nil
				}
			}
			st.Ignore[app] = append(st.Ignore[app], pat)
			if err := settings.Save(st); err != nil {
				return err
			}
			fmt.Printf("ignoring %q for %s\n", pat, app)
			return nil
		},
	}
}

func newIgnoreRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <app|*> <pattern>",
		Aliases: []string{"rm"},
		Short:   "Remove an ignore pattern",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, pat := args[0], args[1]
			st := settings.Load()
			kept := st.Ignore[app][:0]
			removed := false
			for _, g := range st.Ignore[app] {
				if g == pat {
					removed = true
					continue
				}
				kept = append(kept, g)
			}
			if !removed {
				return fmt.Errorf("pattern %q not found for %s", pat, app)
			}
			if len(kept) == 0 {
				delete(st.Ignore, app)
			} else {
				st.Ignore[app] = kept
			}
			if err := settings.Save(st); err != nil {
				return err
			}
			fmt.Printf("removed %q from %s\n", pat, app)
			return nil
		},
	}
}
