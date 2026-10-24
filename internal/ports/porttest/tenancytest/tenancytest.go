// Package tenancytest is the conformance suite for ports.Tenancy and ports.Audit. The in-memory
// fake in porttest and the PostgreSQL adapter run it, so tests on the fake hold for the
// database's rules: unique and reserved slugs, soft delete, purge, and an audit entry in the
// same transaction as every change.
package tenancytest

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Harness is one Tenancy and Audit under test. Other organizations may exist in it already
// (the database's default organization); the suite never assumes it is empty.
type Harness struct {
	Tenancy ports.Tenancy
	Audit   ports.Audit
	// NewFactory returns a new factory of org, for the slot caps.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"CreateOrg_FoundByOrgsAndOrg", testCreate},
		{"CreateOrg_RulesRefused", testCreateRules},
		{"Org_UnknownAndDeletedNotFound", testNotFound},
		{"UpdateOrg_RenamedSlugResolvesAtOnce", testUpdate},
		{"UpdateOrg_RulesRefused", testUpdateRules},
		{"DeleteOrg_GoneAtOnceAndPurgedAfterTheCutoff", testDeleteAndPurge},
		{"Mutations_AppendTheirAuditEntry", testMutationsAudited},
		{"AuditLog_NewestFirstFilteredAndPaged", testAuditLog},
		{"Audit_OtherOrgSeesNothing", testAuditIsolation},
		{"OrgMethods_NoOrgIsAnError", testNoOrg},
		{"UpdateOrg_SlotCaps", testSlotCaps},
		{"UpdateOrg_SlotCapRulesRefused", testSlotCapRules},
		{"PurgeOrg_OneOrgAtATime", testPurgeHook},
		{"PurgeOrg_NothingThatIsNotDue", testPurgeRefused},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.test(&fixture{Harness: newHarness(t), t: t})
		})
	}
}

// Names, actors and times the suite uses.
const (
	acme      = "acme"
	acmeName  = "Acme"
	acmeCo    = "Acme Co"
	labs      = "example-labs"
	labsName  = "Example Labs"
	pulled    = "andon.pulled"
	cleared   = "andon.cleared"
	kindFact  = "factory"
	missingID = "00000000-0000-7000-8000-ffffffffffff"
)

var (
	t0      = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	agent   = domain.Actor{Kind: domain.ActorAgent, ID: "investigator", Name: "investigator"}
	purger  = domain.Actor{Kind: domain.ActorSystem, ID: "purge", Name: "purge"}
	factory = domain.FactoryID("00000000-0000-7000-8000-000000000042")
)

// fixture is one test's harness. It holds the test's T so the test bodies are not mistaken for
// helpers, and its methods fail the test on unexpected errors.
type fixture struct {
	Harness

	t *testing.T
}

func (f *fixture) create(slug, name string) domain.Org {
	f.t.Helper()

	o, err := f.Tenancy.CreateOrg(f.t.Context(), domain.LocalUser, ports.NewOrg{Slug: slug, Name: name})
	if err != nil {
		f.t.Fatalf("CreateOrg(%s): %v", slug, err)
	}

	return o
}

func (f *fixture) in(o domain.Org) context.Context { return domain.WithOrg(f.t.Context(), o.ID) }

func (f *fixture) update(o domain.Org, by domain.Actor, u ports.OrgUpdate) domain.Org {
	f.t.Helper()

	got, err := f.Tenancy.UpdateOrg(f.in(o), by, u)
	if err != nil {
		f.t.Fatalf("UpdateOrg: %v", err)
	}

	return got
}

func (f *fixture) remove(o domain.Org, by domain.Actor) {
	f.t.Helper()

	if err := f.Tenancy.DeleteOrg(f.in(o), by, t0); err != nil {
		f.t.Fatalf("DeleteOrg: %v", err)
	}
}

func (f *fixture) append(o domain.Org, e domain.AuditEntry) {
	f.t.Helper()

	if err := f.Audit.Append(f.in(o), e); err != nil {
		f.t.Fatalf("Append %s: %v", e.Action, err)
	}
}

// found expects Org(ref) to be want.
func (f *fixture) found(ref string, want domain.Org) {
	f.t.Helper()

	if got, err := f.Tenancy.Org(f.t.Context(), ref); err != nil || got != want {
		f.t.Errorf("Org(%q) = %+v, %v; want %+v", ref, got, err, want)
	}
}

