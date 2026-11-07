package confluence_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/adapters/sourcereader/confluence"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/egress"
	"github.com/byFrederick/goal/internal/knowledge"
)

const (
	wikiToken = "wiki-token"
	email     = "ada@example.com"
	token     = "s3cret-api-token"

	kindConfluence = "confluence"
	cfgSpaces      = "spaces"
	cfgRootPage    = "root_page"

	// unresolvable is a host no request may reach: .invalid never resolves.
	unresolvable = "https://confluence.invalid"

	spaceENG      = "ENG"
	statusCurrent = "current"
	nameWiki      = "wiki"
	fieldSite     = "site"
	fieldEmail    = "email"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

// page is a page of the stand-in site; parent is its parent page's id, "" for none.
type page struct {
	id, space, status, title, body, parent string
	version                                int
}

// site is a stand-in Confluence Cloud site over https serving its REST API v2 (under
// /wiki/api/v2) from memory: spaces looked up by key, a space's pages filtered by status, and one
// page with its body in storage format. Listings come pageSize results at a time with a cursor in
// a relative next link, as Confluence's; nextBase puts the links on another host, stuck makes
// every link name the same cursor. It answers only requests with the account's email and token,
// and records the pages asked for with their body.
type site struct {
	mu       sync.Mutex
	srv      *httptest.Server
	spaces   map[string]string // id → key
	homes    map[string]string // space id → its homepage's id
	pages    map[string]page
	fetched  []string
	listed   int // listing requests answered
	pageSize int
	nextBase string
	stuck    bool
	redirect bool
}

func newSite(t *testing.T, pages ...page) *site {
	t.Helper()

	s := &site{
		spaces: map[string]string{"1001": spaceENG, "2002": "SUP", "3003": "HR"}, homes: map[string]string{"1001": "100"},
		pages: map[string]page{}, pageSize: 2,
	}
	s.set(pages...)
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)

	return s
}

// host is the site's host, which the sources name.
func (s *site) host() string { return s.srv.Listener.Addr().String() }

// set adds or replaces pages.
func (s *site) set(pages ...page) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range pages {
		s.pages[p.id] = p
	}
}

// served returns the pages asked for with their body since it was last called, sorted.
func (s *site) served() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := slices.Sorted(slices.Values(s.fetched))
	s.fetched = nil

	return out
}

// listings is how many listing requests the site answered.
func (s *site) listings() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listed
}

func (s *site) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if user, pass, ok := r.BasicAuth(); !ok || user != email || pass != token {
		// The message names the token it refuses, as some gateways do.
		answer(w, http.StatusUnauthorized, map[string]any{"code": http.StatusUnauthorized, "message": "Unauthorized: " + pass + " is not valid"})

		return
	}

	if s.redirect {
		http.Redirect(w, r, unresolvable+"/wiki/api/v2/spaces", http.StatusFound)

		return
	}

	p, ok := strings.CutPrefix(r.URL.Path, "/wiki/api/v2/")

	switch parts := strings.Split(p, "/"); {
	case !ok || r.Method != http.MethodGet:
		notFound(w)
	case p == "spaces":
		s.listSpaces(w, r)
	case len(parts) == 3 && parts[0] == "spaces" && parts[2] == "pages":
		s.listPages(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "pages":
		s.getPage(w, r, parts[1])
	default:
		notFound(w)
	}
}

// listSpaces answers GET /spaces?keys=: the spaces with those keys.
func (s *site) listSpaces(w http.ResponseWriter, r *http.Request) {
	keys := strings.Split(r.URL.Query().Get("keys"), ",")

	var out []any

	for _, id := range slices.Sorted(maps.Keys(s.spaces)) {
		if slices.Contains(keys, s.spaces[id]) {
			sp := map[string]any{"id": id, "key": s.spaces[id], "name": s.spaces[id], "type": "global", "status": statusCurrent}
			if home, ok := s.homes[id]; ok {
				sp["homepageId"] = home
			}

			out = append(out, sp)
		}
	}

	s.list(w, r, out)
}

