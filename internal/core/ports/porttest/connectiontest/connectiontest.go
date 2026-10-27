// Package connectiontest is the conformance suite for ports.Connections (spec 4 §5.3). The
// in-memory fake in porttest and the PostgreSQL adapter run it, so tests on the fake hold for the
// database's rules: kinds of the platform and of each organization, write-only secrets sealed
// under the master key, an audit entry in the same transaction as every change and every use,
// organizations kept apart, and resealing under a new key.
package connectiontest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one Connections under test.
type Harness struct {
	// Connections returns the connections under test, sealing with key; every call shares one
	// store, as goalds with different keys share one database.
	Connections func(key ports.MasterKey) ports.Connections
	// Key returns master keys with ids, the first active; the same id is the same key in every
	// call. With no ids it has none (ports.ErrNoMasterKey).
	Key   func(ids ...string) ports.MasterKey
	Audit ports.Audit
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// Rekey reseals every organization's connections under key's active key, as goal db rekey
	// does, and returns how many it resealed.
	Rekey func(ctx context.Context, key ports.MasterKey) (int, error)
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"Kinds_PlatformAndOrgKinds", testKinds},
		{"PutKind_PlatformNameTakenChangesAudited", testPutKind},
		{"PutKind_SetFieldTurnedSecretIsAConflict", testPutKindMakesASetFieldSecret},
		{"DeleteKind_UsedIsAConflict", testDeleteKind},
		{"SyncPlatformKinds_ReplacesAndKeepsUsed", testSync},
		{"Create_SecretsNeverReturned", testCreate},
		{"Create_RulesRefused", testCreateRules},
		{"Create_SecretReferenceStoredUnsealed", testCreateReference},
		{"Create_PartialReferenceRefused", testPartialReference},
		{"Update_SecretMovesBetweenSealedAndReference", testMoveReference},
		{"Resolve_ReturnsReferencesUnopened", testResolveReferences},
		{"Update_FieldsAndDescription", testUpdate},
		{"Update_ReplaceSecret", testReplaceSecret},
		{"Update_RenameKeepsSecrets", testRename},
		{"Open_ValuesWithoutAUse", testOpen},
		{"Resolve_OpensAuditsAndMarksUse", testResolve},
		{"Resolve_AuditsAMergeByItsPerson", testResolveForAMerge},
		{"RecordTest_StoredAndAudited", testRecordTest},
		{"Delete_GoneAndAudited", testDelete},
		{"OtherOrg_SeesNothing", testIsolation},
		{"NoMasterKey_SecretsUnavailable", testNoKey},
		{"Rekey_ResealsEveryOrg", testRekey},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t, c: h.Connections(h.Key("k1"))}
			f.syncKinds(gitKind(), docsKind(), cloudKind())
			tc.test(f)
		})
	}
}

// Names and values the suite uses.
const (
	gitName   = "acme-git"
	docsName  = "acme-docs"
	cloudName = "acme-cloud"
	ownName   = "acme-api"

	connGit   = "git"
	connDocs  = "docs"
	connCloud = "cloud"
	connOther = "other"

	fieldToken  = "token"
	fieldURL    = "url"
	fieldAPIKey = "api_key"
	fieldAppKey = "app_key"
	fieldSite   = "site"

	sealedValue = "ghp_conformance-sealed-value"
	labsValue   = "labs-sealed-value"
	apiValue    = "api-sealed-value"
	docsURL     = "https://docs.example.com/mcp"
	// gitCred and apiCred are credentials connections refer to as {{name}}.
	gitCred = "git-token"
	apiCred = "api-cred"
	// appValue is app_key's value once it is sealed again.
	appValue = "app-2"
	euSite   = "eu.example.com"
	run      = domain.RunID("00000000-0000-7000-8000-0000000000aa")
)

var (
	admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}
	// forRun is a run's use of connections.
	forRun = domain.ConnectionUse{Run: run}
	editor = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000002", Name: "Eve"}
)

// gitKind is a platform kind like github: a secret token and a host with a default.
func gitKind() domain.ConnectionKind {
	return domain.ConnectionKind{Name: gitName, Title: "Acme Git", Platform: true, Spec: domain.KindSpec{
		Fields: []domain.KindField{
			{Name: fieldToken, Title: "Token", Secret: true, Required: true},
			{Name: "host", Title: "Host", Default: "git.example.com"},
		},
		Env:      map[string]string{"GIT_TOKEN": "{{token}}", "GIT_HOST": "{{host}}"},
		Git:      &domain.KindGit{Host: "{{host}}", Username: "x-access-token", PasswordEnv: "GIT_TOKEN"},
		CodeHost: "github",
	}}
}

// docsKind is a platform kind without secrets: a remote MCP server.
func docsKind() domain.ConnectionKind {
	return domain.ConnectionKind{Name: docsName, Title: "Acme Docs", Platform: true, Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: fieldURL, Title: "URL", Required: true}},
		MCP:    &domain.KindMCP{URL: "{{url}}"},
	}}
}

