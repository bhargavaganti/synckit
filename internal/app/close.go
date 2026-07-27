package app

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// processNames maps an app id to the OS process names to terminate when the
// user opts to force-close it before a restore. Custom apps fall back to their id.
var processNames = map[string][]string{
	"chrome":  {"Google Chrome", "chrome"},
	"firefox": {"firefox", "Firefox"},
	"dbeaver": {"dbeaver", "DBeaver"},
}

// CloseApp force-terminates the given app's processes. Best-effort: nothing
// matching (app already closed) is treated as success. Destructive — callers
// MUST confirm with the user first, since unsaved work in the app is lost.
func CloseApp(appID string) error {
	names := processNames[appID]
	if len(names) == 0 {
		names = []string{appID}
	}
	var firstErr error
	for _, n := range names {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("taskkill", "/IM", n+".exe", "/F", "/T")
		} else {
			cmd = exec.Command("pkill", "-x", n) // -x: exact process-name match
		}
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil // killed something
		}
		// pkill exits 1 / taskkill errors when nothing matched — not a real error.
		low := strings.ToLower(string(out))
		matchless := strings.Contains(low, "not found") || strings.Contains(low, "no tasks") || len(out) == 0
		if !matchless && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %v", n, err)
		}
	}
	return firstErr // nil when nothing matched (already closed)
}
