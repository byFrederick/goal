package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/core/ports"
)

// nobody is the access log's principal of a request that has none.
const nobody = "none"

// logBuffer is a log the server writes and the test reads, safe across their goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// accessLine is the access log's line for the request with id, decoded.
func (b *logBuffer) accessLine(t *testing.T, id string) map[string]any {
	t.Helper()

	sc := bufio.NewScanner(strings.NewReader(b.String()))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}

		if line["msg"] == "api request" && line["request_id"] == id {
			return line
		}
	}

	t.Fatalf("no access log line for request %s in:\n%s", id, b.String())

	return nil
}

// Spec 3 §12 and §11.1 row 24: every request is logged once with its principal (user:<id>,
// service:<key id> or none), the credential it came with (session, key:<id>, pat:<id>), its
// organization's id, operation and status and its request id; never a secret, a cookie or an
// email.
func TestAPI_AccessLogNamesThePrincipalNeverItsSecrets(t *testing.T) {
	t.Parallel()

	logs := &logBuffer{}
	h := keyedWith(t, logs)

	key, _ := h.create(t, ciDeploy)
	tok, _ := h.createToken(t, freshToken, laptop)

	cases := []struct {
		name                  string
		header                http.Header
		path                  string
		status                int
		operation             string
		principal, credential string
		orgID                 string
	}{
		{"a session", cookie(ownerToken), acmePath, http.StatusOK, opGetOrg, "user:" + string(ownerID), "session", string(testOrg)},
		{"a key", bearer(key.Secret), acmePath, http.StatusOK, opGetOrg, "service:" + key.Key.Id, "key:" + key.Key.Id, string(testOrg)},
		{"a personal token", bearer(tok.Secret), acmePath, http.StatusOK, opGetOrg, "user:" + string(ownerID), "pat:" + tok.Token.Id, string(testOrg)},
		{"a key nobody made", bearer("goal_sk_" + strings.Repeat("x", 43)), acmePath, http.StatusUnauthorized, opGetOrg, nobody, "", ""},
		{"nobody, on a public operation", nil, healthPath, http.StatusOK, "getHealth", nobody, "", ""},
	}

	for _, c := range cases {
		res := h.request(t, http.MethodGet, c.path, "", c.header)
		if res.status != c.status {
			t.Fatalf("%s: %d %s; want %d", c.name, res.status, res.body, c.status)
		}

		line := logs.accessLine(t, res.header.Get("X-Request-Id"))
		want := map[string]any{
			"operation": c.operation, "status": float64(c.status), "principal": c.principal, "method": http.MethodGet,
		}

		for k, v := range want {
			if line[k] != v {
				t.Errorf("%s: %s = %v; want %v (line %v)", c.name, k, line[k], v, line)
			}
		}

		if got, _ := line["credential"].(string); got != c.credential {
			t.Errorf("%s: credential = %q; want %q", c.name, got, c.credential)
		}

		if got, _ := line["org_id"].(string); got != c.orgID {
			t.Errorf("%s: org_id = %q; want %q", c.name, got, c.orgID)
		}
	}

	// Paths and query strings are never logged: a device code in the path, an invitation's id in a
	// query (§11.1 row 14).
	device := h.request(t, http.MethodGet, "/v1/auth/device/WXYZ2345?next=/invite/inv-0b6f1c9e", "", cookie(ownerToken))
	if line := logs.accessLine(t, device.header.Get("X-Request-Id")); line["operation"] != "getDeviceRequest" {
		t.Errorf("device request line = %v; want getDeviceRequest", line)
	}

	all := logs.String()
	for _, secret := range []string{ownerToken, freshToken, key.Secret, tok.Secret, "c2lnbmF0dXJl", "@example.com", "WXYZ2345", "inv-0b6f1c9e"} {
		if strings.Contains(all, secret) {
			t.Errorf("the log holds %q:\n%s", secret, all)
		}
	}
}

// Under GOAL_AUTH=none every request is the local user's.
func TestAPI_AccessLogUnderNone(t *testing.T) {
	t.Parallel()

	logs := &logBuffer{}
	h := newHarnessLogged(t, nil, logs)

	res := h.request(t, http.MethodGet, acmePath, "", nil)
	if res.status != http.StatusOK {
		t.Fatalf("GET %s = %d %s", acmePath, res.status, res.body)
	}

	line := logs.accessLine(t, res.header.Get("X-Request-Id"))
	if line["principal"] != "user:local" || line["org_id"] != string(testOrg) || line["operation"] != opGetOrg {
		t.Errorf("line = %v; want the local user in acme's getOrg", line)
	}
}

// meteredDefinitions runs two queries that waited 7ms for their connections on each factory
// listing, as the store's tracer would count them on the request's meter.
type meteredDefinitions struct{ api.Definitions }

func (d meteredDefinitions) Factories(ctx context.Context) ([]ports.FactoryRecord, error) {
	m := ports.DBMeterFrom(ctx)
	m.AddQuery()
	m.AddQuery()
	m.AddWait(7 * time.Millisecond)

	return d.Definitions.Factories(ctx)
}

// Every request's line says how many queries it ran and how long it waited for connections: a
// request that ran none says 0 and 0.
func TestAccessLog_LogsQueriesAndDBWait(t *testing.T) {
	t.Parallel()

	logs := &logBuffer{}
	h := newHarnessLogged(t, nil, logs, func(_ *harness, d *api.Deps) {
		d.Definitions = meteredDefinitions{d.Definitions}
	})

	for _, c := range []struct {
		path            string
		queries, waitMS float64
	}{{acmePath, 2, 7}, {healthPath, 0, 0}} {
		res := h.request(t, http.MethodGet, c.path, "", nil)
		if res.status != http.StatusOK {
			t.Fatalf("GET %s = %d %s", c.path, res.status, res.body)
		}

		line := logs.accessLine(t, res.header.Get("X-Request-Id"))
		if line["queries"] != c.queries || line["db_wait_ms"] != c.waitMS {
			t.Errorf("GET %s: queries = %v, db_wait_ms = %v; want %v, %v (line %v)", c.path, line["queries"], line["db_wait_ms"], c.queries, c.waitMS, line)
		}
	}
}
