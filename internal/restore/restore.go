// Package restore applies a bundle onto the current machine, backing up each
// target profile before overwriting and surfacing portability/skew warnings.
package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bhargav/synckit/internal/app"
	"github.com/bhargav/synckit/internal/bundle"
)

// Options controls a restore run.
type Options struct {
	Src      string          // bundle path
	Selected map[string]bool // adapter id -> include; nil means all in bundle
	Force    bool            // proceed even if the target app is running
	DryRun   bool            // report what would happen, change nothing
	BackupTag string         // suffix for the .bak dir, e.g. a timestamp
}

// AppOutcome is the per-app result of a restore.
type AppOutcome struct {
	App      string
	Instance string
	Target   string   // resolved destination root
	Backup   string   // where the previous profile was moved, if any
	Restored bool
	Warnings []string
	Skipped  string   // non-empty reason if this entry was skipped
}

// Result aggregates outcomes and top-level warnings.
type Result struct {
	Outcomes []AppOutcome
}

// Run restores each selected app entry from the bundle.
func Run(adapters []app.Adapter, opts Options) (*Result, error) {
	meta, err := bundle.ReadMetadata(opts.Src)
	if err != nil {
		return nil, err
	}

	res := &Result{}
	hostChanged := meta.Origin.OS != runtime.GOOS ||
		meta.Origin.Hostname != hostname() ||
		meta.Origin.User != username()

	for _, entry := range meta.Apps {
		if opts.Selected != nil && !opts.Selected[entry.App] {
			continue
		}
		out := AppOutcome{App: entry.App, Instance: entry.Instance}

		ad := findAdapter(adapters, entry.App)
		if ad == nil {
			out.Skipped = "no adapter for app in this build"
			res.Outcomes = append(res.Outcomes, out)
			continue
		}

		// Portability warning: secrets that won't survive a host change.
		if hostChanged && !entry.Portable.SecretsCrossMachine {
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("restoring across machines: %s", entry.Portable.Note))
		}

		target, err := resolveTarget(ad, entry)
		if err != nil {
			out.Skipped = err.Error()
			res.Outcomes = append(res.Outcomes, out)
			continue
		}
		out.Target = target

		// Preflight: refuse to overwrite a profile whose app is running.
		if inst, ok := currentInstance(ad, entry.Instance); ok {
			if running, _ := ad.Running(inst); running && !opts.Force {
				out.Skipped = "target app is running (close it or use --force)"
				res.Outcomes = append(res.Outcomes, out)
				continue
			}
		}

		if opts.DryRun {
			out.Warnings = append(out.Warnings, "dry-run: no changes written")
			res.Outcomes = append(res.Outcomes, out)
			continue
		}

		// Back up the existing profile so a bad restore is reversible.
		if _, err := os.Stat(target); err == nil {
			bak := target + ".bak"
			if opts.BackupTag != "" {
				bak = target + "." + opts.BackupTag + ".bak"
			}
			if err := os.Rename(target, bak); err != nil {
				out.Skipped = fmt.Sprintf("backup failed: %v", err)
				res.Outcomes = append(res.Outcomes, out)
				continue
			}
			out.Backup = bak
		}

		if err := os.MkdirAll(target, 0o755); err != nil {
			out.Skipped = fmt.Sprintf("mkdir target: %v", err)
			res.Outcomes = append(res.Outcomes, out)
			continue
		}
		if err := bundle.ExtractApp(opts.Src, entry, target); err != nil {
			// Best-effort rollback: drop the partial restore, put the backup back.
			_ = os.RemoveAll(target)
			if out.Backup != "" {
				_ = os.Rename(out.Backup, target)
				out.Backup = ""
			}
			out.Skipped = fmt.Sprintf("extract failed (rolled back): %v", err)
			res.Outcomes = append(res.Outcomes, out)
			continue
		}
		out.Restored = true
		res.Outcomes = append(res.Outcomes, out)
	}
	return res, nil
}

// resolveTarget finds where entry should land on this machine. If a matching
// instance already exists we overwrite it in place; otherwise we place it
// alongside detected siblings using the bundle's instance id.
func resolveTarget(ad app.Adapter, entry bundle.AppEntry) (string, error) {
	insts, err := ad.Detect()
	if err != nil {
		return "", err
	}
	for _, inst := range insts {
		if inst.ID == entry.Instance {
			return inst.Root, nil
		}
	}
	// New instance: derive its root from a sibling's parent directory.
	if len(insts) > 0 {
		return filepath.Join(filepath.Dir(insts[0].Root), entry.Instance), nil
	}
	return "", fmt.Errorf("app not installed here; cannot place %q", entry.Instance)
}

func currentInstance(ad app.Adapter, id string) (app.Instance, bool) {
	insts, err := ad.Detect()
	if err != nil {
		return app.Instance{}, false
	}
	for _, inst := range insts {
		if inst.ID == id {
			return inst, true
		}
	}
	return app.Instance{}, false
}

func findAdapter(adapters []app.Adapter, id string) app.Adapter {
	for _, a := range adapters {
		if a.ID() == id {
			return a
		}
	}
	return nil
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func username() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return strings.TrimSpace(os.Getenv("LOGNAME"))
}
