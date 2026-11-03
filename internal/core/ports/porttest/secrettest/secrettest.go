// Package secrettest is the conformance suite for ports.OrgSecrets (ADR 0037 decision 1). The
// in-memory fake in porttest runs it, and the PostgreSQL adapter joins it with the store part, so
// tests on the fake hold for the database's rules: names unique per organization, write-only values
// sealed under the master key and bound to their row, the factory scope enforced by Resolve, an
// audit entry for every change and use, organizations kept apart, and no deletion of a secret that
// a connection refers to as {{name}}.
package secrettest

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

// Harness is one OrgSecrets under test.
type Harness struct {
	// Secrets returns the secrets under test, sealing with key; every call shares one store, as
	// goalds with different keys share one database.
	Secrets func(key ports.MasterKey) ports.OrgSecrets
	// Connections returns connections sealing with key in the secrets' store, so a connection can
	// refer to a secret.
	Connections func(key ports.MasterKey) ports.Connections
	// Key returns master keys with ids, the first active; the same id is the same key in every
	// call. With no ids it has none (ports.ErrNoMasterKey).
	Key func(ids ...string) ports.MasterKey
	// CopySealed copies the sealed value (key id, nonce and ciphertext) of secret from of
	// organization fromOrg onto secret to of organization toOrg, as someone with the database's
	// rows in hand would.
	CopySealed func(t *testing.T, fromOrg domain.OrgID, from string, toOrg domain.OrgID, to string)
	Audit      ports.Audit
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
		{"Resolve_CopiedCiphertextDoesNotOpen", testCopied},
		{"Delete_GoneAndAudited", testDelete},
		{"Delete_ReferencedIsAConflict", testDeleteReferenced},
		{"OtherOrg_SeesNothing", testIsolation},
		{"NoMasterKey_ValuesUnavailable", testNoKey},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t, s: h.Secrets(h.Key("k1"))}
			tc.test(f)
		})
	}
}

// Names and values the suite uses. marker, replaced and labsVal are values no Secret, error or
// audit entry may hold.
const (
	sentry = "sentry-token"
	agent  = "backend"
	deploy = "deploy-key"

	marker   = "sk-secret-conformance-marker"
	replaced = "sk-secret-conformance-replaced"
	labsVal  = "sk-secret-conformance-labs"

	factoryA = domain.FactoryID("00000000-0000-7000-8000-0000000000a1")
	factoryB = domain.FactoryID("00000000-0000-7000-8000-0000000000b1")
	run      = domain.RunID("00000000-0000-7000-8000-0000000000aa")
)

var (
	admin  = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}
	editor = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000002", Name: "Eve"}
	// inA is a run of factory A by its backend agent.
	inA = domain.SecretUse{Factory: factoryA, Agent: agent, Run: run}
	inB = domain.SecretUse{Factory: factoryB, Agent: agent, Run: run}
)

type fixture struct {
	Harness

	t *testing.T
	s ports.OrgSecrets
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

// sentryIn is secret sentry with value, scoped to factory A.
func sentryIn(value string) domain.NewSecret {
	expires := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)

	return domain.NewSecret{
		Name: sentry, Source: domain.SecretSourceValue, Description: "error reports",
		Scope: domain.SecretScope{Factories: []domain.FactoryID{factoryA}}, ExpiresAt: &expires, RemindDays: 14, Value: value,
	}
}

func (f *fixture) create(org domain.OrgID, n domain.NewSecret) domain.Secret {
	f.t.Helper()

	got, err := f.s.Create(f.in(org), n, admin)
	if err != nil {
		f.t.Fatalf("Create %s: %v", n.Name, err)
	}

	f.noValue("Create's secret", got)

	return got
}

