// Package credentialtest is the conformance suite for ports.Credentials (ADR 0017). The in-memory
// fake in porttest runs it, and the PostgreSQL adapter joins it with the store part, so tests on
// the fake hold for the database's rules: names unique per organization, write-only values sealed
// under the master key, the factory scope enforced by Resolve, an audit entry for every change and
// use, organizations kept apart, and an import's dry run that stores nothing.
package credentialtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one Credentials under test.
type Harness struct {
	// Credentials returns the credentials under test, sealing with key; every call shares one
	// store, as goalds with different keys share one database.
	Credentials func(key ports.MasterKey) ports.Credentials
	// Key returns master keys with ids, the first active; the same id is the same key in every
	// call. With no ids it has none (ports.ErrNoMasterKey).
	Key   func(ids ...string) ports.MasterKey
	Audit ports.Audit
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
		{"Create_ValueNeverReturned", testCreate},
		{"Create_DuplicateNameRefused", testDuplicate},
		{"Create_RulesRefused", testCreateRules},
		{"Patch_MetadataAudited", testPatch},
		{"Patch_RenameToATakenNameRefused", testRenameTaken},
		{"Patch_ReplaceChangesWhatResolveOpens", testReplace},
		{"Resolve_OpensAuditsAndMarksUse", testResolve},
		{"Resolve_OutOfScopeFactoryRefused", testScopeRefused},
		{"Resolve_AllScopeAllowsAnyFactory", testAllScope},
		{"Resolve_UnknownNotFound", testResolveUnknown},
		{"Resolve_AnotherKeyDoesNotOpen", testOtherKey},
		{"Delete_GoneAndAudited", testDelete},
		{"OtherOrg_SeesNothing", testIsolation},
		{"NoMasterKey_ValuesUnavailable", testNoKey},
		{"ImportPreview_NewAndExistingStoresNothing", testImportPreview},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t, c: h.Credentials(h.Key("k1"))}
			tc.test(f)
		})
	}
}

// Names and values the suite uses. marker and replaced are values no Credential, error or audit
// entry may hold.
const (
	sentry = "sentry-token"
	deploy = "deploy-key"

	marker   = "sk-conformance-marker-value"
	replaced = "sk-conformance-replaced-value"
	labsVal  = "sk-conformance-labs-value"

	factoryA = domain.FactoryID("00000000-0000-7000-8000-0000000000a1")
	factoryB = domain.FactoryID("00000000-0000-7000-8000-0000000000b1")
	run      = domain.RunID("00000000-0000-7000-8000-0000000000aa")
)

var (
	admin  = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}
	editor = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000002", Name: "Eve"}
	// inA is a run of factory A by its backend agent.
	inA = domain.CredentialUse{Factory: factoryA, Agent: "backend", Run: run}
	inB = domain.CredentialUse{Factory: factoryB, Agent: "backend", Run: run}
)

type fixture struct {
	Harness

	t *testing.T
	c ports.Credentials
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

// sentryIn is credential sentry with value, scoped to factory A with its backend agent.
func sentryIn(value string) domain.NewCredential {
	expires := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)

	return domain.NewCredential{
		Name: sentry, Type: domain.CredentialAPIKey, Description: "error reports",
		Scope: domain.CredentialScope{Factories: []domain.FactoryID{factoryA}}, Agents: map[domain.FactoryID][]string{factoryA: {"backend"}},
		ExpiresAt: &expires, RemindDays: 14, Value: value,
	}
}

func (f *fixture) create(org domain.OrgID, n domain.NewCredential) domain.Credential {
	f.t.Helper()

	got, err := f.c.Create(f.in(org), n, admin)
	if err != nil {
		f.t.Fatalf("Create %s: %v", n.Name, err)
	}

	f.noValue("Create's credential", got)

	return got
}

func (f *fixture) resolve(org domain.OrgID, use domain.CredentialUse, names ...string) []domain.ResolvedCredential {
	f.t.Helper()

	got, err := f.c.Resolve(f.in(org), names, use)
	if err != nil {
		f.t.Fatalf("Resolve %v: %v", names, err)
	}

	return got
}