// cloudKind is a platform kind with two secrets.
func cloudKind() domain.ConnectionKind {
	return domain.ConnectionKind{Name: cloudName, Title: "Acme Cloud", Platform: true, Spec: domain.KindSpec{
		Fields: []domain.KindField{
			{Name: fieldAPIKey, Title: "API key", Secret: true, Required: true},
			{Name: fieldAppKey, Title: "App key", Secret: true},
			{Name: fieldSite, Title: "Site", Default: "example.com"},
		},
		Env: map[string]string{"CLOUD_API_KEY": "{{api_key}}", "CLOUD_APP_KEY": "{{app_key}}", "CLOUD_SITE": "{{site}}"},
	}}
}

// ownKind is an organization's kind.
func ownKind(docs string) domain.ConnectionKind {
	return domain.ConnectionKind{Name: ownName, Title: "Acme API", Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: "key", Title: "Key", Secret: true, Required: true}},
		Env:    map[string]string{"ACME_KEY": "{{key}}"},
		Docs:   docs,
	}}
}

// fixture is one test's harness and its connections under key k1. Its methods fail the test on
// unexpected errors.
type fixture struct {
	Harness

	t *testing.T
	c ports.Connections
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

func (f *fixture) syncKinds(kinds ...domain.ConnectionKind) {
	f.t.Helper()

	if err := f.c.SyncPlatformKinds(f.t.Context(), kinds); err != nil {
		f.t.Fatalf("SyncPlatformKinds: %v", err)
	}
}

func (f *fixture) create(org domain.OrgID, n domain.NewConnection) domain.Connection {
	f.t.Helper()

	got, err := f.c.Create(f.in(org), n, admin)
	if err != nil {
		f.t.Fatalf("Create %s: %v", n.Name, err)
	}

	return got
}

func (f *fixture) git(org domain.OrgID, value string) domain.Connection {
	f.t.Helper()

	return f.create(org, domain.NewConnection{
		Name: connGit, Kind: gitName, Description: "the repositories", Secrets: map[string]string{fieldToken: value},
	})
}

func (f *fixture) docs(org domain.OrgID) domain.Connection {
	f.t.Helper()

	return f.create(org, domain.NewConnection{Name: connDocs, Kind: docsName, Fields: map[string]string{fieldURL: docsURL}})
}

func (f *fixture) get(org domain.OrgID, name string) domain.Connection {
	f.t.Helper()

	got, err := f.c.Get(f.in(org), name)
	if err != nil {
		f.t.Fatalf("Get %s: %v", name, err)
	}

	return got
}

func (f *fixture) resolve(org domain.OrgID, names ...string) []domain.ResolvedConnection {
	f.t.Helper()

	got, err := f.c.Resolve(f.in(org), names, forRun)
	if err != nil {
		f.t.Fatalf("Resolve %v: %v", names, err)
	}

	return got
}

// values are the named connection's values as a run gets them.
func (f *fixture) values(org domain.OrgID, name string) map[string]string {
	f.t.Helper()

	return f.resolve(org, name)[0].Values
}

// audit is org's entries of action, newest first.
func (f *fixture) audit(org domain.OrgID, action string) []domain.AuditEntry {
	f.t.Helper()

	got, err := f.Audit.AuditLog(f.in(org), ports.AuditQuery{Limit: 100, Action: action})
	if err != nil {
		f.t.Fatal(err)
	}

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

// noSecret fails when v, printed, holds one of secrets.
func (f *fixture) noSecret(what string, v any, secrets ...string) {
	f.t.Helper()

	printed := fmt.Sprintf("%+v", v)
	for _, s := range secrets {
		if strings.Contains(printed, s) {
			f.t.Errorf("%s holds a secret value: %s", what, printed)
		}
	}
}

// expect fails the test with the message unless ok.
func (f *fixture) expect(ok bool, format string, args ...any) {
	f.t.Helper()

	if !ok {
		f.t.Errorf(format, args...)
	}
}

func (f *fixture) refused(what string, err, want error) {
	f.t.Helper()

	if !errors.Is(err, want) {
		f.t.Errorf("%s = %v; want %v", what, err, want)
	}
}

// kindNames are the names of the kinds org sees; each but ownName is the platform's.
func (f *fixture) kindNames(org domain.OrgID) []string {
	f.t.Helper()

	kinds, err := f.c.Kinds(f.in(org))
	if err != nil {
		f.t.Fatal(err)
	}

	out := make([]string, 0, len(kinds))

	for _, k := range kinds {
		f.expect(k.Platform == (k.Name != ownName), "kind %s: Platform %v", k.Name, k.Platform)
		out = append(out, k.Name)
	}

	return out
}

func testKinds(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)

	if err := f.c.PutKind(f.in(acme), ownKind("docs"), admin); err != nil {
		f.t.Fatal(err)
	}

	got, want := f.kindNames(acme), []string{ownName, cloudName, docsName, gitName}
	f.expect(slices.Equal(got, want), "Kinds of acme = %v; want %v", got, want)

	got, want = f.kindNames(labs), []string{cloudName, docsName, gitName}
	f.expect(slices.Equal(got, want), "Kinds of another org = %v; want the platform's only, %v", got, want)

	git, err := f.c.Kind(f.in(acme), gitName)
	if err != nil {
		f.t.Fatal(err)
	}

	f.expect(reflect.DeepEqual(git, gitKind()), "Kind(%s) = %+v; want the platform kind as synced", gitName, git)

	own, err := f.c.Kind(f.in(acme), ownName)
	f.expect(err == nil && !own.Platform && own.Spec.Docs == "docs", "Kind(%s) = %+v, %v; want acme's own kind", ownName, own, err)

	_, err = f.c.Kind(f.in(labs), ownName)
	f.refused("Kind of another org's kind", err, ports.ErrNotFound)
}

func testPutKind(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)

	taken := ownKind("")
	taken.Name = gitName
	f.refused("PutKind with a platform kind's name", f.c.PutKind(ctx, taken, admin), ports.ErrNameTaken)

	bad := ownKind("")
	bad.Spec.Env["ACME_KEY"] = "{{nope}}"
	f.refused("PutKind of an invalid kind", f.c.PutKind(ctx, bad, admin), domain.ErrInvalid)

	for _, docs := range []string{"one", "one", "two"} {
		if err := f.c.PutKind(ctx, ownKind(docs), admin); err != nil {
			f.t.Fatalf("PutKind: %v", err)
		}
	}

	k, err := f.c.Kind(ctx, ownName)
	f.expect(err == nil && k.Spec.Docs == "two", "Kind after its change = %+v, %v; want docs two", k, err)

	// An unchanged put records nothing.
	created := f.one(acme, domain.AuditConnectionKindCreated)
	f.one(acme, domain.AuditConnectionKindUpdated)
	f.expect(created.Data["name"] == ownName && created.Actor.ID == admin.ID && created.TargetKind == domain.AuditTargetConnectionKind,
		"connection_kind.created = %+v", created)
}