func (f *fixture) resolve(org domain.OrgID, use domain.SecretUse, names ...string) []domain.ResolvedSecret {
	f.t.Helper()

	got, err := f.s.Resolve(f.in(org), names, use)
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

	f.expect(got.ID != "" && got.Name == sentry && got.Source == domain.SecretSourceValue && got.Description == "error reports",
		"Create = %+v", got)
	f.expect(got.ExpiresAt != nil && got.RemindDays == 14 && got.LastUsedAt == nil, "Create's expiry or last use = %+v", got)
	f.expect(got.CreatedBy == admin && got.UpdatedBy == admin && !got.CreatedAt.IsZero(), "Create's actors = %+v", got)
	f.expect(!got.Scope.All && len(got.Scope.Factories) == 1 && got.Scope.Factories[0] == factoryA, "Create's scope = %+v", got.Scope)

	one, err := f.s.Get(f.in(acme), sentry)
	f.expect(err == nil && one.ID == got.ID && one.Source == domain.SecretSourceValue && one.CreatedBy.ID == admin.ID,
		"Get = %+v, %v", one, err)
	f.noValue("Get's secret", one)

	all, err := f.s.List(f.in(acme))
	f.expect(err == nil && len(all) == 1 && all[0].ID == got.ID, "List = %+v, %v", all, err)
	f.noValue("List's secrets", all)

	e := f.one(acme, domain.AuditSecretCreated)
	f.expect(e.TargetKind == domain.AuditTargetSecret && e.TargetID == string(got.ID) && e.Actor == admin, "secret.created = %+v", e)

	opened := f.resolve(acme, inA, sentry)
	f.expect(len(opened) == 1 && opened[0].Value == marker, "Resolve does not open the value it was created with")
	f.noValue("Resolve's secrets, printed", opened)
}

func testDuplicate(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	_, err := f.s.Create(f.in(acme), sentryIn(replaced), editor)
	f.refused("Create of a taken name", err, ports.ErrNameTaken)

	f.create(labs, sentryIn(labsVal))
	f.expect(f.resolve(acme, inA, sentry)[0].Value == marker, "the refused Create changed the value")
}

func testCreateRules(f *fixture) {
	acme := f.NewOrg(f.t)

	for what, edit := range map[string]func(n *domain.NewSecret){
		"a bad name":            func(n *domain.NewSecret) { n.Name = "Bad_Name" },
		"an unknown source":     func(n *domain.NewSecret) { n.Source = "sign-in" },
		"a reminder, no expiry": func(n *domain.NewSecret) { n.ExpiresAt = nil },
		"a reminder of 91 days": func(n *domain.NewSecret) { n.RemindDays = 91 },
		"a long description":    func(n *domain.NewSecret) { n.Description = strings.Repeat("d", domain.MaxSecretDescription+1) },
		"no value":              func(n *domain.NewSecret) { n.Value = "" },
		"all and a list":        func(n *domain.NewSecret) { n.Scope.All = true },
	} {
		n := sentryIn(marker)
		edit(&n)

		_, err := f.s.Create(f.in(acme), n, admin)
		f.refused("Create with "+what, err, domain.ErrInvalid)
	}

	all, err := f.s.List(f.in(acme))
	f.expect(err == nil && len(all) == 0, "refused Creates stored %+v, %v", all, err)
	f.expect(len(f.audit(acme, domain.AuditSecretCreated)) == 0, "a refused Create was audited")
}

func testPatch(f *fixture) {
	acme := f.NewOrg(f.t)
	before := f.create(acme, sentryIn(marker))

	name, desc := "sentry-token-2027", "rotated yearly"

	got, err := f.s.Patch(f.in(acme), sentry, domain.SecretPatch{
		Name: &name, Description: &desc, Scope: &domain.SecretScope{Factories: []domain.FactoryID{factoryA, factoryB}},
		Expiry: &domain.SecretExpiry{},
	}, editor)
	if err != nil {
		f.t.Fatalf("Patch: %v", err)
	}

	f.noValue("Patch's secret", got)
	f.expect(got.ID == before.ID && got.Name == name && got.Description == desc && got.Source == domain.SecretSourceValue, "Patch = %+v", got)
	f.expect(got.ExpiresAt == nil && got.RemindDays == 0, "Patch did not clear the expiry: %+v", got)
	f.expect(len(got.Scope.Factories) == 2, "Patch's scope = %+v", got.Scope)
	f.expect(got.UpdatedBy == editor && got.CreatedBy == admin, "Patch's actors = %+v", got)

	_, err = f.s.Get(f.in(acme), sentry)
	f.refused("Get of the old name", err, ports.ErrNotFound)

	f.expect(f.resolve(acme, inB, name)[0].Value == marker, "a rename lost the value")
	f.one(acme, domain.AuditSecretUpdated)
	f.expect(len(f.audit(acme, domain.AuditSecretValueReplaced)) == 0, "a metadata Patch audited a replaced value")

	remind := domain.SecretExpiry{RemindDays: 3}
	_, err = f.s.Patch(f.in(acme), name, domain.SecretPatch{Expiry: &remind}, editor)
	f.refused("Patch to a reminder without an expiry", err, domain.ErrInvalid)

	_, err = f.s.Patch(f.in(acme), "missing-one", domain.SecretPatch{Description: &desc}, editor)
	f.refused("Patch of an unknown secret", err, ports.ErrNotFound)
}

