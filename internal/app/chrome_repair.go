package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// RepairChrome rebuilds Chrome's profile registry (Local State →
// profile.info_cache) from the profile folders that exist on disk, so profiles
// restored from another machine actually appear in Chrome's profile switcher.
// It backs up Local State first and refuses to run while Chrome is open.
// Best-effort: cross-OS quirks (os_crypt) may still limit what Chrome accepts.
func RepairChrome() (registered int, err error) {
	c := NewChrome()
	base := c.userDataDir()
	if base == "" {
		return 0, errors.New("Chrome User Data folder not found on this machine")
	}
	for _, n := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		if _, err := os.Lstat(filepath.Join(base, n)); err == nil {
			return 0, errors.New("Chrome appears to be running — fully quit it first, then repair")
		}
	}

	lsPath := filepath.Join(base, "Local State")
	ls := map[string]any{}
	orig, _ := os.ReadFile(lsPath)
	if len(orig) > 0 {
		_ = json.Unmarshal(orig, &ls)
	}
	profile, _ := ls["profile"].(map[string]any)
	if profile == nil {
		profile = map[string]any{}
	}
	infoCache, _ := profile["info_cache"].(map[string]any)
	if infoCache == nil {
		infoCache = map[string]any{}
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		return 0, err
	}
	var order []any
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := e.Name()
		if strings.Contains(dir, ".bak") {
			continue
		}
		if dir != "Default" && !strings.HasPrefix(dir, "Profile ") {
			continue
		}
		if _, err := os.Stat(filepath.Join(base, dir, "Preferences")); err != nil {
			continue
		}
		order = append(order, dir)
		if _, ok := infoCache[dir]; ok {
			continue // already registered
		}
		name := chromeProfileName(base, dir)
		infoCache[dir] = map[string]any{
			"name":                  name,
			"is_using_default_name": name == dir,
			"is_ephemeral":          false,
			"active_time":           0.0,
		}
		registered++
	}
	profile["info_cache"] = infoCache
	profile["profiles_order"] = order
	ls["profile"] = profile

	out, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return 0, err
	}
	if len(orig) > 0 { // back up the original Local State
		_ = os.WriteFile(lsPath+".synckit-bak", orig, 0o644)
	}
	if err := os.WriteFile(lsPath, out, 0o644); err != nil {
		return 0, err
	}
	return registered, nil
}

// chromeProfileName reads a profile's display name from its Preferences.
func chromeProfileName(base, dir string) string {
	b, err := os.ReadFile(filepath.Join(base, dir, "Preferences"))
	if err != nil {
		return dir
	}
	var p struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
	}
	if json.Unmarshal(b, &p) == nil && strings.TrimSpace(p.Profile.Name) != "" {
		return p.Profile.Name
	}
	return dir
}
