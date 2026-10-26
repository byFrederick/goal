package confluence_test

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/egress"
	"github.com/byFrederick/goal/internal/knowledge"
)

const (
	email = "ada@example.com"
	token = "s3cret-api-token"

	kindConfluence = "confluence"
	cfgSpaces      = "spaces"

	// unresolvable is a host no request may reach: .invalid never resolves.
	unresolvable = "https://confluence.invalid"

	spaceENG      = "ENG"
	statusCurrent = "current"
	nameWiki      = "wiki"
	fieldSite     = "site"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

// page is a page of the stand-in site.
type page struct {
	id, space, status, title, body string
	version                        int
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

	s := &site{spaces: map[string]string{"1001": spaceENG, "2002": "SUP", "3003": "HR"}, pages: map[string]page{}, pageSize: 2}
	s.set(pages...)
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)

	return s
}

// host is the site's host, which the connections name.
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
			out = append(out, map[string]any{"id": id, "key": s.spaces[id], "name": s.spaces[id], "type": "global", "status": statusCurrent})
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
	return map[string]any{
		"id": p.id, "status": p.status, "title": p.title, "spaceId": p.space, "parentType": "page",
		"version": map[string]any{"number": p.version, "minorEdit": false, "createdAt": "2026-10-01T09:00:00.000Z"},
		"_links":  map[string]string{"webui": "/spaces/" + s.spaces[p.space] + "/pages/" + p.id, "tinyui": "/x/" + p.id},
	}
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

