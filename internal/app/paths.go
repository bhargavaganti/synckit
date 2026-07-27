package app

import (
	"os"
	"path/filepath"
	"runtime"
)

// homeDir returns the current user's home directory, or "" on failure.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// winAppData returns %APPDATA% (roaming), falling back to the conventional path.
func winAppData() string {
	if v := os.Getenv("APPDATA"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), "AppData", "Roaming")
}

// winLocalAppData returns %LOCALAPPDATA%, falling back to the conventional path.
func winLocalAppData() string {
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), "AppData", "Local")
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// firstExisting returns the first candidate path that is an existing directory,
// or "" if none exist. Used to resolve per-OS install locations.
func firstExisting(candidates ...string) string {
	for _, c := range candidates {
		if c != "" && dirExists(c) {
			return c
		}
	}
	return ""
}

// currentOS exposes runtime.GOOS behind a var so tests can note intent.
var currentOS = runtime.GOOS