// audit is org's entries of action, newest first; none holds a value.
func (f *fixture) audit(org domain.OrgID, action string) []domain.AuditEntry {
	f.t.Helper()

	got, err := f.Audit.AuditLog(f.in(org), ports.AuditQuery{Limit: 100, Action: action})
	if err != nil {
		f.t.Fatal(err)
	}

	f.noValue(action+" entries", got)

	return got
}

// one is org's only entry of action, failing when there is not exactly one.
func (f *fixture) one(org domain.OrgID, action string) domain.AuditEntry {
	f.t.Helper()

	got := f.audit(org, action)
	if len(got) != 1 {
		f.t.Fatalf("%d %s entries; want one: %+v", len(got), action, got)
	}

	return got[0]
}

// noValue fails when v, printed with %v, %+v or %#v, holds one of the suite's values.
func (f *fixture) noValue(what string, v any) {
	f.t.Helper()

	for _, verb := range []string{"%v", "%+v", "%#v"} {
		printed := fmt.Sprintf(verb, v)
		for _, s := range []string{marker, replaced, labsVal} {
			if strings.Contains(printed, s) {
				f.t.Errorf("%s holds a value (%s): %s", what, verb, printed)
			}
		}
	}
}

func (f *fixture) expect(ok bool, format string, args ...any) {
	f.t.Helper()

	if !ok {
		f.t.Errorf(format, args...)
	}
}

// refused fails unless err is want, and when err holds a value.
func (f *fixture) refused(what string, err, want error) {
	f.t.Helper()

	if !errors.Is(err, want) {
		f.t.Errorf("%s = %v; want %v", what, err, want)
	}

	if err != nil {
		f.noValue(what+"'s error", err.Error())
	}
}

func testCreate(f *fixture) {
	acme := f.NewOrg(f.t)
	got := f.create(acme, sentryIn(marker))

	f.expect(got.ID != "" && got.Name == sentry && got.Type == domain.CredentialAPIKey && got.Description == "error reports",
		"Create = %+v", got)
	f.expect(got.ExpiresAt != nil && got.RemindDays == 14 && got.LastUsedAt == nil, "Create's expiry or last use = %+v", got)
	f.expect(got.CreatedBy == admin && got.UpdatedBy == admin && !got.CreatedAt.IsZero(), "Create's actors = %+v", got)
	f.expect(!got.Scope.All && len(got.Scope.Factories) == 1 && got.Scope.Factories[0] == factoryA, "Create's scope = %+v", got.Scope)
	f.expect(got.ForAgent(factoryA, "backend"), "Create's agents = %+v", got.Agents)

	one, err := f.c.Get(f.in(acme), sentry)
	f.expect(err == nil && one.ID == got.ID, "Get = %+v, %v", one, err)
	f.noValue("Get's credential", one)

	all, err := f.c.List(f.in(acme))
	f.expect(err == nil && len(all) == 1 && all[0].ID == got.ID, "List = %+v, %v", all, err)
	f.noValue("List's credentials", all)

	e := f.one(acme, domain.AuditCredentialCreated)
	f.expect(e.TargetKind == domain.AuditTargetCredential && e.TargetID == string(got.ID) && e.Actor == admin, "credential.created = %+v", e)

	opened := f.resolve(acme, inA, sentry)
	f.expect(len(opened) == 1 && opened[0].Value == marker, "Resolve does not open the value it was created with")
	f.noValue("Resolve's credentials, printed", opened)
}

func testDuplicate(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	_, err := f.c.Create(f.in(acme), sentryIn(replaced), editor)
	f.refused("Create of a taken name", err, ports.ErrNameTaken)

	f.create(labs, sentryIn(labsVal))
	f.expect(f.resolve(acme, inA, sentry)[0].Value == marker, "the refused Create changed the value")
}

func testCreateRules(f *fixture) {
	acme := f.NewOrg(f.t)

	for what, edit := range map[string]func(n *domain.NewCredential){
		"a bad name":              func(n *domain.NewCredential) { n.Name = "Bad_Name" },
		"a reserved env name":     func(n *domain.NewCredential) { n.Name = "goal-token" },
		"an unknown type":         func(n *domain.NewCredential) { n.Type = "password" },
		"a reminder, no expiry":   func(n *domain.NewCredential) { n.ExpiresAt = nil },
		"no value":                func(n *domain.NewCredential) { n.Value = "" },
		"agents out of the scope": func(n *domain.NewCredential) { n.Agents = map[domain.FactoryID][]string{factoryB: {"backend"}} },
	} {
		n := sentryIn(marker)
		edit(&n)

		_, err := f.c.Create(f.in(acme), n, admin)
		f.refused("Create with "+what, err, domain.ErrInvalid)
	}

	all, err := f.c.List(f.in(acme))
	f.expect(err == nil && len(all) == 0, "refused Creates stored %+v, %v", all, err)
	f.expect(len(f.audit(acme, domain.AuditCredentialCreated)) == 0, "a refused Create was audited")
}