func testRenameTaken(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	n := sentryIn(replaced)
	n.Name = deploy
	f.create(acme, n)

	taken := sentry
	_, err := f.s.Patch(f.in(acme), deploy, domain.SecretPatch{Name: &taken}, admin)
	f.refused("Patch to a taken name", err, ports.ErrNameTaken)
	f.expect(f.resolve(acme, inA, deploy)[0].Value == replaced, "the refused rename changed the value")
}

func testReplace(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	v := replaced

	got, err := f.s.Patch(f.in(acme), sentry, domain.SecretPatch{Replace: &v}, editor)
	if err != nil {
		f.t.Fatalf("Patch Replace: %v", err)
	}

	f.noValue("Replace's secret", got)
	f.expect(got.UpdatedBy == editor, "Replace's actor = %+v", got.UpdatedBy)
	f.expect(f.resolve(acme, inA, sentry)[0].Value == replaced, "Resolve does not open the replaced value")
	f.one(acme, domain.AuditSecretValueReplaced)

	empty := ""
	_, err = f.s.Patch(f.in(acme), sentry, domain.SecretPatch{Replace: &empty}, editor)
	f.refused("Replace with no value", err, domain.ErrInvalid)
}

func testResolve(f *fixture) {
	acme := f.NewOrg(f.t)
	created := f.create(acme, sentryIn(marker))

	n := sentryIn(replaced)
	n.Name, n.Scope = deploy, domain.SecretScope{All: true}
	f.create(acme, n)

	got := f.resolve(acme, inA, sentry, deploy, sentry)
	f.expect(len(got) == 2 && got[0].Secret.Name == sentry && got[0].Value == marker &&
		got[1].Secret.Name == deploy && got[1].Value == replaced, "Resolve = %v", got)
	f.expect(got[0].Secret.LastUsedAt != nil, "Resolve's secret has no last use")

	one, err := f.s.Get(f.in(acme), sentry)
	f.expect(err == nil && one.LastUsedAt != nil, "Resolve did not set last_used_at: %+v, %v", one, err)

	used := f.audit(acme, domain.AuditSecretUsed)
	f.expect(len(used) == 2, "%d secret.used entries; want one per secret", len(used))

	for _, e := range used {
		if e.TargetID != string(created.ID) {
			continue
		}

		f.expect(e.Actor == domain.EngineActor && e.TargetKind == domain.AuditTargetSecret, "secret.used = %+v", e)
		f.expect(e.Data["name"] == sentry && e.Data["run_id"] == string(run) && e.Data["factory_id"] == string(factoryA) && e.Data["agent"] == agent,
			"secret.used data = %+v", e.Data)
	}
}

func testScopeRefused(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	n := sentryIn(replaced)
	n.Name, n.Scope = deploy, domain.SecretScope{}
	f.create(acme, n)

	_, err := f.s.Resolve(f.in(acme), []string{sentry}, inB)
	f.refused("Resolve for a factory out of the scope", err, ports.ErrSecretNotAllowed)
	f.expect(err != nil && strings.Contains(err.Error(), sentry), "the scope error does not name the secret: %v", err)

	_, err = f.s.Resolve(f.in(acme), []string{deploy}, inA)
	f.refused("Resolve of a secret with no factories", err, ports.ErrSecretNotAllowed)

	f.expect(len(f.audit(acme, domain.AuditSecretUsed)) == 0, "a refused Resolve was audited")

	one, err := f.s.Get(f.in(acme), sentry)
	f.expect(err == nil && one.LastUsedAt == nil, "a refused Resolve set last_used_at: %+v, %v", one, err)
}