// spec is a source reading spaces with a connection of the platform kind confluence on the site.
func (s *site) spec(t *testing.T, spaces string) domain.SyncSpec {
	t.Helper()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == kindConfluence })
	if i < 0 {
		t.Fatal("no platform kind confluence")
	}

	return domain.SyncSpec{
		Source: domain.KnowledgeSource{Name: nameWiki, Kind: domain.SourceSynced, Connection: nameWiki, Config: map[string]string{cfgSpaces: spaces}},
		Connection: &domain.ResolvedConnection{
			Connection: domain.Connection{Name: nameWiki, Kind: kindConfluence}, Kind: kinds[i],
			Values: map[string]string{fieldSite: s.host(), "email": email, "token": token},
		},
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

// TestReader_SyncsIncrementallyByVersion syncs a source of two spaces through the ingestion loop
// twice: the first sync reads every current page of the spaces as Markdown, the second only the
// pages whose version changed and the new one, and deletes the document of a page trashed.
func TestReader_SyncsIncrementallyByVersion(t *testing.T) {
	t.Parallel()

	s := newSite(t, pages...)
	w := newWorld(t, s, "ENG, SUP")

	got := w.sync(s, []string{"11", "12", "13", "21"}, []string{"11", "12", "13", "21"})

	d := got["11"]
	if d.Version != "3" || d.Title != "Runbook" || d.URL != s.pageURL(spaceENG, "11") || d.Status != domain.DocumentIndexed || d.MIME != domain.MediaMarkdown {
		t.Fatalf("page 11 = %+v, want version 3, its title and link, indexed Markdown", d)
	}

	if text := w.text(d.ID); text != strings.TrimSuffix(runbookText, "\n") {
		t.Fatalf("page 11's text =\n%s\nwant\n%s", text, runbookText)
	}

	s.set(
		page{id: "12", space: "1001", status: statusCurrent, title: "Deploy", body: "<p>Ship on Tuesdays and Thursdays.</p>", version: 2},
		page{id: "13", space: "1001", status: "trashed", title: "Paging", body: "<p>Page the on-call.</p>", version: 3},
		page{id: "15", space: "1001", status: statusCurrent, title: "Handover", body: "<p>Write the handover.</p>", version: 1},
	)

	// Only the changed and the new page are fetched; the trashed one's document goes.
	got = w.sync(s, []string{"12", "15"}, []string{"11", "12", "15", "21"})

	if d := got["12"]; d.Version != "2" || w.text(d.ID) != "# Deploy\n\nShip on Tuesdays and Thursdays." {
		t.Fatalf("page 12 = %+v with %q, want version 2 with the new text", d, w.text(d.ID))
	}

	if src := w.source(); src.Status != domain.SyncIdle || src.LastError != "" {
		t.Fatalf("source = %s %q, want idle without an error", src.Status, src.LastError)
	}
}

// TestReader_List covers what a listing yields: the current pages of the configured spaces, page
// by page of the listings, with their versions, titles and links, each space once, and next links
// followed on the site only.
func TestReader_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spaces   string
		nextBase string
		want     []string
	}{
		{name: "one space's current pages", spaces: spaceENG, want: []string{"11", "12", "13"}},
		{name: "spaces separated by commas or new lines, each named once", spaces: "ENG,\nSUP\nENG", want: []string{"11", "12", "13", "21"}},
		{name: "a next link on another host is followed on the site", spaces: spaceENG, nextBase: unresolvable, want: []string{"11", "12", "13"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newSite(t, pages...)
			s.mu.Lock()
			s.nextBase = tt.nextBase
			s.mu.Unlock()

			got := list(t, s.reader(), s.spec(t, tt.spaces))
			if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, tt.want) {
				t.Fatalf("listed %v, want %v", ids, tt.want)
			}

			for id, item := range got {
				i := slices.IndexFunc(pages, func(p page) bool { return p.id == id })
				key := map[string]string{"1001": spaceENG, "2002": "SUP"}[pages[i].space]

				if item.Version != strconv.Itoa(pages[i].version) || item.Title != pages[i].title || item.URL != s.pageURL(key, id) || item.MIME != domain.MediaMarkdown {
					t.Errorf("item %+v: want version %d, title %q, link %s and Markdown", item, pages[i].version, pages[i].title, s.pageURL(key, id))
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
		{name: "no connection", edit: func(_ *site, s *domain.SyncSpec) { s.Connection = nil }, want: "connection"},
		{name: "a site with a scheme", edit: func(c *site, s *domain.SyncSpec) { s.Connection.Values[fieldSite] = "https://" + c.host() }, want: fieldSite},
		{name: "a site with a path", edit: func(c *site, s *domain.SyncSpec) { s.Connection.Values[fieldSite] = c.host() + "/wiki" }, want: fieldSite},
		{name: "a site with credentials", edit: func(c *site, s *domain.SyncSpec) { s.Connection.Values[fieldSite] = "a@" + c.host() }, want: fieldSite},
		{name: "no email", edit: func(_ *site, s *domain.SyncSpec) { delete(s.Connection.Values, "email") }, want: "email"},
		{name: "a token the site refuses", edit: func(_ *site, s *domain.SyncSpec) { s.Connection.Values["token"] = token + "-revoked" }, want: "401"},
		{name: "next links that repeat their cursor", edit: func(c *site, _ *domain.SyncSpec) { c.stuck = true }, want: "cursor"},
		{name: "a redirect, not followed", edit: func(c *site, _ *domain.SyncSpec) { c.redirect = true }, want: "302"},
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

// world is one organization with a connection nameWiki of the platform kind confluence on the
// site and a synced source nameWiki reading spaces, synced by the ingestion loop with the
// confluence reader.
type world struct {
	t     *testing.T
	org   domain.OrgID
	clock *porttest.Clock
	kb    ports.Knowledge
	in    *knowledge.Ingestor
}

func newWorld(t *testing.T, s *site, spaces string) *world {
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

	if _, err := conns.Create(ctx, domain.NewConnection{
		Name: nameWiki, Kind: kindConfluence, Fields: map[string]string{fieldSite: s.host(), "email": email}, Secrets: map[string]string{"token": token},
	}, admin); err != nil {
		t.Fatal(err)
	}

	if _, err := kb.CreateSource(ctx, domain.NewKnowledgeSource{
		Name: nameWiki, Title: "Wiki", Kind: domain.SourceSynced, Connection: nameWiki, Config: map[string]string{cfgSpaces: spaces},
	}, admin); err != nil {
		t.Fatal(err)
	}

	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Connections: conns, Clock: clock,
		Readers: map[string]ports.SourceReader{"confluence": s.reader()},
	})

	return &world{t: t, org: o.ID, clock: clock, kb: kb, in: in}
}

func (w *world) ctx() context.Context { return domain.WithOrg(w.t.Context(), w.org) }

// sync makes the source due, syncs it once and checks that the site served exactly the pages
// fetched and that the source then has the documents of documents; it returns them by id.
func (w *world) sync(s *site, fetched, documents []string) map[string]domain.KnowledgeDocument {
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

	docs, err := w.kb.Documents(w.ctx(), nameWiki, "", 100)
	if err != nil {
		w.t.Fatal(err)
	}

	got := map[string]domain.KnowledgeDocument{}
	for _, d := range docs {
		got[d.ExternalID] = d
	}

	if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, documents) {
		w.t.Fatalf("documents after the sync = %v, want %v", ids, documents)
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

// text is the indexed text of the document id (the extracted text, without its last new line).
func (w *world) text(id domain.KnowledgeDocumentID) string {
	w.t.Helper()

	_, text, err := w.kb.Document(w.ctx(), id)
	if err != nil {
		w.t.Fatal(err)
	}

	return text
}
