// Package web serves the built single-page app (web/dist) from files embedded in goald.
//
// goald built with `-tags webui` (task build does this after task web:build) embeds web/dist.
// Without the tag it embeds web/placeholder, a page that says how to build the UI, so
// `go build ./...` works on a fresh clone with no Node toolchain.
package web

import (
	"bytes"
	_ "embed" // the Content-Security-Policy, csp.txt
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// policy is the Content-Security-Policy of every answer (csp.txt, which vite preview sends too,
// so the e2e tests run the build under it): scripts, workers, fonts and requests only from goald
// itself, inline styles for Radix's positioning and Monaco, images only from goald or data: URIs
// (people show as initials, never as their Google picture), no plugins, forms that submit only
// to goald, no framing (Approve, Merge and the andon cord cannot be clickjacked) and no <base>.
//
//go:embed csp.txt
var policy string

const (
	// Vite names everything under assets/ by content hash, so a URL never changes meaning.
	immutable = "public, max-age=31536000, immutable"
	// index.html and unhashed files (favicon) must be revalidated so a new build shows up.
	revalidate = "no-cache"
	// strictTransport keeps browsers on https for a year once they have opened goald over it.
	strictTransport = "max-age=31536000"
)

// Options shape the headers of the UI's answers.
type Options struct {
	// StrictTransport sends Strict-Transport-Security: people open goald over https
	// (GOAL_PUBLIC_URL).
	StrictTransport bool
}

// Handler serves the embedded UI: files as they are, index.html for client-side routes.
func Handler(o Options) http.Handler {
	files, err := dist()
	if err != nil {
		// The embedded tree is fixed at build time; failing here is a broken build.
		panic(err)
	}

	h, err := New(files, o)
	if err != nil {
		panic(err)
	}

	return h
}

// New serves files, a tree with index.html at its root. Requests for paths that are not files
// get index.html (the router renders them), except API (v1/, api/), MCP and asset paths, which get
// 404 so a typo in a script URL or an unknown endpoint never answers with HTML.
func New(files fs.FS, o Options) (http.Handler, error) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, errors.New("web: the UI build has no index.html")
	}

	return &spa{files: files, index: index, opts: o}, nil
}

type spa struct {
	files fs.FS
	index []byte
	opts  Options
}

func (s *spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.secure(w.Header())

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if s.serveFile(w, r, name) {
		return
	}

	if reserved(name) {
		http.NotFound(w, r)

		return
	}

	w.Header().Set("Cache-Control", revalidate)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(s.index))
}

// secure sets the headers of every answer: the Content-Security-Policy, frame protection, a
// browsing context of its own (no window another origin opened keeps a handle on the app), no
// referrer to other origins, no content sniffing and, over https, Strict-Transport-Security.
func (s *spa) secure(h http.Header) {
	h.Set("Content-Security-Policy", strings.TrimSpace(policy))
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Content-Type-Options", "nosniff")

	if s.opts.StrictTransport {
		h.Set("Strict-Transport-Security", strictTransport)
	}
}

// serveFile serves name when it is a regular file of the build (never index.html, which the
// fallback serves with its own headers) and reports whether it did.
func (s *spa) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	if name == "" || name == "index.html" || !fs.ValidPath(name) {
		return false
	}

	f, err := s.files.Open(name)
	if err != nil {
		return false
	}
	// A read-only embedded file: closing it cannot lose data.
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}

	content, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}

	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", immutable)
	} else {
		w.Header().Set("Cache-Control", revalidate)
	}

	http.ServeContent(w, r, name, info.ModTime(), content)

	return true
}

// reserved reports paths that are never client-side routes.
func reserved(name string) bool {
	if name == "index.html" {
		return false
	}

	for _, prefix := range []string{"v1", "api", "mcp", "assets"} {
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}

	return path.Ext(name) != ""
}