func testPatch(f *fixture) {
	acme := f.NewOrg(f.t)
	before := f.create(acme, sentryIn(marker))

	name, desc, typ := "sentry-token-2027", "rotated yearly", domain.CredentialOAuthToken
	got, err := f.c.Patch(f.in(acme), sentry, domain.CredentialPatch{
		Name: &name, Description: &desc, Type: &typ,
		Scope:  &domain.CredentialScope{Factories: []domain.FactoryID{factoryA, factoryB}},
		Agents: map[domain.FactoryID][]string{factoryB: {"frontend"}}, Expiry: &domain.CredentialExpiry{},
	}, editor)
	if err != nil {
		f.t.Fatalf("Patch: %v", err)
	}

	f.noValue("Patch's credential", got)
	f.expect(got.ID == before.ID && got.Name == name && got.Description == desc && got.Type == typ, "Patch = %+v", got)
	f.expect(got.ExpiresAt == nil && got.RemindDays == 0, "Patch did not clear the expiry: %+v", got)
	f.expect(got.ForAgent(factoryB, "frontend") && !got.ForAgent(factoryA, "backend"), "Patch's agents = %+v", got.Agents)
	f.expect(got.UpdatedBy == editor && got.CreatedBy == admin, "Patch's actors = %+v", got)

	_, err = f.c.Get(f.in(acme), sentry)
	f.refused("Get of the old name", err, ports.ErrNotFound)

	f.expect(f.resolve(acme, inB, name)[0].Value == marker, "a rename lost the value")
	f.one(acme, domain.AuditCredentialUpdated)
	f.expect(len(f.audit(acme, domain.AuditCredentialValueReplaced)) == 0, "a metadata Patch audited a replaced value")

	remind := domain.CredentialExpiry{RemindDays: 3}
	_, err = f.c.Patch(f.in(acme), name, domain.CredentialPatch{Expiry: &remind}, editor)
	f.refused("Patch to a reminder without an expiry", err, domain.ErrInvalid)

	_, err = f.c.Patch(f.in(acme), "missing-one", domain.CredentialPatch{Description: &desc}, editor)
	f.refused("Patch of an unknown credential", err, ports.ErrNotFound)
}

func testRenameTaken(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	n := sentryIn(replaced)
	n.Name = deploy
	f.create(acme, n)

	taken := sentry
	_, err := f.c.Patch(f.in(acme), deploy, domain.CredentialPatch{Name: &taken}, admin)
	f.refused("Patch to a taken name", err, ports.ErrNameTaken)
	f.expect(f.resolve(acme, inA, deploy)[0].Value == replaced, "the refused rename changed the value")
}

func testReplace(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	v := replaced
	got, err := f.c.Patch(f.in(acme), sentry, domain.CredentialPatch{Replace: &v}, editor)
	if err != nil {
		f.t.Fatalf("Patch Replace: %v", err)
	}

	f.noValue("Replace's credential", got)
	f.expect(got.UpdatedBy == editor, "Replace's actor = %+v", got.UpdatedBy)
	f.expect(f.resolve(acme, inA, sentry)[0].Value == replaced, "Resolve does not open the replaced value")
	f.one(acme, domain.AuditCredentialValueReplaced)

	empty := ""
	_, err = f.c.Patch(f.in(acme), sentry, domain.CredentialPatch{Replace: &empty}, editor)
	f.refused("Replace with no value", err, domain.ErrInvalid)
}

