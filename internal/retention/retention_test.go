package retention_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
	"github.com/byFrederick/goal/internal/retention"
)

const week = 7 * 24 * time.Hour

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// failingDelete is a bucket whose deletes fail.
type failingDelete struct{ *porttest.ObjectStore }

func (failingDelete) Delete(context.Context, string) error { return errors.New("bucket unavailable") }

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

		if _, err := f.objects.Put(t.Context(), factorystore.Prefix(o.ID, string(factory))+"factory.yaml", []byte("x")); err != nil {
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

	got, err := f.objects.List(t.Context(), factorystore.OrgPrefix(o.ID))
	if err != nil {
		t.Fatal(err)
	}

	return len(got)
}

func TestPurge_AfterTheRetentionRowsAndObjectsGo(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	p := f.purger(t, f.objects)

	if got := p.PurgeAfter(t0); !got.Equal(t0.Add(week)) {
		t.Errorf("PurgeAfter = %s; want a week after the deletion", got)
	}

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

func TestPurge_NoRetentionPurgesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.clock.Advance(week + time.Second)

	if ids, err := retention.New(f.tenancy, f.objects, f.clock, 0, nil).Purge(t.Context()); err == nil || len(ids) != 0 {
		t.Errorf("Purge without a retention = %v, %v; want an error and nothing purged", ids, err)
	}
}
