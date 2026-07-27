package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bhargav/synckit/internal/vault"
)

// TestEncryptedRoundTrip proves the encryption guarantee: with a vault active,
// the bundle on disk is age-encrypted, the plaintext secret never appears in
// the file, it decrypts back byte-for-byte, and reads fail without the key.
func TestEncryptedRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	const secret = "top-secret-password-12345"

	keyPath := filepath.Join(tmp, "id.key")
	if _, err := vault.Init(keyPath); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Load(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	UseVault(v)
	defer UseVault(nil)

	profile := filepath.Join(tmp, "profile")
	mustWrite(t, filepath.Join(profile, "Login Data"), secret)

	bundlePath := filepath.Join(tmp, "b.zip")
	w, err := Create(bundlePath, Metadata{ID: "enc", CreatedAt: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	entry := AppEntry{App: "demo", Instance: "Default", Path: "demo/Default", Checksums: map[string]string{}}
	sum, size, err := w.AddFile(filepath.Join(profile, "Login Data"), "demo/Default/Login Data")
	if err != nil {
		t.Fatal(err)
	}
	entry.Checksums["Login Data"] = sum
	entry.Bytes, entry.Files = size, 1
	w.SetApps([]AppEntry{entry})
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}

	// On disk: encrypted, and the secret is nowhere in the ciphertext.
	if !isEncryptedFile(bundlePath) {
		t.Fatal("bundle is not age-encrypted")
	}
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("plaintext secret leaked into the encrypted bundle")
	}

	// With the key: metadata + payload decrypt correctly.
	meta, err := ReadMetadata(bundlePath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if len(meta.Apps) != 1 {
		t.Fatalf("unexpected apps: %+v", meta.Apps)
	}
	dst := filepath.Join(tmp, "restored")
	if err := ExtractApp(bundlePath, meta.Apps[0], dst); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := mustRead(t, filepath.Join(dst, "Login Data")); got != secret {
		t.Fatalf("decrypted mismatch: %q", got)
	}

	// Without the key: reads must fail rather than expose anything.
	UseVault(nil)
	if _, err := ReadMetadata(bundlePath); err == nil {
		t.Fatal("expected error reading encrypted bundle without a key")
	}
}