func testAllScope(f *fixture) {
	acme := f.NewOrg(f.t)
	n := sentryIn(marker)
	n.Scope = domain.SecretScope{All: true}
	f.create(acme, n)

	later := domain.SecretUse{Factory: "00000000-0000-7000-8000-0000000000c1", Run: run}
	for _, use := range []domain.SecretUse{inA, inB, later} {
		f.expect(f.resolve(acme, use, sentry)[0].Value == marker, "an All scope does not allow factory %s", use.Factory)
	}
}

func testResolveUnknown(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	_, err := f.s.Resolve(f.in(acme), []string{sentry, "missing-one"}, inA)
	f.refused("Resolve of an unknown secret", err, ports.ErrNotFound)
	f.expect(err != nil && strings.Contains(err.Error(), "missing-one"), "the error does not name the unknown secret: %v", err)
	f.expect(len(f.audit(acme, domain.AuditSecretUsed)) == 0, "a refused Resolve was audited")
}

func testOtherKey(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	_, err := f.Secrets(f.Key("k2")).Resolve(f.in(acme), []string{sentry}, inA)
	f.expect(err != nil, "a value sealed under k1 opened under k2 alone")

	if err != nil {
		f.noValue("another key's error", err.Error())
	}

	got, err := f.Secrets(f.Key("k2", "k1")).Resolve(f.in(acme), []string{sentry}, inA)
	f.expect(err == nil && len(got) == 1 && got[0].Value == marker, "k1 among the keys does not open it: %v", err)
}

// A ciphertext copied to another secret, of the same organization or another, does not open
// there, and Resolve's refusal holds no value: the associated data binds organization and id.
func testCopied(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	n := sentryIn(replaced)
	n.Name = deploy
	f.create(acme, n)
	f.create(labs, sentryIn(labsVal))

	for _, to := range []struct {
		what string
		org  domain.OrgID
		name string
	}{
		{"another secret of the organization", acme, deploy},
		{"a secret of another organization", labs, sentry},
	} {
		f.CopySealed(f.t, acme, sentry, to.org, to.name)

		got, err := f.s.Resolve(f.in(to.org), []string{to.name}, inA)
		f.expect(err != nil, "%s opened a copied ciphertext: %v", to.what, got)

		if err != nil {
			f.noValue(to.what+"'s refusal", err.Error())
		}
	}

	f.expect(f.resolve(acme, inA, sentry)[0].Value == marker, "the original no longer opens")
}

func testDelete(f *fixture) {
	acme := f.NewOrg(f.t)
	got := f.create(acme, sentryIn(marker))

	if err := f.s.Delete(f.in(acme), sentry, editor); err != nil {
		f.t.Fatalf("Delete: %v", err)
	}

	_, err := f.s.Get(f.in(acme), sentry)
	f.refused("Get after Delete", err, ports.ErrNotFound)
	_, err = f.s.Resolve(f.in(acme), []string{sentry}, inA)
	f.refused("Resolve after Delete", err, ports.ErrNotFound)
	f.refused("Delete again", f.s.Delete(f.in(acme), sentry, editor), ports.ErrNotFound)

	e := f.one(acme, domain.AuditSecretDeleted)
	f.expect(e.TargetID == string(got.ID) && e.Actor == editor, "secret.deleted = %+v", e)

	f.create(acme, sentryIn(replaced))
	f.expect(f.resolve(acme, inA, sentry)[0].Value == replaced, "a deleted name could not be created again")
}

// refKind is an organization's connection kind with a secret field that may refer to a secret.
func refKind() domain.ConnectionKind {
	return domain.ConnectionKind{Name: "acme-api", Title: "Acme API", Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: "key", Title: "Key", Secret: true, Required: true}},
		Env:    map[string]string{"ACME_KEY": "{{key}}"},
	}}
}

