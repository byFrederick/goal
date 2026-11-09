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
	paths := []string{"/v1/health", "/v1/orgs", "/api/auth/get-session"}

	for _, p := range paths {
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s while starting: status %d, want 503", p, rec.Code)
		}

		if got := rec.Header().Get("Retry-After"); got != strconv.Itoa(api.StartingRetryAfter) {
			t.Errorf("%s: Retry-After %q, want %d", p, got, api.StartingRetryAfter)
		}

		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control %q, want no-store", p, got)
		}

		if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
			t.Errorf("%s: Content-Type %q, want application/problem+json", p, got)
		}

		var body gen.Problem
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("%s: decode: %v", p, err)
		}

		if body.Status != http.StatusServiceUnavailable || body.Code == nil || *body.Code != "goald-starting" || body.Detail == nil || *body.Detail == "" {
			t.Errorf("%s: problem %+v, want status 503, code goald-starting and a detail", p, body)
		}
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