// A kind change that makes a field secret while a connection holds it as a plain value is refused:
// the value would stay readable by every member and win over a sealed one. Once the connection
// no longer holds it, the change goes through.
func testPutKindMakesASetFieldSecret(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)

	plain := ownKind("")
	plain.Spec.Fields = append(plain.Spec.Fields, domain.KindField{Name: fieldSite, Title: "Site"})
	plain.Spec.Env["ACME_SITE"] = "{{site}}"

	if err := f.c.PutKind(ctx, plain, admin); err != nil {
		f.t.Fatal(err)
	}

	f.create(acme, domain.NewConnection{Name: connOther, Kind: ownName, Fields: map[string]string{fieldSite: euSite}, Secrets: map[string]string{"key": "k"}})

	secret := ownKind("")
	secret.Spec.Fields = append(secret.Spec.Fields, domain.KindField{Name: fieldSite, Title: "Site", Secret: true})
	secret.Spec.Env["ACME_SITE"] = "{{site}}"

	err := f.c.PutKind(ctx, secret, admin)
	f.refused("PutKind making a set field secret", err, ports.ErrConflict)
	f.expect(err != nil && strings.Contains(err.Error(), fieldSite) && strings.Contains(err.Error(), connOther),
		"PutKind error %v; want it to name the field and the connection", err)

	k, err := f.c.Kind(ctx, ownName)
	f.expect(err == nil && !k.Spec.Fields[1].Secret, "Kind after the refused change = %+v, %v; want site still plain", k, err)

	if _, err := f.c.Update(ctx, connOther, domain.ConnectionPatch{Fields: map[string]string{}}, admin); err != nil {
		f.t.Fatal(err)
	}

	if err := f.c.PutKind(ctx, secret, admin); err != nil {
		f.t.Fatalf("PutKind once no connection holds the field: %v", err)
	}
}

func testDeleteKind(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)

	if err := f.c.PutKind(ctx, ownKind(""), admin); err != nil {
		f.t.Fatal(err)
	}

	f.create(acme, domain.NewConnection{Name: connOther, Kind: ownName, Secrets: map[string]string{"key": "k"}})

	f.refused("DeleteKind while used", f.c.DeleteKind(ctx, ownName, admin), ports.ErrConflict)
	f.refused("DeleteKind of a platform kind", f.c.DeleteKind(ctx, gitName, admin), ports.ErrNotFound)
	f.refused("DeleteKind of no kind", f.c.DeleteKind(ctx, "nothing", admin), ports.ErrNotFound)

	if err := f.c.Delete(ctx, connOther, admin); err != nil {
		f.t.Fatal(err)
	}

	if err := f.c.DeleteKind(ctx, ownName, admin); err != nil {
		f.t.Fatalf("DeleteKind once unused: %v", err)
	}

	_, err := f.c.Kind(ctx, ownName)
	f.refused("Kind after DeleteKind", err, ports.ErrNotFound)

	e := f.one(acme, domain.AuditConnectionKindDeleted)
	f.expect(e.Data["name"] == ownName, "connection_kind.deleted = %+v", e)
}

func testSync(f *fixture) {
	acme := f.NewOrg(f.t)
	f.git(acme, sealedValue)

	changed := docsKind()
	changed.Title = "Acme Docs 2"

	// git is used, so it stays though no longer listed; cloud is not, so it goes.
	f.syncKinds(changed)
	f.syncKinds(changed)

	got, want := f.kindNames(acme), []string{docsName, gitName}
	f.expect(slices.Equal(got, want), "Kinds after a sync without git and cloud = %v; want %v", got, want)

	k, err := f.c.Kind(f.in(acme), docsName)
	f.expect(err == nil && k.Title == "Acme Docs 2", "Kind after its change = %+v, %v", k, err)

	v := f.values(acme, connGit)
	f.expect(v[fieldToken] == sealedValue, "a connection of a kind the platform dropped resolves to %v", v)
}

