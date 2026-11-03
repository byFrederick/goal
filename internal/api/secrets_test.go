package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The routes the secret tests call, the secret most of them create and the code of a request the
// secret rules refuse.
//
//nolint:gosec // routes and names, not secrets
const (
	secretsPath        = "/v1/orgs/acme/secrets"
	secretImportPath   = secretsPath + "/import"
	secretName         = "acme-token"
	secretPath         = secretsPath + "/" + secretName
	secretValuePath    = secretPath + "/value"
	codeSecretInvalid  = "secret-invalid"
	codeSecretNotFound = "secret-not-found"
)

// The values the secret tests send; none may come back.
//
//nolint:gosec // test values, not secrets
const (
	secretValue         = "sk-acme-secret-value-4c1f"
	secretReplacedValue = "sk-acme-secret-replaced-8e07"
	secretImportedValue = "sk-secret-imported-2d95"
)

// createSecret creates acme-token, scoped to factory acme, expiring with a reminder.
func (h *harness) createSecret(t *testing.T) gen.Secret {
	t.Helper()

	body := `{"name":"acme-token","description":"the acme API","value":"` + secretValue + `",
"scope":{"all_factories":false,"factories":["` + string(acmeID) + `"]},"expires_at":"2027-01-31T00:00:00Z","remind_days":14}`

	return decode[gen.Secret](t, h.request(t, http.MethodPost, secretsPath, body, nil), http.StatusCreated)
}

// openSecret is secret name's value, opened as a run of factory acme, which its scope must allow.
func (h *harness) openSecret(t *testing.T, name string) string {
	t.Helper()

	got, err := h.secrets.Resolve(inOrg(t), []string{name}, domain.SecretUse{Factory: acmeID, Run: "r1"})
	if err != nil {
		t.Fatalf("open secret %s: %v", name, err)
	}

	return got[0].Value
}

// An admin creates a secret, lists it, reads it, changes its description, scope and expiry,
// replaces its value and deletes it; the store audits each change as secret.*.
func TestAPI_SecretCreateListGetPatchReplaceDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	created := h.createSecret(t)
	expectCreatedSecret(t, created)

	list := decode[gen.SecretList](t, h.request(t, http.MethodGet, secretsPath, "", nil), http.StatusOK)
	if len(list.Secrets) != 1 || list.Secrets[0].Id != created.Id {
		t.Errorf("list = %+v; want acme-token", list.Secrets)
	}

	if got := decode[gen.Secret](t, h.request(t, http.MethodGet, secretPath, "", nil), http.StatusOK); got.Id != created.Id {
		t.Errorf("get = %+v; want acme-token", got)
	}

	patched := decode[gen.Secret](t, h.request(t, http.MethodPatch, secretPath,
		`{"description":"rotated","scope":{"all_factories":true,"factories":[]},"expiry":{}}`, nil), http.StatusOK)
	if patched.Description != "rotated" || !patched.Scope.AllFactories || patched.ExpiresAt != nil || patched.RemindDays != 0 {
		t.Errorf("patched = %+v; want all factories, no expiry", patched)
	}

	decode[gen.Secret](t, h.request(t, http.MethodPut, secretValuePath, `{"value":"`+secretReplacedValue+`"}`, nil), http.StatusOK)

	if v := h.openSecret(t, secretName); v != secretReplacedValue {
		t.Error("PUT …/value left the old value")
	}

	if r := h.request(t, http.MethodDelete, secretPath, "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s; want 204", r.status, r.body)
	}

	refused(t, "get after delete", h.request(t, http.MethodGet, secretPath, "", nil), http.StatusNotFound, codeSecretNotFound)

	audit := decode[gen.AuditPage](t, h.request(t, http.MethodGet, "/v1/orgs/acme/audit?target_kind=secret", "", nil), http.StatusOK)

	want := []string{
		domain.AuditSecretDeleted, domain.AuditSecretUsed, domain.AuditSecretValueReplaced, domain.AuditSecretUpdated, domain.AuditSecretCreated,
	}
	if got := actions(audit.Entries); !slices.Equal(got, want) {
		t.Errorf("audit = %v; want %v", got, want)
	}
}

// expectCreatedSecret checks created is createSecret's acme-token: a value scoped to acme,
// expiring with a reminder, used by nothing.
func expectCreatedSecret(t *testing.T, created gen.Secret) {
	t.Helper()

	if created.Name != secretName || created.Source != gen.SecretSourceValue || created.Description != "the acme API" ||
		created.Scope.AllFactories || !slices.Equal(created.Scope.Factories, []string{string(acmeID)}) ||
		created.ExpiresAt == nil || created.RemindDays != 14 || len(created.Uses) != 0 || created.CreatedBy.Id != domain.LocalUser.ID {
		t.Errorf("created = %+v; want acme-token, a value scoped to acme, expiring, used by nothing", created)
	}
}