func testResolve(f *fixture) {
	acme := f.NewOrg(f.t)
	created := f.create(acme, sentryIn(marker))

	n := sentryIn(replaced)
	n.Name, n.Scope = deploy, domain.CredentialScope{All: true}
	f.create(acme, n)

	got := f.resolve(acme, inA, sentry, deploy, sentry)
	f.expect(len(got) == 2 && got[0].Credential.Name == sentry && got[0].Value == marker &&
		got[1].Credential.Name == deploy && got[1].Value == replaced, "Resolve = %v", got)
	f.expect(got[0].Credential.LastUsedAt != nil, "Resolve's credential has no last use")

	one, err := f.c.Get(f.in(acme), sentry)
	f.expect(err == nil && one.LastUsedAt != nil, "Resolve did not set last_used_at: %+v, %v", one, err)

	used := f.audit(acme, domain.AuditCredentialUsed)
	f.expect(len(used) == 2, "%d credential.used entries; want one per credential", len(used))

	for _, e := range used {
		if e.TargetID != string(created.ID) {
			continue
		}

		f.expect(e.Actor == domain.EngineActor && e.TargetKind == domain.AuditTargetCredential, "credential.used = %+v", e)
		f.expect(e.Data["name"] == sentry && e.Data["run_id"] == string(run) && e.Data["factory_id"] == string(factoryA) && e.Data["agent"] == "backend",
			"credential.used data = %+v", e.Data)
	}
}

func testScopeRefused(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	n := sentryIn(replaced)
	n.Name, n.Scope, n.Agents = deploy, domain.CredentialScope{}, nil
	f.create(acme, n)

	_, err := f.c.Resolve(f.in(acme), []string{sentry}, inB)
	f.refused("Resolve for a factory out of the scope", err, ports.ErrCredentialNotAllowed)
	f.expect(err != nil && strings.Contains(err.Error(), sentry), "the scope error does not name the credential: %v", err)

	_, err = f.c.Resolve(f.in(acme), []string{deploy}, inA)
	f.refused("Resolve of a credential with no factories", err, ports.ErrCredentialNotAllowed)

	f.expect(len(f.audit(acme, domain.AuditCredentialUsed)) == 0, "a refused Resolve was audited")

	one, err := f.c.Get(f.in(acme), sentry)
	f.expect(err == nil && one.LastUsedAt == nil, "a refused Resolve set last_used_at: %+v, %v", one, err)
}

func testAllScope(f *fixture) {
	acme := f.NewOrg(f.t)
	n := sentryIn(marker)
	n.Scope, n.Agents = domain.CredentialScope{All: true}, nil
	f.create(acme, n)

	later := domain.CredentialUse{Factory: "00000000-0000-7000-8000-0000000000c1", Run: run}
	for _, use := range []domain.CredentialUse{inA, inB, later} {
		f.expect(f.resolve(acme, use, sentry)[0].Value == marker, "an All scope does not allow factory %s", use.Factory)
	}
}

func testResolveUnknown(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	_, err := f.c.Resolve(f.in(acme), []string{sentry, "missing-one"}, inA)
	f.refused("Resolve of an unknown credential", err, ports.ErrNotFound)
	f.expect(err != nil && strings.Contains(err.Error(), "missing-one"), "the error does not name the unknown credential: %v", err)
	f.expect(len(f.audit(acme, domain.AuditCredentialUsed)) == 0, "a refused Resolve was audited")
}

func testOtherKey(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	_, err := f.Credentials(f.Key("k2")).Resolve(f.in(acme), []string{sentry}, inA)
	f.expect(err != nil, "a value sealed under k1 opened under k2 alone")

	if err != nil {
		f.noValue("another key's error", err.Error())
	}

	got, err := f.Credentials(f.Key("k2", "k1")).Resolve(f.in(acme), []string{sentry}, inA)
	f.expect(err == nil && len(got) == 1 && got[0].Value == marker, "k1 among the keys does not open it: %v", err)
}

func testDelete(f *fixture) {
	acme := f.NewOrg(f.t)
	got := f.create(acme, sentryIn(marker))

	if err := f.c.Delete(f.in(acme), sentry, editor); err != nil {
		f.t.Fatalf("Delete: %v", err)
	}

	_, err := f.c.Get(f.in(acme), sentry)
	f.refused("Get after Delete", err, ports.ErrNotFound)
	_, err = f.c.Resolve(f.in(acme), []string{sentry}, inA)
	f.refused("Resolve after Delete", err, ports.ErrNotFound)
	f.refused("Delete again", f.c.Delete(f.in(acme), sentry, editor), ports.ErrNotFound)

	e := f.one(acme, domain.AuditCredentialDeleted)
	f.expect(e.TargetID == string(got.ID) && e.Actor == editor, "credential.deleted = %+v", e)

	f.create(acme, sentryIn(replaced))
	f.expect(f.resolve(acme, inA, sentry)[0].Value == replaced, "a deleted name could not be created again")
}

