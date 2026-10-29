package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// DocsPath is where the docs site is served: its build's base path (apps/docs/astro.config.mjs).
const DocsPath = "/docs"

// Docs serves the docs site's build in dir read-only at /docs/ (GOAL_DOCS_DIR): GET and HEAD only,
// a directory's index.html for the directory, and 404 for a missing file, a directory without an
// index (never a listing) and a path that leaves dir. Files are opened through an os.Root, so
// neither `..` nor a symlink pointing outside dir reaches anything beyond it. A directory asked
// for without its trailing slash, /docs included, is redirected to it, so the pages' relative
// links resolve. dir must be a directory.
func Docs(dir string) (http.Handler, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open the docs directory: %w", err)
	}

	return docs{root: root}, nil
}

type docs struct{ root *os.Root }

func (d docs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	rest, ok := strings.CutPrefix(r.URL.Path, DocsPath)
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) {
		http.NotFound(w, r)

		return
	}

	dirWanted := rest == "" || strings.HasSuffix(rest, "/")

	name := strings.Trim(rest, "/")
	if name == "" {
		name = "."
	}

	// fs.ValidPath refuses `..`, empty and doubled elements, so a request names a file in dir.
	if !fs.ValidPath(name) {
		http.NotFound(w, r)

		return
	}

	info, err := d.root.Stat(name)
	if err != nil {
		http.NotFound(w, r)

		return
	}

	if info.IsDir() {
		if !dirWanted || rest == "" {
			http.Redirect(w, r, path.Join(DocsPath, name)+"/", http.StatusMovedPermanently)

			return
		}

		name = path.Join(name, "index.html")
	} else if dirWanted {
		http.NotFound(w, r)

		return
	}

	d.serveFile(w, r, name)
}

// serveFile serves one regular file of the root, or 404.
func (d docs) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f, err := d.root.Open(name)
	if err != nil {
		http.NotFound(w, r)

		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)

		return
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
