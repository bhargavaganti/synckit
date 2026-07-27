package bundle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRoundTrip proves the core guarantee: a payload written into a bundle
// extracts back byte-for-byte, and checksum verification catches tampering.
func TestRoundTrip(t *testing.T) {
	tmp := t.TempDir()

	// A fake profile with a nested file.
	profile := filepath.Join(tmp, "profile")
	mustWrite(t, filepath.Join(profile, "Preferences"), "hello prefs")
	mustWrite(t, filepath.Join(profile, "sub", "data.bin"), "nested payload")

	bundlePath := filepath.Join(tmp, "b.zip")
	w, err := Create(bundlePath, Metadata{ID: "test", CreatedAt: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	entry := AppEntry{App: "demo", Instance: "Default", Path: "demo/Default", Checksums: map[string]string{}}
	for _, rel := range []string{"Preferences", "sub/data.bin"} {
		sum, size, err := w.AddFile(filepath.Join(profile, filepath.FromSlash(rel)), "demo/Default/"+rel)
		if err != nil {
			t.Fatal(err)
		}
		entry.Checksums[rel] = sum
		entry.Bytes += size
		entry.Files++
	}
	w.SetApps([]AppEntry{entry})
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}

	// Metadata reads back.
	meta, err := ReadMetadata(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Apps) != 1 || meta.Apps[0].Files != 2 {
		t.Fatalf("unexpected metadata: %+v", meta.Apps)
	}

	// Extract to a fresh dir and compare bytes.
	dst := filepath.Join(tmp, "restored")
	if err := ExtractApp(bundlePath, meta.Apps[0], dst); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := mustRead(t, filepath.Join(dst, "Preferences")); got != "hello prefs" {
		t.Fatalf("Preferences mismatch: %q", got)
	}
	if got := mustRead(t, filepath.Join(dst, "sub", "data.bin")); got != "nested payload" {
		t.Fatalf("data.bin mismatch: %q", got)
	}

	// Tamper with a recorded checksum → extraction must fail.
	bad := meta.Apps[0]
	bad.Checksums["Preferences"] = "deadbeef"
	if err := ExtractApp(bundlePath, bad, filepath.Join(tmp, "restored2")); err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
