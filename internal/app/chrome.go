package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bhargav/synckit/internal/bundle"
	"github.com/bhargav/synckit/internal/settings"
)

// Chrome is the hardest of the three. Under "User Data" each "Profile N"
// (and "Default") is an instance. Passwords and cookies are encrypted with a
// key held in the OS keyring — DPAPI (bound to the Windows user account),
// Keychain on macOS, gnome-keyring/kwallet on Linux. A full clone copies those
// encrypted stores verbatim, but they only decrypt again on the SAME machine
// and OS user; restored elsewhere, Chrome silently drops them. That's why
// Portability().SecretsCrossMachine is false and restore warns on host change.
type Chrome struct{}

func NewChrome() *Chrome { return &Chrome{} }

func (c *Chrome) ID() string { return "chrome" }

// userDataDir resolves Chrome's "User Data" root per OS.
func (c *Chrome) userDataDir() string {
	switch currentOS {
	case "windows":
		return firstExisting(filepath.Join(winLocalAppData(), "Google", "Chrome", "User Data"))
	case "darwin":
		return firstExisting(filepath.Join(homeDir(), "Library", "Application Support", "Google", "Chrome"))
	default:
		return firstExisting(
			filepath.Join(homeDir(), ".config", "google-chrome"),
			filepath.Join(homeDir(), ".config", "chromium"),
		)
	}
}

// localState is the User Data/Local State file; its "profile.info_cache" maps
// profile dir names to friendly names.
type localState struct {
	Profile struct {
		InfoCache map[string]struct {
			Name string `json:"name"`
		} `json:"info_cache"`
	} `json:"profile"`
}

func (c *Chrome) Detect() ([]Instance, error) {
	base := c.userDataDir()
	if base == "" {
		return nil, nil
	}

	// Whole-User-Data mode: one instance covering Local State + every profile,
	// so Chrome actually recognises restored profiles.
	if settings.Load().ChromeWholeUserData {
		return []Instance{{
			App:   c.ID(),
			ID:    "User Data",
			Label: "Chrome (whole profile folder)",
			Root:  base,
		}}, nil
	}

	// Friendly names, if available.
	names := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(base, "Local State")); err == nil {
		var ls localState
		if json.Unmarshal(b, &ls) == nil {
			for dir, info := range ls.Profile.InfoCache {
				names[dir] = info.Name
			}
		}
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var out []Instance
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := e.Name()
		// Skip synckit restore backups (…-<ts>.bak) so they aren't shown as profiles.
		if strings.Contains(dir, ".bak") {
			continue
		}
		// A profile dir is "Default" or "Profile N"; verify by its Preferences file.
		if dir != "Default" && !hasPrefix(dir, "Profile ") {
			continue
		}
		if _, err := os.Stat(filepath.Join(base, dir, "Preferences")); err != nil {
			continue
		}
		label := dir
		if n, ok := names[dir]; ok && n != "" {
			label = dir + " (" + n + ")"
		}
		out = append(out, Instance{
			App:   c.ID(),
			ID:    dir,
			Label: "Chrome " + label,
			Root:  filepath.Join(base, dir),
		})
	}
	return out, nil
}

// Running: Chrome's SingletonLock lives at the User Data root (not per-profile),
// so any open Chrome window locks all profiles. We check the parent dir.
func (c *Chrome) Running(inst Instance) (bool, error) {
	// The Singleton* lock lives at the User Data root, which is the parent of a
	// profile dir (per-profile mode) or the instance root itself (whole mode).
	for _, base := range []string{inst.Root, filepath.Dir(inst.Root)} {
		for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
			if _, err := os.Lstat(filepath.Join(base, name)); err == nil {
				return true, nil
			}
		}
	}
	return false, nil
}

func (c *Chrome) Version(inst Instance) (string, error) {
	// "Last Version" sits at the User Data root.
	b, err := os.ReadFile(filepath.Join(filepath.Dir(inst.Root), "Last Version"))
	if err != nil {
		return "", nil
	}
	return string(b), nil
}

func (c *Chrome) Portability() bundle.Portability {
	return bundle.Portability{
		SecretsCrossMachine: false,
		Note:                "Chrome passwords/cookies are OS-keyring bound; they only decrypt on the same machine and OS user. Restored elsewhere, Chrome drops them (settings/bookmarks/extensions still restore).",
	}
}

func (c *Chrome) Exclude() []string {
	return []string{
		// caches (regenerable)
		"Cache/**", "Code Cache/**", "GPUCache/**", "ShaderCache/**",
		"Service Worker/**", "GrShaderCache/**", "GraphiteDawnCache/**",
		"DawnGraphiteCache/**", "DawnCache/**", "DawnWebGPUCache/**",
		"component_crx_cache/**", "extensions_crx_cache/**",
		// bulky, non-essential data (often GBs) — not needed for a settings sync
		"optimization_guide_model_store/**", "optimization_guide_prediction_model_downloads/**",
		"segmentation_platform/**", "Crashpad/**", "Crash Reports/**",
		"BrowserMetrics/**", "Safe Browsing/**", "blob_storage/**",
		"GPUCache", "Application Cache/**", "File System/**",
		// volatile / lock / temp / restore backups
		"Singleton*", "*.bak/**", "*.tmp", "*-journal", "*-wal", "*-shm",
	}
}

// hasPrefix is a tiny local helper to avoid importing strings for one call.
func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
