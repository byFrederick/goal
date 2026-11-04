package s3

import (
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

	s, err := New(t.Context(), Config{
		Bucket: "goal-test", Endpoint: "http://127.0.0.1:9000", Region: "auto", AccessKey: "test-access-key", SecretKey: "test-secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	const key = "orgs/acme/knowledge/handbook/snapshot.zip"

	raw, err := s.PresignGet(t.Context(), key, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("PresignGet = an unparsable URL: %v", err)
	}

	if u.Host != "127.0.0.1:9000" || u.Path != "/goal-test/"+key {
		t.Errorf("PresignGet = host %q, path %q; want 127.0.0.1:9000 and /goal-test/%s", u.Host, u.Path, key)
	}

	q := u.Query()
	if got := q.Get("X-Amz-Expires"); got != "900" {
		t.Errorf("X-Amz-Expires = %q, want 900", got)
	}

	if q.Get("X-Amz-Signature") == "" {
		t.Error("the URL is not signed")
	}

	if _, err := s.PresignGet(t.Context(), key, 0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("PresignGet with ttl 0 = %v, want ErrInvalid", err)
	}
}
