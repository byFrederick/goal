package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The routes the connection tests call.
const (
	connectionsPath = "/v1/orgs/acme/connections"
	kindsPath       = "/v1/orgs/acme/connection-kinds"
	githubPath      = connectionsPath + "/github"
	apiPath         = connectionsPath + "/acme-api"
)

// githubToken is the secret of the github connection the tests create; kindGitHub its kind.
const (
	githubToken = "ghp_s3cr3t-github-token-4f1c" //nolint:gosec // a test value, not a credential
	kindGitHub  = "github"
)

// acmeKind is an organization kind whose test calls {{base_url}}/ping with its key.
const acmeKind = `{"apiVersion":"goal/v1","kind":"ConnectionKind","metadata":{"name":"acme-api","title":"Acme API"},
"spec":{"fields":[{"name":"base_url","title":"Base URL","required":true},{"name":"key","title":"Key","secret":true,"required":true}],
"env":{"ACME_URL":"{{base_url}}","ACME_KEY":"{{key}}"},
"test":{"method":"GET","url":"{{base_url}}/ping","headers":{"Authorization":"Bearer {{key}}"}},
"docs":"Call ACME_URL with ACME_KEY."}}`

// expectGitHubKind checks the platform's github kind is listed with its modes and hooks.
func (h *harness) expectGitHubKind(t *testing.T) {
	t.Helper()

	kinds := decode[gen.ConnectionKindList](t, h.request(t, http.MethodGet, kindsPath, "", nil), http.StatusOK)

	i := slices.IndexFunc(kinds.Kinds, func(k gen.ConnectionKind) bool { return k.Name == kindGitHub })
	if i < 0 {
		t.Fatalf("kinds = %+v; want the platform's github", kinds.Kinds)
	}

	k := kinds.Kinds[i]
	if !k.Platform || !k.Testable || valueOf(k.CodeHost) != kindGitHub || !slices.Equal(k.Modes, []gen.ConnectionKindModes{"env", "mcp", "git"}) {
		t.Errorf("github = %+v; want the platform's, testable, a code host, with env, mcp and git", k)
	}
}

func (h *harness) createGitHub(t *testing.T) gen.Connection {
	t.Helper()

	body := `{"name":"github","kind":"github","description":"the repositories","secrets":{"token":"` + githubToken + `"}}`

	return decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath, body, nil), http.StatusCreated)
}

// An admin creates a connection of a platform kind, which members read with the env names a run
// gets from it; its secret is never answered.
func TestConnections_CreateAndRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.expectGitHubKind(t)

	created := h.createGitHub(t)
	if created.Kind != kindGitHub || !slices.Equal(created.SecretsSet, []string{"token"}) || created.Status != gen.ConnectionStatusUntested ||
		created.CreatedBy.Id != domain.LocalUser.ID || len(created.Fields) != 0 {
		t.Errorf("created = %+v; want github with its token set, untested", created)
	}

	got := decode[gen.Connection](t, h.request(t, http.MethodGet, githubPath, "", nil), http.StatusOK)
	if env := valueOf(got.Env); !slices.Equal(env, []string{"GH_HOST", "GH_TOKEN"}) || !strings.Contains(valueOf(got.Docs), "GH_TOKEN") {
		t.Errorf("get = env %v, docs %q; want the env names a run gets and the kind's docs", env, valueOf(got.Docs))
	}
}

// A connection is renamed, changed and deleted, and every change is audited.
func TestConnections_ChangeAndDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	created := h.createGitHub(t)

	renamed := decode[gen.Connection](t, h.request(t, http.MethodPatch, githubPath,
		`{"name":"repos","description":"acme's repositories","fields":{"host":"git.example.com","api_url":"https://git.example.com/api/v3"}}`, nil),
		http.StatusOK)
	if renamed.Name != "repos" || renamed.Id != created.Id || renamed.Fields["host"] != "git.example.com" {
		t.Errorf("patched = %+v; want repos with its host", renamed)
	}

	list := decode[gen.ConnectionList](t, h.request(t, http.MethodGet, connectionsPath+"?kind=github", "", nil), http.StatusOK)
	if len(list.Connections) != 1 || list.Connections[0].Name != "repos" || list.Connections[0].UsedBy.Factories == nil {
		t.Errorf("list by kind = %+v; want repos alone", list.Connections)
	}

	if r := h.request(t, http.MethodDelete, connectionsPath+"/repos", "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s; want 204", r.status, r.body)
	}

	refused(t, "get after delete", h.request(t, http.MethodGet, connectionsPath+"/repos", "", nil), http.StatusNotFound, "connection-not-found")

	audit := decode[gen.AuditPage](t, h.request(t, http.MethodGet, "/v1/orgs/acme/audit?target_kind=connection", "", nil), http.StatusOK)
	if want := []string{domain.AuditConnectionDeleted, domain.AuditConnectionUpdated, domain.AuditConnectionCreated}; !slices.Equal(actions(audit.Entries), want) {
		t.Errorf("audit = %v; want %v", actions(audit.Entries), want)
	}
}

