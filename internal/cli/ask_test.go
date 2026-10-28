package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	webDraftRequests = "/v1/orgs/acme/factories/web/draft/requests"
	askID            = "01900000-0000-7000-8000-000000000001"
)

// goalAsk runs goal factory ask args against api, polling a --wait every millisecond, and returns
// what it printed and its exit code.
func goalAsk(t *testing.T, api string, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer

	out := &printer{w: &stdout, e: &stderr}
	getenv := testEnv(t, api, map[string]string{envOrg: orgAcme})
	code := 0

	if err := errors.Join(factoryAsk(t.Context(), out, getenv, args, time.Millisecond), out.err); err != nil {
		code = 1
		_, _ = fmt.Fprintln(&stderr, "goal:", err)
	}

	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", code, stdout.String(), stderr.String())
}

func draftRequestOf(status gen.DraftRequestStatus) gen.DraftRequest {
	r := gen.DraftRequest{
		Id: askID, Text: "give review a 4th reviewer", Status: status, ChangedPaths: []string{},
		Requester: gen.Actor{Kind: gen.ActorKindUser, Id: "local", Name: "Local user"}, CreatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
	}

	switch status {
	case gen.DraftRequestStatusDone:
		r.Summary, r.ChangedPaths, r.CostUsd, r.Turns = "Added a fourth reviewer to review.", []string{"factory.yaml", "agents/reviewer.md"}, 0.42, 7
	case gen.DraftRequestStatusFailed:
		r.Summary, r.Error = "Nothing was changed.", "factory.yaml changed in the draft while the request ran; ask again"
	case gen.DraftRequestStatusQueued, gen.DraftRequestStatusRunning:
	}

	return r
}

func TestFactoryAsk_PrintsIDAndStatus(t *testing.T) {
	t.Parallel()

	srv, sent := evalGoald(t, map[string]evalAnswer{
		"POST " + webDraftRequests: {http.StatusCreated, draftRequestOf(gen.DraftRequestStatusQueued)},
	})

	got := goalAsk(t, srv.URL, searchFactory, "give review a 4th reviewer")
	want := "exit 0\n--- stdout\nasked web: request " + askID + " is queued\n" +
		"goald changes the draft when it runs; --wait waits for it\n--- stderr\n"

	if got != want {
		t.Errorf("goal factory ask printed\n%s\nwant\n%s", got, want)
	}

	goalAsk(t, srv.URL, searchFactory, "fix the writer", "--agent", "writer")

	wantSent := []string{
		"POST " + webDraftRequests + ` {"text":"give review a 4th reviewer"}`,
		"POST " + webDraftRequests + ` {"agent":"writer","text":"fix the writer"}`,
	}
	if s := sent(); strings.Join(s, "\n") != strings.Join(wantSent, "\n") {
		t.Errorf("goald was sent\n%s\nwant\n%s", strings.Join(s, "\n"), strings.Join(wantSent, "\n"))
	}

	if got := goalAsk(t, srv.URL, searchFactory); !strings.HasPrefix(got, "exit 1") {
		t.Errorf("ask without text printed\n%s\nwant usage", got)
	}
}

// waitGoald answers the create queued and each read running until the third, which is final.
func waitGoald(t *testing.T, final gen.DraftRequest) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var reads atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST "+webDraftRequests, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusCreated, draftRequestOf(gen.DraftRequestStatusQueued))
	})
	mux.HandleFunc("GET "+webDraftRequests+"/"+askID, func(w http.ResponseWriter, _ *http.Request) {
		r := draftRequestOf(gen.DraftRequestStatusRunning)
		if reads.Add(1) > 2 {
			r = final
		}

		writeJSON(t, w, http.StatusOK, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, &reads
}

func TestFactoryAsk_WaitPrintsSummaryAndChangedPaths(t *testing.T) {
	t.Parallel()

	srv, reads := waitGoald(t, draftRequestOf(gen.DraftRequestStatusDone))

	got := goalAsk(t, srv.URL, searchFactory, "give review a 4th reviewer", "--wait")
	want := "exit 0\n--- stdout\nasked web: request " + askID + " is queued\n" +
		"request " + askID + " is done: 7 turns, $0.42\n" +
		"Added a fourth reviewer to review.\n" +
		"changed in the draft:\n  factory.yaml\n  agents/reviewer.md\n" +
		"review the draft and publish it with: goal factory publish web\n--- stderr\n"

	if got != want {
		t.Errorf("goal factory ask --wait printed\n%s\nwant\n%s", got, want)
	}

	if n := reads.Load(); n != 3 {
		t.Errorf("--wait read the request %d times, want 3: until it ended", n)
	}
}

func TestFactoryAsk_WaitFailsWithTheRequestsError(t *testing.T) {
	t.Parallel()

	srv, _ := waitGoald(t, draftRequestOf(gen.DraftRequestStatusFailed))

	got := goalAsk(t, srv.URL, searchFactory, "give review a 4th reviewer", "--wait")
	want := "exit 1\n--- stdout\nasked web: request " + askID + " is queued\n" +
		"request " + askID + " is failed: 0 turns, $0.00\n" +
		"Nothing was changed.\n" +
		"--- stderr\ngoal: request " + askID + " failed: factory.yaml changed in the draft while the request ran; ask again\n"

	if got != want {
		t.Errorf("goal factory ask --wait of a failed request printed\n%s\nwant\n%s", got, want)
	}
}

func TestFactoryAsk_WaitStopsOnCtrlC(t *testing.T) {
	t.Parallel()

	srv, _ := evalGoald(t, map[string]evalAnswer{
		"GET " + webDraftRequests + "/" + askID: {http.StatusOK, draftRequestOf(gen.DraftRequestStatusRunning)},
	})

	c, err := connect(t.Context(), testEnv(t, srv.URL, map[string]string{envOrg: orgAcme}), "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if _, err := awaitDraftRequest(ctx, c, searchFactory, askID, time.Millisecond); err == nil || !strings.Contains(err.Error(), "keeps running") {
		t.Errorf("wait cut short = %v, want that the request keeps running", err)
	}
}

func TestFactoryAsk_ExplainsAnOpenRequest(t *testing.T) {
	t.Parallel()

	code := "draft-request-open"
	detail := "the factory already has a queued or running draft request"
	srv, _ := evalGoald(t, map[string]evalAnswer{
		"POST " + webDraftRequests: {http.StatusConflict, gen.Problem{Status: http.StatusConflict, Title: http.StatusText(http.StatusConflict), Code: &code, Detail: &detail}},
	})

	if got := goalAsk(t, srv.URL, searchFactory, "another"); !strings.HasPrefix(got, "exit 1") || !strings.Contains(got, "one request at a time") {
		t.Errorf("ask while one is open printed\n%s\nwant that a factory runs one request at a time", got)
	}
}
