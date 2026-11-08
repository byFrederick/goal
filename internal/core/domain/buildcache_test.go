package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A build cache's key is 64 lowercase hex characters: a SHA-256 of its inputs.
func TestValidBuildCacheKey(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]bool{
		strings.Repeat("0a", 32):             true,
		strings.Repeat("0A", 32):             false,
		strings.Repeat("0a", 31):             false,
		strings.Repeat("0a", 32) + "0":       false,
		strings.Repeat("g", 64):              false,
		"":                                   false,
		strings.Repeat("0a", 31) + "0\n":     false,
		" " + strings.Repeat("0a", 31) + "a": false,
	} {
		if got := ValidBuildCacheKey(key); got != want {
			t.Errorf("ValidBuildCacheKey(%q) = %t; want %t", key, got, want)
		}
	}
}

// A new build cache needs a repository key, a valid cache key, an object key and a time.
func TestNewBuildCache_Check(t *testing.T) {
	t.Parallel()

	ok := NewBuildCache{
		RepositoryKey: "r1", CacheKey: strings.Repeat("ab", 32), ObjectKey: "orgs/o/build-caches/r1/ab",
		At: time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC),
	}

	if err := ok.Check(); err != nil {
		t.Fatalf("Check() = %v; want nil", err)
	}

	bad := map[string]func(n *NewBuildCache){
		"no repository": func(n *NewBuildCache) { n.RepositoryKey = "" },
		"short key":     func(n *NewBuildCache) { n.CacheKey = "ab" },
		"no object":     func(n *NewBuildCache) { n.ObjectKey = "" },
		"no time":       func(n *NewBuildCache) { n.At = time.Time{} },
	}

	for name, change := range bad {
		n := ok
		change(&n)

		if err := n.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check() = %v; want ErrInvalid", name, err)
		}
	}
}

// A reservation goes stale an hour after it was made, and not at the hour itself.
func TestBuildCacheStale(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)

	if BuildCacheStale(at.Add(-BuildCacheStaleAfter), at) {
		t.Error("a reservation made exactly an hour ago is stale; want fresh")
	}

	if !BuildCacheStale(at.Add(-BuildCacheStaleAfter-time.Second), at) {
		t.Error("a reservation made over an hour ago is fresh; want stale")
	}
}