func testCreate(f *fixture) {
	acme := f.NewOrg(f.t)

	got := f.create(acme, domain.NewConnection{
		Name: connCloud, Kind: cloudName, Description: "metrics", Fields: map[string]string{fieldSite: euSite},
		Secrets: map[string]string{fieldAPIKey: sealedValue, fieldAppKey: ""},
	})

	want := domain.Connection{
		ID: got.ID, Name: connCloud, Kind: cloudName, Description: "metrics", Fields: map[string]string{fieldSite: euSite},
		SecretFields: []string{fieldAPIKey}, CreatedBy: got.CreatedBy, CreatedAt: got.CreatedAt, UpdatedBy: got.UpdatedBy, UpdatedAt: got.UpdatedAt,
	}
	f.expect(got.ID != "" && !got.CreatedAt.IsZero() && fmt.Sprint(got) == fmt.Sprint(want), "Create = %+v; want %+v", got, want)
	f.expect(got.CreatedBy.ID == admin.ID && got.UpdatedBy.ID == admin.ID, "Create's actors = %+v, %+v", got.CreatedBy, got.UpdatedBy)
	f.noSecret("Create", got, sealedValue)

	listed, err := f.c.List(f.in(acme))
	f.expect(err == nil && len(listed) == 1 && listed[0].ID == got.ID, "List = %+v, %v; want the connection", listed, err)
	f.noSecret("List", listed, sealedValue)

	one := f.get(acme, connCloud)
	f.expect(one.ID == got.ID && one.Fields[fieldSite] == euSite && one.LastUsedAt == nil && one.LastTest == nil, "Get = %+v", one)
	f.noSecret("Get", one, sealedValue)

	e := f.one(acme, domain.AuditConnectionCreated)
	f.expect(e.TargetKind == domain.AuditTargetConnection && e.TargetID == string(got.ID) && e.Data["kind"] == cloudName,
		"connection.created = %+v", e)
	f.noSecret("the audit entry", e, sealedValue)

	docs := f.docs(acme)
	f.expect(len(docs.SecretFields) == 0, "a connection without secrets has secret fields %v", docs.SecretFields)
}

func testCreateRules(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.git(acme, sealedValue)

	secret := map[string]string{fieldToken: "t"}
	tests := []struct {
		name string
		n    domain.NewConnection
		want error
	}{
		{"bad name", domain.NewConnection{Name: "Git", Kind: gitName, Secrets: secret}, domain.ErrInvalid},
		{"name taken", domain.NewConnection{Name: connGit, Kind: gitName, Secrets: secret}, ports.ErrNameTaken},
		{"unknown kind", domain.NewConnection{Name: connOther, Kind: "nothing"}, ports.ErrNotFound},
		{"required secret missing", domain.NewConnection{Name: connOther, Kind: gitName}, domain.ErrInvalid},
		{"secret as a field", domain.NewConnection{Name: connOther, Kind: gitName, Fields: secret, Secrets: secret}, domain.ErrInvalid},
		{"field as a secret", domain.NewConnection{Name: connOther, Kind: gitName, Secrets: map[string]string{fieldToken: "t", "host": "h"}}, domain.ErrInvalid},
		{"unknown field", domain.NewConnection{Name: connOther, Kind: docsName, Fields: map[string]string{fieldURL: "u", "org": "acme"}}, domain.ErrInvalid},
		{"long description", domain.NewConnection{
			Name: connOther, Kind: docsName, Fields: map[string]string{fieldURL: "u"}, Description: strings.Repeat("x", domain.MaxConnectionDescription+1),
		}, domain.ErrInvalid},
	}

	for _, tt := range tests {
		_, err := f.c.Create(ctx, tt.n, admin)
		f.refused("Create with "+tt.name, err, tt.want)
	}

	got, err := f.c.List(ctx)
	f.expect(err == nil && len(got) == 1, "List after refusals = %+v, %v; want only git", got, err)
	f.one(acme, domain.AuditConnectionCreated)
}

// ref is a secret value that refers to credential name.
func ref(name string) string { return "{{" + name + "}}" }

func testCreateReference(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)

	// Only references: nothing to seal, so no master key is needed.
	got, err := f.Connections(f.Key()).Create(ctx, domain.NewConnection{Name: connGit, Kind: gitName, Secrets: map[string]string{fieldToken: ref(gitCred)}}, admin)
	if err != nil {
		f.t.Fatalf("Create with a reference without a master key = %v", err)
	}

	f.expect(slices.Equal(got.SecretFields, []string{fieldToken}) && reflect.DeepEqual(got.SecretRefs, map[string]string{fieldToken: gitCred}),
		"Create with a reference = %+v; want token set and referring to %s", got, gitCred)

	one := f.get(acme, connGit)
	f.expect(reflect.DeepEqual(one.SecretRefs, map[string]string{fieldToken: gitCred}), "Get's references = %v", one.SecretRefs)

	listed, err := f.c.List(ctx)
	f.expect(err == nil && len(listed) == 1 && listed[0].SecretRefs[fieldToken] == gitCred, "List = %+v, %v", listed, err)

	// A reference beside a sealed value.
	mixed := f.create(acme, domain.NewConnection{Name: connCloud, Kind: cloudName, Secrets: map[string]string{fieldAPIKey: ref(apiCred), fieldAppKey: sealedValue}})
	f.expect(slices.Equal(mixed.SecretFields, []string{fieldAPIKey, fieldAppKey}) && reflect.DeepEqual(mixed.SecretRefs, map[string]string{fieldAPIKey: apiCred}),
		"Create with a reference and a sealed value = %+v", mixed)
	f.noSecret("Create", mixed, sealedValue)

	_, err = f.Connections(f.Key()).Open(ctx, connCloud)
	f.refused("Open of a connection with a sealed value without a master key", err, ports.ErrNoMasterKey)

	docs := f.docs(acme)
	f.expect(len(docs.SecretRefs) == 0, "a connection without secrets has references %v", docs.SecretRefs)
}

