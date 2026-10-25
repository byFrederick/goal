package gitdocs_test

import (
	"context"
	"crypto/sha1" //nolint:gosec // git's blob ids are SHA-1; the stand-in computes them as git does
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/adapters/gitdocs"
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/egress"
	"github.com/byFrederick/goal/internal/knowledge"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const (
	token = "glpat-s3cret-token"

	kindGitHub = "github"
	kindGitLab = "gitlab"

	// The keys of a source's config.
	cfgRepository = "repository"
	cfgBranch     = "branch"
	cfgInclude    = "include"
	cfgExclude    = "exclude"

	readmeMD = "README.md"
	deployMD = "docs/deploy.md"
	pagingMD = "docs/oncall/paging.md"

	// project is the repository the stand-ins serve.
	project = "acme/docs"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

// blobID is git's id of a blob with content c.
func blobID(c string) string {
	h := sha1.New() //nolint:gosec // git's blob ids are SHA-1
	_, _ = fmt.Fprintf(h, "blob %d\x00%s", len(c), c)

	return hex.EncodeToString(h.Sum(nil))
}

// codeHost is a stand-in GitHub Enterprise Server (REST under /api/v3) and GitLab (REST v4 under
// /api/v4) over https, both serving the repository acme/docs from memory: its branches' files,
// a symbolic link and a submodule in every tree, which are not documents. GitLab lists its tree
// pageSize entries a page. It answers only requests with the token, and records the files it
// served raw.
type codeHost struct {
	mu        sync.Mutex
	srv       *httptest.Server
	def       string
	branches  map[string]map[string]string
	raws      []string
	pageSize  int
	truncated bool
}

func newCodeHost(t *testing.T, files map[string]string) *codeHost {
	t.Helper()

	h := &codeHost{def: "main", branches: map[string]map[string]string{"main": maps.Clone(files)}, pageSize: 2}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.srv.Close)

	return h
}

// host is the stand-in's host, which the connections name.
func (h *codeHost) host() string { return h.srv.Listener.Addr().String() }

// repository is the repository's web URL.
func (h *codeHost) repository() string { return "https://" + h.host() + "/" + project }

func (h *codeHost) set(branch string, files map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.branches[branch] = maps.Clone(files)
}

// fetched returns the files served raw since it was last called, sorted.
func (h *codeHost) fetched() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := slices.Sorted(slices.Values(h.raws))
	h.raws = nil

	return out
}

type entry struct {
	path, mode, kind, id string
}

// tree is a branch's entries in path order; ok false for an unknown branch.
func (h *codeHost) tree(branch string) ([]entry, bool) {
	files, ok := h.branches[branch]
	if !ok {
		return nil, false
	}

	out := []entry{
		{path: "docs", mode: "040000", kind: "tree", id: blobID("tree")},
		{path: "docs/link.md", mode: "120000", kind: "blob", id: blobID(deployMD)},
		{path: "docs/vendored.md", mode: "160000", kind: "commit", id: blobID("module")},
	}

	for _, p := range slices.Sorted(maps.Keys(files)) {
		out = append(out, entry{path: p, mode: "100644", kind: "blob", id: blobID(files[p])})
	}

	return out, true
}

func (h *codeHost) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	p := r.URL.EscapedPath()

	switch {
	case r.Method != http.MethodGet:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	case strings.HasPrefix(p, "/api/v3/"):
		if r.Header.Get("Authorization") != "Bearer "+token {
			refuse(w, http.StatusUnauthorized, "Bad credentials")

			return
		}

		h.github(w, r, strings.TrimPrefix(p, "/api/v3/repos/"+project))
	case strings.HasPrefix(p, "/api/v4/"):
		if r.Header.Get("PRIVATE-TOKEN") != token {
			// GitLab names the token it refuses, as some hosts do.
			refuse(w, http.StatusUnauthorized, "401 Unauthorized: "+r.Header.Get("PRIVATE-TOKEN")+" is revoked")

			return
		}

		h.gitlab(w, r, strings.TrimPrefix(p, "/api/v4/projects/"+url.PathEscape(project)))
	default:
		http.NotFound(w, r)
	}
}

// refuse answers status with a code host's message.
func refuse(w http.ResponseWriter, status int, message string) {
	answer(w, status, map[string]string{"message": message})
}

func answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ref is the request's ref query, the default branch when there is none.
func (h *codeHost) ref(r *http.Request) string {
	if ref := r.URL.Query().Get("ref"); ref != "" {
		return ref
	}

	return h.def
}

