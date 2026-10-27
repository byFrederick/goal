package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The routes the credential tests call, the credential most of them create and the code of a
// request the credential rules refuse.
//
//nolint:gosec // routes and names, not credentials
const (
	credentialsPath = "/v1/orgs/acme/credentials"
	credImportPath  = credentialsPath + "/import"
	tokenName       = "acme-token"
	tokenPath       = credentialsPath + "/" + tokenName
	codeCredInvalid = "credential-invalid"
)

// The values the credential tests send; none may come back.
//
//nolint:gosec // test values, not credentials
const (
	tokenValue    = "sk-acme-credential-value-7d2e"
	replacedValue = "sk-acme-replaced-value-0b91"
	importedValue = "sk-imported-value-5a3c"
)

// createToken creates acme-token, scoped to factory acme with its agent coder.
func (h *harness) createToken(t *testing.T) gen.Credential {
	t.Helper()

	body := `{"name":"acme-token","type":"api_key","description":"the acme API","value":"` + tokenValue + `",
"scope":{"all_factories":false,"factories":["` + string(acmeID) + `"]},"agents":{"` + string(acmeID) + `":["coder"]},
"expires_at":"2027-01-31T00:00:00Z","remind_days":14}`

	return decode[gen.Credential](t, h.request(t, http.MethodPost, credentialsPath, body, nil), http.StatusCreated)
}

// An admin creates a credential, lists it, reads it, changes its metadata, replaces its value and
// deletes it; the store audits each change.
func TestCredentials_CreateListGetPatchDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	created := h.createToken(t)
	expectToken(t, created)

	list := decode[gen.CredentialList](t, h.request(t, http.MethodGet, credentialsPath, "", nil), http.StatusOK)
	if len(list.Credentials) != 1 || list.Credentials[0].Id != created.Id {
		t.Errorf("list = %+v; want acme-token", list.Credentials)
	}

	if got := decode[gen.Credential](t, h.request(t, http.MethodGet, tokenPath, "", nil), http.StatusOK); got.Id != created.Id {
		t.Errorf("get = %+v; want acme-token", got)
	}

	h.patchToken(t)

	decode[gen.Credential](t, h.request(t, http.MethodPatch, tokenPath, `{"value":"`+replacedValue+`"}`, nil), http.StatusOK)

	if r := h.request(t, http.MethodDelete, tokenPath, "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s; want 204", r.status, r.body)
	}

	refused(t, "get after delete", h.request(t, http.MethodGet, tokenPath, "", nil), http.StatusNotFound, "credential-not-found")

	audit := decode[gen.AuditPage](t, h.request(t, http.MethodGet, "/v1/orgs/acme/audit?target_kind=credential", "", nil), http.StatusOK)

	want := []string{domain.AuditCredentialDeleted, domain.AuditCredentialValueReplaced, domain.AuditCredentialUpdated, domain.AuditCredentialCreated}
	if !slices.Equal(actions(audit.Entries), want) {
		t.Errorf("audit = %v; want %v", actions(audit.Entries), want)
	}
}

// expectToken checks created is createToken's acme-token, scoped to acme for coder.
func expectToken(t *testing.T, created gen.Credential) {
	t.Helper()

	if created.Name != tokenName || created.Type != gen.CredentialTypeApiKey || created.RemindDays != 14 || created.ExpiresAt == nil ||
		!slices.Equal(created.Scope.Factories, []string{string(acmeID)}) || created.Scope.AllFactories ||
		!slices.Equal(created.Agents[string(acmeID)], []string{"coder"}) || created.CreatedBy.Id != domain.LocalUser.ID {
		t.Errorf("created = %+v; want acme-token scoped to acme for coder", created)
	}

	if want := []gen.CredentialAgent{{FactoryId: string(acmeID), Agent: "coder"}}; !slices.Equal(created.UsedBy.Agents, want) ||
		len(created.UsedBy.Connections) != 0 {
		t.Errorf("used by = %+v; want coder of acme and no connection", created.UsedBy)
	}
}

