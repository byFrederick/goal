// Package masterkey implements ports.MasterKey with AES-256-GCM under the platform's master keys
// (spec 4 §5.2), read from GOAL_MASTER_KEYS or the file GOAL_MASTER_KEY_FILE: "<id>:<base64 of
// 32 bytes>" entries, the first one active. Older keys stay listed until `goal db rekey` has
// resealed every connection under the active one, so a key rotates without downtime. On
// Cloudflare the control Worker passes a Secrets Store value as GOAL_MASTER_KEYS. A KMS adapter
// can implement the same port later.
package masterkey

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/ports"
)

// The variables that hold the master keys: the keys themselves, or a file of them.
const (
	KeysEnv = "GOAL_MASTER_KEYS"
	FileEnv = "GOAL_MASTER_KEY_FILE"
)

// keyBytes is an AES-256 key's length; nonceBytes GCM's standard nonce.
const (
	keyBytes   = 32
	nonceBytes = 12
)

// keyIDPattern is a key id: short, and free of the associated data's separator.
var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var _ ports.MasterKey = (*Keys)(nil)

// Keys are the master keys, the first one active. The zero value has none: Seal fails with
// ports.ErrNoMasterKey and Open with an unknown key id. Keys is safe for concurrent use.
type Keys struct {
	ids   []string
	aeads map[string]cipher.AEAD
}

// Key is one master key.
type Key struct {
	ID     string
	Secret []byte
}

// New returns keys, the first one active. Each id is unique and of at most 64 letters, digits,
// dots, dashes or underscores; each secret 32 bytes.
func New(keys ...Key) (*Keys, error) {
	k := &Keys{aeads: make(map[string]cipher.AEAD, len(keys))}

	for _, key := range keys {
		if !keyIDPattern.MatchString(key.ID) {
			return nil, fmt.Errorf("master key id %q: up to 64 letters, digits, dots, dashes or underscores", key.ID)
		}

		if _, dup := k.aeads[key.ID]; dup {
			return nil, fmt.Errorf("master key id %q is listed twice", key.ID)
		}

		if len(key.Secret) != keyBytes {
			return nil, fmt.Errorf("master key %q has %d bytes; want %d", key.ID, len(key.Secret), keyBytes)
		}

		block, err := aes.NewCipher(key.Secret)
		if err != nil {
			return nil, fmt.Errorf("master key %q: %w", key.ID, err)
		}

		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("master key %q: %w", key.ID, err)
		}

		k.ids = append(k.ids, key.ID)
		k.aeads[key.ID] = aead
	}

	return k, nil
}

// Load reads the master keys from GOAL_MASTER_KEYS (comma-separated) or the file named by
// GOAL_MASTER_KEY_FILE (one per line; blank lines and # comments ignored), never both. The file
// must be a regular file, not a symlink, readable by its owner only (mode 0600 or 0400). Neither
// set is no keys, not an error. Errors name ids and settings, never key material.
func Load(getenv func(string) string) (*Keys, error) {
	inline, file := strings.TrimSpace(getenv(KeysEnv)), strings.TrimSpace(getenv(FileEnv))

	switch {
	case inline != "" && file != "":
		return nil, fmt.Errorf("set %s or %s, not both", KeysEnv, FileEnv)
	case inline != "":
		return parse(KeysEnv, strings.Split(inline, ","))
	case file != "":
		lines, err := readFile(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", FileEnv, err)
		}

		return parse(FileEnv, lines)
	default:
		return &Keys{}, nil
	}
}

// readFile reads the key file's lines, refusing a symlink, anything but a regular file, and a
// mode that lets anyone but its owner read or write it.
func readFile(path string) ([]string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, fmt.Errorf("%s is a symlink; name the file itself", path)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("%s is not a regular file", path)
	case info.Mode().Perm()&0o077 != 0:
		return nil, fmt.Errorf("%s has mode %04o; want 0600 (chmod 600 %s)", path, info.Mode().Perm(), path)
	}

	b, err := os.ReadFile(path) //nolint:gosec // the operator's own key file, checked above
	if err != nil {
		return nil, err
	}

	var lines []string

	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}

	return lines, sc.Err()
}

// parse reads "<id>:<base64>" entries from setting.
func parse(setting string, entries []string) (*Keys, error) {
	keys := make([]Key, 0, len(entries))

	for i, e := range entries {
		id, b64, ok := strings.Cut(strings.TrimSpace(e), ":")
		if !ok || id == "" {
			return nil, fmt.Errorf("%s: entry %d is not <id>:<base64 of 32 bytes>", setting, i+1)
		}

		secret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return nil, fmt.Errorf("%s: key %q is not base64 (openssl rand -base64 32 makes one)", setting, id)
		}

		keys = append(keys, Key{ID: id, Secret: secret})
	}

	k, err := New(keys...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", setting, err)
	}

	return k, nil
}

// IDs are the key ids, the active one first.
func (k *Keys) IDs() []string { return slices.Clone(k.ids) }

// Active implements ports.MasterKey.
func (k *Keys) Active() string {
	if len(k.ids) == 0 {
		return ""
	}

	return k.ids[0]
}

// Seal implements ports.MasterKey with a fresh random nonce per call.
func (k *Keys) Seal(plaintext, aad []byte) (keyID string, nonce, ciphertext []byte, err error) {
	id := k.Active()
	if id == "" {
		return "", nil, nil, ports.ErrNoMasterKey
	}

	nonce = make([]byte, nonceBytes)
	_, _ = rand.Read(nonce) // crypto/rand.Read never fails (Go 1.24)

	return id, nonce, k.aeads[id].Seal(nil, nonce, plaintext, aad), nil
}

// errOpen is every failure to open: a wrong key, aad or ciphertext look the same.
var errOpen = errors.New("master key: the sealed value does not open (wrong key, associated data or ciphertext)")

// Open implements ports.MasterKey.
func (k *Keys) Open(keyID string, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, ok := k.aeads[keyID]
	if !ok {
		if len(k.ids) == 0 {
			return nil, ports.ErrNoMasterKey
		}

		return nil, fmt.Errorf("master key %q is not configured (%s has %s)", keyID, KeysEnv, strings.Join(k.ids, ", "))
	}

	if len(nonce) != nonceBytes {
		return nil, errOpen
	}

	out, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, errOpen
	}

	return out, nil
}