// raw serves a file of the request's ref as it is.
func (h *codeHost) raw(w http.ResponseWriter, r *http.Request, escaped string) {
	p, err := url.PathUnescape(escaped)
	content, ok := h.branches[h.ref(r)][p]

	if err != nil || !ok {
		refuse(w, http.StatusNotFound, "404 File Not Found")

		return
	}

	h.raws = append(h.raws, p)
	_, _ = w.Write([]byte(content))
}

func (h *codeHost) github(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case rest == "":
		answer(w, http.StatusOK, map[string]string{"default_branch": h.def, "full_name": project})
	case strings.HasPrefix(rest, "/git/trees/") && r.URL.Query().Get("recursive") == "1":
		ref, _ := url.PathUnescape(strings.TrimPrefix(rest, "/git/trees/"))

		entries, ok := h.tree(ref)
		if !ok {
			refuse(w, http.StatusNotFound, "Not Found")

			return
		}

		tree := []map[string]string{}
		for _, e := range entries {
			tree = append(tree, map[string]string{"path": e.path, "mode": e.mode, "type": e.kind, "sha": e.id})
		}

		answer(w, http.StatusOK, map[string]any{"sha": blobID(ref), "tree": tree, "truncated": h.truncated})
	case strings.HasPrefix(rest, "/contents/") && r.Header.Get("Accept") == "application/vnd.github.raw+json":
		h.raw(w, r, strings.TrimPrefix(rest, "/contents/"))
	default:
		http.NotFound(w, r)
	}
}

func (h *codeHost) gitlab(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case rest == "":
		answer(w, http.StatusOK, map[string]any{"default_branch": h.def, "path_with_namespace": project})
	case rest == "/repository/tree" && r.URL.Query().Get("recursive") == "true":
		entries, ok := h.tree(h.ref(r))
		if !ok {
			refuse(w, http.StatusNotFound, "404 Tree Not Found")

			return
		}

		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 1 {
			page = 1
		}

		start := min((page-1)*h.pageSize, len(entries))
		end := min(start+h.pageSize, len(entries))

		if end < len(entries) {
			w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
		}

		out := []map[string]string{}
		for _, e := range entries[start:end] {
			out = append(out, map[string]string{"id": e.id, "path": e.path, "mode": e.mode, "type": e.kind, "name": e.path})
		}

		answer(w, http.StatusOK, out)
	case strings.HasPrefix(rest, "/repository/files/") && strings.HasSuffix(rest, "/raw"):
		h.raw(w, r, strings.TrimSuffix(strings.TrimPrefix(rest, "/repository/files/"), "/raw"))
	default:
		http.NotFound(w, r)
	}
}

// transport reaches the stand-in through the public-only dialer, loopback allowed as
// GOAL_ALLOW_PRIVATE_UPSTREAMS allows it, trusting its certificate.
func (h *codeHost) transport(allowPrivate bool) *http.Transport {
	base, _ := h.srv.Client().Transport.(*http.Transport)

	return &http.Transport{DialContext: egress.PublicDialer{AllowPrivate: allowPrivate}.DialContext, TLSClientConfig: base.TLSClientConfig.Clone()}
}

// spec is a source of config read with a connection on the stand-in of the platform kind named kind.
func (h *codeHost) spec(t *testing.T, kind string, config map[string]string) domain.SyncSpec {
	t.Helper()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == kind })
	if i < 0 {
		t.Fatalf("no platform kind %s", kind)
	}

	return domain.SyncSpec{
		Source: domain.KnowledgeSource{Name: "docs", Kind: domain.SourceSynced, Connection: kind, Config: config},
		Connection: &domain.ResolvedConnection{
			Connection: domain.Connection{Name: kind, Kind: kind}, Kind: kinds[i],
			Values: map[string]string{"token": token, "host": h.host()},
		},
	}
}

// hosts are the code hosts gitdocs reads, by platform kind.
var hosts = []string{kindGitHub, kindGitLab}

// list returns what the reader lists for spec by id, failing the test on an error.
func list(t *testing.T, r *gitdocs.Reader, spec domain.SyncSpec) map[string]domain.SourceItem {
	t.Helper()

	out := map[string]domain.SourceItem{}

	for item, err := range r.List(t.Context(), spec) {
		if err != nil {
			t.Fatal(err)
		}

		out[item.ID] = item
	}

	return out
}

// listErr is the error listing spec ends with.
func listErr(t *testing.T, r *gitdocs.Reader, spec domain.SyncSpec) error {
	t.Helper()

	for _, err := range r.List(t.Context(), spec) {
		if err != nil {
			return err
		}
	}

	return nil
}

