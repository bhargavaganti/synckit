// Package snapshot captures selected app instances into a bundle.
package snapshot

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bhargav/synckit/internal/app"
	"github.com/bhargav/synckit/internal/bundle"
)

// Options controls a snapshot run.
type Options struct {
	Dst      string          // output bundle path
	Now      time.Time       // caller-supplied timestamp (core takes no wall clock)
	Origin   bundle.Origin   // source host identity
	ID       string          // bundle id
	Force    bool            // proceed even if an app appears to be running
	Selected map[string]bool // adapter id -> include; nil means all detected
	// Ignore adds extra exclude globs per app id ("*" applies to all) on top of
	// each adapter's built-in excludes — user-configurable trimming.
	Ignore map[string][]string
	// Include, when set for an app id, restricts that app's snapshot to ONLY
	// files matching these globs (still honoring excludes). Empty = include all.
	Include map[string][]string
}

// Result summarizes what was captured.
type Result struct {
	Bundle string
	Apps   []bundle.AppEntry
	Skipped []string // "app/instance: reason"
}

// Run detects instances for each adapter, filters by Options.Selected, refuses
// running apps (unless Force), and writes them all into one bundle.
func Run(adapters []app.Adapter, opts Options) (*Result, error) {
	w, err := bundle.Create(opts.Dst, bundle.Metadata{
		ID:        opts.ID,
		CreatedAt: opts.Now,
		Origin:    opts.Origin,
	})
	if err != nil {
		return nil, err
	}

	res := &Result{Bundle: opts.Dst}
	var entries []bundle.AppEntry

	for _, ad := range adapters {
		if opts.Selected != nil && !opts.Selected[ad.ID()] {
			continue
		}
		insts, err := ad.Detect()
		if err != nil {
			w.Abort()
			return nil, fmt.Errorf("detect %s: %w", ad.ID(), err)
		}
		for _, inst := range insts {
			running, _ := ad.Running(inst)
			if running && !opts.Force {
				res.Skipped = append(res.Skipped,
					fmt.Sprintf("%s/%s: app is running (close it or use --force)", ad.ID(), inst.ID))
				continue
			}
			extra := append(append([]string{}, opts.Ignore["*"]...), opts.Ignore[ad.ID()]...)
			entry, skipped, err := captureInstance(w, ad, inst, extra, opts.Include[ad.ID()])
			if err != nil {
				w.Abort()
				return nil, fmt.Errorf("capture %s/%s: %w", ad.ID(), inst.ID, err)
			}
			if skipped > 0 {
				res.Skipped = append(res.Skipped,
					fmt.Sprintf("%s/%s: %d file(s) unreadable/locked (app may be running)", ad.ID(), inst.ID, skipped))
			}
			entries = append(entries, entry)
		}
	}

	if len(entries) == 0 {
		w.Abort()
		return res, fmt.Errorf("nothing captured (no matching instances, or all were running)")
	}

	w.SetApps(entries)
	if err := w.Finish(); err != nil {
		return nil, err
	}
	res.Apps = entries
	return res, nil
}

// captureInstance walks one instance's tree, honoring the adapter's excludes,
// and streams each file into the bundle under payload/<app>/<inst>/...
func captureInstance(w *bundle.Writer, ad app.Adapter, inst app.Instance, extraExcludes, includeOnly []string) (bundle.AppEntry, int, error) {
	version, _ := ad.Version(inst)
	excludes := append(append([]string{}, ad.Exclude()...), extraExcludes...)
	skipped := 0

	entry := bundle.AppEntry{
		App:       ad.ID(),
		Instance:  inst.ID,
		Label:     inst.Label,
		Version:   version,
		Path:      path.Join(ad.ID(), inst.ID),
		Portable:  ad.Portability(),
		Checksums: map[string]string{},
	}

	err := filepath.WalkDir(inst.Root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(inst.Root, p)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if len(includeOnly) > 0 && !matchesAny(relSlash, includeOnly) {
			return nil // safe mode: only the whitelisted files
		}
		if matchesAny(relSlash, excludes) {
			return nil
		}
		// Skip sockets/irregular files that can't be meaningfully cloned.
		if de.Type()&(fs.ModeSocket|fs.ModeNamedPipe|fs.ModeDevice) != 0 {
			return nil
		}
		arcRel := path.Join(entry.Path, relSlash)
		sum, size, err := w.AddFile(p, arcRel)
		if err != nil {
			// A file that vanished, is locked by a running app, or is otherwise
			// unreadable is skipped — one bad file must not abort the snapshot.
			skipped++
			return nil
		}
		entry.Checksums[relSlash] = sum
		entry.Bytes += size
		entry.Files++
		return nil
	})
	if err != nil {
		return bundle.AppEntry{}, skipped, err
	}
	entry.Fingerprint = bundle.Fingerprint(entry.Checksums)
	return entry, skipped, nil
}

// matchesAny reports whether rel matches any glob. Patterns ending in "/**"
// match the directory and everything beneath it; otherwise path.Match on the
// full relative path, and a bare basename pattern also matches by basename.
func matchesAny(rel string, globs []string) bool {
	base := path.Base(rel)
	for _, g := range globs {
		if strings.HasSuffix(g, "/**") {
			dir := strings.TrimSuffix(g, "/**")
			if rel == dir || strings.HasPrefix(rel, dir+"/") {
				return true
			}
			continue
		}
		if ok, _ := path.Match(g, rel); ok {
			return true
		}
		if ok, _ := path.Match(g, base); ok {
			return true
		}
	}
	return false
}
