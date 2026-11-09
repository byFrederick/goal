package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// buildCachesTests are the BuildCaches cases (ADR 0045), registered by Run under BuildCaches_.
var buildCachesTests = []suiteCase{
	{"BuildCaches_FindHitSetsUsedAt", testBuildCachesFindHit},
	{"BuildCaches_FindSavingNotFound", testBuildCachesFindSaving},
	{"BuildCaches_ConcurrentReservesOneWins", testBuildCachesConcurrentReserve},
	{"BuildCaches_StaleReservationTakenOver", testBuildCachesStaleTakeover},
	{"BuildCaches_ReadyNeverReservedAgain", testBuildCachesReadyNotReserved},
	{"BuildCaches_CommitOnlyTheSavingReservation", testBuildCachesCommit},
	{"BuildCaches_PruneKeepsTheNewestReady", testBuildCachesPrune},
	{"BuildCaches_OtherOrgNeverSeenOrPruned", testBuildCachesTenancy},
}

const cacheRepo = "repo-1"

// cacheKey is the n-th well-formed cache key.
func cacheKey(n int) string { return fmt.Sprintf("%064x", n) }

// reserveCache reserves key of repo with object obj at, in ctx, and reports whether it got it.
func (f *fixture) reserveCache(ctx context.Context, t *testing.T, repo, key, obj string, at time.Time) bool {
	t.Helper()

	got, err := f.BuildCaches.ReserveBuildCache(ctx, domain.NewBuildCache{RepositoryKey: repo, CacheKey: key, ObjectKey: obj, At: at})
	if err != nil {
		t.Fatalf("ReserveBuildCache(%s): %v", key, err)
	}

	return got
}

// readyCache reserves key of repo at and commits it a minute later with size 100, in ctx.
func (f *fixture) readyCache(ctx context.Context, t *testing.T, repo, key, obj string, at time.Time) {
	t.Helper()

	if !f.reserveCache(ctx, t, repo, key, obj, at) {
		t.Fatalf("ReserveBuildCache(%s) = false; want true", key)
	}

	if err := f.BuildCaches.CommitBuildCache(ctx, repo, key, obj, 100, at.Add(time.Minute)); err != nil {
		t.Fatalf("CommitBuildCache(%s): %v", key, err)
	}
}

func testBuildCachesFindHit(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	if _, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(1), t0); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("FindBuildCache of nothing = %v; want ErrNotFound", err)
	}

	f.readyCache(ctx, t, cacheRepo, cacheKey(1), "obj-1", t0)

	got, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(1), t1)
	if err != nil {
		t.Fatalf("FindBuildCache: %v", err)
	}

	want := domain.BuildCache{
		RepositoryKey: cacheRepo, CacheKey: cacheKey(1), ObjectKey: "obj-1", SizeBytes: 100, State: domain.BuildCacheReady, CreatedAt: t0, UsedAt: t1,
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UsedAt.Equal(want.UsedAt) {
		t.Errorf("FindBuildCache times = %v, %v; want created %v, used %v", got.CreatedAt, got.UsedAt, want.CreatedAt, want.UsedAt)
	}

	got.CreatedAt, got.UsedAt = want.CreatedAt, want.UsedAt
	if got != want {
		t.Errorf("FindBuildCache = %+v; want %+v", got, want)
	}

	if _, err := f.BuildCaches.FindBuildCache(ctx, "repo-2", cacheKey(1), t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("FindBuildCache of another repository = %v; want ErrNotFound", err)
	}
}

func testBuildCachesFindSaving(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	if !f.reserveCache(ctx, t, cacheRepo, cacheKey(1), "obj-1", t0) {
		t.Fatal("ReserveBuildCache = false; want true")
	}

	if _, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(1), t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("FindBuildCache of a saving cache = %v; want ErrNotFound", err)
	}
}