// Requests the secret rules refuse answer their codes, and change nothing.
func TestAPI_SecretRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t)

	tests := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"bad name", http.MethodPost, secretsPath, `{"name":"Acme_Token","value":"v"}`, http.StatusBadRequest, codeSecretInvalid},
		{"no value", http.MethodPost, secretsPath, `{"name":"other-token"}`, http.StatusBadRequest, codeSecretInvalid},
		{"reminder without expiry", http.MethodPost, secretsPath, `{"name":"other-token","value":"v","remind_days":3}`, http.StatusBadRequest, codeSecretInvalid},
		{
			"all factories and a list", http.MethodPost, secretsPath, `{"name":"other-token","value":"v","scope":{"all_factories":true,"factories":["f"]}}`,
			http.StatusBadRequest, codeSecretInvalid,
		},
		{"empty patch", http.MethodPatch, secretPath, `{}`, http.StatusBadRequest, codeSecretInvalid},
		{"empty value", http.MethodPut, secretValuePath, `{"value":""}`, http.StatusBadRequest, codeSecretInvalid},
		{"name in use", http.MethodPost, secretsPath, `{"name":"acme-token","value":"v"}`, http.StatusConflict, "secret-name-taken"},
		{"unknown secret", http.MethodGet, secretsPath + "/nothing", "", http.StatusNotFound, codeSecretNotFound},
		{"patch of no secret", http.MethodPatch, secretsPath + "/nothing", `{"description":"nobody"}`, http.StatusNotFound, codeSecretNotFound},
		{"value of no secret", http.MethodPut, secretsPath + "/nothing/value", `{"value":"v"}`, http.StatusNotFound, codeSecretNotFound},
		{"delete of no secret", http.MethodDelete, secretsPath + "/nothing", "", http.StatusNotFound, codeSecretNotFound},
	}

	for _, tt := range tests {
		refused(t, tt.name, h.request(t, tt.method, tt.path, tt.body, nil), tt.status, tt.code)
	}

	list := decode[gen.SecretList](t, h.request(t, http.MethodGet, secretsPath, "", nil), http.StatusOK)
	if len(list.Secrets) != 1 || list.Secrets[0].Description != "the acme API" {
		t.Errorf("secrets after the refusals = %+v; want acme-token unchanged", list.Secrets)
	}

	if v := h.openSecret(t, secretName); v != secretValue {
		t.Error("a refused request changed acme-token's value")
	}
}

// A member reads secrets but changes none of them; another organization's owner sees none.
func TestAPI_SecretMemberMayOnlyRead(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	if _, err := h.secrets.Create(inOrg(t), domain.NewSecret{Name: secretName, Value: secretValue}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if got := decode[gen.SecretList](t, h.as(t, viewerToken, http.MethodGet, secretsPath, ""), http.StatusOK); len(got.Secrets) != 1 {
		t.Errorf("a member's list = %+v; want acme-token", got.Secrets)
	}

	decode[gen.Secret](t, h.as(t, viewerToken, http.MethodGet, secretPath, ""), http.StatusOK)

	for _, w := range []struct{ method, path, body string }{
		{http.MethodPost, secretsPath, `{"name":"mine","value":"v"}`},
		{http.MethodPatch, secretPath, `{"description":"mine"}`},
		{http.MethodPut, secretValuePath, `{"value":"mine"}`},
		{http.MethodDelete, secretPath, ""},
		{http.MethodPost, secretImportPath, `{"format":"env","text":"MINE=v"}`},
	} {
		refused(t, "a member's "+w.method+" "+w.path, h.as(t, viewerToken, w.method, w.path, w.body), http.StatusForbidden, "forbidden")
	}

	if res := h.as(t, strangerToken, http.MethodGet, secretsPath, ""); res.status != http.StatusNotFound {
		t.Errorf("another organization's owner listing = %d; want 404", res.status)
	}

	if got, err := h.secrets.Get(inOrg(t), secretName); err != nil || got.Description != "" {
		t.Errorf("acme-token after a member's writes = %+v, %v; want it unchanged", got, err)
	}
}

// A secret a connection refers to as {{name}} lists the connection in its uses and is not
// deleted: the 409 names each use (ADR 0037 decision 4).
func TestAPI_SecretDeleteInUse(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t)

	for _, name := range []string{kindGitHub, "github-ci"} {
		decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath,
			`{"name":"`+name+`","kind":"github","secrets":{"token":"{{acme-token}}"}}`, nil), http.StatusCreated)
	}

	want := []gen.SecretUse{{Kind: gen.SecretUseConnection, Name: kindGitHub}, {Kind: gen.SecretUseConnection, Name: "github-ci"}}

	if got := decode[gen.Secret](t, h.request(t, http.MethodGet, secretPath, "", nil), http.StatusOK); !slices.Equal(got.Uses, want) {
		t.Errorf("uses = %+v; want %+v", got.Uses, want)
	}

	p := refused(t, "delete in use", h.request(t, http.MethodDelete, secretPath, "", nil), http.StatusConflict, "secret-in-use")
	if p.Uses == nil || !slices.Equal(*p.Uses, want) {
		t.Errorf("problem uses = %+v; want %+v", p.Uses, want)
	}

	if !strings.Contains(valueOf(p.Detail), "github-ci") {
		t.Errorf("problem detail = %q; want it to name the connections", valueOf(p.Detail))
	}

	decode[gen.Secret](t, h.request(t, http.MethodGet, secretPath, "", nil), http.StatusOK)
}

