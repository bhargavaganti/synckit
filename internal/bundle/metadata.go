// Package bundle defines the on-disk format shared by every transport.
//
// A bundle is a plain .zip so it doubles as an export/import artifact:
//
//	metadata.json          — this file's Metadata, describing the bundle
//	payload/<app>/<inst>/  — verbatim copy of each profile/workspace tree
//
// The same bytes are moved by the file transport and the Tailscale daemon,
// so there is exactly one format to reason about.
package bundle

import "time"

// FormatVersion is bumped when the bundle layout changes incompatibly.
const FormatVersion = 1

// Portability records how far an app's payload can travel and still work.
// Full-clone copies secrets verbatim, but some secrets are bound to the
// origin machine's OS keyring and cannot be decrypted elsewhere.
type Portability struct {
	// SecretsCrossMachine is true when encrypted stores (passwords, cookies)
	// remain usable after restoring onto a different machine.
	//   dbeaver  → true  (fixed AES key baked into DBeaver)
	//   firefox  → true  (key4.db travels with logins.json)
	//   chrome   → false (DPAPI/Keychain key is machine+OS-user bound)
	SecretsCrossMachine bool `json:"secretsCrossMachine"`

	// Note is a human-readable caveat surfaced on restore mismatch.
	Note string `json:"note,omitempty"`
}

// AppEntry describes one captured app instance inside the bundle.
type AppEntry struct {
	App      string      `json:"app"`      // "chrome" | "firefox" | "dbeaver"
	Instance string      `json:"instance"` // profile/workspace id, e.g. "Default"
	Label    string      `json:"label"`    // display name, e.g. "Profile 1 (work)"
	Version  string      `json:"version"`  // app version at snapshot time
	Path     string      `json:"path"`     // payload dir relative to bundle root
	Bytes    int64       `json:"bytes"`    // uncompressed payload size
	Files    int         `json:"files"`    // file count in payload
	Portable Portability `json:"portable"`
	// Checksums maps each payload-relative file path to its SHA-256 hex digest.
	Checksums map[string]string `json:"checksums"`
	// Fingerprint is a single content hash over Checksums — identical content on
	// two machines yields the same fingerprint, so synckit can tell "in sync"
	// from "differs" and skip re-snapshotting unchanged profiles.
	Fingerprint string `json:"fingerprint"`
}

// Origin captures where a bundle was produced, for skew/portability warnings.
type Origin struct {
	OS       string `json:"os"`       // runtime.GOOS: "windows" | "darwin" | "linux"
	Arch     string `json:"arch"`     // runtime.GOARCH
	Hostname string `json:"hostname"` // source machine hostname
	User     string `json:"user"`     // source OS username (matters for Chrome DPAPI)
}

// Metadata is serialized to metadata.json at the bundle root.
type Metadata struct {
	Format    int        `json:"format"`    // FormatVersion
	ID        string     `json:"id"`        // unique bundle id
	CreatedAt time.Time  `json:"createdAt"` // caller-supplied (no wall clock in core)
	Origin    Origin     `json:"origin"`
	Apps      []AppEntry `json:"apps"`
}