func testBuildCachesConcurrentReserve(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	const n = 8

	won := make([]bool, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			won[i], errs[i] = f.BuildCaches.ReserveBuildCache(ctx, domain.NewBuildCache{
				RepositoryKey: cacheRepo, CacheKey: cacheKey(1), ObjectKey: fmt.Sprintf("obj-%d", i), At: t0,
			})
		})
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		t.Fatalf("ReserveBuildCache: %v", err)
	}

	winner := slices.Index(won, true)
	if c := len(slices.DeleteFunc(slices.Clone(won), func(w bool) bool { return !w })); c != 1 {
		t.Fatalf("%d of %d concurrent reserves got the key; want exactly 1", c, n)
	}

	// Only the winner's object key commits it.
	for i := range n {
		err := f.BuildCaches.CommitBuildCache(ctx, cacheRepo, cacheKey(1), fmt.Sprintf("obj-%d", i), 1, t1)
		if i == winner && err != nil {
			t.Errorf("CommitBuildCache by the winner: %v", err)
		} else if i != winner && !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("CommitBuildCache by loser %d = %v; want ErrNotFound", i, err)
		}
	}
}

func testBuildCachesStaleTakeover(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	f.reserveCache(ctx, t, cacheRepo, cacheKey(1), "obj-a", t0)

	if f.reserveCache(ctx, t, cacheRepo, cacheKey(1), "obj-b", t0.Add(domain.BuildCacheStaleAfter)) {
		t.Error("ReserveBuildCache of a reservation an hour old = true; want false")
	}

	taken := t0.Add(domain.BuildCacheStaleAfter + time.Second)
	if !f.reserveCache(ctx, t, cacheRepo, cacheKey(1), "obj-c", taken) {
		t.Fatal("ReserveBuildCache of a reservation over an hour old = false; want true")
	}

	if err := f.BuildCaches.CommitBuildCache(ctx, cacheRepo, cacheKey(1), "obj-a", 1, taken); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("CommitBuildCache by the box taken over = %v; want ErrNotFound", err)
	}

	if err := f.BuildCaches.CommitBuildCache(ctx, cacheRepo, cacheKey(1), "obj-c", 1, taken); err != nil {
		t.Fatalf("CommitBuildCache by the new holder: %v", err)
	}

	got, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(1), taken)
	if err != nil {
		t.Fatal(err)
	}

	if got.ObjectKey != "obj-c" || !got.CreatedAt.Equal(taken) {
		t.Errorf("taken-over cache = %+v; want object obj-c created %v", got, taken)
	}
}

func testBuildCachesReadyNotReserved(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	f.readyCache(ctx, t, cacheRepo, cacheKey(1), "obj-1", t0)

	if f.reserveCache(ctx, t, cacheRepo, cacheKey(1), "obj-2", t0.Add(48*time.Hour)) {
		t.Error("ReserveBuildCache of a ready cache = true; want false")
	}

	got, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(1), t1)
	if err != nil || got.ObjectKey != "obj-1" || got.SizeBytes != 100 {
		t.Errorf("FindBuildCache = %+v, %v; want obj-1 unchanged", got, err)
	}
}

func testBuildCachesCommit(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	if err := f.BuildCaches.CommitBuildCache(ctx, cacheRepo, cacheKey(1), "obj-1", 1, t0); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("CommitBuildCache of nothing = %v; want ErrNotFound", err)
	}

	f.reserveCache(ctx, t, cacheRepo, cacheKey(1), "obj-1", t0)

	if err := f.BuildCaches.CommitBuildCache(ctx, cacheRepo, cacheKey(1), "obj-1", -1, t0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("CommitBuildCache of a negative size = %v; want ErrInvalid", err)
	}

	if err := f.BuildCaches.CommitBuildCache(ctx, cacheRepo, cacheKey(1), "obj-1", 0, t1); err != nil {
		t.Fatalf("CommitBuildCache: %v", err)
	}

	if err := f.BuildCaches.CommitBuildCache(ctx, cacheRepo, cacheKey(1), "obj-1", 5, t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("CommitBuildCache of a ready cache = %v; want ErrNotFound", err)
	}

	got, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(1), t1)
	if err != nil || got.SizeBytes != 0 || got.State != domain.BuildCacheReady {
		t.Errorf("FindBuildCache = %+v, %v; want ready with size 0", got, err)
	}

	_, err = f.BuildCaches.ReserveBuildCache(ctx, domain.NewBuildCache{RepositoryKey: cacheRepo, CacheKey: "ABC", ObjectKey: "o", At: t0})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("ReserveBuildCache of a malformed key = %v; want ErrInvalid", err)
	}
}

