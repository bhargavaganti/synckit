package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bhargav/synckit/internal/bundle"
)

// AppConfig is a user-defined app declared in ~/.synckit/apps.json. It lets you
// sync any app synckit doesn't build in — VS Code, Sublime, JetBrains, Obsidian,
// SSH config, dotfiles — just by naming its per-OS config directory.
type AppConfig struct {
	ID    string `json:"id"`    // unique, e.g. "vscode"
	Label string `json:"label"` // display name, e.g. "VS Code"

	// Paths is the config/profile directory per OS ("windows"|"darwin"|"linux").
	// Supports ~, $VAR/${VAR}, and %VAR% (Windows). The resolved directory IS the
	// synced instance.
	Paths map[string]string `json:"paths"`

	// SecretsCrossMachine mirrors the built-in portability flag: true if this
	// app's secrets remain usable when restored on a different machine. Leave
	// false if you're unsure — it only affects the warning shown on restore.
	SecretsCrossMachine bool   `json:"secretsCrossMachine"`
	Note                string `json:"note,omitempty"`

	// LockFiles are paths (relative to the config dir) whose existence means the
	// app is running; snapshot/restore refuse while any is present.
	LockFiles []string `json:"lockFiles,omitempty"`

	// Exclude are payload-relative globs to skip (caches, logs, lock files).
	Exclude []string `json:"exclude,omitempty"`
}

type appsFile struct {
	Apps []AppConfig `json:"apps"`
}

// DefaultConfigPath is ~/.synckit/apps.json.
func DefaultConfigPath() string {
	return filepath.Join(homeDir(), ".synckit", "apps.json")
}

// LoadConfigs reads user-defined apps from path (missing file => none) and
// returns them as adapters. Malformed entries (no id / no path for this OS) are
// skipped rather than failing the whole load.
func LoadConfigs(path string) ([]Adapter, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f appsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	var out []Adapter
	for _, c := range f.Apps {
		if c.ID == "" {
			continue
		}
		root := expandPath(c.Paths[currentOS])
		out = append(out, &configAdapter{cfg: c, root: root})
	}
	return out, nil
}

// configAdapter adapts an AppConfig to the Adapter interface. Each configured
// app resolves to a single instance rooted at its per-OS directory.
type configAdapter struct {
	cfg  AppConfig
	root string // resolved for currentOS; "" if no path configured for this OS
}

func (c *configAdapter) ID() string { return c.cfg.ID }

func (c *configAdapter) Detect() ([]Instance, error) {
	if c.root == "" || !dirExists(c.root) {
		return nil, nil
	}
	label := c.cfg.Label
	if label == "" {
		label = c.cfg.ID
	}
	return []Instance{{App: c.cfg.ID, ID: "default", Label: label, Root: c.root}}, nil
}

func (c *configAdapter) Running(Instance) (bool, error) {
	for _, lf := range c.cfg.LockFiles {
		if _, err := os.Stat(filepath.Join(c.root, filepath.FromSlash(lf))); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (c *configAdapter) Version(Instance) (string, error) { return "", nil }

func (c *configAdapter) Portability() bundle.Portability {
	return bundle.Portability{SecretsCrossMachine: c.cfg.SecretsCrossMachine, Note: c.cfg.Note}
}

func (c *configAdapter) Exclude() []string { return c.cfg.Exclude }

// ExampleConfigJSON seeds ~/.synckit/apps.json with common apps to edit. Paths
// support ~, $VAR/${VAR} and %VAR%. Entries whose directory doesn't exist on a
// given machine are simply skipped there.
const ExampleConfigJSON = `{
  "apps": [
    {
      "id": "vscode",
      "label": "VS Code",
      "secretsCrossMachine": true,
      "paths": {
        "windows": "%APPDATA%/Code/User",
        "darwin": "~/Library/Application Support/Code/User",
        "linux": "~/.config/Code/User"
      },
      "exclude": ["workspaceStorage/**", "globalStorage/**/cache/**", "logs/**", "*.log"]
    },
    {
      "id": "sublime",
      "label": "Sublime Text",
      "secretsCrossMachine": true,
      "paths": {
        "windows": "%APPDATA%/Sublime Text/Packages/User",
        "darwin": "~/Library/Application Support/Sublime Text/Packages/User",
        "linux": "~/.config/sublime-text/Packages/User"
      }
    },
    {
      "id": "obsidian",
      "label": "Obsidian config",
      "secretsCrossMachine": true,
      "paths": {
        "windows": "%APPDATA%/obsidian",
        "darwin": "~/Library/Application Support/obsidian",
        "linux": "~/.config/obsidian"
      },
      "exclude": ["Cache/**", "GPUCache/**", "*.log"]
    }
  ]
}
`

// winVar matches %NAME% Windows-style environment references.
var winVar = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%`)

// expandPath resolves ~, $VAR/${VAR}, and %VAR% in a configured path.
func expandPath(p string) string {
	if p == "" {
		return ""
	}
	p = winVar.ReplaceAllStringFunc(p, func(m string) string {
		return os.Getenv(m[1 : len(m)-1])
	})
	p = os.ExpandEnv(p) // $VAR and ${VAR}
	if p == "~" {
		p = homeDir()
	} else if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		p = filepath.Join(homeDir(), p[2:])
	}
	return filepath.Clean(filepath.FromSlash(p))
}
