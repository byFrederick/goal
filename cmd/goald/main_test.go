package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/web"
)

// Without a gateway listener the main one serves the gateway beside the API and the SPA; with one,
// it serves only /mcp and the main one answers /mcp with 404 (phase 3 §5.5, threat model row 4).
func TestRoutes_TheGatewayListenerServesOnlyTheGateway(t *testing.T) {
	t.Parallel()

	const toAPI, toGateway, toSPA, notFound = "api", "gateway", "spa", "404"

	named := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(name)) })
	}

	served := func(h http.Handler, path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil))

		if rec.Code == http.StatusNotFound {
			return notFound
		}

		return rec.Body.String()
	}

	paths := []string{"/mcp", "/v1/health", "/v1/orgs", "/", "/board"}

	one, none := routes(false, named(toAPI), named(toGateway), named(toSPA))
	if none != nil {
		t.Fatal("no GOAL_GATEWAY_LISTEN, yet a gateway listener")
	}

	primary, gatewayOnly := routes(true, named(toAPI), named(toGateway), named(toSPA))

	tests := []struct {
		name string
		h    http.Handler
		want []string // for paths, in order
	}{
		{name: "one listener", h: one, want: []string{toGateway, toAPI, toAPI, toSPA, toSPA}},
		{name: "main listener beside a gateway listener", h: primary, want: []string{notFound, toAPI, toAPI, toSPA, toSPA}},
		{name: "gateway listener", h: gatewayOnly, want: []string{toGateway, notFound, notFound, notFound, notFound}},
	}

	for _, tt := range tests {
		for i, path := range paths {
			if got := served(tt.h, path); got != tt.want[i] {
				t.Errorf("%s: %s served by %s, want %s", tt.name, path, got, tt.want[i])
			}
		}
	}
}

// goald forwards /api/auth/* to the identity service only under GOAL_AUTH=session; under none it
// is no route at all (the UI's 404, never its index page), and the service is never reached.
func TestHTTPServers_ProxyAuthOnlyUnderSession(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	ui := web.Handler(web.Options{})
	stub := http.NotFoundHandler()

	tests := []struct {
		mode authMode
		want int
		hits int32
	}{
		{mode: authNone, want: http.StatusNotFound, hits: 0},
		{mode: authSession, want: http.StatusOK, hits: 1},
	}

	for _, tt := range tests {
		hits.Store(0)

		s := settings{Listen: defaultListen, Auth: tt.mode, AuthUpstream: u}
		main := httpServers(s, "proxy-secret-for-tests-only-0123456789", stub, stub, ui, nil)[0]

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/get-session", nil)
		r.Host = defaultListen

		rec := httptest.NewRecorder()
		main.Handler.ServeHTTP(rec, r)

		if rec.Code != tt.want || hits.Load() != tt.hits {
			t.Errorf("GOAL_AUTH=%s: status %d, the service reached %d times; want %d and %d", tt.mode, rec.Code, hits.Load(), tt.want, tt.hits)
		}
	}
}

// The server bounds how long a client may take to send a request and how long an idle
// connection stays open, but never how long a response takes: the realtime stream is one
// response that lasts as long as the client listens.
func TestHTTPServer_BoundsReadsAndIdleConnectionsButNotStreams(t *testing.T) {
	t.Parallel()

	srv := httpServer(defaultListen, http.NotFoundHandler())

	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Errorf("timeouts: read header %s, read %s, idle %s; want each bounded", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}

	if srv.WriteTimeout != 0 {
		t.Errorf("write timeout %s would end every stream; want none", srv.WriteTimeout)
	}

	if srv.Addr != defaultListen {
		t.Errorf("addr %s, want the listen address", srv.Addr)
	}
}

// Threat model row 36: GOAL_AUTH=none, only ever chosen explicitly from 3-16, says at startup that
// every request, agents' included, acts as the local user, and looks nothing up.
func TestAuthentication_NoneWarnsThatEveryRequestIsTheLocalUser(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	identity, _, err := authentication(t.Context(), settings{Auth: authNone, Listen: "127.0.0.1:8080"}, nil, nil, slog.New(slog.NewTextHandler(&buf, nil)))
	if err != nil || identity != nil {
		t.Fatalf("authentication under none = %v, %v; want no identity lookups", identity, err)
	}

	if log := buf.String(); !strings.Contains(log, "level=WARN") || !strings.Contains(log, "acts as the local user") ||
		!strings.Contains(log, "agent containers included") {
		t.Errorf("startup log %q; want the local-user warning", log)
	}
}
