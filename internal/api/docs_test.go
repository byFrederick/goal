package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api"
)

// docsTree writes a small docs build: an index, a nested page, a directory without an index and,
// beside the docs directory, a secret that must never be served.
func docsTree(t *testing.T) string {
	t.Helper()

	base := t.TempDir()
	dir := filepath.Join(base, "docs")

	files := map[string]string{
		"index.html":              "docs home",
		"guides/index.html":       "guides",
		"guides/first/index.html": "first guide",
		"pagefind/pagefind.js":    "search",
		"empty/readme.txt":        "no index here",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func docsHandler(t *testing.T, dir string) http.Handler {
	t.Helper()

	h, err := api.Docs(dir)
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func serveDocs(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))

	return rec
}

func TestDocs_ServesIndexNestedPageAnd404(t *testing.T) {
	t.Parallel()

	h := docsHandler(t, docsTree(t))

	tests := []struct {
		method, path string
		code         int
		body         string // a substring of the body, or the Location for a redirect
	}{
		{http.MethodGet, "/docs/", http.StatusOK, "docs home"},
		{http.MethodGet, "/docs", http.StatusMovedPermanently, "/docs/"},
		{http.MethodGet, "/docs/guides/", http.StatusOK, "guides"},
		{http.MethodGet, "/docs/guides", http.StatusMovedPermanently, "/docs/guides/"},
		{http.MethodGet, "/docs/guides/first/", http.StatusOK, "first guide"},
		{http.MethodGet, "/docs/pagefind/pagefind.js", http.StatusOK, "search"},
		{http.MethodHead, "/docs/guides/", http.StatusOK, ""},
		{http.MethodGet, "/docs/missing/", http.StatusNotFound, ""},
		{http.MethodGet, "/docs/guides/missing.html", http.StatusNotFound, ""},
		{http.MethodGet, "/docs/pagefind/pagefind.js/", http.StatusNotFound, ""},
		// A directory without an index is a 404, never a listing.
		{http.MethodGet, "/docs/empty/", http.StatusNotFound, ""},
		{http.MethodGet, "/docs/index.html/", http.StatusNotFound, ""},
		{http.MethodPost, "/docs/", http.StatusMethodNotAllowed, ""},
		{http.MethodGet, "/docsx", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		rec := serveDocs(t, h, tt.method, tt.path)
		if rec.Code != tt.code {
			t.Errorf("%s %s: status %d, want %d", tt.method, tt.path, rec.Code, tt.code)

			continue
		}

		switch {
		case tt.code == http.StatusMovedPermanently:
			if got := rec.Header().Get("Location"); got != tt.body {
				t.Errorf("%s %s: Location %q, want %q", tt.method, tt.path, got, tt.body)
			}
		case !strings.Contains(rec.Body.String(), tt.body):
			t.Errorf("%s %s: body %q, want it to contain %q", tt.method, tt.path, rec.Body.String(), tt.body)
		}

		if tt.path == "/docs/empty/" && strings.Contains(rec.Body.String(), "readme.txt") {
			t.Errorf("GET /docs/empty/ lists the directory: %q", rec.Body.String())
		}
	}

	if rec := serveDocs(t, h, http.MethodGet, "/docs/"); rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", rec.Header().Get("X-Content-Type-Options"))
	}
}

func TestDocs_RefusesPathTraversal(t *testing.T) {
	t.Parallel()

	dir := docsTree(t)
	// A symlink inside the build that points outside it.
	if err := os.Symlink(filepath.Join(filepath.Dir(dir), "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Dir(dir), filepath.Join(dir, "up")); err != nil {
		t.Fatal(err)
	}

	h := docsHandler(t, dir)

	for _, target := range []string{
		"/docs/../secret.txt",
		"/docs/%2e%2e/secret.txt",
		"/docs/guides/%2E%2E/%2e%2e/secret.txt",
		"/docs/..%2fsecret.txt",
		"/docs/link.txt",
		"/docs/up/secret.txt",
		"/docs/up/",
	} {
		rec := serveDocs(t, h, http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rec.Code)
		}

		if strings.Contains(rec.Body.String(), "outside") {
			t.Errorf("GET %s served a file outside the docs directory", target)
		}
	}
}

func TestDocs_RefusesAMissingDirectory(t *testing.T) {
	t.Parallel()

	if _, err := api.Docs(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("Docs of a missing directory: no error")
	}
}
