package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// listFactoriesRoute is goald's route of an organization's factories.
const listFactoriesRoute = "GET /v1/orgs/acme/factories"

// routeGoald serves routes, answering each with status and v as JSON, and records the requests it
// was sent with their bodies.
func routeGoald(t *testing.T, routes map[string]goaldAnswer) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu   sync.Mutex
		sent []string
	)

	mux := http.NewServeMux()

	for pattern, a := range routes {
		b, err := json.Marshal(a.body)
		if err != nil {
			t.Fatal(err)
		}

		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)

			mu.Lock()

			sent = append(sent, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+string(body)))
			mu.Unlock()

			ct := "application/json"
			if a.status >= 400 {
				ct = "application/problem+json"
			}

			w.Header().Set("Content-Type", ct)
			w.WriteHeader(a.status)
			_, _ = w.Write(b)
		})
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), sent...)
	}
}

// goaldAnswer is what one route of routeGoald answers.
type goaldAnswer struct {
	status int
	body   any
}

// writeJSON writes v as JSON with status.
func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}