func testBuildCachesPrune(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	for i := range 5 {
		f.readyCache(ctx, t, cacheRepo, cacheKey(i), fmt.Sprintf("ready-%d", i), t0.Add(time.Duration(i)*time.Minute))
	}

	at := t0.Add(3 * time.Hour)
	f.reserveCache(ctx, t, cacheRepo, cacheKey(10), "stale", t0.Add(time.Hour))
	f.reserveCache(ctx, t, cacheRepo, cacheKey(11), "fresh", at.Add(-10*time.Minute))
	f.readyCache(ctx, t, "repo-2", cacheKey(0), "other-repo", t0)

	if _, err := f.BuildCaches.PruneBuildCaches(ctx, cacheRepo, -1, at); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("PruneBuildCaches keeping -1 = %v; want ErrInvalid", err)
	}

	got, err := f.BuildCaches.PruneBuildCaches(ctx, cacheRepo, domain.BuildCacheKeep, at)
	if err != nil {
		t.Fatalf("PruneBuildCaches: %v", err)
	}

	slices.Sort(got)

	if want := []string{"ready-0", "ready-1", "stale"}; !slices.Equal(got, want) {
		t.Errorf("PruneBuildCaches = %v; want %v", got, want)
	}

	for i := range 5 {
		_, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(i), at)
		if kept := i >= 2; kept != (err == nil) {
			t.Errorf("FindBuildCache(ready-%d) after prune = %v; want kept %t", i, err, kept)
		}
	}

	if f.reserveCache(ctx, t, cacheRepo, cacheKey(11), "again", at) {
		t.Error("the fresh reservation was pruned; want it kept")
	}

	if !f.reserveCache(ctx, t, cacheRepo, cacheKey(10), "again", at) {
		t.Error("the stale reservation was kept; want it pruned")
	}

	if _, err := f.BuildCaches.FindBuildCache(ctx, "repo-2", cacheKey(0), at); err != nil {
		t.Errorf("another repository's cache after prune: %v; want kept", err)
	}

	if got, err := f.BuildCaches.PruneBuildCaches(ctx, cacheRepo, domain.BuildCacheKeep, at); err != nil || len(got) != 0 {
		t.Errorf("PruneBuildCaches again = %v, %v; want nothing", got, err)
	}
}

func testBuildCachesTenancy(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	other := domain.WithOrg(t.Context(), f.NewOrg(t))

	f.readyCache(other, t, cacheRepo, cacheKey(1), "theirs", t0)

	if _, err := f.BuildCaches.FindBuildCache(ctx, cacheRepo, cacheKey(1), t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("FindBuildCache of another organization's cache = %v; want ErrNotFound", err)
	}

	if !f.reserveCache(ctx, t, cacheRepo, cacheKey(1), "ours", t0) {
		t.Error("ReserveBuildCache of a key another organization holds = false; want true")
	}

	if got, err := f.BuildCaches.PruneBuildCaches(ctx, cacheRepo, 0, t0.Add(48*time.Hour)); err != nil || !slices.Equal(got, []string{"ours"}) {
		t.Errorf("PruneBuildCaches = %v, %v; want only [ours]", got, err)
	}

	got, err := f.BuildCaches.FindBuildCache(other, cacheRepo, cacheKey(1), t1)
	if err != nil || got.ObjectKey != "theirs" {
		t.Errorf("the other organization's cache after our prune = %+v, %v; want kept", got, err)
	}

	if _, err := f.BuildCaches.FindBuildCache(t.Context(), cacheRepo, cacheKey(1), t1); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("FindBuildCache without an organization = %v; want ErrNoOrg", err)
	}
}
