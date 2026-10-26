package masterkey_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/adapters/masterkey/masterkey"
	"github.com/byFrederick/goal/internal/core/ports"
)

// secret is a key of 32 bytes made of b.
func secret(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func entry(id string, b byte) string { return id + ":" + base64.StdEncoding.EncodeToString(secret(b)) }

func env(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }

func TestKeys_SealOpen(t *testing.T) {
	t.Parallel()

	k, err := masterkey.New(masterkey.Key{ID: "k1", Secret: secret(1)})
	if err != nil {
		t.Fatal(err)
	}

	plain, aad := []byte(`{"token":"ghp_x"}`), []byte("goal/connection/v1|org|conn|k1")

	id, nonce, ct, err := k.Seal(plain, aad)
	if err != nil || id != "k1" || len(nonce) != 12 || bytes.Contains(ct, plain) {
		t.Fatalf("Seal = %q, %x, %x, %v; want k1, a 12-byte nonce and a ciphertext without the plaintext", id, nonce, ct, err)
	}

	if got, err := k.Open(id, nonce, ct, aad); err != nil || !bytes.Equal(got, plain) {
		t.Errorf("Open = %q, %v; want the plaintext", got, err)
	}

	if _, again, _, _ := k.Seal(plain, aad); bytes.Equal(again, nonce) {
		t.Error("two seals used the same nonce")
	}

	if _, err := k.Open(id, nonce, ct, []byte("goal/connection/v1|other|conn|k1")); err == nil {
		t.Error("Open with another aad succeeded")
	}

	tampered := bytes.Clone(ct)
	tampered[0] ^= 1

	if _, err := k.Open(id, nonce, tampered, aad); err == nil {
		t.Error("Open of a tampered ciphertext succeeded")
	}

	if _, err := k.Open("k2", nonce, ct, aad); err == nil || !strings.Contains(err.Error(), `"k2"`) {
		t.Errorf("Open with an unknown key id = %v; want an error naming it", err)
	}
}

// Rotation: a new key in front seals, the old one still opens what it sealed.
func TestKeys_Rotation(t *testing.T) {
	t.Parallel()

	old, err := masterkey.New(masterkey.Key{ID: "k1", Secret: secret(1)})
	if err != nil {
		t.Fatal(err)
	}

	id, nonce, ct, err := old.Seal([]byte("v"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := masterkey.New(masterkey.Key{ID: "k2", Secret: secret(2)}, masterkey.Key{ID: "k1", Secret: secret(1)})
	if err != nil {
		t.Fatal(err)
	}

	if got, err := rotated.Open(id, nonce, ct, []byte("aad")); err != nil || string(got) != "v" {
		t.Errorf("Open under the old key after rotation = %q, %v", got, err)
	}

	if rotated.Active() != "k2" {
		t.Errorf("Active = %q; want k2", rotated.Active())
	}

	if id, _, _, _ := rotated.Seal([]byte("v"), nil); id != "k2" {
		t.Errorf("Seal after rotation used %q; want k2", id)
	}
}

// Without keys nothing seals or opens, and goald says why.
func TestKeys_None(t *testing.T) {
	t.Parallel()

	k, err := masterkey.Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := k.Seal([]byte("v"), nil); !errors.Is(err, ports.ErrNoMasterKey) {
		t.Errorf("Seal = %v; want ErrNoMasterKey", err)
	}

	if _, err := k.Open("k1", make([]byte, 12), []byte("x"), nil); !errors.Is(err, ports.ErrNoMasterKey) {
		t.Errorf("Open = %v; want ErrNoMasterKey", err)
	}

	if k.Active() != "" || len(k.IDs()) != 0 {
		t.Errorf("Active %q, IDs %v; want none", k.Active(), k.IDs())
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}

		return p
	}

	good := file("keys", "# rotated 2026-10\n"+entry("k2", 2)+"\n\n"+entry("k1", 1)+"\n", 0o600)
	open := file("open", entry("k1", 1), 0o644)

	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}

	leak := base64.StdEncoding.EncodeToString(secret(7)[:31])

	tests := []struct {
		name string
		vars map[string]string
		ids  string // the ids, comma-separated, when it loads
		want string // the error's text otherwise
	}{
		{name: "inline", vars: map[string]string{masterkey.KeysEnv: entry("k2", 2) + ", " + entry("k1", 1)}, ids: "k2,k1"},
		{name: "file", vars: map[string]string{masterkey.FileEnv: good}, ids: "k2,k1"},
		{name: "both", vars: map[string]string{masterkey.KeysEnv: entry("k1", 1), masterkey.FileEnv: good}, want: "not both"},
		{name: "not id:key", vars: map[string]string{masterkey.KeysEnv: "k1"}, want: "<id>:<base64"},
		{name: "not base64", vars: map[string]string{masterkey.KeysEnv: "k1:%%%"}, want: "not base64"},
		{name: "short key", vars: map[string]string{masterkey.KeysEnv: "k1:" + leak}, want: "has 31 bytes"},
		{name: "id twice", vars: map[string]string{masterkey.KeysEnv: entry("k1", 1) + "," + entry("k1", 2)}, want: "twice"},
		{name: "bad id", vars: map[string]string{masterkey.KeysEnv: entry("k|1", 1)}, want: "master key id"},
		{name: "file others may read", vars: map[string]string{masterkey.FileEnv: open}, want: "mode 0644"},
		{name: "file through a symlink", vars: map[string]string{masterkey.FileEnv: link}, want: "symlink"},
		{name: "missing file", vars: map[string]string{masterkey.FileEnv: filepath.Join(dir, "none")}, want: "no such file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			k, err := masterkey.Load(env(tt.vars))

			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("Load = %v", err)
			case tt.want == "":
				if got := strings.Join(k.IDs(), ","); got != tt.ids {
					t.Errorf("IDs = %s; want %s", got, tt.ids)
				}
			case err == nil || !strings.Contains(err.Error(), tt.want):
				t.Errorf("Load = %v; want an error mentioning %q", err, tt.want)
			case strings.Contains(err.Error(), leak):
				t.Errorf("Load's error %q holds key material", err)
			}
		})
	}
}
