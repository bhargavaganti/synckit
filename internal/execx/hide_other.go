//go:build !windows

package execx

import "os/exec"

// Hide is a no-op on non-Windows platforms (no console windows to hide).
func Hide(cmd *exec.Cmd) {}