func testPartialReference(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.create(acme, domain.NewConnection{Name: connCloud, Kind: cloudName, Secrets: map[string]string{fieldAPIKey: apiValue}})

	refused := map[string]domain.NewConnection{
		"an embedded reference": {Name: connGit, Kind: gitName, Secrets: map[string]string{fieldToken: "Bearer " + ref(gitCred) + sealedValue}},
		"two references":        {Name: connGit, Kind: gitName, Secrets: map[string]string{fieldToken: ref("a-key") + ref("b-key")}},
		"a reference in a field": {
			Name: connGit, Kind: gitName, Fields: map[string]string{"host": ref(gitCred)}, Secrets: map[string]string{fieldToken: sealedValue},
		},
	}

	for what, n := range refused {
		_, err := f.c.Create(ctx, n, admin)
		f.refused("Create with "+what, err, domain.ErrInvalid)

		if err != nil {
			f.noSecret("the error of Create with "+what, err, sealedValue)
		}

		_, err = f.c.Update(ctx, connCloud, domain.ConnectionPatch{Fields: n.Fields, Secrets: map[string]string{fieldAppKey: n.Secrets[fieldToken]}}, admin)
		f.refused("Update with "+what, err, domain.ErrInvalid)
	}

	_, err := f.c.Get(ctx, connGit)
	f.refused("Get of a refused connection", err, ports.ErrNotFound)

	got := f.get(acme, connCloud)
	f.expect(slices.Equal(got.SecretFields, []string{fieldAPIKey}) && len(got.SecretRefs) == 0 && len(got.Fields) == 0,
		"a refused update changed the connection: %+v", got)
	f.one(acme, domain.AuditConnectionCreated)
}

func testMoveReference(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.create(acme, domain.NewConnection{Name: connCloud, Kind: cloudName, Secrets: map[string]string{fieldAPIKey: apiValue, fieldAppKey: "app-1"}})

	update := func(secrets map[string]string, fields []string, refs map[string]string) {
		f.t.Helper()

		got, err := f.c.Update(ctx, connCloud, domain.ConnectionPatch{Secrets: secrets}, admin)
		if err != nil {
			f.t.Fatalf("Update %v: %v", secrets, err)
		}

		f.expect(slices.Equal(got.SecretFields, fields) && reflect.DeepEqual(got.SecretRefs, refs),
			"Update %v = fields %v, references %v; want %v, %v", secrets, got.SecretFields, got.SecretRefs, fields, refs)
		f.noSecret("Update", got, apiValue, "app-1", appValue)
	}

	both := []string{fieldAPIKey, fieldAppKey}

	update(map[string]string{fieldAppKey: ref(apiCred)}, both, map[string]string{fieldAppKey: apiCred})

	v := f.values(acme, connCloud)
	f.expect(v[fieldAPIKey] == apiValue && v[fieldAppKey] == "", "values after app_key became a reference = %v; want only api_key", v)

	update(map[string]string{fieldAPIKey: ref(gitCred)}, both, map[string]string{fieldAPIKey: gitCred, fieldAppKey: apiCred})

	// Nothing is sealed any more: the connection opens without a master key and holds no secret.
	opened, err := f.Connections(f.Key()).Open(ctx, connCloud)
	f.expect(err == nil && opened.Values[fieldAPIKey] == "" && opened.Values[fieldAppKey] == "",
		"Open of a reference-only connection without a master key = %+v, %v", opened, err)

	update(map[string]string{fieldAppKey: appValue}, both, map[string]string{fieldAPIKey: gitCred})

	v = f.values(acme, connCloud)
	f.expect(v[fieldAppKey] == appValue && v[fieldAPIKey] == "", "values after app_key was sealed again = %v", v)

	update(map[string]string{fieldAppKey: ""}, []string{fieldAPIKey}, map[string]string{fieldAPIKey: gitCred})

	_, err = f.c.Update(ctx, connCloud, domain.ConnectionPatch{Secrets: map[string]string{fieldAPIKey: ""}}, admin)
	f.refused("clearing a required reference", err, domain.ErrInvalid)

	f.noSecret("the audit entries", f.audit(acme, domain.AuditConnectionSecretReplaced), apiValue, appValue)
}

