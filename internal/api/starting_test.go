package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
)

// Until goald has started, the health check, the API and the auth proxy answer 503
// goald-starting at once, never cached, with Retry-After; once it is ready they reach the real
// handler.
func TestStarting_AnswersStartingUntilReady(t *testing.T) {
	t.Parallel()

	gate := api.NewStarting()
	paths := []string{healthPath, "/v1/orgs", "/api/auth/get-session"}

	for _, p := range paths {
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))
		checkStarting(t, p, rec)
	}

	gate.Ready(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reached", r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}))

	for _, p := range paths {
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))

		if rec.Code != http.StatusTeapot || rec.Header().Get("X-Reached") != p {
			t.Errorf("%s after Ready: status %d, reached %q; want the real handler", p, rec.Code, rec.Header().Get("X-Reached"))
		}

		if rec.Header().Get("Retry-After") != "" {
			t.Errorf("%s after Ready: Retry-After %q, want none", p, rec.Header().Get("Retry-After"))
		}
	}
}

// checkStarting fails t unless rec is the 503 goald-starting answer.
func checkStarting(t *testing.T, path string, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("%s while starting: status %d, want 503", path, rec.Code)
	}

	h := rec.Header()
	if h.Get("Retry-After") != strconv.Itoa(api.StartingRetryAfter) || h.Get("Cache-Control") != "no-store" || h.Get("Content-Type") != problemType {
		t.Errorf("%s: Retry-After %q, Cache-Control %q, Content-Type %q; want %d, no-store and %s",
			path, h.Get("Retry-After"), h.Get("Cache-Control"), h.Get("Content-Type"), api.StartingRetryAfter, problemType)
	}

	var body gen.Problem
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("%s: decode: %v", path, err)
	}

	if body.Status != http.StatusServiceUnavailable || body.Code == nil || *body.Code != "goald-starting" || body.Detail == nil || *body.Detail == "" {
		t.Errorf("%s: problem %+v, want status 503, code goald-starting and a detail", path, body)
	}
}