// Requests the connection rules refuse answer their codes.
func TestConnections_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createGitHub(t)

	tests := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"unknown kind", http.MethodPost, connectionsPath, `{"name":"x-y","kind":"nothing"}`, http.StatusNotFound, "connection-kind-not-found"},
		{"required secret missing", http.MethodPost, connectionsPath, `{"name":"gh-two","kind":"github"}`, http.StatusBadRequest, "field-required"},
		{
			"unknown field", http.MethodPost, connectionsPath, `{"name":"gh-two","kind":"github","fields":{"org":"acme"},"secrets":{"token":"t"}}`,
			http.StatusBadRequest, "field-unknown",
		},
		{"secret as a field", http.MethodPost, connectionsPath, `{"name":"gh-two","kind":"github","fields":{"token":"t"}}`, http.StatusBadRequest, "field-unknown"},
		{"bad name", http.MethodPost, connectionsPath, `{"name":"GitHub","kind":"github","secrets":{"token":"t"}}`, http.StatusBadRequest, ""},
		{"name taken", http.MethodPost, connectionsPath, `{"name":"github","kind":"github","secrets":{"token":"t"}}`, http.StatusConflict, "connection-name-taken"},
		{"unknown connection", http.MethodPatch, connectionsPath + "/nothing", `{"description":"d"}`, http.StatusNotFound, "connection-not-found"},
		{"clearing a required secret", http.MethodPatch, githubPath, `{"secrets":{"token":""}}`, http.StatusBadRequest, "field-required"},
		{"untestable kind", http.MethodPost, connectionsPath + "/docs/test", "", http.StatusConflict, "connection-not-testable"},
		{"delete of no connection", http.MethodDelete, connectionsPath + "/nothing", "", http.StatusNotFound, "connection-not-found"},
	}

	decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath, `{"name":"docs","kind":"mcp-server","fields":{"url":"https://docs.example.com/mcp"}}`, nil),
		http.StatusCreated)

	for _, tt := range tests {
		refused(t, tt.name, h.request(t, tt.method, tt.path, tt.body, nil), tt.status, tt.code)
	}

	list := decode[gen.ConnectionList](t, h.request(t, http.MethodGet, connectionsPath, "", nil), http.StatusOK)
	if len(list.Connections) != 2 {
		t.Errorf("connections after the refusals = %+v; want github and docs only", list.Connections)
	}
}