func testResolveReferences(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.create(acme, domain.NewConnection{Name: connCloud, Kind: cloudName, Secrets: map[string]string{fieldAPIKey: ref(apiCred), fieldAppKey: sealedValue}})

	want := map[string]string{fieldAPIKey: apiCred}

	got := f.resolve(acme, connCloud)
	f.expect(len(got) == 1 && reflect.DeepEqual(got[0].SecretRefs, want) && got[0].Connection.SecretRefs[fieldAPIKey] == apiCred,
		"Resolve's references = %+v; want %v", got, want)
	f.expect(got[0].Values[fieldAppKey] == sealedValue, "Resolve's values = %v; want app_key opened", got[0].Values)

	_, held := got[0].Values[fieldAPIKey]
	f.expect(!held, "Resolve put a reference in Values: %v", got[0].Values)

	opened, err := f.c.Open(ctx, connCloud)
	f.expect(err == nil && reflect.DeepEqual(opened.SecretRefs, want) && opened.Values[fieldAppKey] == sealedValue,
		"Open = %+v, %v; want the reference unopened and app_key opened", opened, err)
}

func testUpdate(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.create(acme, domain.NewConnection{Name: connCloud, Kind: cloudName, Secrets: map[string]string{fieldAPIKey: apiValue, fieldAppKey: "app-1"}})

	desc := "the metrics account"

	got, err := f.c.Update(ctx, connCloud, domain.ConnectionPatch{Description: &desc, Fields: map[string]string{fieldSite: euSite}}, editor)
	if err != nil {
		f.t.Fatal(err)
	}

	f.expect(got.Description == desc && got.Fields[fieldSite] == euSite && got.UpdatedBy.ID == editor.ID,
		"Update of description and fields = %+v", got)
	f.expect(slices.Equal(got.SecretFields, []string{fieldAPIKey, fieldAppKey}), "secret fields after an update of fields = %v", got.SecretFields)
	f.expect(f.get(acme, connCloud).Description == desc, "Get after Update has the old description")

	_, err = f.c.Update(ctx, "nothing", domain.ConnectionPatch{Description: &desc}, admin)
	f.refused("Update of no connection", err, ports.ErrNotFound)

	e := f.one(acme, domain.AuditConnectionUpdated)
	f.expect(e.Actor.ID == editor.ID && slices.Equal(anyStrings(e.Data["fields"]), []string{fieldSite}), "connection.updated = %+v", e)
}

func testReplaceSecret(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.create(acme, domain.NewConnection{Name: connCloud, Kind: cloudName, Secrets: map[string]string{fieldAPIKey: apiValue, fieldAppKey: "app-1"}})

	got, err := f.c.Update(ctx, connCloud, domain.ConnectionPatch{Secrets: map[string]string{fieldAppKey: "app-2"}}, admin)
	f.expect(err == nil && slices.Equal(got.SecretFields, []string{fieldAPIKey, fieldAppKey}), "Update of one secret = %+v, %v", got, err)
	f.noSecret("Update", got, "app-2", apiValue)

	v := f.values(acme, connCloud)
	f.expect(v[fieldAPIKey] == apiValue && v[fieldAppKey] == "app-2", "values after replacing app_key = %v; want api_key kept", v)

	got, err = f.c.Update(ctx, connCloud, domain.ConnectionPatch{Secrets: map[string]string{fieldAppKey: ""}}, admin)
	f.expect(err == nil && slices.Equal(got.SecretFields, []string{fieldAPIKey}), "clearing an optional secret = %+v, %v", got, err)

	_, err = f.c.Update(ctx, connCloud, domain.ConnectionPatch{Secrets: map[string]string{fieldAPIKey: ""}}, admin)
	f.refused("clearing a required secret", err, domain.ErrInvalid)

	_, err = f.c.Update(ctx, connCloud, domain.ConnectionPatch{Secrets: map[string]string{fieldSite: "x"}}, admin)
	f.refused("a field as a secret", err, domain.ErrInvalid)

	f.expect(f.values(acme, connCloud)[fieldAPIKey] == apiValue, "a refused update changed the secrets")

	replaced := f.audit(acme, domain.AuditConnectionSecretReplaced)
	f.expect(len(replaced) == 2 && slices.Equal(anyStrings(replaced[1].Data["secrets"]), []string{fieldAppKey}),
		"connection.secret_replaced = %+v; want two naming app_key", replaced)
	f.expect(len(f.audit(acme, domain.AuditConnectionUpdated)) == 0, "a secret's replacement was audited as connection.updated")
	f.noSecret("the audit entries", replaced, "app-2", apiValue)
}

func testRename(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	git := f.git(acme, sealedValue)
	f.docs(acme)

	name := "repos"

	got, err := f.c.Update(ctx, connGit, domain.ConnectionPatch{Name: &name}, editor)
	f.expect(err == nil && got.Name == name && got.ID == git.ID, "Update of the name = %+v, %v", got, err)
	f.expect(f.values(acme, name)[fieldToken] == sealedValue, "the renamed connection's secret does not open")

	_, err = f.c.Get(ctx, connGit)
	f.refused("Get of the old name", err, ports.ErrNotFound)

	taken := connDocs
	_, err = f.c.Update(ctx, name, domain.ConnectionPatch{Name: &taken}, admin)
	f.refused("a rename to a taken name", err, ports.ErrNameTaken)

	bad := "Repos"
	_, err = f.c.Update(ctx, name, domain.ConnectionPatch{Name: &bad}, admin)
	f.refused("a rename to a bad name", err, domain.ErrInvalid)

	e := f.one(acme, domain.AuditConnectionUpdated)
	f.expect(e.Data["name"] == name && e.Data["from"] == connGit && e.TargetID == string(git.ID), "connection.updated of a rename = %+v", e)
}