// listPages answers GET /spaces/{id}/pages?status=: the space's pages of that status (any
// without one), without their bodies.
func (s *site) listPages(w http.ResponseWriter, r *http.Request, space string) {
	status := r.URL.Query().Get("status")

	var out []any

	for _, id := range slices.Sorted(maps.Keys(s.pages)) {
		if pg := s.pages[id]; pg.space == space && (status == "" || status == pg.status) {
			out = append(out, s.page(pg))
		}
	}

	s.list(w, r, out)
}

// getPage answers GET /pages/{id}?body-format=storage: the page with its body.
func (s *site) getPage(w http.ResponseWriter, r *http.Request, id string) {
	s.fetched = append(s.fetched, id)

	pg, ok := s.pages[id]
	if !ok || r.URL.Query().Get("body-format") != "storage" {
		notFound(w)

		return
	}

	out := s.page(pg)
	out["body"] = map[string]any{"storage": map[string]string{"value": pg.body, "representation": "storage"}}
	answer(w, http.StatusOK, out)
}

// page is how the API shows p.
func (s *site) page(p page) map[string]any {
	out := map[string]any{
		"id": p.id, "status": p.status, "title": p.title, "spaceId": p.space, "parentId": nil, "parentType": nil,
		"version": map[string]any{"number": p.version, "minorEdit": false, "createdAt": "2026-10-01T09:00:00.000Z"},
		"_links":  map[string]string{"webui": "/spaces/" + s.spaces[p.space] + "/pages/" + p.id, "tinyui": "/x/" + p.id},
	}

	if p.parent != "" {
		out["parentId"], out["parentType"] = p.parent, "page"
	}

	return out
}

// list answers one page of a listing: pageSize results from the request's cursor (an offset),
// and a link to the next while there are more.
func (s *site) list(w http.ResponseWriter, r *http.Request, results []any) {
	s.listed++

	q := r.URL.Query()
	start, _ := strconv.Atoi(strings.TrimPrefix(q.Get("cursor"), "c"))

	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 250 {
		answer(w, http.StatusBadRequest, map[string]any{"errors": []map[string]any{{"code": "INVALID_REQUEST_PARAMETER", "title": "limit"}}})

		return
	}

	start = min(start, len(results))
	end := min(start+min(limit, s.pageSize), len(results))
	links := map[string]string{"base": "https://" + s.host() + "/wiki"}

	switch {
	case s.stuck:
		q.Set("cursor", "c1")
		links["next"] = r.URL.Path + "?" + q.Encode()
	case end < len(results):
		q.Set("cursor", "c"+strconv.Itoa(end))
		links["next"] = s.nextBase + r.URL.Path + "?" + q.Encode()
	}

	answer(w, http.StatusOK, map[string]any{"results": append([]any{}, results[start:end]...), "_links": links})
}

func notFound(w http.ResponseWriter) {
	answer(w, http.StatusNotFound, map[string]any{"errors": []map[string]any{{"code": "NOT_FOUND", "title": "Not Found", "detail": nil}}})
}

func answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// transport reaches the site through the public-only dialer, loopback allowed as
// GOAL_ALLOW_PRIVATE_UPSTREAMS allows it, trusting its certificate.
func (s *site) transport(allowPrivate bool) *http.Transport {
	base, _ := s.srv.Client().Transport.(*http.Transport)

	return &http.Transport{DialContext: egress.PublicDialer{AllowPrivate: allowPrivate}.DialContext, TLSClientConfig: base.TLSClientConfig.Clone()}
}

func (s *site) reader() *confluence.Reader {
	return confluence.New(confluence.Config{Transport: s.transport(true)})
}

// spec is a source reading spaces of the site with a secret.
func (s *site) spec(t *testing.T, spaces string) domain.SyncSpec {
	t.Helper()

	return domain.SyncSpec{
		Source: domain.KnowledgeSource{Name: nameWiki, Kind: domain.SourceSynced, Secret: wikiToken, Config: map[string]string{
			cfgSpaces: spaces, fieldSite: s.host(), fieldEmail: email,
		}},
		Secret: &domain.ResolvedSecret{Secret: domain.Secret{Name: wikiToken}, Value: token},
	}
}