var docs = map[string]string{
	readmeMD:              "# Docs\n\nThe docs of Acme.\n",
	deployMD:              "# Deploy\n\nRun task deploy from a clean main.\n",
	pagingMD:              "# Paging\n\nPage the on-call through PagerDuty.\n",
	"docs/drafts/idea.md": "# Idea\n\nNot ready.\n",
	"docs/api.yaml":       "openapi: 3.1.0\n",
	"main.go":             "package main\n",
}

// TestReader_SyncsIncrementallyByBlobID syncs a source through the ingestion loop twice on each
// code host: the first sync reads the files its globs choose, the second only those whose blob
// changed or that are new, and deletes the documents of files gone.
func TestReader_SyncsIncrementallyByBlobID(t *testing.T) {
	t.Parallel()

	for _, kind := range hosts {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			h := newCodeHost(t, docs)
			w := newWorld(t, h, kind, map[string]string{
				cfgRepository: h.repository() + ".git", cfgInclude: "docs/**/*.md, docs/*.yaml", cfgExclude: "docs/drafts/**",
			})

			got := w.sync(h, []string{"docs/api.yaml", deployMD, pagingMD}, []string{"docs/api.yaml", deployMD, pagingMD})

			d := got[deployMD]
			wantURL := h.repository() + "/blob/main/docs/deploy.md"

			if kind == kindGitLab {
				wantURL = h.repository() + "/-/blob/main/docs/deploy.md"
			}

			if d.Version != blobID(docs[deployMD]) || d.URL != wantURL || d.Status != domain.DocumentIndexed || d.MIME != knowledge.TypeMarkdown {
				t.Fatalf("docs/deploy.md = %+v, want blob %s, url %s, indexed Markdown", d, blobID(docs[deployMD]), wantURL)
			}

			changed := maps.Clone(docs)
			changed[deployMD] = "# Deploy\n\nRun task deploy from a clean main, then watch the dashboards.\n"
			changed["docs/oncall/handover.md"] = "# Handover\n\nWrite the handover at the end of the week.\n"
			delete(changed, "docs/api.yaml")
			h.set("main", changed)

			// Only the changed and the new file are fetched; the deleted one's document goes.
			got = w.sync(h, []string{deployMD, "docs/oncall/handover.md"}, []string{deployMD, "docs/oncall/handover.md", pagingMD})

			if got[deployMD].Version != blobID(changed[deployMD]) {
				t.Fatalf("docs/deploy.md's version = %s, want the new blob", got[deployMD].Version)
			}

			if s := w.source(); s.Status != domain.SyncIdle || s.LastError != "" {
				t.Fatalf("source = %s %q, want idle without an error", s.Status, s.LastError)
			}
		})
	}
}

// TestReader_List covers what a listing yields on each code host: the default branch or the
// configured one, only blobs (no link, submodule or directory), the default glob and the globs'
// rules; GitLab's every page.
func TestReader_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config map[string]string
		want   []string
	}{
		{
			name: "the default glob reads Markdown on the default branch", config: map[string]string{},
			want: []string{readmeMD, deployMD, "docs/drafts/idea.md", pagingMD},
		},
		{name: "a branch", config: map[string]string{cfgBranch: "release/v1"}, want: []string{"docs/v1.md"}},
		{name: "a glob without a directory matches at the root only", config: map[string]string{cfgInclude: "*.md"}, want: []string{readmeMD}},
		{
			name: "includes are any of several, excludes win", config: map[string]string{cfgInclude: "*.go\n/docs/*/*.md", cfgExclude: "**/idea.*"},
			want: []string{pagingMD, "main.go"},
		},
		{
			name: "a character class and a single character", config: map[string]string{cfgInclude: "docs/[a-d]?????.?d"},
			want: []string{deployMD},
		},
	}

	for _, kind := range hosts {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			h := newCodeHost(t, docs)
			h.set("release/v1", map[string]string{"docs/v1.md": "# v1\n"})
			r := gitdocs.New(gitdocs.Config{Transport: h.transport(true)})

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					config := maps.Clone(tt.config)
					config[cfgRepository] = h.repository()

					got := list(t, r, h.spec(t, kind, config))
					if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, tt.want) {
						t.Fatalf("listed %v, want %v", ids, tt.want)
					}

					for id, item := range got {
						if item.Title != id || item.Version == "" || !strings.HasPrefix(item.URL, h.repository()+"/") {
							t.Errorf("item %+v: want its path as title, a version and a link into the repository", item)
						}
					}
				})
			}
		})
	}
}

