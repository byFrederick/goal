package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The API never accepts an agent's task token, nor any other credential it cannot use: such a
// request is 401 with one body, never the local user, while a request without a credential (the
// SPA and the CLI under GOAL_AUTH=none) and the credential shapes phase 3 resolves still pass
// (threat model rows 4 and 21).
func TestAPI_RefusesTaskTokensAndCredentialsItCannotUse(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	task, err := domain.SignTaskToken([]byte("gateway-key-0123456789abcdef0123"), domain.TaskClaims{
		OrgID: testOrg, FactoryID: acmeID, VersionID: "v1", ItemID: "i1", Station: "build", Agent: "engineer", RunID: "r1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		header string
		ok     bool
	}{
		{name: "no credential", ok: true},
		{name: "an API key", header: "Bearer goal_sk_" + strings.Repeat("A", 43), ok: true},
		{name: "a session token", header: "bearer " + strings.Repeat("a1", 16), ok: true},
		{name: "a task token", header: "Bearer " + task},
		{name: "a task token without the scheme", header: task},
		{name: "a task token in another scheme", header: "Basic " + task},
		{name: "a short API key", header: "Bearer goal_sk_" + strings.Repeat("A", 42)},
		{name: "a session token with a signature", header: "Bearer " + strings.Repeat("a", 32) + ".sig"},
		{name: "an empty bearer", header: "Bearer "},
	}

	for _, tt := range tests {
		for _, call := range []struct{ method, path, body string }{
			{http.MethodGet, "/v1/health", ""},
			{http.MethodGet, "/v1/orgs/acme/factories", ""},
			{http.MethodPost, acmeItems, submitFix},
		} {
			before := h.items(t)

			var header http.Header
			if tt.header != "" {
				header = http.Header{"Authorization": {tt.header}}
			}

			res := h.request(t, call.method, call.path, call.body, header)

			if tt.ok {
				if res.status >= 400 {
					t.Errorf("%s: %s %s = %d %s, want it served", tt.name, call.method, call.path, res.status, res.body)
				}

				continue
			}

			if !unauthenticated(res) {
				t.Errorf("%s: %s %s = %d %s (WWW-Authenticate %q), want 401 unauthenticated without detail",
					tt.name, call.method, call.path, res.status, res.body, res.header.Get("WWW-Authenticate"))
			}

			if after := h.items(t); after != before {
				t.Errorf("%s: %d items after %d, want nothing written", tt.name, after, before)
			}
		}
	}
}

// unauthenticated reports whether res is the API's one 401: problem unauthenticated without
// detail, with the bearer challenge.
func unauthenticated(res response) bool {
	var p gen.Problem
	if err := json.Unmarshal(res.body, &p); err != nil {
		return false
	}

	return res.status == http.StatusUnauthorized && p.Code != nil && *p.Code == "unauthenticated" && p.Detail == nil &&
		res.header.Get("WWW-Authenticate") == `Bearer realm="goal"`
}
