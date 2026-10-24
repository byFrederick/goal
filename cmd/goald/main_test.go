package main

import (
	"net/http"
	"testing"
)

// The server bounds how long a client may take to send a request and how long an idle
// connection stays open, but never how long a response takes: the realtime stream is one
// response that lasts as long as the client listens.
func TestHTTPServer_BoundsReadsAndIdleConnectionsButNotStreams(t *testing.T) {
	t.Parallel()

	srv := httpServer(settings{Listen: "127.0.0.1:8080"}, http.NotFoundHandler())

	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Errorf("timeouts: read header %s, read %s, idle %s; want each bounded", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}

	if srv.WriteTimeout != 0 {
		t.Errorf("write timeout %s would end every stream; want none", srv.WriteTimeout)
	}

	if srv.Addr != "127.0.0.1:8080" {
		t.Errorf("addr %s, want the listen address", srv.Addr)
	}
}
