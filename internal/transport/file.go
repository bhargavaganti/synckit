package transport

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bhargav/synckit/internal/bundle"
)

// File is the export/import transport: bundles are plain .zip files living in a
// directory (a synced folder, USB drive, or anywhere on disk).
type File struct {
	Dir string // directory holding bundle .zip files
}

func NewFile(dir string) *File { return &File{Dir: dir} }

func (f *File) Name() string { return "file" }

func (f *File) Put(srcPath string) (Ref, error) {
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return Ref{}, err
	}
	dst := filepath.Join(f.Dir, filepath.Base(srcPath))
	if err := copyFile(srcPath, dst); err != nil {
		return Ref{}, err
	}
	meta, _ := bundle.ReadMetadata(dst)
	return Ref{ID: meta.ID, Location: dst, Meta: meta}, nil
}

func (f *File) Get(ref Ref, dstPath string) error {
	return copyFile(ref.Location, dstPath)
}

func (f *File) List() ([]Ref, error) {
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var refs []Ref
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		p := filepath.Join(f.Dir, e.Name())
		meta, err := bundle.ReadMetadata(p)
		if err != nil {
			continue // not a synckit bundle
		}
		refs = append(refs, Ref{ID: meta.ID, Location: p, Meta: meta})
	}
	return refs, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
