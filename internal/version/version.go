// Package version holds the build version, injected at release time via
// -ldflags "-X github.com/bhargav/synckit/internal/version.Version=vX.Y.Z".
package version

// Version is the synckit build version. "dev" for local/unversioned builds.
var Version = "dev"

// String returns the version, always prefixed with a leading "v" for releases.
func String() string { return Version }
