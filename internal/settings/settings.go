// Package settings persists small user preferences to ~/.synckit/settings.json.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings are the persisted user preferences.
type Settings struct {
	// TailscalePath overrides Tailscale CLI auto-detection when set — useful when
	// the CLI lives somewhere non-standard or the default one doesn't work.
	TailscalePath string `json:"tailscalePath,omitempty"`
}

// Path is ~/.synckit/settings.json.
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".synckit", "settings.json")
	}
	return filepath.Join(home, ".synckit", "settings.json")
}

// Load reads settings, returning zero values if the file is missing or invalid.
func Load() Settings {
	var s Settings
	b, err := os.ReadFile(Path())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

// Save writes settings to disk (creating ~/.synckit if needed).
func Save(s Settings) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
