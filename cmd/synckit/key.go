package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/vault"
)

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage the shared encryption key (bundles are encrypted at rest & in transit)",
		Long: `synckit encrypts bundles with age using one shared key that lives on every
machine at ~/.synckit/identity.key. Set it up once, then copy it to your other
machines so they can decrypt each other's bundles:

  machine A:  synckit key init
              synckit key export > synckit.key      # move this file securely
  machine B:  synckit key import synckit.key

Without a key, bundles are written in PLAINTEXT (and synckit warns).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newKeyInitCmd(), newKeyStatusCmd(), newKeyExportCmd(), newKeyImportCmd())
	return cmd
}

func newKeyInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a new encryption key on this machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := vault.DefaultPath()
			recipient, err := vault.Init(path)
			if err != nil {
				return err
			}
			fmt.Printf("Generated synckit key: %s\n", path)
			fmt.Printf("Public recipient:      %s\n\n", recipient)
			fmt.Println("Copy this key to your other machines so they can decrypt:")
			fmt.Println("  synckit key export > synckit.key   # here")
			fmt.Println("  synckit key import synckit.key     # on each other machine")
			return nil
		},
	}
}

func newKeyStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether an encryption key is configured",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := vault.DefaultPath()
			v, err := vault.Load(path)
			if errors.Is(err, vault.ErrNoKey) {
				fmt.Println("No key configured — bundles are UNENCRYPTED.")
				fmt.Println("Run `synckit key init` to enable encryption.")
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Printf("Encryption ON. Key: %s\n", path)
			fmt.Printf("Public recipient:   %s\n", v.Recipient())
			return nil
		},
	}
}

func newKeyExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Print the key file to stdout (to move to another machine)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := vault.DefaultPath()
			b, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					return errors.New("no key to export (run `synckit key init` first)")
				}
				return err
			}
			fmt.Fprint(os.Stderr, "⚠ this is your SECRET key — transfer it over a trusted channel.\n")
			_, err = os.Stdout.Write(b)
			return err
		},
	}
}

func newKeyImportCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Install a key from a file (or stdin) on this machine",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := vault.DefaultPath()
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to replace)", path)
			}
			var data []byte
			var err error
			if len(args) == 1 {
				data, err = os.ReadFile(args[0])
			} else {
				data, err = io.ReadAll(os.Stdin)
			}
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return err
			}
			// Validate it parses.
			if _, err := vault.Load(path); err != nil {
				return fmt.Errorf("imported key is invalid: %w", err)
			}
			fmt.Printf("Imported key to %s. Encryption is now ON.\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing key")
	return cmd
}