// A secret a knowledge source reads with lists the source in its uses and is not deleted: the 409
// names it. Once the source is gone, the secret deletes.
func TestAPI_SecretDeleteRefusedWhileASourceUsesIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)

	decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath, `{"name":"runbooks","title":"Runbooks","kind":"synced",
"secret":"`+repoSecret+`","config":{"reader":"gitdocs","repository":"https://github.com/acme/docs"}}`, nil), http.StatusCreated)

	path := secretsPath + "/" + repoSecret
	want := []gen.SecretUse{{Kind: gen.SecretUseKnowledgeSource, Name: "runbooks"}}

	if got := decode[gen.Secret](t, h.request(t, http.MethodGet, path, "", nil), http.StatusOK); !slices.Equal(got.Uses, want) {
		t.Errorf("uses = %+v; want %+v", got.Uses, want)
	}

	p := refused(t, "delete in use", h.request(t, http.MethodDelete, path, "", nil), http.StatusConflict, "secret-in-use")
	if p.Uses == nil || !slices.Equal(*p.Uses, want) || !strings.Contains(valueOf(p.Detail), "knowledge_source runbooks") {
		t.Errorf("problem = %+v, detail %q; want it to name the source", p.Uses, valueOf(p.Detail))
	}

	if r := h.request(t, http.MethodDelete, sourcesPath+"/runbooks", "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete the source = %d %s", r.status, r.body)
	}

	if r := h.request(t, http.MethodDelete, path, "", nil); r.status != http.StatusNoContent {
		t.Errorf("delete the secret no source uses = %d %s; want 204", r.status, r.body)
	}
}

// An import's dry run answers each name as new or existing, with what uses the existing one, and
// stores nothing; a real run creates the new names open to no factory, and skips or replaces the
// existing ones.
func TestAPI_SecretImport(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t)

	decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath,
		`{"name":"github","kind":"github","secrets":{"token":"{{acme-token}}"}}`, nil), http.StatusCreated)

	text, err := json.Marshal("ACME_TOKEN=" + secretReplacedValue + "\n# a comment\nSENTRY_TOKEN=\"" + secretImportedValue + "\"\n")
	if err != nil {
		t.Fatal(err)
	}

	importBody := func(extra string) string { return `{"format":"env","text":` + string(text) + extra + `}` }

	expectDryRun(t, decode[gen.SecretImportResult](t, h.request(t, http.MethodPost, secretImportPath, importBody(`,"dry_run":true`), nil), http.StatusOK))

	if _, err := h.secrets.Get(inOrg(t), "sentry-token"); err == nil {
		t.Error("a dry run created sentry-token")
	}

	skipped := decode[gen.SecretImportResult](t, h.request(t, http.MethodPost, secretImportPath, importBody(`,"on_conflict":"skip"`), nil), http.StatusOK)
	if secretOutcomes(skipped) != "acme-token:skipped sentry-token:created" {
		t.Errorf("import with skip = %s; want acme-token skipped, sentry-token created", secretOutcomes(skipped))
	}

	sentry, err := h.secrets.Get(inOrg(t), "sentry-token")
	if err != nil || sentry.Scope.All || len(sentry.Scope.Factories) != 0 || sentry.Source != domain.SecretSourceValue {
		t.Errorf("sentry-token = %+v, %v; want a value open to no factory", sentry, err)
	}

	if v := h.openSecret(t, secretName); v != secretValue {
		t.Error("an import with skip replaced acme-token's value")
	}

	replaced := decode[gen.SecretImportResult](t, h.request(t, http.MethodPost, secretImportPath, importBody(`,"on_conflict":"replace"`), nil), http.StatusOK)
	if secretOutcomes(replaced) != "acme-token:replaced sentry-token:replaced" {
		t.Errorf("import with replace = %s; want both replaced", secretOutcomes(replaced))
	}

	if v := h.openSecret(t, secretName); v != secretReplacedValue {
		t.Error("an import with replace left acme-token's value")
	}
}