func testOpen(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.git(acme, sealedValue)

	got, err := f.c.Open(f.in(acme), connGit)
	f.expect(err == nil && got.Values[fieldToken] == sealedValue && got.Kind.Name == gitName, "Open = %+v, %v; want its token and kind", got, err)
	f.expect(len(f.audit(acme, domain.AuditConnectionUsed)) == 0 && f.get(acme, connGit).LastUsedAt == nil,
		"Open recorded a use: a test is not a run's use")

	_, err = f.c.Open(f.in(acme), "nothing")
	f.refused("Open of no connection", err, ports.ErrNotFound)

	_, err = f.c.Open(f.in(labs), connGit)
	f.refused("another org's Open", err, ports.ErrNotFound)

	_, err = f.Connections(f.Key()).Open(f.in(acme), connGit)
	f.refused("Open without a master key", err, ports.ErrNoMasterKey)
}

// anyStrings is a []string as an audit entry's data holds it.
func anyStrings(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}

	var out []string

	list, _ := v.([]any)
	for _, e := range list {
		if str, ok := e.(string); ok {
			out = append(out, str)
		}
	}

	return out
}

func testResolve(f *fixture) {
	acme := f.NewOrg(f.t)
	git := f.git(acme, sealedValue)
	f.docs(acme)

	got := f.resolve(acme, connDocs, connGit, connDocs)
	if len(got) != 2 || got[0].Connection.Name != connDocs || got[1].Connection.Name != connGit {
		f.t.Fatalf("Resolve = %+v; want docs then git, once each", got)
	}

	f.expect(got[1].Values[fieldToken] == sealedValue && got[1].Kind.Name == gitName && got[1].Kind.Platform,
		"Resolve git = %+v; want its token and kind", got[1])

	rendered, err := got[1].Kind.Render(got[1].Values, nil)
	f.expect(err == nil && rendered.Env["GIT_TOKEN"] == sealedValue && rendered.Env["GIT_HOST"] == "git.example.com",
		"Render of the resolved git = %+v, %v", rendered, err)

	used := f.audit(acme, domain.AuditConnectionUsed)
	f.expect(len(used) == 2 && used[0].Data["run_id"] == string(run) && used[0].Actor == domain.EngineActor,
		"connection.used = %+v; want one per connection naming the run", used)
	f.noSecret("connection.used", used, sealedValue)

	after := f.get(acme, connGit)
	f.expect(after.LastUsedAt != nil && !after.LastUsedAt.Before(git.CreatedAt.Add(-time.Minute)), "Get after Resolve = %+v; want last used set", after)

	_, err = f.c.Resolve(f.in(acme), []string{connGit, "nothing"}, forRun)
	f.refused("Resolve of an unknown connection", err, ports.ErrNotFound)
	f.expect(len(f.audit(acme, domain.AuditConnectionUsed)) == 2, "a refused Resolve was audited")
}

// A person's merge of an item's pull requests is no run: its connection.used row names the person
// and the item.
func testResolveForAMerge(f *fixture) {
	acme := f.NewOrg(f.t)
	f.git(acme, sealedValue)

	const item = domain.ItemID("00000000-0000-7000-8000-0000000000bb")

	if _, err := f.c.Resolve(f.in(acme), []string{connGit}, domain.ConnectionUse{By: admin, Item: item}); err != nil {
		f.t.Fatal(err)
	}

	used := f.one(acme, domain.AuditConnectionUsed)
	f.expect(used.Actor.ID == admin.ID && used.Data["item_id"] == string(item) && used.Data["name"] == connGit && used.Data["run_id"] == nil,
		"connection.used = %+v; want one by the person, naming the item and no run", used)
}

func testRecordTest(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.git(acme, sealedValue)

	at := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	if err := f.c.RecordTest(ctx, connGit, domain.ConnectionTest{At: at, Error: "401 Unauthorized " + strings.Repeat("x", 600)}, admin); err != nil {
		f.t.Fatal(err)
	}

	failed := f.get(acme, connGit).LastTest
	f.expect(failed != nil && failed.At.Equal(at) && !failed.OK, "the last test after a failed one = %+v", failed)
	f.expect(failed != nil && len([]rune(failed.Error)) == 500 && strings.HasPrefix(failed.Error, "401"), "its error, cut to 500 = %+v", failed)

	if err := f.c.RecordTest(ctx, connGit, domain.ConnectionTest{At: at.Add(time.Hour), OK: true}, admin); err != nil {
		f.t.Fatal(err)
	}

	passed := f.get(acme, connGit).LastTest
	f.expect(passed != nil && passed.OK && passed.Error == "", "the last test after a passing one = %+v", passed)

	f.refused("RecordTest of no connection", f.c.RecordTest(ctx, "nothing", domain.ConnectionTest{OK: true}, admin), ports.ErrNotFound)

	tested := f.audit(acme, domain.AuditConnectionTested)
	f.expect(len(tested) == 2 && tested[0].Data["ok"] == true, "connection.tested = %+v", tested)
}

func testDelete(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	git := f.git(acme, sealedValue)

	if err := f.c.Delete(ctx, connGit, admin); err != nil {
		f.t.Fatal(err)
	}

	_, err := f.c.Get(ctx, connGit)
	f.refused("Get after Delete", err, ports.ErrNotFound)
	f.refused("Delete again", f.c.Delete(ctx, connGit, admin), ports.ErrNotFound)

	e := f.one(acme, domain.AuditConnectionDeleted)
	f.expect(e.TargetID == string(git.ID) && e.Data["kind"] == gitName, "connection.deleted = %+v", e)

	f.git(acme, labsValue)
	f.expect(f.values(acme, connGit)[fieldToken] == labsValue, "a new connection of the deleted one's name resolves to the old value")
}