// pageURL is where the site shows the page id of the space key.
func (s *site) pageURL(key, id string) string {
	return "https://" + s.host() + "/wiki/spaces/" + key + "/pages/" + id
}

// list returns what the reader lists for spec by id, failing the test on an error.
func list(t *testing.T, r *confluence.Reader, spec domain.SyncSpec) map[string]domain.SourceItem {
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
func listErr(t *testing.T, r *confluence.Reader, spec domain.SyncSpec) error {
	t.Helper()

	for _, err := range r.List(t.Context(), spec) {
		if err != nil {
			return err
		}
	}

	return nil
}

const (
	runbookBody = `<h1>Deploy</h1><p>Run <code>task deploy</code> from a clean main.</p>` +
		`<ac:structured-macro ac:name="toc" />` +
		`<table><tbody><tr><th>Step</th><th>Who</th></tr><tr><td>Tag</td><td>Release lead</td></tr></tbody></table>`
	runbookText = "# Runbook\n\n## Deploy\n\nRun `task deploy` from a clean main.\n\n| Step | Who |\n| --- | --- |\n| Tag | Release lead |\n"
)

// pages are the site's pages: ENG's three current ones and an archived one, SUP's one, HR's one.
var pages = []page{
	{id: "11", space: "1001", status: statusCurrent, title: "Runbook", body: runbookBody, version: 3},
	{id: "12", space: "1001", status: statusCurrent, title: "Deploy", body: "<p>Ship on Tuesdays.</p>", version: 1},
	{id: "13", space: "1001", status: statusCurrent, title: "Paging", body: "<p>Page the on-call.</p>", version: 2},
	{id: "14", space: "1001", status: "archived", title: "Old plan", body: "<p>Gone.</p>", version: 7},
	{id: "21", space: "2002", status: statusCurrent, title: "Postmortems", body: "<ul><li>Write one per incident.</li></ul>", version: 1},
	{id: "31", space: "3003", status: statusCurrent, title: "Salaries", body: "<p>Not for the agents.</p>", version: 1},
}

// tree is a page tree in ENG under its homepage 100, a page whose parent the account cannot see
// and SUP's page at its top.
var tree = []page{
	{id: "100", space: "1001", status: statusCurrent, title: "ENG home", body: "<p>Home.</p>", version: 1},
	{id: "110", space: "1001", status: statusCurrent, title: "Runbooks", body: "<p>Runbooks.</p>", version: 2, parent: "100"},
	{id: "111", space: "1001", status: statusCurrent, title: "Paging: on-call", body: "<p>Page.</p>", version: 3, parent: "110"},
	{id: "112", space: "1001", status: statusCurrent, title: "Escalation", body: "<p>Escalate.</p>", version: 1, parent: "111"},
	{id: "113", space: "1001", status: "archived", title: "Old", body: "<p>Old.</p>", version: 4, parent: "110"},
	{id: "120", space: "1001", status: statusCurrent, title: "Orphan", body: "<p>Alone.</p>", version: 5, parent: "999"},
	{id: "210", space: "2002", status: statusCurrent, title: "Postmortems", body: "<p>One per incident.</p>", version: 1},
}

// placed is where a listed page sits in its tree: its folder and its version.
type placed struct{ folder, version string }

// TestReader_SyncsIncrementallyByVersion syncs a source of two spaces through the ingestion loop
// twice: the first sync reads every current page of the spaces as Markdown, the second only the
// pages whose version changed and the new one, and deletes the document of a page trashed.
func TestReader_SyncsIncrementallyByVersion(t *testing.T) {
	t.Parallel()

	s := newSite(t, pages...)
	w := newWorld(t, s, "ENG, SUP")

	got := w.sync(s, []string{"11", "12", "13", "21"}, []string{"11", "12", "13", "21"})

	d := got["11"]
	if d.Version != "3" || d.Path != "ENG/runbook-11.md" || d.Title != "Runbook" || d.URL != s.pageURL(spaceENG, "11") {
		t.Fatalf("page 11 = %+v, want version 3 at ENG/runbook-11.md, its title and link", d)
	}

	if d.Text != strings.TrimSuffix(runbookText, "\n") {
		t.Fatalf("page 11's text =\n%s\nwant\n%s", d.Text, runbookText)
	}

	s.set(
		page{id: "12", space: "1001", status: statusCurrent, title: "Deploy", body: "<p>Ship on Tuesdays and Thursdays.</p>", version: 2},
		page{id: "13", space: "1001", status: "trashed", title: "Paging", body: "<p>Page the on-call.</p>", version: 3},
		page{id: "15", space: "1001", status: statusCurrent, title: "Handover", body: "<p>Write the handover.</p>", version: 1},
	)

	// Only the changed and the new page are fetched; the trashed one's document goes.
	got = w.sync(s, []string{"12", "15"}, []string{"11", "12", "15", "21"})

	if d := got["12"]; d.Version != "2" || d.Text != "# Deploy\n\nShip on Tuesdays and Thursdays." {
		t.Fatalf("page 12 = %+v, want version 2 with the new text", d)
	}

	if src := w.source(); src.Status != domain.SyncIdle || src.LastError != "" {
		t.Fatalf("source = %s %q, want idle without an error", src.Status, src.LastError)
	}

	// A page moved under another, its version number unchanged, is fetched again at its new path.
	s.set(page{id: "12", space: "1001", status: statusCurrent, title: "Deploy", body: "<p>Ship on Tuesdays and Thursdays.</p>", version: 2, parent: "11"})

	got = w.sync(s, []string{"12"}, []string{"11", "12", "15", "21"})

	if d := got["12"]; d.Path != "ENG/runbook/deploy-12.md" || d.Version != "2@11" {
		t.Fatalf("page 12 = %+v, want version 2@11 at ENG/runbook/deploy-12.md", d)
	}
}

// TestReader_ARootPageNotInTheSpacesKeepsTheFiles: a root page the spaces do not have fails the
// sync, naming it, and the source keeps every file.
func TestReader_ARootPageNotInTheSpacesKeepsTheFiles(t *testing.T) {
	t.Parallel()

	s := newSite(t, pages...)
	w := newWorld(t, s, spaceENG)
	w.sync(s, []string{"11", "12", "13"}, []string{"11", "12", "13"})

	cfg := maps.Clone(w.source().Config)
	cfg[cfgRootPage] = "999"

	if _, err := w.kb.UpdateSource(w.ctx(), nameWiki, domain.KnowledgeSourcePatch{Config: cfg}, admin); err != nil {
		t.Fatal(err)
	}

	before := w.files()

	if err := w.kb.SyncNow(w.ctx(), nameWiki); err != nil {
		t.Fatal(err)
	}

	w.clock.Advance(time.Minute)

	if _, err := w.in.SyncDue(w.t.Context()); err != nil {
		t.Fatal(err)
	}

	if src := w.source(); !strings.Contains(src.LastError, "999") {
		t.Fatalf("source's last error = %q, want the root page named", src.LastError)
	}

	if after := w.files(); !maps.EqualFunc(before, after, func(a, b docFile) bool { return a.Path == b.Path && a.Version == b.Version }) {
		t.Fatalf("files after the failed sync = %v, want %v", slices.Sorted(maps.Keys(after)), slices.Sorted(maps.Keys(before)))
	}
}

// TestReader_List covers what a listing yields: the current pages of the configured spaces, page
// by page of the listings, with their versions, titles and links, each space once, and next links
// followed on the site only. Each page sits in its space's page tree: its folder is the space key
// and its ancestors' slugs under the homepage, its version its number and its ancestors' ids; a
// page whose parent is not listed sits at the space's top. With a root page only it and the pages
// under it are listed, their folders starting at its slug.
func TestReader_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pages    []page
		spaces   string
		root     string
		nextBase string
		want     []string
		// placed is each page's folder and version; nil: the space key and the version number.
		placed map[string]placed
	}{
		{name: "one space's current pages", spaces: spaceENG, want: []string{"11", "12", "13"}},
		{name: "spaces separated by commas or new lines, each named once", spaces: "ENG,\nSUP\nENG", want: []string{"11", "12", "13", "21"}},
		{name: "a next link on another host is followed on the site", spaces: spaceENG, nextBase: unresolvable, want: []string{"11", "12", "13"}},
		{
			name: "pages at the paths of their tree", pages: tree, spaces: "ENG, SUP",
			want: []string{"100", "110", "111", "112", "120", "210"},
			placed: map[string]placed{
				"100": {spaceENG, "1"}, "110": {spaceENG, "2"}, "111": {"ENG/runbooks", "3@110"},
				"112": {"ENG/runbooks/paging-on-call", "1@110/111"}, "120": {spaceENG, "5"}, "210": {"SUP", "1"},
			},
		},
		{
			name: "only the pages under the root page", pages: tree, spaces: "ENG, SUP", root: "110",
			want:   []string{"110", "111", "112"},
			placed: map[string]placed{"110": {"", "2"}, "111": {"runbooks", "3@110"}, "112": {"runbooks/paging-on-call", "1@110/111"}},
		},
		{
			name: "a page in a cycle sits as far up as its ancestors go", spaces: spaceENG,
			pages: []page{
				{id: "41", space: "1001", status: statusCurrent, title: "A", version: 1, parent: "42"},
				{id: "42", space: "1001", status: statusCurrent, title: "B", version: 1, parent: "41"},
				{id: "43", space: "1001", status: statusCurrent, title: "C", version: 1, parent: "41"},
			},
			want:   []string{"41", "42", "43"},
			placed: map[string]placed{"41": {"ENG/b", "1@42"}, "42": {"ENG/a", "1@41"}, "43": {"ENG/b/a", "1@42/41"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			all := tt.pages
			if all == nil {
				all = pages
			}

			s := newSite(t, all...)
			s.mu.Lock()
			s.nextBase = tt.nextBase
			s.mu.Unlock()

			spec := s.spec(t, tt.spaces)
			if tt.root != "" {
				spec.Source.Config[cfgRootPage] = tt.root
			}

			got := list(t, s.reader(), spec)
			if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, tt.want) {
				t.Fatalf("listed %v, want %v", ids, tt.want)
			}

			for id, item := range got {
				i := slices.IndexFunc(all, func(p page) bool { return p.id == id })
				key := map[string]string{"1001": spaceENG, "2002": "SUP"}[all[i].space]

				want, ok := tt.placed[id]
				if !ok {
					want = placed{key, strconv.Itoa(all[i].version)}
				}

				if item.Version != want.version || item.Folder != want.folder || item.Title != all[i].title || item.URL != s.pageURL(key, id) || item.MIME != domain.MediaMarkdown {
					t.Errorf("item %+v: want version %s, folder %q, title %q, link %s and Markdown", item, want.version, want.folder, all[i].title, s.pageURL(key, id))
				}
			}

			// ENG's three pages come two a page: the spaces' listing and two of pages at least.
			if n := s.listings(); n < 3 {
				t.Fatalf("the site answered %d listings, want the pages read page by page", n)
			}
		})
	}
}