// expectDryRun checks an import's dry run of acme-token, used by the github connection, and
// sentry-token, which is new: what each is, and no outcome.
func expectDryRun(t *testing.T, dry gen.SecretImportResult) {
	t.Helper()

	want := []gen.SecretImportOutcome{
		{Name: secretName, Exists: true, Uses: []gen.SecretUse{{Kind: gen.SecretUseConnection, Name: kindGitHub}}},
		{Name: "sentry-token", Uses: []gen.SecretUse{}},
	}

	if !dry.DryRun || !reflect.DeepEqual(dry.Secrets, want) {
		t.Errorf("dry run = %+v; want %+v", dry, want)
	}
}

// An import of an unknown format, of text that does not parse, of a reserved name or of nothing,
// or with an unknown on_conflict, is refused and stores nothing.
func TestAPI_SecretImportRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, bad := range []struct{ name, body string }{
		{"unknown format", `{"format":"toml","text":"a = 1"}`},
		{"text that does not parse", `{"format":"env","text":"no equals sign"}`},
		{"a reserved name", `{"format":"env","text":"PATH=/bin"}`},
		{"an empty import", `{"format":"env","text":"# nothing"}`},
		{"unknown on_conflict", `{"format":"env","text":"NEW_ONE=c","on_conflict":"merge"}`},
	} {
		refused(t, bad.name, h.request(t, http.MethodPost, secretImportPath, bad.body, nil), http.StatusBadRequest, codeSecretInvalid)
	}

	if list := decode[gen.SecretList](t, h.request(t, http.MethodGet, secretsPath, "", nil), http.StatusOK); len(list.Secrets) != 0 {
		t.Errorf("secrets after refused imports = %+v; want none", list.Secrets)
	}
}

// secretOutcomes is an import's answer as name:outcome pairs.
func secretOutcomes(r gen.SecretImportResult) string {
	parts := make([]string, 0, len(r.Secrets))
	for _, s := range r.Secrets {
		parts = append(parts, s.Name+":"+string(valueOf(s.Outcome)))
	}

	return strings.Join(parts, " ")
}

// No answer of the secret routes holds a value sent in: not create, list, get, patch, PUT …/value,
// import, delete nor their refusals, nor goald's log or the audit log.
func TestAPI_SecretResponsesHoldNoValue(t *testing.T) {
	t.Parallel()

	logs := &logBuffer{}
	h := newHarnessLogged(t, nil, logs)

	var bodies []string

	call := func(method, path, body string) {
		bodies = append(bodies, string(h.request(t, method, path, body, nil).body))
	}

	call(http.MethodPost, secretsPath, `{"name":"acme-token","value":"`+secretValue+`"}`)
	call(http.MethodPost, secretsPath, `{"name":"acme-token","value":"`+secretValue+`"}`)
	call(http.MethodPost, secretsPath, `{"name":"Bad_Name","value":"`+secretValue+`"}`)
	call(http.MethodPut, secretValuePath, `{"value":"`+secretReplacedValue+`"}`)
	call(http.MethodPut, secretsPath+"/nothing/value", `{"value":"`+secretReplacedValue+`"}`)
	call(http.MethodPatch, secretPath, `{"description":"`+secretReplacedValue+`x","expiry":{"remind_days":3}}`)
	call(http.MethodGet, secretPath, "")
	call(http.MethodGet, secretsPath, "")
	call(http.MethodPost, secretImportPath, `{"format":"env","text":"SENTRY_TOKEN=`+secretImportedValue+`","dry_run":true}`)
	call(http.MethodPost, secretImportPath, `{"format":"env","text":"SENTRY_TOKEN=`+secretImportedValue+`"}`)
	call(http.MethodPost, secretImportPath, `{"format":"env","text":"SENTRY_TOKEN='`+secretImportedValue+`"}`)
	call(http.MethodPost, secretImportPath, `{"format":"yaml","text":"credentials:\n  acme-token: `+secretImportedValue+`\n  x: [`+secretImportedValue+`]"}`)
	call(http.MethodGet, "/v1/orgs/acme/audit?target_kind=secret", "")
	call(http.MethodDelete, secretPath, "")

	values := []string{secretValue, secretReplacedValue, secretImportedValue}

	for _, value := range values {
		for i, b := range bodies {
			if strings.Contains(b, value) {
				t.Errorf("response %d holds a value: %s", i, b)
			}
		}

		if strings.Contains(logs.String(), value) {
			t.Errorf("goald's log holds a value: %s", logs.String())
		}
	}

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}

	for _, value := range values {
		if strings.Contains(string(raw), value) {
			t.Errorf("the audit log holds a value: %s", raw)
		}
	}
}