func testIsolation(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.git(acme, sealedValue)

	got, err := f.c.List(f.in(labs))
	f.expect(err == nil && len(got) == 0, "another org's List = %+v, %v; want nothing", got, err)

	_, err = f.c.Get(f.in(labs), connGit)
	f.refused("another org's Get", err, ports.ErrNotFound)

	_, err = f.c.Resolve(f.in(labs), []string{connGit}, forRun)
	f.refused("another org's Resolve", err, ports.ErrNotFound)

	desc := "taken over"
	_, err = f.c.Update(f.in(labs), connGit, domain.ConnectionPatch{Description: &desc}, admin)
	f.refused("another org's Update", err, ports.ErrNotFound)
	f.refused("another org's Delete", f.c.Delete(f.in(labs), connGit, admin), ports.ErrNotFound)
	f.refused("another org's RecordTest", f.c.RecordTest(f.in(labs), connGit, domain.ConnectionTest{OK: true}, admin), ports.ErrNotFound)

	f.git(labs, labsValue)
	f.expect(f.values(acme, connGit)[fieldToken] == sealedValue, "acme's git changed when labs made its own")
	f.one(labs, domain.AuditConnectionCreated)
}

func testNoKey(f *fixture) {
	acme := f.NewOrg(f.t)
	f.git(acme, sealedValue)

	none := f.Connections(f.Key())
	ctx := f.in(acme)

	_, err := none.Create(ctx, domain.NewConnection{Name: connOther, Kind: gitName, Secrets: map[string]string{fieldToken: "t"}}, admin)
	f.refused("Create with a secret without a master key", err, ports.ErrNoMasterKey)

	_, err = none.Resolve(ctx, []string{connGit}, forRun)
	f.refused("Resolve of a sealed connection without a master key", err, ports.ErrNoMasterKey)

	_, err = none.Create(ctx, domain.NewConnection{Name: connDocs, Kind: docsName, Fields: map[string]string{fieldURL: docsURL}}, admin)
	f.expect(err == nil, "Create without secrets without a master key = %v", err)

	got, err := none.List(ctx)
	f.expect(err == nil && len(got) == 2, "List without a master key = %+v, %v", got, err)

	resolved, err := none.Resolve(ctx, []string{connDocs}, forRun)
	f.expect(err == nil && len(resolved) == 1, "Resolve without secrets without a master key = %+v, %v", resolved, err)

	_, err = f.Connections(f.Key("k9")).Resolve(ctx, []string{connGit}, forRun)
	f.expect(err != nil && strings.Contains(err.Error(), "k1"), "Resolve under another key = %v; want an error naming k1", err)
}

func testRekey(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.git(acme, sealedValue)
	f.git(labs, labsValue)
	f.docs(acme)

	_, err := f.Rekey(f.t.Context(), f.Key())
	f.refused("Rekey without a key", err, ports.ErrNoMasterKey)

	rotated := f.Key("k2", "k1")

	n, err := f.Rekey(f.t.Context(), rotated)
	if err != nil || n != 2 {
		f.t.Fatalf("Rekey = %d, %v; want the two sealed connections", n, err)
	}

	n, err = f.Rekey(f.t.Context(), rotated)
	f.expect(err == nil && n == 0, "Rekey again = %d, %v; want nothing left", n, err)

	onlyNew := f.Connections(f.Key("k2"))

	for org, want := range map[domain.OrgID]string{acme: sealedValue, labs: labsValue} {
		got, err := onlyNew.Resolve(f.in(org), []string{connGit}, forRun)
		f.expect(err == nil && got[0].Values[fieldToken] == want, "Resolve under the new key alone = %+v, %v; want %s", got, err, want)
	}

	_, err = f.Connections(f.Key("k1")).Resolve(f.in(acme), []string{connGit}, forRun)
	f.expect(err != nil, "the old key alone still opens a resealed connection")

	got, err := onlyNew.Get(f.in(acme), connGit)
	f.expect(err == nil && got.UpdatedBy.ID == admin.ID, "a resealed connection = %+v, %v; want it unchanged otherwise", got, err)
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()
	desc := "x"

	_, err := f.c.List(ctx)
	f.refused("List", err, domain.ErrNoOrg)
	_, err = f.c.Kinds(ctx)
	f.refused("Kinds", err, domain.ErrNoOrg)
	_, err = f.c.Create(ctx, domain.NewConnection{Name: connDocs, Kind: docsName, Fields: map[string]string{fieldURL: docsURL}}, admin)
	f.refused("Create", err, domain.ErrNoOrg)
	_, err = f.c.Update(ctx, connDocs, domain.ConnectionPatch{Description: &desc}, admin)
	f.refused("Update", err, domain.ErrNoOrg)
	_, err = f.c.Resolve(ctx, []string{connDocs}, forRun)
	f.refused("Resolve", err, domain.ErrNoOrg)
	f.refused("PutKind", f.c.PutKind(ctx, ownKind(""), admin), domain.ErrNoOrg)
}
