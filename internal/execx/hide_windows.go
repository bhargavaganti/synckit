//go:build windows

// Package execx hides the console window that Windows would otherwise flash
// each time a GUI app shells out to a CLI (tailscale, taskkill, …).
package execx

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// Hide configures cmd so no console window appears when it runs.
func Hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
