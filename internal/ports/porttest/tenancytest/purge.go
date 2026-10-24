package tenancytest

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Each organization is purged on its own: the hook runs once its rows are gone, and an error from
// it keeps that organization, rows and all, and no other.
func testPurgeHook(f *fixture) {
	first, second := f.create(acme, acmeName), f.create(labs, labsName)
	f.remove(first, domain.LocalUser)
	f.remove(second, domain.LocalUser)

	cutoff := t0.Add(time.Second)
	f.deletedOrgs(cutoff, first.ID, second.ID)

	failing := func(context.Context, domain.OrgID) error { return errors.New("bucket unavailable") }

	if ok, err := f.Tenancy.PurgeOrg(f.t.Context(), second.ID, cutoff, failing); ok || err == nil {
		f.t.Errorf("PurgeOrg with a failing hook = %v, %v; want an error and nothing purged", ok, err)
	}

	var seen []domain.OrgID

	hook := func(_ context.Context, org domain.OrgID) error {
		seen = append(seen, org)

		return nil
	}

	if ok, err := f.Tenancy.PurgeOrg(f.t.Context(), first.ID, cutoff, hook); !ok || err != nil {
		f.t.Errorf("PurgeOrg after another org's failure = %v, %v; want it purged", ok, err)
	}

	if got := f.log(second, ports.AuditQuery{}); len(got) == 0 {
		f.t.Error("a failed purge removed its org's audit log; want the org kept for the next run")
	}

	if got := f.log(first, ports.AuditQuery{}); len(got) != 0 {
		f.t.Errorf("a purged org still has %d audit entries", len(got))
	}

	if ok, err := f.Tenancy.PurgeOrg(f.t.Context(), second.ID, cutoff, hook); !ok || err != nil {
		f.t.Errorf("PurgeOrg of the failed org on the next run = %v, %v; want it purged", ok, err)
	}

	if !slices.Equal(seen, []domain.OrgID{first.ID, second.ID}) {
		f.t.Errorf("the hook saw %v; want each purged org once, in purge order", seen)
	}
}

// Nothing is purged that is live, was deleted on or after the cutoff, or is already gone, and the
// hook does not run for it.
func testPurgeRefused(f *fixture) {
	gone, live := f.create(acme, acmeName), f.create(labs, labsName)
	f.remove(gone, domain.LocalUser)

	hook := func(_ context.Context, org domain.OrgID) error {
		f.t.Errorf("the hook ran for %s", org)

		return nil
	}

	for what, c := range map[string]struct {
		org    domain.OrgID
		cutoff time.Time
	}{
		"a live org":                   {live.ID, t0.Add(time.Second)},
		"an org deleted at the cutoff": {gone.ID, t0},
		"an org that does not exist":   {domain.OrgID("00000000-0000-7000-8000-00000000dead"), t0.Add(time.Second)},
	} {
		if ok, err := f.Tenancy.PurgeOrg(f.t.Context(), c.org, c.cutoff, hook); ok || err != nil {
			f.t.Errorf("PurgeOrg of %s = %v, %v; want false and no error", what, ok, err)
		}
	}

	f.found(labs, live)

	if got := f.purge(t0.Add(time.Second)); !slices.Equal(got, []domain.OrgID{gone.ID}) {
		f.t.Fatalf("purge after the cutoff = %v; want [%s]", got, gone.ID)
	}

	if ok, err := f.Tenancy.PurgeOrg(f.t.Context(), gone.ID, t0.Add(time.Second), hook); ok || err != nil {
		f.t.Errorf("PurgeOrg of an org already purged = %v, %v; want false and no error", ok, err)
	}
}

// deletedOrgs expects DeletedOrgs before cutoff to be want, in any order, and its limit to hold.
func (f *fixture) deletedOrgs(cutoff time.Time, want ...domain.OrgID) {
	f.t.Helper()

	due, err := f.Tenancy.DeletedOrgs(f.t.Context(), cutoff, 10)
	slices.Sort(due)
	slices.Sort(want)

	if err != nil || !slices.Equal(due, want) {
		f.t.Fatalf("DeletedOrgs = %v, %v; want %v", due, err, want)
	}

	if got, err := f.Tenancy.DeletedOrgs(f.t.Context(), cutoff, 1); err != nil || len(got) != 1 {
		f.t.Errorf("DeletedOrgs with limit 1 = %v, %v; want one org", got, err)
	}
}
