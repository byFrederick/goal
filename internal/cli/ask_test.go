package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

const webDraftRequests = "/v1/orgs/acme/factories/web/draft/requests"

// goalAsk runs goal factory ask args against api and returns what it printed and its exit code.
func goalAsk(t *testing.T, api string, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer

	out := &printer{w: &stdout, e: &stderr}
	getenv := testEnv(t, api, map[string]string{envOrg: orgAcme})
	code := 0

	if err := errors.Join(factoryAsk(t.Context(), out, getenv, args), out.err); err != nil {
		code = 1
		_, _ = fmt.Fprintln(&stderr, "goal:", err)
	}

	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", code, stdout.String(), stderr.String())
}

// notBuiltGoald answers every ask 501 not-built, as goald does until a draft request runner is
// wired, and fails the test when the request is read.
func notBuiltGoald(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var asks atomic.Int32

	code, detail := "not-built", "draft requests are not built yet: goald has no runner for them, so it does not queue them"
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+webDraftRequests, func(w http.ResponseWriter, _ *http.Request) {
		asks.Add(1)
		writeJSON(t, w, http.StatusNotImplemented, gen.Problem{
			Status: http.StatusNotImplemented, Title: http.StatusText(http.StatusNotImplemented), Code: &code, Detail: &detail,
		})
	})
	mux.HandleFunc("GET "+webDraftRequests+"/", func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("goal factory ask read %s; a refused request has nothing to wait for", r.URL.Path)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, &asks
}

func TestFactoryAsk_RefusesNotBuilt(t *testing.T) {
	t.Parallel()

	srv, asks := notBuiltGoald(t)

	for _, args := range [][]string{
		{searchFactory, "give review a 4th reviewer"},
		{searchFactory, "give review a 4th reviewer", "--wait"},
		{searchFactory, "fix the writer", "--agent", "writer", "--wait"},
	} {
		got := goalAsk(t, srv.URL, args...)
		want := "exit 1\n--- stdout\n--- stderr\ngoal: refused: draft requests are not built yet; goald does not run them, " +
			"so goal factory ask cannot change the draft of web. Change it with goal factory pull and goal factory push\n"

		if got != want {
			t.Errorf("goal factory ask %v printed\n%s\nwant\n%s", args, got, want)
		}
	}

	if n := asks.Load(); n != 3 {
		t.Errorf("goald was asked %d times, want 3", n)
	}

	if got := goalAsk(t, srv.URL, searchFactory); !strings.HasPrefix(got, "exit 1") {
		t.Errorf("ask without text printed\n%s\nwant usage", got)
	}
}

// A goald that still queues requests gets its answer printed, and --wait does not read it.
func TestFactoryAsk_PrintsAQueuedRequestWithoutWaiting(t *testing.T) {
	t.Parallel()

	id := "01900000-0000-7000-8000-000000000001"
	srv, sent := routeGoald(t, map[string]goaldAnswer{
		"POST " + webDraftRequests: {http.StatusCreated, gen.DraftRequest{Id: id, Text: "fix it", Status: gen.DraftRequestStatusQueued, ChangedPaths: []string{}}},
	})

	got := goalAsk(t, srv.URL, searchFactory, "fix it", "--wait")
	if want := "exit 0\n--- stdout\nasked web: request " + id + " is queued\n--- stderr\n"; got != want {
		t.Errorf("goal factory ask printed\n%s\nwant\n%s", got, want)
	}

	if s := sent(); len(s) != 1 {
		t.Errorf("goald was sent %v, want only the ask", s)
	}
}

func TestFactoryAsk_ExplainsAnOpenRequest(t *testing.T) {
	t.Parallel()

	code := "draft-request-open"
	detail := "the factory already has a queued or running draft request"
	srv, _ := routeGoald(t, map[string]goaldAnswer{
		"POST " + webDraftRequests: {http.StatusConflict, gen.Problem{Status: http.StatusConflict, Title: http.StatusText(http.StatusConflict), Code: &code, Detail: &detail}},
	})

	if got := goalAsk(t, srv.URL, searchFactory, "another"); !strings.HasPrefix(got, "exit 1") || !strings.Contains(got, "one request at a time") {
		t.Errorf("ask while one is open printed\n%s\nwant that a factory runs one request at a time", got)
	}
}
