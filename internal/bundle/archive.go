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
	"sort"
	"strings"
)

// metadataName is the fixed path of the metadata document inside every bundle.
const metadataName = "metadata.json"

// payloadPrefix is the bundle-internal directory holding app payloads.
const payloadPrefix = "payload"

// Vaulter encrypts and decrypts bundle streams. It is satisfied by
// *vault.Vault; kept as an interface so this package stays crypto-agnostic.
type Vaulter interface {
	EncryptWriter(dst io.Writer) (io.WriteCloser, error)
	DecryptReader(src io.Reader) (io.Reader, error)
}

// activeVault, when set via UseVault, transparently encrypts bundles on Create
// and decrypts them on read. Nil => plaintext bundles (backward compatible).
var activeVault Vaulter

// UseVault enables encryption at rest/in transit for all subsequent bundle I/O.
func UseVault(v Vaulter) { activeVault = v }

// Encrypted reports whether bundle encryption is currently enabled.
func Encrypted() bool { return activeVault != nil }

// Fingerprint reduces a file->checksum map to one stable content hash. Two
// profiles with identical files (regardless of walk order) hash the same.
func Fingerprint(checksums map[string]string) string {
	paths := make([]string, 0, len(checksums))
	for p := range checksums {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write([]byte(checksums[p]))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ageMagic is the header of an age-encrypted stream.
const ageMagic = "age-encryption.org/v1"

// Writer builds a bundle .zip incrementally: add payload files per app, then
// Finish to flush metadata.json. It computes SHA-256 for each file as it writes.
// When a vault is active, the whole zip stream is age-encrypted to the file.
type Writer struct {
	zw   *zip.Writer
	f    *os.File
	enc  io.WriteCloser // non-nil when encrypting; wraps f
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

	var sink io.Writer = f
	var enc io.WriteCloser
	if activeVault != nil {
		enc, err = activeVault.EncryptWriter(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		sink = enc
	}
	return &Writer{zw: zip.NewWriter(sink), f: f, enc: enc, meta: meta}, nil
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
	if w.enc != nil {
		if err := w.enc.Close(); err != nil { // flush the age stream
			return err
		}
	}
	return w.f.Close()
}

// Abort closes and removes a partially written bundle.
func (w *Writer) Abort() {
	_ = w.zw.Close()
	if w.enc != nil {
		_ = w.enc.Close()
	}
	name := w.f.Name()
	_ = w.f.Close()
	_ = os.Remove(name)
}

// openZip opens a bundle for reading, decrypting to a temp file first when the
// bundle is encrypted. The returned cleanup MUST be called by the caller.
func openZip(path string) (*zip.Reader, func(), error) {
	if isEncryptedFile(path) {
		if activeVault == nil {
			return nil, nil, errors.New("bundle is encrypted but no synckit key is configured (run `synckit key init` / import the key)")
		}
		in, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		defer in.Close()
		r, err := activeVault.DecryptReader(in)
		if err != nil {
			return nil, nil, fmt.Errorf("decrypt bundle: %w", err)
		}
		tmp, err := os.CreateTemp("", "synckit-dec-*.zip")
		if err != nil {
			return nil, nil, err
		}
		if _, err := io.Copy(tmp, r); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, nil, err
		}
		fi, err := tmp.Stat()
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, nil, err
		}
		zr, err := zip.NewReader(tmp, fi.Size())
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, nil, err
		}
		return zr, func() { tmp.Close(); os.Remove(tmp.Name()) }, nil
	}
	zrc, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	return &zrc.Reader, func() { zrc.Close() }, nil
}

// isEncryptedFile reports whether path begins with the age magic header.
func isEncryptedFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(ageMagic))
	n, _ := io.ReadFull(f, buf)
	return string(buf[:n]) == ageMagic
}

// ReadMetadata opens a bundle and returns just its metadata, without extracting.
func ReadMetadata(src string) (Metadata, error) {
	zr, cleanup, err := openZip(src)
	if err != nil {
		return Metadata{}, err
	}
	defer cleanup()
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
	return ExtractAppProgress(src, entry, dstRoot, nil)
}

// ExtractAppProgress is ExtractApp with a callback invoked with the number of
// bytes written as extraction proceeds (delta per call), for progress bars.
func ExtractAppProgress(src string, entry AppEntry, dstRoot string, onBytes func(delta int64)) error {
	zr, cleanup, err := openZip(src)
	if err != nil {
		return err
	}
	defer cleanup()

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
		if err := extractOne(zf, filepath.Join(dstRoot, filepath.FromSlash(rel)), want, onBytes); err != nil {
			return err
		}
		seen++
	}
	if seen != len(entry.Checksums) {
		return fmt.Errorf("bundle: %s payload incomplete (%d of %d files)", entry.App, seen, len(entry.Checksums))
	}
	return nil
}

// countWriter reports bytes written (delta) to a callback.
type countWriter struct{ cb func(int64) }

func (c countWriter) Write(b []byte) (int, error) { c.cb(int64(len(b))); return len(b), nil }

func extractOne(zf *zip.File, dst, wantSum string, onBytes func(delta int64)) error {
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
	var w io.Writer = io.MultiWriter(out, h)
	if onBytes != nil {
		w = io.MultiWriter(out, h, countWriter{onBytes})
	}
	if _, err := io.Copy(w, rc); err != nil {
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