// TestReader_Fetch reads a listed file of the configured branch with its type, and refuses one
// outside the source's globs, a path that leaves the repository and a file over the upload limit.
func TestReader_Fetch(t *testing.T) {
	t.Parallel()

	for _, kind := range hosts {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			big := strings.Repeat("a", knowledge.DefaultMaxUpload+1)
			files := maps.Clone(docs)
			files["docs/big.md"] = big
			h := newCodeHost(t, files)
			h.set("release/v1", map[string]string{deployMD: "# Deploy v1\n"})
			r := gitdocs.New(gitdocs.Config{Transport: h.transport(true)})

			spec := h.spec(t, kind, map[string]string{cfgRepository: h.repository(), cfgInclude: "docs/**", cfgBranch: "release/v1"})

			doc, err := r.Fetch(t.Context(), spec, deployMD)
			if err != nil {
				t.Fatal(err)
			}

			if string(doc.Body) != "# Deploy v1\n" || doc.MIME != knowledge.TypeMarkdown {
				t.Fatalf("fetched %q as %q, want release/v1's Markdown", doc.Body, doc.MIME)
			}

			spec.Source.Config[cfgBranch] = ""
			for _, id := range []string{readmeMD, "docs/../README.md", "/docs/deploy.md", "docs/big.md"} {
				if _, err := r.Fetch(t.Context(), spec, id); err == nil {
					t.Errorf("Fetch(%q) = nil error, want a refusal", id)
				}
			}

			if got := h.fetched(); !slices.Equal(got, []string{"docs/big.md", deployMD}) {
				t.Fatalf("served %v: a refused path must not be asked for", got)
			}
		})
	}
}

// TestReader_Refusals: what the reader refuses before or after asking the code host, the token in
// no error.
func TestReader_Refusals(t *testing.T) {
	t.Parallel()

	h := newCodeHost(t, docs)
	r := gitdocs.New(gitdocs.Config{Transport: h.transport(true)})

	tests := []struct {
		name string
		kind string
		edit func(*domain.SyncSpec)
		want string
	}{
		{
			name: "a repository on another host", kind: kindGitHub,
			edit: func(s *domain.SyncSpec) { s.Source.Config[cfgRepository] = "https://git.example.com/" + project },
			want: "not on",
		},
		{
			name: "a repository URL that is not https", kind: kindGitLab,
			edit: func(s *domain.SyncSpec) { s.Source.Config[cfgRepository] = "http://" + h.host() + "/" + project },
			want: "https",
		},
		{name: "no repository", kind: kindGitHub, edit: func(s *domain.SyncSpec) { delete(s.Source.Config, cfgRepository) }, want: "repository"},
		{
			name: "a GitHub repository path that is not owner/name", kind: kindGitHub,
			edit: func(s *domain.SyncSpec) { s.Source.Config[cfgRepository] = h.repository() + "/tree/main" }, want: "owner",
		},
		{name: "a malformed glob", kind: kindGitLab, edit: func(s *domain.SyncSpec) { s.Source.Config[cfgInclude] = "docs/[" }, want: "glob"},
		{
			name: "a kind without a code host", kind: kindGitHub,
			edit: func(s *domain.SyncSpec) { s.Connection.Kind.Spec.CodeHost = "" }, want: "GitHub or GitLab",
		},
		{name: "no connection", kind: kindGitHub, edit: func(s *domain.SyncSpec) { s.Connection = nil }, want: "connection"},
		{name: "an unknown branch", kind: kindGitHub, edit: func(s *domain.SyncSpec) { s.Source.Config[cfgBranch] = "nope" }, want: "404"},
		{name: "an unknown GitLab branch", kind: kindGitLab, edit: func(s *domain.SyncSpec) { s.Source.Config[cfgBranch] = "nope" }, want: "404"},
		{
			name: "a token the host refuses", kind: kindGitLab,
			edit: func(s *domain.SyncSpec) { s.Connection.Values["token"] = token + "-revoked" }, want: "401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec := h.spec(t, tt.kind, map[string]string{cfgRepository: h.repository()})
			tt.edit(&spec)

			err := listErr(t, r, spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("List = %v, want an error naming %q", err, tt.want)
			}

			if strings.Contains(err.Error(), token) {
				t.Fatalf("the error %q holds the token", err)
			}
		})
	}
}