// TestReader_Fetch reads a page of a configured space as Markdown with its version, and refuses an
// id that is not a page id, a page of another space, a page not current and one over the upload
// limit.
func TestReader_Fetch(t *testing.T) {
	t.Parallel()

	big := page{id: "16", space: "1001", status: statusCurrent, title: "Big", body: "<p>" + strings.Repeat("a", domain.DefaultMaxUpload) + "</p>", version: 1}
	s := newSite(t, append(slices.Clone(pages), big)...)
	r := s.reader()
	spec := s.spec(t, spaceENG)

	doc, err := r.Fetch(t.Context(), spec, "11")
	if err != nil {
		t.Fatal(err)
	}

	if string(doc.Body) != runbookText || doc.MIME != domain.MediaMarkdown || doc.Version != "3" || doc.Title != "Runbook" || doc.URL != s.pageURL(spaceENG, "11") {
		t.Fatalf("fetched %+v with %q, want the runbook as Markdown", doc.SourceItem, doc.Body)
	}

	for _, id := range []string{"", "abc", "11/../31", "-1", "31", "14"} {
		if _, err := r.Fetch(t.Context(), spec, id); err == nil {
			t.Errorf("Fetch(%q) = nil error, want a refusal", id)
		}
	}

	if _, err := r.Fetch(t.Context(), spec, "16"); !errors.Is(err, ports.ErrTooLarge) {
		t.Errorf("Fetch(16) = %v, want the page refused as too large", err)
	}

	// HR's page and the archived one are read to learn their space and status, the big one to
	// learn its size; the ids that are not page ids are not asked for.
	if got := s.served(); !slices.Equal(got, []string{"11", "14", "16", "31"}) {
		t.Fatalf("served %v, want only page ids asked for", got)
	}
}

