package s3

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// PresignGet signs a path-style GET of the key with the store's keys, valid for the ttl, without
// a network call.
func TestStore_PresignGet(t *testing.T) {
	t.Parallel()

	s := newPresignStore(t)
	checkPresigned(t, "PresignGet", "orgs/acme/knowledge/handbook/snapshot.zip", s.PresignGet)
}

// PresignPut signs a path-style PUT of the key with the store's keys, valid for the ttl, without
// a network call.
func TestStore_PresignPut(t *testing.T) {
	t.Parallel()

	s := newPresignStore(t)
	checkPresigned(t, "PresignPut", "orgs/acme/cache/repo/go.tar.zst", s.PresignPut)
}

// newPresignStore is a store with fake keys against an endpoint nothing listens on: signing
// makes no call.
func newPresignStore(t *testing.T) *Store {
	t.Helper()

	s, err := New(t.Context(), Config{
		Bucket: "goal-test", Endpoint: "http://127.0.0.1:9000", Region: "auto", AccessKey: "test-access-key", SecretKey: "test-secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// checkPresigned asserts that sign, named name, returns a path-style URL for key on the store's
// endpoint, signed for 15 minutes, and refuses a ttl of 0.
func checkPresigned(t *testing.T, name, key string, sign func(context.Context, string, time.Duration) (string, error)) {
	t.Helper()

	raw, err := sign(t.Context(), key, 15*time.Minute)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%s = an unparsable URL: %v", name, err)
	}

	if u.Host != "127.0.0.1:9000" || u.Path != "/goal-test/"+key {
		t.Errorf("%s = host %q, path %q; want 127.0.0.1:9000 and /goal-test/%s", name, u.Host, u.Path, key)
	}

	q := u.Query()
	if got := q.Get("X-Amz-Expires"); got != "900" {
		t.Errorf("X-Amz-Expires = %q, want 900", got)
	}

	if q.Get("X-Amz-Signature") == "" {
		t.Error("the URL is not signed")
	}

	if _, err := sign(t.Context(), key, 0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("%s with ttl 0 = %v, want ErrInvalid", name, err)
	}
}