// gone expects Org(ref) to be ErrNotFound.
func (f *fixture) gone(ref string) {
	f.t.Helper()

	if got, err := f.Tenancy.Org(f.t.Context(), ref); !errors.Is(err, ports.ErrNotFound) {
		f.t.Errorf("Org(%q) = %+v, %v; want ErrNotFound", ref, got, err)
	}
}

// refused expects err to wrap want.
func (f *fixture) refused(what string, err, want error) {
	f.t.Helper()

	if !errors.Is(err, want) {
		f.t.Errorf("%s = %v; want %v", what, err, want)
	}
}

func (f *fixture) orgs() []domain.OrgSummary {
	f.t.Helper()

	orgs, err := f.Tenancy.Orgs(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}

	return orgs
}

// listed returns o's summary in Orgs and whether ActiveOrgs has it; both or neither must hold.
func (f *fixture) listed(o domain.Org) (domain.OrgSummary, bool) {
	f.t.Helper()

	orgs := f.orgs()
	i := slices.IndexFunc(orgs, func(s domain.OrgSummary) bool { return s.ID == o.ID })

	ids, err := f.Tenancy.ActiveOrgs(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}

	if active := slices.Contains(ids, o.ID); active != (i >= 0) {
		f.t.Errorf("org %s: in Orgs %v, in ActiveOrgs %v; want both or neither", o.Slug, i >= 0, active)
	}

	if i < 0 {
		return domain.OrgSummary{}, false
	}

	return orgs[i], true
}

func (f *fixture) log(o domain.Org, q ports.AuditQuery) []domain.AuditEntry {
	f.t.Helper()

	if q.Limit == 0 {
		q.Limit = 100
	}

	got, err := f.Audit.AuditLog(f.in(o), q)
	if err != nil {
		f.t.Fatal(err)
	}

	return got
}

// purge purges every organization deleted before cutoff, as the purge job does, and returns them.
func (f *fixture) purge(cutoff time.Time) []domain.OrgID {
	f.t.Helper()

	due, err := f.Tenancy.DeletedOrgs(f.t.Context(), cutoff, 10)
	if err != nil {
		f.t.Fatal(err)
	}

	var out []domain.OrgID

	for _, id := range due {
		ok, err := f.Tenancy.PurgeOrg(f.t.Context(), id, cutoff, nil)
		if err != nil {
			f.t.Fatal(err)
		}

		if ok {
			out = append(out, id)
		}
	}

	return out
}

func ids(entries []domain.AuditEntry) []int64 {
	out := make([]int64, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}

	return out
}

func testCreate(f *fixture) {
	o := f.create(acme, acmeName)

	if o.ID == "" || o.Slug != acme || o.Name != acmeName || o.Mark != "Ac" || o.CreatedAt.IsZero() {
		f.t.Errorf("CreateOrg = %+v; want acme with the default mark Ac and a creation time", o)
	}

	f.found(acme, o)
	f.found(string(o.ID), o)

	if s, ok := f.listed(o); !ok || s.Org != o || s.FactoryCount != 0 {
		f.t.Errorf("Orgs has %+v, %v; want the new org with no factories", s, ok)
	}

	marked, err := f.Tenancy.CreateOrg(f.t.Context(), domain.LocalUser, ports.NewOrg{Slug: labs, Name: labsName, Mark: "XL"})
	if err != nil || marked.Mark != "XL" {
		f.t.Errorf("CreateOrg with a mark = %+v, %v; want mark XL", marked, err)
	}

	if orgs := f.orgs(); !slices.IsSortedFunc(orgs, func(a, b domain.OrgSummary) int { return strings.Compare(a.Name, b.Name) }) {
		f.t.Errorf("Orgs are not by name: %+v", orgs)
	}
}

func testCreateRules(f *fixture) {
	f.create(acme, acmeName)
	before := len(f.orgs())

	for what, n := range map[string]ports.NewOrg{
		"taken slug":     {Slug: acme, Name: "Another Acme"},
		"reserved slug":  {Slug: "settings", Name: "Settings"},
		"default slug":   {Slug: "default", Name: "Default"},
		"malformed slug": {Slug: acmeCo, Name: acmeCo},
		"empty name":     {Slug: "acme-co", Name: ""},
		"long mark":      {Slug: "acme-co", Name: acmeCo, Mark: "ACO"},
	} {
		_, err := f.Tenancy.CreateOrg(f.t.Context(), domain.LocalUser, n)
		f.refused("CreateOrg with a "+what, err, domain.ErrInvalid)
	}

	if after := len(f.orgs()); after != before {
		f.t.Errorf("refused creates left %d orgs; want %d", after, before)
	}
}