// TestReader_RefusesTruncatedGitHubTree: a tree GitHub cuts short would delete the documents it
// leaves out, so the listing fails instead.
func TestReader_RefusesTruncatedGitHubTree(t *testing.T) {
	t.Parallel()

	h := newCodeHost(t, docs)
	h.truncated = true

	err := listErr(t, gitdocs.New(gitdocs.Config{Transport: h.transport(true)}), h.spec(t, kindGitHub, map[string]string{cfgRepository: h.repository()}))
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("List = %v, want the truncated tree refused", err)
	}
}

// TestReader_DialsPublicAddressesOnly: through goald's public-only dialer a repository on a
// loopback address is not reached (threat row 4).
func TestReader_DialsPublicAddressesOnly(t *testing.T) {
	t.Parallel()

	for _, kind := range hosts {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			h := newCodeHost(t, docs)
			r := gitdocs.New(gitdocs.Config{Transport: h.transport(false)})

			if err := listErr(t, r, h.spec(t, kind, map[string]string{cfgRepository: h.repository()})); !errors.Is(err, egress.ErrNotPublic) {
				t.Fatalf("List = %v, want the loopback address refused", err)
			}
		})
	}
}

// world is one organization with a connection named after kind on the stand-in and a synced
// source "docs" reading it with config, synced by the ingestion loop with the gitdocs reader.
type world struct {
	t     *testing.T
	org   domain.OrgID
	clock *porttest.Clock
	kb    ports.Knowledge
	in    *knowledge.Ingestor
}

func newWorld(t *testing.T, h *codeHost, kind string, config map[string]string) *world {
	t.Helper()

	clock := porttest.NewClock(time.Now().UTC().Add(time.Second))
	tenancy := porttest.NewTenancy(clock.Now)
	conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)
	kb := porttest.NewKnowledge(conns, tenancy)

	o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	ctx := domain.WithOrg(t.Context(), o.ID)

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	if err := conns.SyncPlatformKinds(t.Context(), kinds); err != nil {
		t.Fatal(err)
	}

	fields := map[string]string{"host": h.host()}
	if kind == kindGitHub {
		fields["api_url"] = "https://" + h.host() + "/api/v3" // a kind with a login is tested on its host
	}

	if _, err := conns.Create(ctx, domain.NewConnection{Name: kind, Kind: kind, Fields: fields, Secrets: map[string]string{"token": token}}, admin); err != nil {
		t.Fatal(err)
	}

	if _, err := kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: "docs", Title: "Docs", Kind: domain.SourceSynced, Connection: kind, Config: config}, admin); err != nil {
		t.Fatal(err)
	}

	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Orgs: tenancy, Connections: conns, Clock: clock,
		Readers: map[string]ports.SourceReader{"gitdocs": gitdocs.New(gitdocs.Config{Transport: h.transport(true)})},
	})

	return &world{t: t, org: o.ID, clock: clock, kb: kb, in: in}
}

func (w *world) ctx() context.Context { return domain.WithOrg(w.t.Context(), w.org) }

// sync makes the source due, syncs it once and checks that the stand-in served exactly the files
// fetched and that the source then has the documents of documents; it returns them by id.
func (w *world) sync(h *codeHost, fetched, documents []string) map[string]domain.KnowledgeDocument {
	w.t.Helper()

	if err := w.kb.SyncNow(w.ctx(), "docs"); err != nil {
		w.t.Fatal(err)
	}

	w.clock.Advance(time.Minute)

	if n, err := w.in.SyncDue(w.t.Context()); err != nil || n != 1 {
		w.t.Fatalf("SyncDue = %d, %v; want the source synced", n, err)
	}

	if s := w.source(); s.LastError != "" {
		w.t.Fatalf("the sync failed: %s", s.LastError)
	}

	if got := h.fetched(); !slices.Equal(got, fetched) {
		w.t.Fatalf("the sync fetched %v, want %v", got, fetched)
	}

	got := w.documents()
	if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, documents) {
		w.t.Fatalf("documents after the sync = %v, want %v", ids, documents)
	}

	return got
}

func (w *world) source() domain.KnowledgeSource {
	w.t.Helper()

	s, err := w.kb.Source(w.ctx(), "docs")
	if err != nil {
		w.t.Fatal(err)
	}

	return s
}

// documents are the source's documents by external id.
func (w *world) documents() map[string]domain.KnowledgeDocument {
	w.t.Helper()

	docs, err := w.kb.Documents(w.ctx(), "docs", "", 100)
	if err != nil {
		w.t.Fatal(err)
	}

	out := map[string]domain.KnowledgeDocument{}
	for _, d := range docs {
		out[d.ExternalID] = d
	}

	return out
}
