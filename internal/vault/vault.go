// Package vault provides encryption at rest (and in transit) for synckit
// bundles, using age (https://age-encryption.org) — a small, audited, modern
// encryption format with streaming support.
//
// Model: one shared "synckit key" (an age X25519 identity) lives on every
// machine at ~/.synckit/identity.key. Bundles are encrypted to that identity's
// recipient, so any machine holding the key can decrypt any machine's bundles.
// Set the key up once (`synckit key init`) and copy it to your other machines
// (`synckit key export` / `import`). Without a key, synckit falls back to
// plaintext bundles and warns loudly.
package vault

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// ErrNoKey is returned by Load when no identity file exists.
var ErrNoKey = errors.New("no synckit key configured")

// Vault holds the shared identity used to encrypt and decrypt bundles.
type Vault struct {
	id *age.X25519Identity
}

// DefaultPath is ~/.synckit/identity.key.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".synckit", "identity.key")
	}
	return filepath.Join(home, ".synckit", "identity.key")
}

// Load reads the identity at path. Returns ErrNoKey if the file is absent.
func Load(path string) (*Vault, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoKey
		}
		return nil, err
	}
	line := keyLine(string(b))
	if line == "" {
		return nil, fmt.Errorf("vault: no AGE-SECRET-KEY found in %s", path)
	}
	id, err := age.ParseX25519Identity(line)
	if err != nil {
		return nil, fmt.Errorf("vault: parse identity: %w", err)
	}
	return &Vault{id: id}, nil
}

// Init generates a new identity and writes it to path with 0600 perms. It
// refuses to overwrite an existing key. Returns the public recipient string.
func Init(path string) (recipient string, err error) {
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("vault: key already exists at %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	content := fmt.Sprintf(
		"# synckit encryption key — keep secret, copy to your other machines.\n"+
			"# public recipient: %s\n%s\n", id.Recipient(), id)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return id.Recipient().String(), nil
}

// Recipient returns the public recipient string for this vault's identity.
func (v *Vault) Recipient() string { return v.id.Recipient().String() }

// EncryptWriter wraps dst so everything written to the returned WriteCloser is
// age-encrypted. The caller MUST Close it to flush the stream.
func (v *Vault) EncryptWriter(dst io.Writer) (io.WriteCloser, error) {
	return age.Encrypt(dst, v.id.Recipient())
}

// DecryptReader wraps src so reads from the returned Reader are plaintext.
func (v *Vault) DecryptReader(src io.Reader) (io.Reader, error) {
	return age.Decrypt(src, v.id)
}

// ageHeader is the magic prefix of an age v1 file.
const ageHeader = "age-encryption.org/v1"

// IsEncrypted reports whether the file at path is an age-encrypted bundle.
func IsEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(ageHeader))
	n, _ := io.ReadFull(f, buf)
	return string(buf[:n]) == ageHeader
}

// keyLine extracts the AGE-SECRET-KEY line from a key file, ignoring comments.
func keyLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "AGE-SECRET-KEY-") {
			return ln
		}
	}
	return ""
}