func testIsolation(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	got, err := f.c.List(f.in(labs))
	f.expect(err == nil && len(got) == 0, "another org's List = %+v, %v; want nothing", got, err)

	_, err = f.c.Get(f.in(labs), sentry)
	f.refused("another org's Get", err, ports.ErrNotFound)
	_, err = f.c.Resolve(f.in(labs), []string{sentry}, inA)
	f.refused("another org's Resolve", err, ports.ErrNotFound)

	desc := "taken over"
	_, err = f.c.Patch(f.in(labs), sentry, domain.CredentialPatch{Description: &desc}, admin)
	f.refused("another org's Patch", err, ports.ErrNotFound)
	f.refused("another org's Delete", f.c.Delete(f.in(labs), sentry, admin), ports.ErrNotFound)

	preview, err := f.c.ImportPreview(f.in(labs), []string{sentry})
	f.expect(err == nil && len(preview) == 1 && !preview[0].Exists, "another org's ImportPreview = %+v, %v", preview, err)

	f.create(labs, sentryIn(labsVal))
	f.expect(f.resolve(acme, inA, sentry)[0].Value == marker, "acme's value changed when labs made its own")
	f.expect(len(f.audit(labs, domain.AuditCredentialCreated)) == 1, "labs sees acme's audit entries")
}

func testNoKey(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	none := f.Credentials(f.Key())
	n := sentryIn(replaced)
	n.Name = deploy

	_, err := none.Create(f.in(acme), n, admin)
	f.refused("Create without a master key", err, ports.ErrNoMasterKey)

	v := replaced
	_, err = none.Patch(f.in(acme), sentry, domain.CredentialPatch{Replace: &v}, admin)
	f.refused("Replace without a master key", err, ports.ErrNoMasterKey)

	_, err = none.Resolve(f.in(acme), []string{sentry}, inA)
	f.refused("Resolve without a master key", err, ports.ErrNoMasterKey)

	all, err := none.List(f.in(acme))
	f.expect(err == nil && len(all) == 1, "List without a master key = %+v, %v; metadata needs no key", all, err)
	f.expect(f.resolve(acme, inA, sentry)[0].Value == marker, "a refused Replace changed the value")
}

func testImportPreview(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	got, err := f.c.ImportPreview(f.in(acme), []string{deploy, sentry})
	if err != nil {
		f.t.Fatalf("ImportPreview: %v", err)
	}

	f.expect(len(got) == 2 && got[0] == domain.CredentialImportPreview{Name: deploy} &&
		got[1] == domain.CredentialImportPreview{Name: sentry, Exists: true}, "ImportPreview = %+v", got)

	all, err := f.c.List(f.in(acme))
	f.expect(err == nil && len(all) == 1, "ImportPreview stored something: %+v, %v", all, err)

	_, err = f.c.ImportPreview(f.in(acme), []string{"Bad_Name"})
	f.refused("ImportPreview of a bad name", err, domain.ErrInvalid)
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()
	desc := "x"

	_, err := f.c.List(ctx)
	f.refused("List", err, domain.ErrNoOrg)
	_, err = f.c.Get(ctx, sentry)
	f.refused("Get", err, domain.ErrNoOrg)
	_, err = f.c.Create(ctx, sentryIn(marker), admin)
	f.refused("Create", err, domain.ErrNoOrg)
	_, err = f.c.Patch(ctx, sentry, domain.CredentialPatch{Description: &desc}, admin)
	f.refused("Patch", err, domain.ErrNoOrg)
	f.refused("Delete", f.c.Delete(ctx, sentry, admin), domain.ErrNoOrg)
	_, err = f.c.Resolve(ctx, []string{sentry}, inA)
	f.refused("Resolve", err, domain.ErrNoOrg)
	_, err = f.c.ImportPreview(ctx, []string{sentry})
	f.refused("ImportPreview", err, domain.ErrNoOrg)
}