// patchToken changes acme-token's metadata to an oauth_token for all factories, with no agents
// and no expiry.
func (h *harness) patchToken(t *testing.T) {
	t.Helper()

	patched := decode[gen.Credential](t, h.request(t, http.MethodPatch, tokenPath,
		`{"type":"oauth_token","description":"rotated","scope":{"all_factories":true,"factories":[]},"agents":{},"expiry":{}}`, nil), http.StatusOK)
	if patched.Type != gen.CredentialTypeOauthToken || patched.Description != "rotated" || !patched.Scope.AllFactories ||
		len(patched.Agents) != 0 || patched.ExpiresAt != nil || patched.RemindDays != 0 {
		t.Errorf("patched = %+v; want an oauth_token for all factories, no agents, no expiry", patched)
	}
}

// Requests the credential rules refuse answer their codes, and change nothing.
func TestCredentials_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createToken(t)

	tests := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"bad name", http.MethodPost, credentialsPath, `{"name":"Acme_Token","type":"api_key","value":"v"}`, http.StatusBadRequest, codeCredInvalid},
		{"reserved env name", http.MethodPost, credentialsPath, `{"name":"goal-token","type":"api_key","value":"v"}`, http.StatusBadRequest, codeCredInvalid},
		{"reserved env name, renamed", http.MethodPatch, tokenPath, `{"name":"home"}`, http.StatusBadRequest, codeCredInvalid},
		{"no value", http.MethodPost, credentialsPath, `{"name":"other-token","type":"api_key"}`, http.StatusBadRequest, codeCredInvalid},
		{"unknown type", http.MethodPost, credentialsPath, `{"name":"other-token","type":"password","value":"v"}`, http.StatusBadRequest, codeCredInvalid},
		{
			"reminder without expiry", http.MethodPost, credentialsPath, `{"name":"other-token","type":"api_key","value":"v","remind_days":3}`,
			http.StatusBadRequest, codeCredInvalid,
		},
		{"empty patch", http.MethodPatch, tokenPath, `{}`, http.StatusBadRequest, codeCredInvalid},
		{"name taken", http.MethodPost, credentialsPath, `{"name":"acme-token","type":"api_key","value":"v"}`, http.StatusConflict, "credential-name-taken"},
		{"unknown credential", http.MethodGet, credentialsPath + "/nothing", "", http.StatusNotFound, "credential-not-found"},
		{"patch of no credential", http.MethodPatch, credentialsPath + "/nothing", `{"description":"nobody"}`, http.StatusNotFound, "credential-not-found"},
		{"delete of no credential", http.MethodDelete, credentialsPath + "/nothing", "", http.StatusNotFound, "credential-not-found"},
	}

	for _, tt := range tests {
		refused(t, tt.name, h.request(t, tt.method, tt.path, tt.body, nil), tt.status, tt.code)
	}

	list := decode[gen.CredentialList](t, h.request(t, http.MethodGet, credentialsPath, "", nil), http.StatusOK)
	if len(list.Credentials) != 1 || list.Credentials[0].Name != "acme-token" || list.Credentials[0].Description != "the acme API" {
		t.Errorf("credentials after the refusals = %+v; want acme-token unchanged", list.Credentials)
	}
}

