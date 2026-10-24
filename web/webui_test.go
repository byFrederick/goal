package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// index is what every client-side route answers with.
const index = "<title>Goal</title>"

func testSPA(t *testing.T) http.Handler {
	t.Helper()

	h, err := New(fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>" + index)},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"assets/index-a1b2.js": {Data: []byte("console.log(1)")},
	})
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func TestSPA_ServesFilesAndFallsBackToIndexForRoutes(t *testing.T) {
	t.Parallel()

	h := testSPA(t)

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
		wantCache  string
	}{
		{"/", 200, index, revalidate},
		{"/f/goal/board", 200, index, revalidate},
		{"/f/goal/items/12", 200, index, revalidate},
		{"/index.html", 200, index, revalidate},
		{"/assets/index-a1b2.js", 200, "console.log(1)", immutable},
		{"/favicon.svg", 200, "<svg/>", revalidate},
		{"/assets/index-gone.js", 404, "", ""},
		{"/assets/", 404, "", ""},
		{"/v1/unknown", 404, "", ""},
		{"/mcp/anything", 404, "", ""},
		{"/robots.txt", 404, "", ""},
		{"/../../etc/passwd", 200, index, revalidate}, // cleaned to a route, never a file outside the tree
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}

			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
		})
	}
}

// Every answer carries the Content-Security-Policy and frame protection: Approve, Merge and the
// andon cord must not be clickable through a frame once goald is reachable beyond localhost.
func TestSPA_SetsContentSecurityPolicyAndFrameProtection(t *testing.T) {
	t.Parallel()

	const want = "default-src 'self'; script-src 'self'; worker-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'"

	h := testSPA(t)
	for _, p := range []string{"/", "/o/acme/f/goal/board", "/assets/index-a1b2.js", "/favicon.svg", "/v1/unknown"} {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))

			if got := rec.Header().Get("Content-Security-Policy"); got != want {
				t.Errorf("Content-Security-Policy = %q, want %q", got, want)
			}

			if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
		})
	}
}

func TestSPA_RefusesWrites(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testSPA(t).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/f/goal", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestNew_NeedsIndex(t *testing.T) {
	t.Parallel()

	if _, err := New(fstest.MapFS{"assets/a.js": {Data: []byte("x")}}); err == nil {
		t.Fatal("New accepted a tree without index.html")
	}
}

// The embedded tree (the placeholder without -tags webui, the Vite build with it) must serve
// its index for client routes and, for a build, its hashed assets with a long cache.
func TestHandler_ServesTheEmbeddedTree(t *testing.T) {
	t.Parallel()

	h := Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/f/goal/board", nil))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("status = %d, body = %.80q", rec.Code, rec.Body.String())
	}

	asset := regexp.MustCompile(`/assets/[^"]+\.js`).FindString(rec.Body.String())
	if asset == "" {
		return // the placeholder has no assets
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, asset, nil))

	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != immutable {
		t.Fatalf("%s: status = %d, Cache-Control = %q", asset, rec.Code, rec.Header().Get("Cache-Control"))
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("%s: Content-Type = %q", asset, ct)
	}
}