func testDeleteReferenced(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.create(acme, sentryIn(marker))

	conns := f.Connections(f.Key("k1"))
	if err := conns.PutKind(ctx, refKind(), admin); err != nil {
		f.t.Fatalf("PutKind: %v", err)
	}

	n := domain.NewConnection{Name: "a-conn", Kind: refKind().Name, Secrets: map[string]string{"key": "{{" + sentry + "}}"}}
	if _, err := conns.Create(ctx, n, admin); err != nil {
		f.t.Fatalf("Create connection: %v", err)
	}

	err := f.s.Delete(ctx, sentry, admin)
	f.refused("Delete of a referenced secret", err, ports.ErrConflict)
	f.expect(err != nil && strings.Contains(err.Error(), "a-conn"), "the conflict does not name the connection: %v", err)

	_, err = f.s.Get(ctx, sentry)
	f.expect(err == nil, "a refused Delete deleted the secret: %v", err)
	f.expect(len(f.audit(acme, domain.AuditSecretDeleted)) == 0, "a refused Delete was audited")

	if err := conns.Delete(ctx, "a-conn", admin); err != nil {
		f.t.Fatalf("Delete a-conn: %v", err)
	}

	f.expect(f.s.Delete(ctx, sentry, admin) == nil, "Delete of a secret no connection refers to was refused")
}

func testIsolation(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	got, err := f.s.List(f.in(labs))
	f.expect(err == nil && len(got) == 0, "another org's List = %+v, %v; want nothing", got, err)

	_, err = f.s.Get(f.in(labs), sentry)
	f.refused("another org's Get", err, ports.ErrNotFound)
	_, err = f.s.Resolve(f.in(labs), []string{sentry}, inA)
	f.refused("another org's Resolve", err, ports.ErrNotFound)

	desc := "taken over"
	_, err = f.s.Patch(f.in(labs), sentry, domain.SecretPatch{Description: &desc}, admin)
	f.refused("another org's Patch", err, ports.ErrNotFound)
	f.refused("another org's Delete", f.s.Delete(f.in(labs), sentry, admin), ports.ErrNotFound)

	f.create(labs, sentryIn(labsVal))
	f.expect(f.resolve(acme, inA, sentry)[0].Value == marker, "acme's value changed when labs made its own")
	f.expect(len(f.audit(labs, domain.AuditSecretCreated)) == 1, "labs sees acme's audit entries")
}

func testNoKey(f *fixture) {
	acme := f.NewOrg(f.t)
	f.create(acme, sentryIn(marker))

	none := f.Secrets(f.Key())
	n := sentryIn(replaced)
	n.Name = deploy

	_, err := none.Create(f.in(acme), n, admin)
	f.refused("Create without a master key", err, ports.ErrNoMasterKey)

	v := replaced
	_, err = none.Patch(f.in(acme), sentry, domain.SecretPatch{Replace: &v}, admin)
	f.refused("Replace without a master key", err, ports.ErrNoMasterKey)

	_, err = none.Resolve(f.in(acme), []string{sentry}, inA)
	f.refused("Resolve without a master key", err, ports.ErrNoMasterKey)

	all, err := none.List(f.in(acme))
	f.expect(err == nil && len(all) == 1, "List without a master key = %+v, %v; metadata needs no key", all, err)
	f.expect(f.resolve(acme, inA, sentry)[0].Value == marker, "a refused Replace changed the value")
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()
	desc := "x"

	_, err := f.s.List(ctx)
	f.refused("List", err, domain.ErrNoOrg)
	_, err = f.s.Get(ctx, sentry)
	f.refused("Get", err, domain.ErrNoOrg)
	_, err = f.s.Create(ctx, sentryIn(marker), admin)
	f.refused("Create", err, domain.ErrNoOrg)
	_, err = f.s.Patch(ctx, sentry, domain.SecretPatch{Description: &desc}, admin)
	f.refused("Patch", err, domain.ErrNoOrg)
	f.refused("Delete", f.s.Delete(ctx, sentry, admin), domain.ErrNoOrg)
	_, err = f.s.Resolve(ctx, []string{sentry}, inA)
	f.refused("Resolve", err, domain.ErrNoOrg)
}
