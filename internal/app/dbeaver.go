package app

import (
	"os"
	"path/filepath"

	"github.com/bhargav/synckit/internal/bundle"
)

// DBeaver stores everything under a single "workspace6" directory, which makes
// it the most portable of the three: connection definitions live in
// General/.dbeaver/data-sources.json and credentials in credentials-config.json,
// the latter encrypted with a fixed AES key baked into DBeaver itself — so the
// whole workspace travels across machines intact.
type DBeaver struct{}

func NewDBeaver() *DBeaver { return &DBeaver{} }

func (d *DBeaver) ID() string { return "dbeaver" }

// workspaceRoot resolves the DBeaverData directory per OS. DBeaver has used
// workspace6 since v6; we also probe older names for resilience.
func (d *DBeaver) dbeaverDataDir() string {
	switch currentOS {
	case "windows":
		return firstExisting(filepath.Join(winAppData(), "DBeaverData"))
	case "darwin":
		return firstExisting(filepath.Join(homeDir(), "Library", "DBeaverData"))
	default: // linux and others
		return firstExisting(
			filepath.Join(homeDir(), ".local", "share", "DBeaverData"),
			filepath.Join(homeDir(), ".dbeaver"),
		)
	}
}

func (d *DBeaver) Detect() ([]Instance, error) {
	base := d.dbeaverDataDir()
	if base == "" {
		return nil, nil
	}
	var out []Instance
	// Each "workspaceN" directory is an independent instance.
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) >= 9 && name[:9] == "workspace" {
			out = append(out, Instance{
				App:   d.ID(),
				ID:    name,
				Label: "DBeaver " + name,
				Root:  filepath.Join(base, name),
			})
		}
	}
	return out, nil
}

// Running: DBeaver keeps a .metadata/.lock file in the workspace while open.
func (d *DBeaver) Running(inst Instance) (bool, error) {
	lock := filepath.Join(inst.Root, ".metadata", ".lock")
	if _, err := os.Stat(lock); err == nil {
		return true, nil
	}
	return false, nil
}

// Version is not reliably recorded in the workspace; leave empty for now.
// (Can later be parsed from .metadata/version.ini if present.)
func (d *DBeaver) Version(inst Instance) (string, error) {
	return "", nil
}

func (d *DBeaver) Portability() bundle.Portability {
	return bundle.Portability{
		SecretsCrossMachine: true,
		Note:                "DBeaver credentials use a fixed AES key; portable across machines.",
	}
}

func (d *DBeaver) Exclude() []string {
	return []string{
		".metadata/.lock",
		".metadata/.log",
		".metadata/*.log",
	}
}
