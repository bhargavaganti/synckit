package app

import (
	"os"
	"path/filepath"

	"github.com/bhargav/synckit/internal/bundle"
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

	// Chrome is ALWAYS synced as one whole unit — the entire User Data folder
	// (Local State + every profile). Per-profile copying never works across
	// machines (Chrome ignores folders not in its Local State registry), so
	// there's only this mode, and both machines are automatically consistent.
	return []Instance{{
		App:   c.ID(),
		ID:    "User Data",
		Label: "Chrome (whole profile folder)",
		Root:  base,
	}}, nil
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
	ex := []string{
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
	// Lean by default: drop the multi-GB per-site storage so a whole-folder
	// snapshot carries profiles/bookmarks/settings/extensions but stays small.
	ex = append(ex,
		"IndexedDB/**", "Local Storage/**", "Session Storage/**",
		"Media Cache/**", "Network/**", "Sessions/**",
	)
	return ex
}

// hasPrefix is a tiny local helper to avoid importing strings for one call.
func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
