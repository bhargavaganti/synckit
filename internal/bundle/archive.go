package bundle

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// metadataName is the fixed path of the metadata document inside every bundle.
const metadataName = "metadata.json"

// payloadPrefix is the bundle-internal directory holding app payloads.
const payloadPrefix = "payload"

// Writer builds a bundle .zip incrementally: add payload files per app, then
// Finish to flush metadata.json. It computes SHA-256 for each file as it writes.
type Writer struct {
	zw   *zip.Writer
	f    *os.File
	meta Metadata
}

// Create opens dst for writing a new bundle. CreatedAt/Origin/ID are supplied
// by the caller (the core takes no wall clock or host identity of its own).
func Create(dst string, meta Metadata) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	meta.Format = FormatVersion
	return &Writer{zw: zip.NewWriter(f), f: f, meta: meta}, nil
}

// AddFile copies src into the bundle at payload/<appRel> and records its
// checksum against the returned payload-relative path. appRel must be a
// forward-slash path unique within the bundle.
func (w *Writer) AddFile(src, appRel string) (sum string, size int64, err error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()

	arcPath := path.Join(payloadPrefix, appRel)
	hdr, err := w.zw.Create(arcPath)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(hdr, h), in)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// SetApps records the per-app metadata entries (with their checksum maps).
func (w *Writer) SetApps(apps []AppEntry) { w.meta.Apps = apps }

// Finish writes metadata.json and closes the archive.
func (w *Writer) Finish() error {
	mw, err := w.zw.Create(metadataName)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(mw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(w.meta); err != nil {
		return err
	}
	if err := w.zw.Close(); err != nil {
		return err
	}
	return w.f.Close()
}

// Abort closes and removes a partially written bundle.
func (w *Writer) Abort() {
	_ = w.zw.Close()
	name := w.f.Name()
	_ = w.f.Close()
	_ = os.Remove(name)
}

// ReadMetadata opens a bundle and returns just its metadata, without extracting.
func ReadMetadata(src string) (Metadata, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return Metadata{}, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name == metadataName {
			rc, err := zf.Open()
			if err != nil {
				return Metadata{}, err
			}
			defer rc.Close()
			var m Metadata
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				return Metadata{}, err
			}
			return m, nil
		}
	}
	return Metadata{}, errors.New("bundle: metadata.json not found")
}

// ExtractApp writes one app's payload tree to dstRoot, verifying every file's
// checksum against the metadata as it goes. dstRoot receives the files that
// lived under entry.Path inside the bundle, so dstRoot is the profile root.
func ExtractApp(src string, entry AppEntry, dstRoot string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()

	// Bundle-internal prefix for this app's files, e.g. "payload/chrome/Default/".
	prefix := path.Join(payloadPrefix, entry.Path) + "/"
	seen := 0
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() || !strings.HasPrefix(zf.Name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(zf.Name, prefix) // path relative to profile root
		want, ok := entry.Checksums[rel]
		if !ok {
			return fmt.Errorf("bundle: %s missing from metadata checksums", rel)
		}
		if err := extractOne(zf, filepath.Join(dstRoot, filepath.FromSlash(rel)), want); err != nil {
			return err
		}
		seen++
	}
	if seen != len(entry.Checksums) {
		return fmt.Errorf("bundle: %s payload incomplete (%d of %d files)", entry.App, seen, len(entry.Checksums))
	}
	return nil
}

func extractOne(zf *zip.File, dst, wantSum string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), rc); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		return fmt.Errorf("bundle: checksum mismatch for %s (want %s, got %s)", dst, wantSum, got)
	}
	return nil
}
