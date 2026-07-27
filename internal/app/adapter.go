// Package app defines the Adapter contract and its per-application
// implementations. An adapter knows, for one app on the current OS:
//   - where its profiles/workspaces live
//   - how to tell if the app is currently running (lock preflight)
//   - which version produced them
//   - how portable its secrets are
//
// Everything above the adapter layer (snapshot, restore, transport) is
// generic and never hard-codes a path or a process name.
package app

import "github.com/bhargav/synckit/internal/bundle"

// Instance is a single syncable unit of an app: one Chrome/Firefox profile,
// or one DBeaver workspace.
type Instance struct {
	App   string // owning adapter id: "chrome" | "firefox" | "dbeaver"
	ID    string // stable id used in the bundle, e.g. "Default", "workspace6"
	Label string // human label, e.g. "Profile 1 (work)"
	Root  string // absolute path to the profile/workspace directory
}

// Adapter is implemented once per application.
type Adapter interface {
	// ID is the adapter's stable identifier: "chrome" | "firefox" | "dbeaver".
	ID() string

	// Detect enumerates the app's installed instances on this machine.
	// Returns an empty slice (not an error) when the app isn't installed.
	Detect() ([]Instance, error)

	// Running reports whether the app currently holds locks on the given
	// instance's files. Snapshot and restore both refuse to proceed when true,
	// because copying live SQLite/WAL files corrupts the profile.
	Running(Instance) (bool, error)

	// Version returns the app version that produced the instance, for skew
	// warnings on restore. Empty string if it can't be determined.
	Version(Instance) (string, error)

	// Portability describes how far this app's secrets can travel.
	Portability() bundle.Portability

	// Exclude returns payload-relative glob patterns to skip even under
	// full-clone (volatile caches, lock files, single-instance sockets).
	// Keeping these out avoids copying gigabytes of regenerable cache and
	// stale locks that would block the restored app from starting.
	Exclude() []string
}

// Registry returns the built-in adapters plus any user-defined apps declared in
// ~/.synckit/apps.json. Custom apps therefore flow through detection, snapshot,
// restore, sync and the capability matrix exactly like the built-ins.
func Registry() []Adapter {
	base := []Adapter{
		NewChrome(),
		NewFirefox(),
		NewDBeaver(),
	}
	custom, _ := LoadConfigs(DefaultConfigPath())
	return append(base, custom...)
}

// Find returns the adapter with the given id, or nil.
func Find(id string) Adapter {
	for _, a := range Registry() {
		if a.ID() == id {
			return a
		}
	}
	return nil
}