// TestReader_Refusals: what the reader refuses before or after asking the site, the token in no
// error.
func TestReader_Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func(*site, *domain.SyncSpec)
		want string
	}{
		{name: "no spaces", edit: func(_ *site, s *domain.SyncSpec) { s.Source.Config[cfgSpaces] = " ,\n" }, want: "spaces"},
		{name: "a space key that is not one", edit: func(_ *site, s *domain.SyncSpec) { s.Source.Config[cfgSpaces] = "ENG, a/b" }, want: `"a/b"`},
		{name: "a space the site has not", edit: func(_ *site, s *domain.SyncSpec) { s.Source.Config[cfgSpaces] = "ENG, NOPE" }, want: "NOPE"},
		{name: "no secret", edit: func(_ *site, s *domain.SyncSpec) { s.Secret = nil }, want: "secret"},
		{name: "a site with a scheme", edit: func(c *site, s *domain.SyncSpec) { s.Source.Config[fieldSite] = "https://" + c.host() }, want: fieldSite},
		{name: "a site with a path", edit: func(c *site, s *domain.SyncSpec) { s.Source.Config[fieldSite] = c.host() + "/wiki" }, want: fieldSite},
		{name: "a site with credentials", edit: func(c *site, s *domain.SyncSpec) { s.Source.Config[fieldSite] = "a@" + c.host() }, want: fieldSite},
		{name: "no email", edit: func(_ *site, s *domain.SyncSpec) { delete(s.Source.Config, fieldEmail) }, want: "email"},
		{name: "a token the site refuses", edit: func(_ *site, s *domain.SyncSpec) { s.Secret.Value = token + "-revoked" }, want: "401"},
		{name: "next links that repeat their cursor", edit: func(c *site, _ *domain.SyncSpec) { c.stuck = true }, want: "cursor"},
		{name: "a redirect, not followed", edit: func(c *site, _ *domain.SyncSpec) { c.redirect = true }, want: "302"},
		{name: "a root page not in the spaces", edit: func(_ *site, s *domain.SyncSpec) { s.Source.Config[cfgRootPage] = "999" }, want: "999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newSite(t, pages...)
			spec := s.spec(t, spaceENG)
			s.mu.Lock()
			tt.edit(s, &spec)
			s.mu.Unlock()

			err := listErr(t, s.reader(), spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("List = %v, want an error naming %s", err, tt.want)
			}

			if strings.Contains(err.Error(), token) {
				t.Fatalf("the error %q holds the token", err)
			}
		})
	}
}

