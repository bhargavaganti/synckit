// Command synckit clones and syncs app profiles (Chrome, Firefox, DBeaver)
// across machines, via portable .zip bundles and a Tailscale peer daemon.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
