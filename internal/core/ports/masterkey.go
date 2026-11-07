package ports

import "errors"

// ErrNoMasterKey means goald has no master key, so no secret can be sealed or opened: secrets and
// credentials are unavailable until one is configured (spec 4 §5.2).
var ErrNoMasterKey = errors.New("connections are unavailable: no master key is configured")

// MasterKey seals and opens small secrets with the platform's master key (AES-256-GCM).
type MasterKey interface {
	// Seal encrypts with the active key and returns its id. Without a key it fails with
	// ErrNoMasterKey.
	Seal(plaintext, aad []byte) (keyID string, nonce, ciphertext []byte, err error)
	// Open decrypts with the named key; an unknown key id or a wrong aad is an error.
	Open(keyID string, nonce, ciphertext, aad []byte) ([]byte, error)
	// Active is the id of the key Seal uses; "" without one.
	Active() string
}
