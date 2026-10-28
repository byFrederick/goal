package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
)

// A rebinding page's name and an API path it would call.
const (
	evilHost  = "evil.example"
	anAPIPath = "/v1/orgs/acme/factories"
)

// The Host check refuses, with 421, every request but the health check and the agents' gateway
// whose Host names a host other than goald's own, so a page that rebinds its name to the loopback
// address cannot use the API or the SPA even though its requests look same-origin.
func TestCheckHost(t *testing.T) {
	t.Parallel()

	served := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := api.CheckHost([]string{"localhost", "127.0.0.1", "::1", "127.0.0.2", "goal.example.com"}, served)

	tests := []struct {
		name, method, host, path string
		want                     int
	}{
		{name: "rebound name reading the API", method: http.MethodGet, host: "evil.example:8080", path: anAPIPath, want: http.StatusMisdirectedRequest},
		{name: "rebound name changing things", method: http.MethodPost, host: evilHost, path: anAPIPath, want: http.StatusMisdirectedRequest},
		{name: "rebound name loading the SPA", method: http.MethodGet, host: evilHost, path: "/", want: http.StatusMisdirectedRequest},
		{name: "a name ending in an allowed one", method: http.MethodGet, host: "localhost.evil.example", path: anAPIPath, want: http.StatusMisdirectedRequest},
		{name: "no Host", method: http.MethodGet, host: "", path: anAPIPath, want: http.StatusMisdirectedRequest},
		{name: "a path under the gateway's", method: http.MethodGet, host: evilHost, path: "/mcp/x", want: http.StatusMisdirectedRequest},
		{name: "loopback name on the listen port", method: http.MethodGet, host: "127.0.0.1:8080", path: anAPIPath, want: http.StatusNoContent},
		{name: "localhost through the dev server's proxy", method: http.MethodPost, host: "localhost:5173", path: anAPIPath, want: http.StatusNoContent},
		{name: "IPv6 loopback", method: http.MethodGet, host: "[::1]:8080", path: "/", want: http.StatusNoContent},
		{name: "the listen host", method: http.MethodGet, host: "127.0.0.2:8080", path: anAPIPath, want: http.StatusNoContent},
		{name: "the public URL's host, any case", method: http.MethodGet, host: "Goal.Example.com", path: "/", want: http.StatusNoContent},
		{name: "health check from anywhere", method: http.MethodGet, host: evilHost, path: "/v1/health", want: http.StatusNoContent},
		{name: "agents reach the gateway as the Docker host", method: http.MethodPost, host: "host.docker.internal:8080", path: api.GatewayPath, want: http.StatusNoContent},
		{name: "and its run bundle", method: http.MethodGet, host: "host.docker.internal:8080", path: api.RunPath, want: http.StatusNoContent},
		{name: "and its tool approvals", method: http.MethodPost, host: "host.docker.internal:8080", path: api.ToolApprovalPath, want: http.StatusNoContent},
		{name: "a path under the tool approvals'", method: http.MethodPost, host: evilHost, path: api.ToolApprovalPath + "/x", want: http.StatusMisdirectedRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			r.Host = tt.host
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Origin", "http://"+tt.host)

			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != tt.want {
				t.Fatalf("%s %s with Host %q = %d, want %d", tt.method, tt.path, tt.host, w.Code, tt.want)
			}

			if tt.want != http.StatusMisdirectedRequest {
				return
			}

			var p gen.Problem
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.Status != http.StatusMisdirectedRequest {
				t.Errorf("body %q, want a 421 problem", w.Body.String())
			}
		})
	}
}
