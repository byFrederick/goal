package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
