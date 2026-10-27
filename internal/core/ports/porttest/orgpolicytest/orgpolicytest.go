// Package orgpolicytest is the conformance suite for ports.OrgPolicies (ADR 0024). The in-memory
// fake in porttest and the PostgreSQL adapter run it, so tests on the fake hold for the
// database's rules: the platform default when nothing is saved, saves fenced by the revision, the
// policy's rules refused, one audit entry per save and organizations kept apart.
package orgpolicytest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Models are the platform's models the suite builds its OrgPolicies with.
var Models = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}

// Harness is one OrgPolicies under test.
type Harness struct {
	// OrgPolicies returns the policies under test with models as the platform's models.
	OrgPolicies func(models []string) ports.OrgPolicies
	Audit       ports.Audit
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"Get_NothingSavedIsTheDefault", testDefault},
		{"Put_ThenGetRoundTrips", testRoundTrip},
		{"Put_StaleRevisionConflicts", testStale},
		{"Put_InvalidRefused", testInvalid},
		{"Put_AuditedOncePerSave", testAudit},
		{"OtherOrg_ReadsTheDefault", testIsolation},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t, p: h.OrgPolicies(Models)}
			f.org = h.NewOrg(t)
			tc.test(f)
		})
	}
}

var admin = domain.Actor{Kind: domain.ActorUser, ID: "u-admin", Name: "Ada Admin"}

