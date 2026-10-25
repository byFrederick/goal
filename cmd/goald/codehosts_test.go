package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/domain"
)

// Every platform kind that names a code host has its adapter in goald, so its connections merge.
func TestCodeHosts_EveryPlatformCodeHostHasAnAdapter(t *testing.T) {
	t.Parallel()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	hosts := codeHosts(false)

	for _, k := range kinds {
		if name := k.Spec.CodeHost; name != "" && hosts[name] == nil {
			t.Errorf("kind %s names code host %q, which goald lacks", k.Name, name)
		}
	}
}

// A merge reaches the connection's host through the connection transport: public addresses
// only (threat row 4), so a connection whose host is goald's own network gets no request.
func TestCodeHosts_ReachPublicAddressesOnly(t *testing.T) {
	t.Parallel()

	var asked atomic.Int64

	private := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked.Add(1) }))
	t.Cleanup(private.Close)

	host := strings.TrimPrefix(private.URL, "https://")

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	for name, ch := range codeHosts(false) {
		var kind domain.ConnectionKind

		for _, k := range kinds {
			if k.Spec.CodeHost == name {
				kind = k
			}
		}

		conn := domain.ResolvedConnection{
			Connection: domain.Connection{Name: name, Kind: kind.Name}, Kind: kind,
			Values: map[string]string{"token": "a-token-never-sent", "host": host}, //nolint:gosec // a made-up token
		}

		url := "https://" + host + "/acme/app/pull/1"
		if name == "gitlab" {
			url = "https://" + host + "/acme/app/-/merge_requests/1"
		}

		if _, err := ch.PullRequest(t.Context(), conn, url); err == nil || strings.Contains(err.Error(), "a-token-never-sent") {
			t.Errorf("%s: PullRequest on a loopback host = %v; want it refused, without the token", name, err)
		}
	}

	if n := asked.Load(); n != 0 {
		t.Errorf("the private host was asked %d times; want never", n)
	}
}
