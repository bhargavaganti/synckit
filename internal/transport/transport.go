// Package transport moves bundles between machines. Every transport handles the
// exact same bundle .zip the file exporter produces, so adding a transport never
// touches the snapshot/restore core.
package transport

import "github.com/bhargav/synckit/internal/bundle"

// Ref identifies a stored bundle within a transport.
type Ref struct {
	ID       string          // bundle id
	Location string          // transport-specific handle (path, peer:path, ...)
	Meta     bundle.Metadata // metadata, when cheaply available
}

// Transport is the pluggable movement layer.
type Transport interface {
	// Name is the transport id: "file" | "tailscale".
	Name() string

	// Put stores the bundle at srcPath and returns its reference.
	Put(srcPath string) (Ref, error)

	// Get fetches the bundle identified by ref into dstPath.
	Get(ref Ref, dstPath string) error

	// List enumerates bundles visible to this transport.
	List() ([]Ref, error)
}