// A member reads credentials but changes none of them; another organization's owner sees none.
func TestCredentials_AMemberMayOnlyRead(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	if _, err := h.creds.Create(inOrg(t), domain.NewCredential{Name: tokenName, Type: domain.CredentialAPIKey, Value: tokenValue}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if got := decode[gen.CredentialList](t, h.as(t, viewerToken, http.MethodGet, credentialsPath, ""), http.StatusOK); len(got.Credentials) != 1 {
		t.Errorf("a member's list = %+v; want acme-token", got.Credentials)
	}

	decode[gen.Credential](t, h.as(t, viewerToken, http.MethodGet, tokenPath, ""), http.StatusOK)

	for _, w := range []struct{ method, path, body string }{
		{http.MethodPost, credentialsPath, `{"name":"mine","type":"api_key","value":"v"}`},
		{http.MethodPatch, tokenPath, `{"description":"mine"}`},
		{http.MethodPatch, tokenPath, `{"value":"mine"}`},
		{http.MethodDelete, tokenPath, ""},
		{http.MethodPost, credImportPath, `{"format":"env","text":"MINE=v"}`},
	} {
		refused(t, "a member's "+w.method+" "+w.path, h.as(t, viewerToken, w.method, w.path, w.body), http.StatusForbidden, "forbidden")
	}

	if res := h.as(t, strangerToken, http.MethodGet, credentialsPath, ""); res.status != http.StatusNotFound {
		t.Errorf("another organization's owner listing = %d; want 404", res.status)
	}

	if got, err := h.creds.Get(inOrg(t), tokenName); err != nil || got.Description != "" {
		t.Errorf("acme-token after a member's writes = %+v, %v; want it unchanged", got, err)
	}
}

// A credential a connection refers to as {{name}} is not deleted, and the answer names the
// connection (ADR 0017 decision 8).
func TestCredentials_DeleteInUse(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createToken(t)

	decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath,
		`{"name":"github","kind":"github","secrets":{"token":"{{acme-token}}"}}`, nil), http.StatusCreated)

	got := decode[gen.Credential](t, h.request(t, http.MethodGet, tokenPath, "", nil), http.StatusOK)
	if !slices.Equal(got.UsedBy.Connections, []string{kindGitHub}) {
		t.Errorf("used by = %v; want the github connection", got.UsedBy.Connections)
	}

	p := refused(t, "delete in use", h.request(t, http.MethodDelete, tokenPath, "", nil), http.StatusConflict, "credential-in-use")
	if !strings.Contains(valueOf(p.Detail), kindGitHub) {
		t.Errorf("problem = %q; want it to name the github connection", valueOf(p.Detail))
	}

	decode[gen.Credential](t, h.request(t, http.MethodGet, tokenPath, "", nil), http.StatusOK)
}

// An import's dry run answers each name as new or existing and stores nothing; a real run creates
// the new names as api_keys with no factories, and skips or replaces the existing ones.
func TestCredentials_Import(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createToken(t)

	decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath,
		`{"name":"github","kind":"github","secrets":{"token":"{{acme-token}}"}}`, nil), http.StatusCreated)

	text, err := json.Marshal("ACME_TOKEN=" + replacedValue + "\n# a comment\nSENTRY_TOKEN=\"" + importedValue + "\"\n")
	if err != nil {
		t.Fatal(err)
	}

	importBody := func(extra string) string { return `{"format":"env","text":` + string(text) + extra + `}` }

	dry := decode[gen.CredentialImportResult](t, h.request(t, http.MethodPost, credImportPath, importBody(`,"dry_run":true`), nil), http.StatusOK)

	want := []gen.CredentialImportOutcome{{Name: tokenName, Exists: true, Connections: 1}, {Name: "sentry-token", Exists: false, Connections: 0}}
	if !dry.DryRun || !slices.Equal(dry.Credentials, want) {
		t.Errorf("dry run = %+v; want %+v", dry, want)
	}

	if _, err := h.creds.Get(inOrg(t), "sentry-token"); err == nil {
		t.Error("a dry run created sentry-token")
	}

	skipped := decode[gen.CredentialImportResult](t, h.request(t, http.MethodPost, credImportPath, importBody(`,"on_conflict":"skip"`), nil), http.StatusOK)
	if outcomes(skipped) != "acme-token:skipped sentry-token:created" {
		t.Errorf("import with skip = %s; want acme-token skipped, sentry-token created", outcomes(skipped))
	}

	sentry, err := h.creds.Get(inOrg(t), "sentry-token")
	if err != nil || sentry.Type != domain.CredentialAPIKey || sentry.Scope.All || len(sentry.Scope.Factories) != 0 {
		t.Errorf("sentry-token = %+v, %v; want an api_key with no factories", sentry, err)
	}

	if v := h.openCredential(t, tokenName); v != tokenValue {
		t.Error("an import with skip replaced acme-token's value")
	}

	replaced := decode[gen.CredentialImportResult](t, h.request(t, http.MethodPost, credImportPath, importBody(`,"on_conflict":"replace"`), nil), http.StatusOK)
	if outcomes(replaced) != "acme-token:replaced sentry-token:replaced" {
		t.Errorf("import with replace = %s; want both replaced", outcomes(replaced))
	}

	if v := h.openCredential(t, tokenName); v != replacedValue {
		t.Error("an import with replace left acme-token's value")
	}
}

