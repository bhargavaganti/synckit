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

	// Ignore holds extra exclude globs on top of each app's built-in ones, keyed
	// by app id ("chrome", "firefox", …) or "*" for all apps. Use it to trim
	// bulky, non-essential data (caches, site storage, ML models) so bundles
	// stay small. Patterns are payload-relative; "dir/**" excludes a whole tree.
	Ignore map[string][]string `json:"ignore,omitempty"`

	// IncludeOnly, when set for an app, restricts its snapshot to ONLY these
	// payload-relative globs (still honoring excludes). Used for browser "safe
	// mode" — e.g. Chrome bookmarks only — since a full Chrome profile can't be
	// copied across machines (HMAC-signed prefs, OS-encrypted secrets).
	IncludeOnly map[string][]string `json:"includeOnly,omitempty"`

	// ChromeWholeUserData syncs Chrome as one unit — the entire User Data folder
	// including Local State (Chrome's profile registry + settings seed) — instead
	// of per-profile. This is what lets copied profiles actually appear in Chrome
	// (bookmarks/settings/extensions). Saved passwords still don't cross OSes.
	ChromeWholeUserData bool `json:"chromeWholeUserData,omitempty"`
}

// IgnoreFor returns the merged extra ignore globs for an app (its own + "*").
func (s Settings) IgnoreFor(appID string) []string {
	var out []string
	out = append(out, s.Ignore["*"]...)
	out = append(out, s.Ignore[appID]...)
	return out
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
