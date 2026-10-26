package retention_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/retention"
)

const week = 7 * 24 * time.Hour

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// failingDelete is a bucket whose purges fail.
type failingDelete struct{ *porttest.ObjectStore }

func (failingDelete) DeletePrefix(context.Context, string) (int, error) {
	return 0, errors.New("bucket unavailable")
}

type fixture struct {
	tenancy *porttest.Tenancy
	store   *porttest.Store
	objects *porttest.ObjectStore
	clock   *porttest.Clock
	gone    domain.Org
	kept    domain.Org
}

// newFixture has two organizations with a factory and an object each; gone is deleted at t0.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	f := &fixture{store: porttest.NewStore(), objects: porttest.NewObjectStore(), clock: porttest.NewClock(t0)}
	f.tenancy = porttest.NewTenancy(f.clock.Now).WithStore(f.store)

	factories := []domain.FactoryID{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"}

	for i, slug := range []string{"acme", "example-labs"} {
		o, err := f.tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: slug, Name: slug})
		if err != nil {
			t.Fatal(err)
		}

		factory := factories[i]
		f.store.AddFactory(o.ID, factory)

		if _, err := f.objects.Put(t.Context(), domain.BlobKey(o.ID, domain.HashBlob([]byte("x"))), []byte("x")); err != nil {
			t.Fatal(err)
		}

		if i == 0 {
			f.gone = o
		} else {
			f.kept = o
		}
	}

	if err := f.tenancy.DeleteOrg(domain.WithOrg(t.Context(), f.gone.ID), domain.LocalUser, t0); err != nil {
		t.Fatal(err)
	}

	return f
}

func (f *fixture) purger(t *testing.T, objects ports.ObjectStore) *retention.Purger {
	t.Helper()

	return retention.New(f.tenancy, objects, f.clock, week, nil)
}

// objectsOf counts an organization's current objects.
func (f *fixture) objectsOf(t *testing.T, o domain.Org) int {
	t.Helper()

	got, err := f.objects.List(t.Context(), domain.OrgPrefix(o.ID))
	if err != nil {
		t.Fatal(err)
	}

	return len(got)
}

func TestPurge_AfterTheRetentionRowsAndObjectsGo(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	p := f.purger(t, f.objects)

	f.clock.Advance(week - time.Second)

	if ids, err := p.Purge(t.Context()); err != nil || len(ids) != 0 || f.objectsOf(t, f.gone) != 1 {
		t.Fatalf("Purge within the retention = %v, %v; want nothing purged and the objects kept", ids, err)
	}

	f.clock.Advance(2 * time.Second)

	ids, err := p.Purge(t.Context())
	if err != nil || !slices.Equal(ids, []domain.OrgID{f.gone.ID}) {
		t.Fatalf("Purge after the retention = %v, %v; want [%s]", ids, err, f.gone.ID)
	}

	if n := f.objectsOf(t, f.gone); n != 0 {
		t.Errorf("the purged org keeps %d objects; want none", n)
	}

	if slots, err := f.tenancy.FactorySlots(domain.WithOrg(t.Context(), f.kept.ID)); err != nil || len(slots) != 1 || f.objectsOf(t, f.kept) != 1 {
		t.Errorf("the live org has factories %v (%v) and %d objects; want its own untouched", slots, err, f.objectsOf(t, f.kept))
	}

	if ids, err := p.Purge(t.Context()); err != nil || len(ids) != 0 {
		t.Errorf("Purge again = %v, %v; want nothing left", ids, err)
	}
}

func TestPurge_ABucketFailureKeepsTheOrgForTheNextRun(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.clock.Advance(week + time.Second)

	if ids, err := f.purger(t, failingDelete{f.objects}).Purge(t.Context()); err == nil || len(ids) != 0 {
		t.Fatalf("Purge with a failing bucket = %v, %v; want an error and nothing purged", ids, err)
	}

	if ids, err := f.purger(t, f.objects).Purge(t.Context()); err != nil || !slices.Equal(ids, []domain.OrgID{f.gone.ID}) ||
		f.objectsOf(t, f.gone) != 0 {
		t.Errorf("the next Purge = %v, %v; want the org purged with its objects", ids, err)
	}
}

// failingFor is a bucket whose purge of one organization's prefix fails.
type failingFor struct {
	*porttest.ObjectStore

	org domain.OrgID
}

func (b failingFor) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if prefix == domain.OrgPrefix(b.org) {
		return 0, errors.New("bucket unavailable")
	}

	return b.ObjectStore.DeletePrefix(ctx, prefix)
}

// Each organization is purged on its own: one whose objects cannot be deleted is kept, with its
// rows and objects, and reported, while the organizations after it are purged in the same run.
func TestPurge_AFailingOrgDoesNotHoldUpTheOthers(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	if err := f.tenancy.DeleteOrg(domain.WithOrg(t.Context(), f.kept.ID), domain.LocalUser, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	f.clock.Advance(week + 2*time.Second)

	ids, err := f.purger(t, failingFor{f.objects, f.gone.ID}).Purge(t.Context())
	if !slices.Equal(ids, []domain.OrgID{f.kept.ID}) || err == nil || !strings.Contains(err.Error(), string(f.gone.ID)) {
		t.Fatalf("Purge with one org's objects failing = %v, %v; want the other org purged and an error naming %s", ids, err, f.gone.ID)
	}

	if n := f.objectsOf(t, f.gone); n != 1 {
		t.Errorf("the failing org has %d objects left; want its object kept for the next run", n)
	}

	if ids, err := f.purger(t, f.objects).Purge(t.Context()); err != nil || !slices.Equal(ids, []domain.OrgID{f.gone.ID}) {
		t.Errorf("the next Purge = %v, %v; want [%s]", ids, err, f.gone.ID)
	}
}

func TestPurge_NoRetentionPurgesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.clock.Advance(week + time.Second)

	if ids, err := retention.New(f.tenancy, f.objects, f.clock, 0, nil).Purge(t.Context()); err == nil || len(ids) != 0 {
		t.Errorf("Purge without a retention = %v, %v; want an error and nothing purged", ids, err)
	}
}