// An import of an unknown format, of text that does not parse, of a reserved name or of nothing,
// or with an unknown on_conflict, is refused and stores nothing.
func TestCredentials_ImportRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, bad := range []struct{ name, body string }{
		{"unknown format", `{"format":"toml","text":"a = 1"}`},
		{"text that does not parse", `{"format":"env","text":"no equals sign"}`},
		{"yaml that does not parse", `{"format":"yaml","text":"other:\n  a-b: c"}`},
		{"a reserved name", `{"format":"env","text":"PATH=/bin"}`},
		{"an empty import", `{"format":"env","text":"# nothing"}`},
		{"unknown on_conflict", `{"format":"env","text":"A_B=c","on_conflict":"merge"}`},
	} {
		refused(t, bad.name, h.request(t, http.MethodPost, credImportPath, bad.body, nil), http.StatusBadRequest, codeCredInvalid)
	}

	if list := decode[gen.CredentialList](t, h.request(t, http.MethodGet, credentialsPath, "", nil), http.StatusOK); len(list.Credentials) != 0 {
		t.Errorf("credentials after refused imports = %+v; want none", list.Credentials)
	}
}

// outcomes is an import's answer as name:outcome pairs.
func outcomes(r gen.CredentialImportResult) string {
	parts := make([]string, 0, len(r.Credentials))
	for _, c := range r.Credentials {
		parts = append(parts, c.Name+":"+string(valueOf(c.Outcome)))
	}

	return strings.Join(parts, " ")
}

// openCredential is credential name's value, opened as a run of factory acme would.
func (h *harness) openCredential(t *testing.T, name string) string {
	t.Helper()

	if _, err := h.creds.Patch(inOrg(t), name, domain.CredentialPatch{Scope: &domain.CredentialScope{All: true}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	got, err := h.creds.Resolve(inOrg(t), []string{name}, domain.CredentialUse{Factory: acmeID, Run: "r1"})
	if err != nil {
		t.Fatal(err)
	}

	return got[0].Value
}

// No answer of the credential routes holds a value sent in: not create, list, get, patch, import
// nor their refusals, nor goald's log or the audit log.
func TestAPI_CredentialResponsesHoldNoValue(t *testing.T) {
	t.Parallel()

	logs := &logBuffer{}
	h := newHarnessLogged(t, nil, logs)

	var bodies []string

	call := func(method, path, body string) {
		bodies = append(bodies, string(h.request(t, method, path, body, nil).body))
	}

	call(http.MethodPost, credentialsPath, `{"name":"acme-token","type":"api_key","value":"`+tokenValue+`"}`)
	call(http.MethodPost, credentialsPath, `{"name":"acme-token","type":"api_key","value":"`+tokenValue+`"}`)
	call(http.MethodPost, credentialsPath, `{"name":"goal-x","type":"api_key","value":"`+tokenValue+`"}`)
	call(http.MethodPatch, tokenPath, `{"value":"`+replacedValue+`"}`)
	call(http.MethodPatch, tokenPath, `{"value":"`+replacedValue+`","remind_days":-1,"expiry":{"remind_days":3}}`)
	call(http.MethodGet, tokenPath, "")
	call(http.MethodGet, credentialsPath, "")
	call(http.MethodPost, credImportPath, `{"format":"env","text":"SENTRY_TOKEN=`+importedValue+`","dry_run":true}`)
	call(http.MethodPost, credImportPath, `{"format":"env","text":"SENTRY_TOKEN=`+importedValue+`"}`)
	call(http.MethodPost, credImportPath, `{"format":"env","text":"SENTRY_TOKEN='`+importedValue+`"}`)
	call(http.MethodPost, credImportPath, `{"format":"yaml","text":"credentials:\n  acme-token: `+importedValue+`\n  x: [`+importedValue+`]"}`)
	call(http.MethodGet, "/v1/orgs/acme/audit?target_kind=credential", "")
	call(http.MethodDelete, tokenPath, "")

	for _, value := range []string{tokenValue, replacedValue, importedValue} {
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

	for _, value := range []string{tokenValue, replacedValue, importedValue} {
		if strings.Contains(string(raw), value) {
			t.Errorf("the audit log holds a value: %s", raw)
		}
	}
}
