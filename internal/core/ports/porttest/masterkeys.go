package porttest

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/core/ports"
)

// MasterKeys stand in for the platform's master keys without cryptography: Seal returns random
// bytes and remembers what they seal, Open gives it back only for the same key id and associated
// data, as AES-GCM would. Keys made by one MasterKeys share what they sealed.
type MasterKeys struct {
	mu     sync.Mutex
	sealed map[string]sealedValue // by nonce and ciphertext
}

type sealedValue struct {
	keyID      string
	aad, plain []byte
}

// NewMasterKeys returns master keys that sealed nothing yet.
func NewMasterKeys() *MasterKeys { return &MasterKeys{sealed: map[string]sealedValue{}} }

// Key returns the keys ids, the first active; with none, Seal fails with ports.ErrNoMasterKey.
func (m *MasterKeys) Key(ids ...string) ports.MasterKey {
	return &fakeKey{m: m, ids: slices.Clone(ids)}
}

type fakeKey struct {
	m   *MasterKeys
	ids []string
}

func (k *fakeKey) Active() string {
	if len(k.ids) == 0 {
		return ""
	}

	return k.ids[0]
}

func (k *fakeKey) Seal(plaintext, aad []byte) (keyID string, nonce, ciphertext []byte, err error) {
	if len(k.ids) == 0 {
		return "", nil, nil, ports.ErrNoMasterKey
	}

	nonce, ciphertext = make([]byte, 12), make([]byte, 16+len(plaintext))
	_, _ = rand.Read(nonce)
	_, _ = rand.Read(ciphertext)

	k.m.mu.Lock()
	defer k.m.mu.Unlock()

	k.m.sealed[string(nonce)+string(ciphertext)] = sealedValue{keyID: k.ids[0], aad: bytes.Clone(aad), plain: bytes.Clone(plaintext)}

	return k.ids[0], nonce, ciphertext, nil
}

func (k *fakeKey) Open(keyID string, nonce, ciphertext, aad []byte) ([]byte, error) {
	if !slices.Contains(k.ids, keyID) {
		if len(k.ids) == 0 {
			return nil, ports.ErrNoMasterKey
		}

		return nil, fmt.Errorf("master key %q is not configured", keyID)
	}

	k.m.mu.Lock()
	defer k.m.mu.Unlock()

	v, ok := k.m.sealed[string(nonce)+string(ciphertext)]
	if !ok || v.keyID != keyID || !bytes.Equal(v.aad, aad) {
		return nil, errors.New("the sealed value does not open")
	}

	return bytes.Clone(v.plain), nil
}
