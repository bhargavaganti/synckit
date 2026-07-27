package app

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/bhargav/synckit/internal/bundle"
)

// Firefox is profile-based. profiles.ini in the Mozilla data dir lists each
// profile and its path. Unlike Chrome, Firefox's password store (logins.json +
// key4.db) is not bound to the OS keyring, so a full clone carries secrets
// across machines as long as both files travel together (they do, under
// full-clone) and any primary password is known to the user.
type Firefox struct{}

func NewFirefox() *Firefox { return &Firefox{} }

func (f *Firefox) ID() string { return "firefox" }

// mozillaDir resolves the Firefox profile root per OS.
func (f *Firefox) mozillaDir() string {
	switch currentOS {
	case "windows":
		return firstExisting(filepath.Join(winAppData(), "Mozilla", "Firefox"))
	case "darwin":
		return firstExisting(filepath.Join(homeDir(), "Library", "Application Support", "Firefox"))
	default:
		return firstExisting(
			filepath.Join(homeDir(), ".mozilla", "firefox"),
			// Snap/Flatpak variants
			filepath.Join(homeDir(), "snap", "firefox", "common", ".mozilla", "firefox"),
		)
	}
}

func (f *Firefox) Detect() ([]Instance, error) {
	base := f.mozillaDir()
	if base == "" {
		return nil, nil
	}
	ini := filepath.Join(base, "profiles.ini")
	data, err := os.Open(ini)
	if err != nil {
		return nil, nil // no profiles.ini → treat as not installed
	}
	defer data.Close()

	var out []Instance
	var curName, curPath string
	var relative = true
	flush := func() {
		if curPath == "" {
			return
		}
		root := curPath
		if relative {
			root = filepath.Join(base, filepath.FromSlash(curPath))
		}
		label := curName
		if label == "" {
			label = filepath.Base(curPath)
		}
		out = append(out, Instance{
			App:   f.ID(),
			ID:    filepath.Base(curPath),
			Label: "Firefox " + label,
			Root:  root,
		})
	}

	sc := bufio.NewScanner(data)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[Profile"):
			flush()
			curName, curPath, relative = "", "", true
		case strings.HasPrefix(line, "Name="):
			curName = strings.TrimPrefix(line, "Name=")
		case strings.HasPrefix(line, "Path="):
			curPath = strings.TrimPrefix(line, "Path=")
		case strings.HasPrefix(line, "IsRelative="):
			relative = strings.TrimPrefix(line, "IsRelative=") != "0"
		case strings.HasPrefix(line, "["):
			// entering a non-profile section (e.g. [General], [Install...])
			flush()
			curName, curPath, relative = "", "", true
		}
	}
	flush()
	return out, sc.Err()
}

// Running: Firefox holds a lock file in the profile while open
// (parent.lock on Windows/macOS, lock symlink on Linux).
func (f *Firefox) Running(inst Instance) (bool, error) {
	for _, name := range []string{"parent.lock", "lock", ".parentlock"} {
		if _, err := os.Lstat(filepath.Join(inst.Root, name)); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (f *Firefox) Version(inst Instance) (string, error) {
	// compatibility.ini records LastVersion=... under [Compatibility].
	b, err := os.ReadFile(filepath.Join(inst.Root, "compatibility.ini"))
	if err != nil {
		return "", nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "LastVersion=") {
			v := strings.TrimPrefix(line, "LastVersion=")
			if i := strings.IndexByte(v, '_'); i >= 0 {
				v = v[:i]
			}
			return v, nil
		}
	}
	return "", nil
}

func (f *Firefox) Portability() bundle.Portability {
	return bundle.Portability{
		SecretsCrossMachine: true,
		Note:                "Firefox logins travel with key4.db; a primary password (if set) is still required.",
	}
}

func (f *Firefox) Exclude() []string {
	return []string{
		"parent.lock", "lock", ".parentlock",
		"cache2/**", "startupCache/**", "shader-cache/**",
		"lock/**", "*.sqlite-wal", "*.sqlite-shm",
	}
}