// TestReader_List_StopsPast32Ancestors: a page deeper than 32 pages sits at its space's top, its
// version its number only, so no version grows past domain.MaxFileVersion.
func TestReader_List_StopsPast32Ancestors(t *testing.T) {
	t.Parallel()

	var chain []page

	for i := range 40 {
		p := page{id: strconv.Itoa(500 + i), space: "1001", status: statusCurrent, title: "Level " + strconv.Itoa(i), version: 1}
		if i > 0 {
			p.parent = strconv.Itoa(500 + i - 1)
		}

		chain = append(chain, p)
	}

	s := newSite(t, chain...)
	s.pageSize = 250

	got := list(t, s.reader(), s.spec(t, spaceENG))

	if d := got["532"]; strings.Count(d.Folder, "/") != 32 || strings.Count(d.Version, "/") != 31 {
		t.Errorf("page 532 = %+v, want its 32 ancestors", d)
	}

	for _, id := range []string{"533", "539"} {
		if d := got[id]; d.Folder != spaceENG || d.Version != "1" {
			t.Errorf("page %s = %+v, want it at ENG's top with version 1", id, d)
		}
	}

	for _, d := range got {
		if len(d.Version) > domain.MaxFileVersion {
			t.Errorf("page %s's version has %d characters", d.ID, len(d.Version))
		}
	}
}