func testNotFound(f *fixture) {
	o := f.create(acme, acmeName)
	f.remove(o, domain.LocalUser)

	for _, ref := range []string{"nope", missingID, acme, string(o.ID), "not a slug"} {
		f.gone(ref)
	}
}

func testUpdate(f *fixture) {
	o := f.create(acme, acmeName)

	got := f.update(o, domain.LocalUser, ports.OrgUpdate{Slug: new("acme-co"), Name: new(acmeCo), Mark: new("AC")})
	want := domain.Org{ID: o.ID, Slug: "acme-co", Name: acmeCo, Mark: "AC", CreatedAt: o.CreatedAt}

	if got != want {
		f.t.Errorf("UpdateOrg = %+v; want %+v", got, want)
	}

	f.found("acme-co", want)
	f.gone(acme)

	if again := f.update(o, domain.LocalUser, ports.OrgUpdate{Name: new(acmeCo)}); again != want {
		f.t.Errorf("UpdateOrg without a change = %+v; want %+v", again, want)
	}
}

func testUpdateRules(f *fixture) {
	o := f.create(acme, acmeName)
	f.create(labs, labsName)

	for what, u := range map[string]ports.OrgUpdate{
		"taken slug":    {Slug: new(labs)},
		"reserved slug": {Slug: new("admin")},
		"empty name":    {Name: new("")},
		"long mark":     {Mark: new("ACO")},
	} {
		_, err := f.Tenancy.UpdateOrg(f.in(o), domain.LocalUser, u)
		f.refused("UpdateOrg with a "+what, err, domain.ErrInvalid)
	}

	f.found(acme, o)
	f.remove(o, domain.LocalUser)

	_, err := f.Tenancy.UpdateOrg(f.in(o), domain.LocalUser, ports.OrgUpdate{Name: new("Gone")})
	f.refused("UpdateOrg of a deleted org", err, ports.ErrNotFound)
	f.refused("DeleteOrg twice", f.Tenancy.DeleteOrg(f.in(o), domain.LocalUser, t0), ports.ErrNotFound)
}

func testDeleteAndPurge(f *fixture) {
	gone := f.create(acme, acmeName)
	kept := f.create(labs, labsName)
	f.remove(gone, domain.LocalUser)

	if _, ok := f.listed(gone); ok {
		f.t.Error("a deleted org is still listed")
	}

	if _, ok := f.listed(kept); !ok {
		f.t.Error("deleting one org hid another")
	}

	_, err := f.Tenancy.CreateOrg(f.t.Context(), domain.LocalUser, ports.NewOrg{Slug: acme, Name: acmeName})
	f.refused("CreateOrg with a deleted org's slug before the purge", err, domain.ErrInvalid)

	if got := f.purge(t0); len(got) != 0 {
		f.t.Errorf("purge before the cutoff = %v; want none", got)
	}

	if got := f.purge(t0.Add(time.Second)); !slices.Equal(got, []domain.OrgID{gone.ID}) {
		f.t.Errorf("purge after the cutoff = %v; want [%s]", got, gone.ID)
	}

	if got := f.purge(t0.Add(time.Second)); len(got) != 0 {
		f.t.Errorf("purge again = %v; want none", got)
	}

	if got := f.log(gone, ports.AuditQuery{}); len(got) != 0 {
		f.t.Errorf("a purged org still has %d audit entries", len(got))
	}

	f.create(acme, acmeName) // the slug is free again
	f.found(labs, kept)
}

