package gitdocs_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/adapters/sourcereader/gitdocs"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// archiveShape shapes the archives the stand-in serves: paths written as symbolic links, and
// extra entries by their full name in the zip.
type archiveShape struct {
	links map[string]bool
	extra map[string]string
}

// zipball answers GitHub's archive request as GitHub does: a redirect to a download host, here
// the stand-in's /codeload/.
func (h *codeHost) zipball(w http.ResponseWriter, r *http.Request, escapedRef string) {
	ref, _ := url.PathUnescape(escapedRef)
	if ref == "" {
		ref = h.def
	}

	http.Redirect(w, r, "https://"+h.host()+"/codeload/"+url.PathEscape(ref), http.StatusFound)
}

// gitlabArchive serves GitLab's archive of the sha query, the default branch without one.
func (h *codeHost) gitlabArchive(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("sha")
	if ref == "" {
		ref = h.def
	}

	h.serveZip(w, ref)
}

// codeload serves the archive GitHub redirected to, refusing a request that carries a token.
func (h *codeHost) codeload(w http.ResponseWriter, r *http.Request, escapedRef string) {
	if r.Header.Get("Authorization") != "" || r.Header.Get("PRIVATE-TOKEN") != "" {
		http.Error(w, "the download host takes no token", http.StatusBadRequest)

		return
	}

	ref, _ := url.PathUnescape(escapedRef)
	h.serveZip(w, ref)
}

// serveZip writes the zip of ref's files under one top folder, as both hosts do, with a
// directory entry and the shape's links and extra entries.
func (h *codeHost) serveZip(w http.ResponseWriter, ref string) {
	files, ok := h.branches[ref]
	if !ok {
		refuse(w, http.StatusNotFound, "404 Not Found")

		return
	}

	h.archives++

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)
	top := "acme-docs-" + blobID(ref)[:7] + "/"

	add := func(name, body string, mode fs.FileMode) {
		fh := &zip.FileHeader{Name: name, Method: zip.Deflate}
		fh.SetMode(mode)

		f, err := zw.CreateHeader(fh)
		if err == nil {
			_, _ = f.Write([]byte(body))
		}
	}

	add(top, "", fs.ModeDir|0o755)

	for _, p := range slices.Sorted(maps.Keys(files)) {
		mode := fs.FileMode(0o644)
		if h.archive.links[p] {
			mode = fs.ModeSymlink | 0o777
		}

		add(top+p, files[p], mode)
	}

	for _, name := range slices.Sorted(maps.Keys(h.archive.extra)) {
		add(name, h.archive.extra[name], 0o644)
	}

	_ = zw.Close()

	w.Header().Set("Content-Type", "application/zip")
	_, _ = w.Write(buf.Bytes())
}

func (h *codeHost) archived() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.archives
}

// archive reads spec's archive for items and returns what it yields by id, and the error it
// ends with.
func archive(t *testing.T, r *gitdocs.Reader, spec domain.SyncSpec, items []domain.SourceItem) (map[string]domain.SourceDoc, error) {
	t.Helper()

	out := map[string]domain.SourceDoc{}

	for doc, err := range r.Archive(t.Context(), spec, items) {
		if err != nil {
			return out, err
		}

		out[doc.ID] = doc
	}

	return out, nil
}

// listed is every item r lists for spec.
func listed(t *testing.T, r *gitdocs.Reader, spec domain.SyncSpec) []domain.SourceItem {
	t.Helper()

	return slices.Collect(maps.Values(list(t, r, spec)))
}

