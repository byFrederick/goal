package web

import (
	"fmt"
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

	return testSPAWith(t, Options{})
}

func testSPAWith(t *testing.T, o Options) http.Handler {
	t.Helper()

	h, err := New(fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>" + index)},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"assets/index-a1b2.js": {Data: []byte("console.log(1)")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Goal"}`)},
	}, o)
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
		{"/manifest.webmanifest", 200, `"name":"Goal"`, revalidate},
		{"/assets/index-gone.js", 404, "", ""},
		{"/assets/", 404, "", ""},
		{"/v1/unknown", 404, "", ""},
		{"/api", 404, "", ""},
		{"/api/auth/get-session", 404, "", ""},
		{"/api/unknown", 404, "", ""},
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

// Every answer carries the Content-Security-Policy, frame protection and the other security
// headers: Approve, Merge and the andon cord must not be clickable through a frame, no script of
// another origin may run, and no other origin keeps a handle on the app's window, once goald is
// reachable beyond localhost. Over https, browsers are told to keep to it.
func TestSPA_SetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	const csp = "default-src 'self'; script-src 'self'; worker-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; " +
		"form-action 'self'; frame-ancestors 'none'"

	want := map[string]string{
		"Content-Security-Policy":    csp,
		"X-Frame-Options":            "DENY",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Referrer-Policy":            "same-origin",
		"X-Content-Type-Options":     "nosniff",
	}

	for _, https := range []bool{false, true} {
		h := testSPAWith(t, Options{StrictTransport: https})

		for _, req := range []struct{ method, path string }{
			{http.MethodGet, "/"},
			{http.MethodGet, "/o/acme/f/goal/board"},
			{http.MethodGet, "/assets/index-a1b2.js"},
			{http.MethodGet, "/favicon.svg"},
			{http.MethodGet, "/v1/unknown"},
			{http.MethodGet, "/api/unknown"},
			{http.MethodPost, "/o/acme"},
		} {
			t.Run(fmt.Sprintf("https=%v %s %s", https, req.method, req.path), func(t *testing.T) {
				t.Parallel()

				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), req.method, req.path, nil))

				for name, value := range want {
					if got := rec.Header().Get(name); got != value {
						t.Errorf("%s = %q, want %q", name, got, value)
					}
				}

				hsts := rec.Header().Get("Strict-Transport-Security")
				if https && hsts != "max-age=31536000" || !https && hsts != "" {
					t.Errorf("Strict-Transport-Security = %q over https=%v", hsts, https)
				}
			})
		}
	}
}

// The manifest is served as application/manifest+json (Go's mime table does not know it), revalidated
// like the other unhashed files, and with the same security headers.
func TestSPA_ServesTheManifestAsWebManifest(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testSPA(t).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/manifest.webmanifest", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Errorf("Content-Type = %q, want application/manifest+json", got)
	}

	if got := rec.Header().Get("Cache-Control"); got != revalidate {
		t.Errorf("Cache-Control = %q, want %q", got, revalidate)
	}

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("Content-Security-Policy = %q, want the policy of every answer", got)
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

	if _, err := New(fstest.MapFS{"assets/a.js": {Data: []byte("x")}}, Options{}); err == nil {
		t.Fatal("New accepted a tree without index.html")
	}
}

// The embedded tree (the placeholder without -tags webui, the Vite build with it) must serve
// its index for client routes and, for a build, its hashed assets with a long cache.
func TestHandler_ServesTheEmbeddedTree(t *testing.T) {
	t.Parallel()

	h := Handler(Options{})
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