func testMutationsAudited(f *fixture) {
	o := f.create(acme, acmeName)
	f.update(o, agent, ports.OrgUpdate{Name: new(acmeCo), Mark: new("Ac")})
	f.update(o, agent, ports.OrgUpdate{Name: new(acmeCo)}) // no change, no entry
	f.remove(o, purger)

	got := f.log(o, ports.AuditQuery{})
	want := []struct {
		action string
		actor  domain.Actor
		data   map[string]any
	}{
		{domain.AuditOrgDeleted, purger, map[string]any{"slug": acme, "name": acmeCo}},
		{domain.AuditOrgUpdated, agent, map[string]any{"name": map[string]any{"from": acmeName, "to": acmeCo}}},
		{domain.AuditOrgCreated, domain.LocalUser, map[string]any{"slug": acme, "name": acmeName}},
	}

	if len(got) != len(want) {
		f.t.Fatalf("audit log has %d entries; want %d: %+v", len(got), len(want), got)
	}

	for i, w := range want {
		e := got[i]
		ok := e.ID != 0 && !e.At.IsZero() && e.Action == w.action && e.Actor == w.actor && e.FactoryID == "" &&
			e.TargetKind == domain.AuditTargetOrg && e.TargetID == string(o.ID) && reflect.DeepEqual(e.Data, w.data)

		if !ok {
			f.t.Errorf("entry %d = %+v; want %s by %+v on org %s with %v", i, e, w.action, w.actor, o.ID, w.data)
		}
	}
}

func testAuditLog(f *fixture) {
	o := f.create(acme, acmeName)
	f.append(o, domain.AuditEntry{
		Actor: agent, Action: pulled, TargetKind: kindFact, TargetID: string(factory), FactoryID: factory,
		Data: map[string]any{"reason": "red"},
	})
	f.append(o, domain.AuditEntry{Actor: agent, Action: cleared, TargetKind: kindFact, TargetID: string(factory), FactoryID: factory})
	f.append(o, domain.AuditEntry{Actor: agent, Action: pulled, TargetKind: kindFact, TargetID: string(factory), FactoryID: factory, At: t0})

	f.refused("Append with a malformed action",
		f.Audit.Append(f.in(o), domain.AuditEntry{Actor: agent, Action: "Not An Action", TargetKind: kindFact, TargetID: "x"}), domain.ErrInvalid)

	all := f.log(o, ports.AuditQuery{})
	actions := make([]string, 0, len(all))

	for _, e := range all {
		actions = append(actions, e.Action)
	}

	if !slices.Equal(actions, []string{pulled, cleared, pulled, domain.AuditOrgCreated}) {
		f.t.Fatalf("AuditLog actions = %v; want the appends newest first, then org.created", actions)
	}

	if !all[0].At.Equal(t0) || all[2].Data["reason"] != "red" || all[2].FactoryID != factory {
		f.t.Errorf("AuditLog lost a field: %+v", all)
	}

	if got := ids(f.log(o, ports.AuditQuery{Action: pulled})); !slices.Equal(got, []int64{all[0].ID, all[2].ID}) {
		f.t.Errorf("AuditLog by action = %v; want both pulls", got)
	}

	if got := f.log(o, ports.AuditQuery{FactoryID: factory, TargetKind: kindFact, TargetID: string(factory)}); len(got) != 3 {
		f.t.Errorf("AuditLog by factory and target = %d entries; want 3", len(got))
	}

	page := f.log(o, ports.AuditQuery{Limit: 2})
	next := f.log(o, ports.AuditQuery{Limit: 2, Before: page[len(page)-1].ID})

	if got := append(ids(page), ids(next)...); !slices.Equal(got, ids(all)) {
		f.t.Errorf("two pages = %v; want %v", got, ids(all))
	}
}

func testAuditIsolation(f *fixture) {
	a := f.create(acme, acmeName)
	b := f.create(labs, labsName)
	f.append(a, domain.AuditEntry{Actor: agent, Action: pulled, TargetKind: kindFact, TargetID: "f"})

	for _, e := range f.log(b, ports.AuditQuery{}) {
		if e.Action != domain.AuditOrgCreated || e.TargetID != string(b.ID) {
			f.t.Errorf("org b sees %+v", e)
		}
	}

	f.update(b, domain.LocalUser, ports.OrgUpdate{Name: new("Labs")})
	f.found(acme, a)
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()

	_, err := f.Tenancy.UpdateOrg(ctx, domain.LocalUser, ports.OrgUpdate{Name: new("x")})
	f.refused("UpdateOrg without an org", err, domain.ErrNoOrg)
	f.refused("DeleteOrg without an org", f.Tenancy.DeleteOrg(ctx, domain.LocalUser, t0), domain.ErrNoOrg)
	f.refused("Append without an org",
		f.Audit.Append(ctx, domain.AuditEntry{Actor: agent, Action: pulled, TargetKind: kindFact, TargetID: "f"}), domain.ErrNoOrg)

	_, err = f.Audit.AuditLog(ctx, ports.AuditQuery{Limit: 10})
	f.refused("AuditLog without an org", err, domain.ErrNoOrg)
}