// archiveReads checks that a reader of kind reads the source's listed files from one archive,
// with their content and type, and no file raw: only paths that were listed and that the globs
// want, so a listed path the globs exclude stays out.
func archiveReads(t *testing.T, kind string) {
	t.Helper()

	h := newCodeHost(t, docs)
	h.set("next", map[string]string{deployMD: "# Deploy\n\nOn the next branch.\n", readmeMD: docs[readmeMD]})
	r := gitdocs.New(gitdocs.Config{Transport: h.transport(true)})

	for _, branch := range []string{"", "next"} {
		spec := h.spec(t, kind, map[string]string{cfgRepository: h.repository(), cfgBranch: branch, cfgInclude: "docs/**/*.md"})
		items := listed(t, r, spec)
		// main.go is not one the globs want, even with its right version.
		items = append(items, domain.SourceItem{ID: "main.go", Version: blobID(docs["main.go"])})

		got, err := archive(t, r, spec, items)
		if err != nil {
			t.Fatalf("branch %q: %v", branch, err)
		}

		want := map[string]string{deployMD: docs[deployMD], pagingMD: docs[pagingMD], ideaMD: docs[ideaMD]}
		if branch == "next" {
			want = map[string]string{deployMD: "# Deploy\n\nOn the next branch.\n"}
		}

		if len(got) != len(want) {
			t.Fatalf("branch %q: the archive yields %v, want %v", branch, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
		}

		for p, body := range want {
			if d := got[p]; string(d.Body) != body || d.MIME != domain.MediaMarkdown {
				t.Fatalf("branch %q: %s = %q (%s), want %q as Markdown", branch, p, d.Body, d.MIME, body)
			}
		}
	}

	if n := h.archived(); n != 2 {
		t.Fatalf("archives served = %d, want 2", n)
	}

	if raws := h.fetched(); len(raws) != 0 {
		t.Fatalf("files read raw = %v, want none", raws)
	}
}

// GitHub's archive is its zipball, read after the redirect to its download host without the token.
func TestReader_ArchiveGitHub(t *testing.T) {
	t.Parallel()
	archiveReads(t, kindGitHub)
}

// GitLab's archive is /repository/archive.zip of the branch.
func TestReader_ArchiveGitLab(t *testing.T) {
	t.Parallel()
	archiveReads(t, kindGitLab)
}

// An archive past its bound, or with an entry whose path escapes its root, fails as a whole; a
// listed path that is a symbolic link in it, or whose content changed since it was listed, is not
// yielded, so the caller fetches it.
func TestReader_ArchiveRefusals(t *testing.T) {
	t.Parallel()

	fails := map[string]struct {
		shape archiveShape
		bound int64
		want  string
	}{
		"over the size bound": {bound: 100, want: "larger than 100 bytes"},
		"a path out of the root": {
			shape: archiveShape{extra: map[string]string{"acme-docs-1234567/../../evil.md": "# Evil\n"}}, want: "escapes",
		},
		"an absolute path": {shape: archiveShape{extra: map[string]string{"/etc/evil.md": "# Evil\n"}}, want: "escapes"},
	}

	for _, kind := range hosts {
		for name, tc := range fails {
			t.Run(kind+"/"+name, func(t *testing.T) {
				t.Parallel()

				h := newCodeHost(t, docs)
				h.archive = tc.shape
				r := gitdocs.New(gitdocs.Config{Transport: h.transport(true)})

				if tc.bound > 0 {
					gitdocs.SetMaxArchiveBytes(r, tc.bound)
				}

				spec := h.spec(t, kind, map[string]string{cfgRepository: h.repository()})

				got, err := archive(t, r, spec, listed(t, r, spec))
				if err == nil || !strings.Contains(err.Error(), tc.want) || len(got) != 0 {
					t.Fatalf("archive = %v, %v; want no file and an error saying %q", slices.Sorted(maps.Keys(got)), err, tc.want)
				}

				if tc.bound > 0 && !errors.Is(err, ports.ErrTooLarge) {
					t.Fatalf("error = %v, want ports.ErrTooLarge", err)
				}
			})
		}

		t.Run(kind+"/a link and a changed file", func(t *testing.T) {
			t.Parallel()

			h := newCodeHost(t, docs)
			h.archive = archiveShape{links: map[string]bool{deployMD: true}}
			r := gitdocs.New(gitdocs.Config{Transport: h.transport(true)})
			spec := h.spec(t, kind, map[string]string{cfgRepository: h.repository()})
			items := listed(t, r, spec)

			// A push after the listing changes paging.md.
			pushed := maps.Clone(docs)
			pushed[pagingMD] = "# Paging\n\nPushed after the listing.\n"
			h.set("main", pushed)

			got, err := archive(t, r, spec, items)
			if err != nil {
				t.Fatal(err)
			}

			if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, []string{readmeMD, ideaMD}) {
				t.Fatalf("the archive yields %v, want README.md and docs/drafts/idea.md", ids)
			}
		})
	}
}