// An organization kind is put from its file, refused when it breaks the rules or takes a platform
// kind's name, and deleted once no connection is of it.
func TestConnectionKinds_PutAndDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got := decode[gen.ConnectionKind](t, h.request(t, http.MethodPut, kindsPath+"/acme-api", acmeKind, nil), http.StatusOK)
	if got.Name != "acme-api" || got.Platform || !got.Testable || !slices.Equal(got.Env, []string{"ACME_KEY", "ACME_URL"}) || len(got.Fields) != 2 {
		t.Errorf("put = %+v; want the organization's acme-api", got)
	}

	refused(t, "a name that is not the path's", h.request(t, http.MethodPut, kindsPath+"/acme-other", acmeKind, nil),
		http.StatusBadRequest, "connection-kind-invalid")

	broken := strings.Replace(acmeKind, `"{{key}}"`, `"{{nokey}}"`, 1)
	if p := refused(t, "a template naming no field", h.request(t, http.MethodPut, kindsPath+"/acme-api", broken, nil),
		http.StatusBadRequest, "connection-kind-invalid"); !strings.Contains(valueOf(p.Detail), "nokey") {
		t.Errorf("refusal detail %q; want it to name the broken template", valueOf(p.Detail))
	}

	github := strings.Replace(acmeKind, `"name":"acme-api"`, `"name":"github"`, 1)
	refused(t, "a platform kind's name", h.request(t, http.MethodPut, kindsPath+"/github", github, nil), http.StatusConflict, "connection-kind-name-taken")

	decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath,
		`{"name":"acme-api","kind":"acme-api","fields":{"base_url":"https://api.example.com"},"secrets":{"key":"k"}}`, nil), http.StatusCreated)
	refused(t, "delete in use", h.request(t, http.MethodDelete, kindsPath+"/acme-api", "", nil), http.StatusConflict, "connection-kind-in-use")

	secretURL := strings.NewReplacer(`"title":"Base URL","required":true`, `"title":"Base URL","required":true,"secret":true`,
		`"url":"{{base_url}}/ping"`, `"url":"https://api.example.com/ping"`).Replace(acmeKind)
	if p := refused(t, "a field a connection holds made secret", h.request(t, http.MethodPut, kindsPath+"/acme-api", secretURL, nil),
		http.StatusConflict, "connection-kind-in-use"); !strings.Contains(valueOf(p.Detail), "base_url") || strings.Contains(valueOf(p.Detail), "api.example.com") {
		t.Errorf("refusal detail %q; want it to name the field, not its value", valueOf(p.Detail))
	}

	refused(t, "delete of a platform kind", h.request(t, http.MethodDelete, kindsPath+"/github", "", nil), http.StatusNotFound, "connection-kind-not-found")

	if r := h.request(t, http.MethodDelete, apiPath, "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete the connection = %d %s", r.status, r.body)
	}

	if r := h.request(t, http.MethodDelete, kindsPath+"/acme-api", "", nil); r.status != http.StatusNoContent {
		t.Errorf("delete the kind = %d %s; want 204", r.status, r.body)
	}
}

// pingServer is a connection's test endpoint: it records the Authorization headers it gets and
// answers status.
type pingServer struct {
	srv *httptest.Server

	mu   sync.Mutex
	auth []string
}

func newPingServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *pingServer {
	t.Helper()

	p := &pingServer{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.auth = append(p.auth, r.Header.Get("Authorization"))
		p.mu.Unlock()

		handler(w, r)
	}))
	t.Cleanup(p.srv.Close)

	return p
}

func (p *pingServer) calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Clone(p.auth)
}

// putAcmeAPI puts the acme-api kind and a connection of it to base, with key as its secret.
func (h *harness) putAcmeAPI(t *testing.T, base, key string) {
	t.Helper()

	decode[gen.ConnectionKind](t, h.request(t, http.MethodPut, kindsPath+"/acme-api", acmeKind, nil), http.StatusOK)
	decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath,
		`{"name":"acme-api","kind":"acme-api","fields":{"base_url":"`+base+`"},"secrets":{"key":"`+key+`"}}`, nil), http.StatusCreated)
}

// Spec 4 §11.2 row 4: a connection's test reaches public addresses only, so a URL naming goald's
// own network (here the test's server on 127.0.0.1) fails the test without a request; the failure
// is the connection's last test.
func TestConnectionTest_RefusesPrivateURL(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publicOnly.Store(true)

	ping := newPingServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h.putAcmeAPI(t, ping.srv.URL, "k-private")

	got := decode[gen.ConnectionTestResult](t, h.request(t, http.MethodPost, apiPath+"/test", "", nil), http.StatusOK)
	if got.Ok || got.Status != nil || !strings.Contains(valueOf(got.Error), "not a public address") {
		t.Errorf("test of a loopback URL = %+v (%s); want it failed as not public, with no status", got, valueOf(got.Error))
	}

	if calls := ping.calls(); len(calls) != 0 {
		t.Errorf("the private server got %d requests; want none", len(calls))
	}

	conn := decode[gen.Connection](t, h.request(t, http.MethodGet, apiPath, "", nil), http.StatusOK)
	if conn.Status != gen.ConnectionStatusFailing || conn.LastTest == nil || conn.LastTest.Ok {
		t.Errorf("connection after the test = %s %+v; want failing", conn.Status, conn.LastTest)
	}
}

