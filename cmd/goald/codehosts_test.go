package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A merge reaches the repository's host through the connection transport: public addresses
// only (threat row 4), so a connection whose host is goald's own network gets no request.
func TestCodeHosts_ReachPublicAddressesOnly(t *testing.T) {
	t.Parallel()

	var asked atomic.Int64

	private := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked.Add(1) }))
	t.Cleanup(private.Close)

	host := strings.TrimPrefix(private.URL, "https://")

	for name, ch := range codeHosts(false) {
		// Built by hand: the organization's list never holds a host with a port.
		repo := domain.CodeRepository{Host: domain.CodeHost{Host: host, Kind: domain.CodeHostKind(name)}, Owner: "acme", Name: "app"}

		url := "https://" + host + "/acme/app/pull/1"
		if name == "gitlab" {
			url = "https://" + host + "/acme/app/-/merge_requests/1"
		}

		if _, err := ch.PullRequest(t.Context(), repo, "a-token-never-sent", url); err == nil || strings.Contains(err.Error(), "a-token-never-sent") {
			t.Errorf("%s: PullRequest on a loopback host = %v; want it refused, without the token", name, err)
		}
	}

	if n := asked.Load(); n != 0 {
		t.Errorf("the private host was asked %d times; want never", n)
	}
}