// TestReader_RefusesARootPageThatIsNotAnID before asking the site anything, as a bad space key.
func TestReader_RefusesARootPageThatIsNotAnID(t *testing.T) {
	t.Parallel()

	for _, root := range []string{"abc", "11/../31", "-1", strings.Repeat("1", 21)} {
		s := newSite(t, pages...)
		spec := s.spec(t, spaceENG)
		spec.Source.Config[cfgRootPage] = root

		if err := listErr(t, s.reader(), spec); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), cfgRootPage) {
			t.Errorf("root page %q: List = %v, want domain.ErrInvalid naming %s", root, err, cfgRootPage)
		}

		if n := s.listings(); n != 0 {
			t.Errorf("root page %q: the site answered %d listings, want none", root, n)
		}
	}
}

// TestReader_DialsPublicAddressesOnly: through goald's public-only dialer a site on a loopback
// address is not reached (threat row 4).
func TestReader_DialsPublicAddressesOnly(t *testing.T) {
	t.Parallel()

	s := newSite(t, pages...)
	r := confluence.New(confluence.Config{Transport: s.transport(false)})

	if err := listErr(t, r, s.spec(t, spaceENG)); !errors.Is(err, egress.ErrNotPublic) {
		t.Fatalf("List = %v, want the loopback address refused", err)
	}
}

// world is one organization with a secret and a Confluence source nameWiki reading spaces on the
// site, synced by the ingestion loop with the confluence reader.
type world struct {
	t     *testing.T
	org   domain.OrgID
	clock *porttest.Clock
	kb    ports.Knowledge
	in    *knowledge.Ingestor
	// objects is the bucket the snapshots go to.
	objects ports.ObjectStore
}

func newWorld(t *testing.T, s *site, spaces string) *world {
	t.Helper()

	clock := porttest.NewClock(time.Now().UTC().Add(time.Second))
	tenancy := porttest.NewTenancy(clock.Now)
	kb := porttest.NewKnowledge(tenancy)

	o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	ctx := domain.WithOrg(t.Context(), o.ID)

	secrets := porttest.NewOrgSecrets(porttest.NewMasterKeys().Key("k1"), tenancy)
	kb.ReadSecrets(secrets)

	if _, err := secrets.Create(ctx, domain.NewSecret{Name: wikiToken, Value: token, Scope: domain.SecretScope{All: true}}, admin); err != nil {
		t.Fatal(err)
	}

	if _, err := kb.CreateSource(ctx, domain.NewKnowledgeSource{
		Name: nameWiki, Title: "Wiki", Kind: domain.SourceConfluence, Secret: wikiToken,
		Config: map[string]string{cfgSpaces: spaces, fieldSite: s.host(), fieldEmail: email},
	}, admin); err != nil {
		t.Fatal(err)
	}

	objects := porttest.NewObjectStore()
	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Secrets: secrets, Objects: objects, Clock: clock,
		Readers: map[string]ports.SourceReader{"confluence": s.reader()},
	})

	return &world{t: t, org: o.ID, clock: clock, kb: kb, in: in, objects: objects}
}

func (w *world) ctx() context.Context { return domain.WithOrg(w.t.Context(), w.org) }