// A test sends the kind's request with the connection's values, follows no redirect, and passes
// on the status the kind expects.
func TestConnectionTest_SendsTheKindsRequest(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var redirect atomic.Bool

	ping := newPingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if redirect.Load() {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	})
	h.putAcmeAPI(t, ping.srv.URL, "k-ping")

	got := decode[gen.ConnectionTestResult](t, h.request(t, http.MethodPost, apiPath+"/test", "", nil), http.StatusOK)
	if !got.Ok || valueOf(got.Status) != http.StatusOK || got.Error != nil {
		t.Errorf("test = %+v; want ok with 200", got)
	}

	if calls := ping.calls(); !slices.Equal(calls, []string{"Bearer k-ping"}) {
		t.Errorf("the endpoint got %v; want one request with the key", calls)
	}

	redirect.Store(true)

	got = decode[gen.ConnectionTestResult](t, h.request(t, http.MethodPost, apiPath+"/test", "", nil), http.StatusOK)
	if got.Ok || valueOf(got.Status) != http.StatusFound || len(ping.calls()) != 2 {
		t.Errorf("test answered with a redirect = %+v after %d requests; want failed with 302, not followed", got, len(ping.calls()))
	}

	tested := decode[gen.AuditPage](t, h.request(t, http.MethodGet, "/v1/orgs/acme/audit?action=connection.tested", "", nil), http.StatusOK)
	if len(tested.Entries) != 2 {
		t.Errorf("connection.tested entries = %d; want one per test", len(tested.Entries))
	}
}

// Spec 4 §11.2 row 3: no response of the connection routes, no audit entry and no log line holds a
// secret value, on success or refusal, after a secret's replacement and a test that echoes it.
func TestAPI_ConnectionResponsesHoldNoSecret(t *testing.T) {
	t.Parallel()

	const first, second = "sk-first-secret-value-91c2", "sk-second-secret-value-d40b"

	logs := &logBuffer{}
	h := newHarnessLogged(t, nil, logs)
	echo := newPingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(r.Header.Get("Authorization"))) //nolint:gosec // a test endpoint that echoes the secret on purpose
	})

	var bodies []string

	call := func(method, path, body string) {
		bodies = append(bodies, string(h.request(t, method, path, body, nil).body))
	}

	call(http.MethodPut, kindsPath+"/acme-api", acmeKind)
	call(http.MethodPost, connectionsPath, `{"name":"acme-api","kind":"acme-api","fields":{"base_url":"`+echo.srv.URL+`"},"secrets":{"key":"`+first+`"}}`)
	call(http.MethodPost, connectionsPath, `{"name":"acme-two","kind":"acme-api","fields":{"nope":"x"},"secrets":{"key":"`+first+`"}}`)
	call(http.MethodPatch, apiPath, `{"secrets":{"key":"`+second+`"}}`)
	call(http.MethodPost, apiPath+"/test", "")
	call(http.MethodGet, apiPath, "")
	call(http.MethodGet, connectionsPath, "")
	call(http.MethodGet, kindsPath, "")
	call(http.MethodGet, "/v1/orgs/acme/audit?target_kind=connection", "")
	call(http.MethodDelete, apiPath, "")

	if calls := echo.calls(); len(calls) != 1 || calls[0] != "Bearer "+second {
		t.Fatalf("the test endpoint got %v; want one request with the replaced key", calls)
	}

	for _, secret := range []string{first, second} {
		for i, b := range bodies {
			if strings.Contains(b, secret) {
				t.Errorf("response %d holds a secret: %s", i, b)
			}
		}

		if strings.Contains(logs.String(), secret) {
			t.Errorf("goald's log holds a secret: %s", logs.String())
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

	if strings.Contains(string(raw), first) || strings.Contains(string(raw), second) {
		t.Errorf("the audit log holds a secret: %s", raw)
	}
}

// A connection event from the store's notices reaches every member's stream as `event: connection`.
func TestAPI_StreamSendsConnectionEvents(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, _, events := h.stream(t, "")
	events.next(t) // retry

	data := `{"connection_id":"0b6f1c9e-0000-7000-8000-000000000001","name":"github","change":"tested"}`
	h.notices.Publish(ports.ItemNotice{OrgID: testOrg, Org: &ports.OrgEvent{Name: "connection", Data: json.RawMessage(data)}})

	got := events.next(t)
	if len(got) != 2 || got[0] != "event: connection" || got[1] != "data: "+data {
		t.Fatalf("event %q; want the connection event", got)
	}

	var e gen.ConnectionEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[1], "data: ")), &e); err != nil || e.Change != gen.Tested || e.Name != kindGitHub {
		t.Errorf("connection event = %+v, %v; want github tested", e, err)
	}
}