type fixture struct {
	Harness

	t   *testing.T
	p   ports.OrgPolicies
	org domain.OrgID
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

func (f *fixture) get(org domain.OrgID) domain.OrgPolicy {
	f.t.Helper()

	p, err := f.p.Get(f.in(org))
	if err != nil {
		f.t.Fatalf("Get: %v", err)
	}

	return p
}

func (f *fixture) put(p domain.OrgPolicy, revision int64) domain.OrgPolicy {
	f.t.Helper()

	out, err := f.p.Put(f.in(f.org), p, revision, admin)
	if err != nil {
		f.t.Fatalf("Put at revision %d: %v", revision, err)
	}

	return out
}

func (f *fixture) audit(org domain.OrgID) []domain.AuditEntry {
	f.t.Helper()

	got, err := f.Audit.AuditLog(f.in(org), ports.AuditQuery{Limit: 100, Action: domain.AuditOrgPolicyUpdated})
	if err != nil {
		f.t.Fatal(err)
	}

	return got
}

// narrowed is a policy that differs from the default in every field.
func narrowed() domain.OrgPolicy {
	return domain.OrgPolicy{
		AllowedModels: []string{"claude-sonnet-5-5", "claude-haiku-5-5"}, DefaultModel: "claude-haiku-5-5",
		DeniedCommands:     []string{"Bash(terraform apply:*)", "Bash(git push --force:*)"},
		WriteOutsideBranch: domain.ToolOff, MergePullRequest: domain.ToolOff, ReadWeb: domain.ToolAsk, UseCredentials: domain.ToolOff,
		BoxTimeLimitMinutes: 45,
	}
}

// samePolicy fails when got's fields, revision aside, are not want's.
func (f *fixture) samePolicy(what string, got, want domain.OrgPolicy) {
	f.t.Helper()

	if !slices.Equal(got.AllowedModels, want.AllowedModels) || got.DefaultModel != want.DefaultModel ||
		!slices.Equal(got.DeniedCommands, want.DeniedCommands) || got.WriteOutsideBranch != want.WriteOutsideBranch ||
		got.MergePullRequest != want.MergePullRequest || got.ReadWeb != want.ReadWeb || got.UseCredentials != want.UseCredentials ||
		got.BoxTimeLimitMinutes != want.BoxTimeLimitMinutes {
		f.t.Errorf("%s = %+v; want %+v", what, got, want)
	}
}

func (f *fixture) isDefault(what string, got domain.OrgPolicy) {
	f.t.Helper()

	f.samePolicy(what, got, domain.DefaultOrgPolicy(Models))

	if got.Revision != 0 {
		f.t.Errorf("%s revision %d; want 0", what, got.Revision)
	}
}

func testDefault(f *fixture) {
	f.isDefault("Get with nothing saved", f.get(f.org))
}

func testRoundTrip(f *fixture) {
	want := narrowed()

	first := f.put(want, 0)
	if first.Revision != 1 || first.UpdatedBy.Ref() != admin.Ref() || first.UpdatedAt.IsZero() {
		f.t.Errorf("first Put = revision %d by %q at %v; want revision 1 by %s", first.Revision, first.UpdatedBy.Ref(), first.UpdatedAt, admin.Ref())
	}

	f.samePolicy("Put's answer", first, want)

	got := f.get(f.org)
	f.samePolicy("Get after Put", got, want)

	if got.Revision != 1 || got.UpdatedBy.Ref() != admin.Ref() {
		f.t.Errorf("Get after Put: revision %d by %q; want 1 by %s", got.Revision, got.UpdatedBy.Ref(), admin.Ref())
	}

	// A save with no denied commands reads back as none, and the next revision is 2.
	want.DeniedCommands = nil
	if second := f.put(want, 1); second.Revision != 2 {
		f.t.Errorf("second Put revision %d; want 2", second.Revision)
	}

	if got := f.get(f.org); got.Revision != 2 || len(got.DeniedCommands) != 0 {
		f.t.Errorf("Get after the second Put: revision %d, denied %v; want 2 and none", got.Revision, got.DeniedCommands)
	}
}

func testStale(f *fixture) {
	ctx := f.in(f.org)

	// Nothing is saved, so only revision 0 saves.
	if _, err := f.p.Put(ctx, narrowed(), 3, admin); !errors.Is(err, ports.ErrConflict) {
		f.t.Errorf("Put at revision 3 with nothing saved = %v; want ErrConflict", err)
	}

	f.isDefault("Get after the refused Put", f.get(f.org))

	saved := f.put(narrowed(), 0)
	other := domain.DefaultOrgPolicy(Models)

	for _, rev := range []int64{0, 2, -1} {
		if _, err := f.p.Put(ctx, other, rev, admin); !errors.Is(err, ports.ErrConflict) {
			f.t.Errorf("Put at revision %d over revision 1 = %v; want ErrConflict", rev, err)
		}
	}

	got := f.get(f.org)
	f.samePolicy("Get after the refused Puts", got, saved)

	if got.Revision != 1 {
		f.t.Errorf("revision %d after the refused Puts; want 1", got.Revision)
	}

	if n := len(f.audit(f.org)); n != 1 {
		f.t.Errorf("%d audit entries; want 1, refused Puts record nothing", n)
	}
}

func testInvalid(f *fixture) {
	bad := narrowed()
	bad.DefaultModel = "claude-opus-5-5" // not among the allowed models

	if _, err := f.p.Put(f.in(f.org), bad, 0, admin); !errors.Is(err, domain.ErrInvalid) {
		f.t.Errorf("Put of a default outside the allowed models = %v; want ErrInvalid", err)
	}

	bad = narrowed()
	bad.BoxTimeLimitMinutes = 0

	if _, err := f.p.Put(f.in(f.org), bad, 0, admin); !errors.Is(err, domain.ErrInvalid) {
		f.t.Errorf("Put of a 0 time limit = %v; want ErrInvalid", err)
	}

	f.isDefault("Get after the invalid Puts", f.get(f.org))

	if n := len(f.audit(f.org)); n != 0 {
		f.t.Errorf("%d audit entries; want none", n)
	}
}

func testAudit(f *fixture) {
	f.put(narrowed(), 0)
	f.put(domain.DefaultOrgPolicy(Models), 1)

	got := f.audit(f.org)
	if len(got) != 2 {
		f.t.Fatalf("%d %s entries; want 2: %+v", len(got), domain.AuditOrgPolicyUpdated, got)
	}

	for _, e := range got {
		if e.Actor.Ref() != admin.Ref() || e.TargetKind != domain.AuditTargetOrgPolicy || e.TargetID != string(f.org) {
			f.t.Errorf("entry %+v; want by %s on %s %s", e, admin.Ref(), domain.AuditTargetOrgPolicy, f.org)
		}
	}
}

func testIsolation(f *fixture) {
	other := f.NewOrg(f.t)

	f.put(narrowed(), 0)

	f.isDefault("Get in another organization", f.get(other))

	if n := len(f.audit(other)); n != 0 {
		f.t.Errorf("another organization has %d audit entries; want none", n)
	}

	// The other organization saves its own first policy at revision 0.
	if _, err := f.p.Put(f.in(other), domain.DefaultOrgPolicy(Models), 0, admin); err != nil {
		f.t.Errorf("Put in another organization at revision 0: %v", err)
	}

	f.samePolicy("Get after the other organization saved", f.get(f.org), narrowed())
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()

	if _, err := f.p.Get(ctx); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("Get without an organization = %v; want ErrNoOrg", err)
	}

	if _, err := f.p.Put(ctx, narrowed(), 0, admin); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("Put without an organization = %v; want ErrNoOrg", err)
	}
}