// docFile is a page of the source's snapshot: its file, the link its first line cites and its
// text after that.
type docFile struct {
	domain.KnowledgeFile
	URL, Text string
}

// sync makes the source due, syncs it once and checks that the site served exactly the pages
// fetched and that the source's snapshot then has the pages of documents; it returns them by id.
func (w *world) sync(s *site, fetched, documents []string) map[string]docFile {
	w.t.Helper()

	if err := w.kb.SyncNow(w.ctx(), nameWiki); err != nil {
		w.t.Fatal(err)
	}

	w.clock.Advance(time.Minute)

	if n, err := w.in.SyncDue(w.t.Context()); err != nil || n != 1 {
		w.t.Fatalf("SyncDue = %d, %v; want the source synced", n, err)
	}

	if src := w.source(); src.LastError != "" {
		w.t.Fatalf("the sync failed: %s", src.LastError)
	}

	if got := s.served(); !slices.Equal(got, fetched) {
		w.t.Fatalf("the sync fetched %v, want %v", got, fetched)
	}

	got := w.files()
	if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, documents) {
		w.t.Fatalf("pages after the sync = %v, want %v", ids, documents)
	}

	return got
}

func (w *world) source() domain.KnowledgeSource {
	w.t.Helper()

	s, err := w.kb.Source(w.ctx(), nameWiki)
	if err != nil {
		w.t.Fatal(err)
	}

	return s
}

// files are the source's current files by the id of the page they came from (the end of their
// path).
func (w *world) files() map[string]docFile {
	w.t.Helper()

	snaps, err := w.kb.Snapshots(w.ctx(), nameWiki)
	if err != nil || len(snaps) == 0 {
		w.t.Fatalf("snapshots = %v, %v; want one", snaps, err)
	}

	obj, err := w.objects.Get(w.ctx(), snaps[0].Key)
	if err != nil {
		w.t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(obj.Body), int64(len(obj.Body)))
	if err != nil {
		w.t.Fatal(err)
	}

	bodies := map[string]string{}

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			w.t.Fatal(err)
		}

		body, _ := io.ReadAll(rc)
		_ = rc.Close()

		bodies[f.Name] = string(body)
	}

	listed, err := w.kb.Files(w.ctx(), domain.KnowledgeFileQuery{Source: nameWiki, Limit: 100})
	if err != nil {
		w.t.Fatal(err)
	}

	out := map[string]docFile{}

	for _, f := range listed {
		base := strings.TrimSuffix(f.Path, ".md")
		id := base[strings.LastIndex(base, "-")+1:]

		head, text, _ := strings.Cut(strings.TrimPrefix(bodies[f.Path], "---\n"), "---\n\n")
		out[id] = docFile{KnowledgeFile: f, URL: strings.TrimPrefix(strings.TrimSpace(head), "source: "), Text: strings.TrimSuffix(text, "\n")}
	}

	return out
}

// A source on a secret reads the site and email its config names with the secret's value as the
// API token; a config without them is refused before anything is sent.
func TestReader_ReadsWithASecret(t *testing.T) {
	t.Parallel()

	s := newSite(t, pages...)
	spec := domain.SyncSpec{
		Source: domain.KnowledgeSource{Name: nameWiki, Kind: domain.SourceSynced, Secret: wikiToken, Config: map[string]string{
			domain.SourceReaderKey: "confluence", fieldSite: s.host(), fieldEmail: email, cfgSpaces: spaceENG,
		}},
		Secret: &domain.ResolvedSecret{Secret: domain.Secret{Name: wikiToken}, Value: token},
	}

	if got := list(t, s.reader(), spec); !slices.Equal(slices.Sorted(maps.Keys(got)), []string{"11", "12", "13"}) {
		t.Errorf("listed %v; want ENG's current pages", slices.Sorted(maps.Keys(got)))
	}

	delete(spec.Source.Config, fieldEmail)

	if err := listErr(t, s.reader(), spec); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("no email: err = %v; want domain.ErrInvalid", err)
	}
}
