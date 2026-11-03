// Package confluence implements ports.SourceReader for the knowledge base's synced sources whose
// connection's kind syncs with confluence (spec 4 §4.8, §5.6): the pages of one or more spaces of
// a Confluence Cloud site, read through its REST API v2 with the connection's Atlassian account.
//
// A source on a secret (ADR 0037 decision 14) names the site (site: its host, such as
// acme.atlassian.net) and the account's email (email) in its config, and its secret holds the
// API token; a source still on a connection takes all three from the connection's fields (the
// platform kind confluence's). They are sent with basic authentication. A source's config names
// the spaces (spaces: space keys separated by commas or new lines, such as ENG, SUP).
//
// A listing is every current page of the spaces (GET /wiki/api/v2/spaces/{id}/pages, 250 a
// request, following the cursor of each answer's next link), so a sync fetches only the pages
// edited or moved since and the ingestion loop deletes the documents of pages trashed, archived,
// moved out or no longer visible to the account. A space the site does not show fails the listing
// instead of deleting its documents.
//
// Each space's listing is read whole before its pages are yielded, so each page's place in the
// page tree comes from the parent ids it holds, without a request more. A page's folder is its
// space key, then the slugs of its ancestors' titles top-down (domain.Slug): Paging under
// Runbooks in ENG lands at ENG/runbooks/paging-<id>.md. The space's homepage, every top page's
// parent, is no folder. A page whose parent is not listed (one the account cannot see, a folder,
// a whiteboard) sits at its space's top, and the walk up stops at a cycle or past 32 ancestors.
// Its version is its version number and its ancestors' ids, <number>@<id>/<id> (3 for a page with
// none), so moving a page fetches it again at its new path.
//
// A source's config may name a root page (root_page: a page id). Its listing is then only that
// page and the pages under it, their folders starting at its slug without the space key: the root
// page Runbooks lands at runbooks-<id>.md, Paging under it at runbooks/paging-<id>.md, and the
// ancestors in a version start at the root page. A root page that is not a page id is refused
// before any request, and one the spaces do not have fails the listing. A fetch reads one page
// in storage format (GET /wiki/api/v2/pages/{id}?body-format=storage), at most an upload's size
// (domain.DefaultMaxUpload), and turns it into Markdown.
//
// Every request goes to the connection's site: a next link only lends its cursor, redirects are
// not followed and no error carries the token. goald gives it a transport that dials public
// addresses only (egress.PublicDialer).
package confluence

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.SourceReader = (*Reader)(nil)

// Config tunes the reader. The zero value of each field means its default.
type Config struct {
	// Transport carries every request; nil is http.DefaultTransport. goald's dials public
	// addresses only unless GOAL_ALLOW_PRIVATE_UPSTREAMS (egress.PublicDialer).
	Transport http.RoundTripper
	// Timeout bounds one request. Default: 60s.
	Timeout time.Duration
}

const (
	// The values the reader reads: the platform kind confluence's fields, and the config keys of a
	// source on a secret (site, email).
	fieldSite  = "site"
	fieldEmail = "email"
	fieldToken = "token"

	// cfgSpaces is the source's config key naming its spaces, cfgRootPage the one naming the
	// page whose tree it holds.
	cfgSpaces   = "spaces"
	cfgRootPage = "root_page"

	// statusCurrent is the status of a page neither archived, trashed nor a draft.
	statusCurrent = "current"

	// pageLimit is how many results one listing request asks for, the API's most.
	pageLimit = 250
	// maxListingBytes bounds one listing answer: 250 pages without their bodies.
	maxListingBytes = 16 << 20
	// maxProblemBytes bounds the part of a refusal read for its message.
	maxProblemBytes = 4 << 10
	// maxSpaceKey is the longest space key Confluence takes.
	maxSpaceKey = 255
	// maxID is the most digits of a page id.
	maxID = 20
	// maxAncestors is the most ancestors of a page the reader walks up to; a version of a page's
	// number and its ancestors' ids then stays under domain.MaxFileVersion.
	maxAncestors = 32
)

// Reader reads confluence sources. It is safe for concurrent use and keeps no state.
type Reader struct {
	http *http.Client
}

// New returns a reader.
func New(cfg Config) *Reader {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	return &Reader{http: &http.Client{
		Timeout: cfg.Timeout, Transport: cfg.Transport,
		// A credentialed request is never sent anywhere but the connection's site.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// List implements ports.SourceReader: the current pages of the source's spaces, or of its root
// page's tree, each in its folder of the page tree, versioned by their version numbers and
// ancestors.
func (r *Reader) List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	return func(yield func(domain.SourceItem, error) bool) {
		items, err := r.list(ctx, s)
		if err != nil {
			yield(domain.SourceItem{}, err)

			return
		}

		for _, item := range items {
			if !yield(item, nil) {
				return
			}
		}
	}
}

// list reads every space's listing whole, then places each page in its tree.
func (r *Reader) list(ctx context.Context, s domain.SyncSpec) ([]domain.SourceItem, error) {
	src, err := open(s)
	if err != nil {
		return nil, err
	}

	spaces, err := r.spaces(ctx, src)
	if err != nil {
		return nil, err
	}

	var (
		items []domain.SourceItem
		root  bool
	)

	for _, sp := range spaces {
		t := tree{home: sp.HomepageID.String(), root: src.root, pages: map[string]page{}}
		q := url.Values{"status": {statusCurrent}, "limit": {strconv.Itoa(pageLimit)}}

		if err := walk(ctx, r, src, "/spaces/"+sp.ID.String()+"/pages", q, func(p page) bool {
			t.order = append(t.order, p.ID.String())
			t.pages[p.ID.String()] = p

			return true
		}); err != nil {
			return nil, fmt.Errorf("the pages of space %s: %w", sp.Key, err)
		}

		if _, ok := t.pages[src.root]; ok {
			root = true
		}

		for _, id := range t.order {
			ancestors, ok := t.ancestors(t.pages[id])
			if !ok {
				continue
			}

			item, err := src.item(sp, t.pages[id], ancestors)
			if err != nil {
				return nil, fmt.Errorf("the pages of space %s: %w", sp.Key, err)
			}

			items = append(items, item)
		}
	}

	// A root page not found would list nothing, which would delete every file.
	if src.root != "" && !root {
		return nil, fmt.Errorf("root page %s is not a current page of the spaces %s, or the connection's account cannot see it", src.root, strings.Join(src.keys, ", "))
	}

	return items, nil
}

// tree is a space's current pages by id, in the listing's order, its homepage's id and the
// source's root page ("": none).
type tree struct {
	pages      map[string]page
	order      []string
	home, root string
}

// ancestors are the pages above p in its folder, top-down: up to the space's homepage, the first
// parent not listed or a cycle, at most maxAncestors (past it, none). With a root page they start
// at the root page, and ok is false for a page not under it.
func (t tree) ancestors(p page) (out []page, ok bool) {
	if t.root != "" && p.ID.String() == t.root {
		return nil, true
	}

	seen := map[string]bool{p.ID.String(): true}

	for cur := p; ; {
		id := cur.ParentID.String()

		parent, listed := t.pages[id]
		if !listed || seen[id] || (t.root == "" && id == t.home) {
			slices.Reverse(out)

			return out, t.root == ""
		}

		// Too deep: a page with no place is at the space's top, or not known to be under the root.
		if len(out) == maxAncestors {
			return nil, t.root == ""
		}

		seen[id] = true

		out = append(out, parent)

		if id == t.root {
			slices.Reverse(out)

			return out, true
		}

		cur = parent
	}
}

// Fetch implements ports.SourceReader: one current page of the source's spaces as Markdown. An id
// that is not a page id is refused without asking the site.
func (r *Reader) Fetch(ctx context.Context, s domain.SyncSpec, id string) (domain.SourceDoc, error) {
	src, err := open(s)
	if err != nil {
		return domain.SourceDoc{}, err
	}

	if !isID(id) {
		return domain.SourceDoc{}, fmt.Errorf("%q is not a page id: %w", id, domain.ErrInvalid)
	}

	var p page
	if err := r.getJSON(ctx, src, "/pages/"+id, url.Values{"body-format": {"storage"}}, &p, domain.DefaultMaxUpload); err != nil {
		return domain.SourceDoc{}, fmt.Errorf("page %s: %w", id, err)
	}

	if p.Status != statusCurrent || p.ID.String() != id {
		return domain.SourceDoc{}, fmt.Errorf("page %s is %s, not current: %w", id, p.Status, domain.ErrInvalid)
	}

	spaces, err := r.spaces(ctx, src)
	if err != nil {
		return domain.SourceDoc{}, err
	}

	i := slices.IndexFunc(spaces, func(sp space) bool { return sp.ID == p.SpaceID })
	if i < 0 {
		return domain.SourceDoc{}, fmt.Errorf("page %s is not in the spaces of source %s: %w", id, s.Source.Name, domain.ErrInvalid)
	}

	item, err := src.item(spaces[i], p, nil)
	if err != nil {
		return domain.SourceDoc{}, err
	}

	text, err := toMarkdown(p.Title, p.Body.Storage.Value)
	if err != nil {
		return domain.SourceDoc{}, fmt.Errorf("page %s: %w", id, err)
	}

	return domain.SourceDoc{SourceItem: item, Body: []byte(text)}, nil
}

// source is a confluence source opened: the connection's site and account, the keys of the
// spaces it reads and its root page's id ("": none).
type source struct {
	// site is the host every request goes to.
	site         string
	email, token string
	keys         []string
	root         string
}

// open reads s: the site and account (the source's config and its secret's value, or the
// connection's fields), and the source's spaces.
func open(s domain.SyncSpec) (source, error) {
	var values map[string]string

	owner := ""

	switch {
	case s.Secret != nil:
		values = map[string]string{fieldSite: s.Source.Config[fieldSite], fieldEmail: s.Source.Config[fieldEmail], fieldToken: s.Secret.Value}
		owner = "source " + s.Source.Name
	case s.Connection != nil:
		values, owner = s.Connection.Values, "connection "+s.Connection.Connection.Name
	default:
		return source{}, fmt.Errorf("source %s has no secret or connection to read with: %w", s.Source.Name, domain.ErrInvalid)
	}

	site, err := siteHost(values[fieldSite])
	if err != nil {
		return source{}, fmt.Errorf("%s: %w", owner, err)
	}

	src := source{site: site, email: strings.TrimSpace(values[fieldEmail]), token: values[fieldToken]}
	if src.email == "" || src.token == "" {
		return source{}, fmt.Errorf("%s has no %s and %s for the site: %w", owner, fieldEmail, fieldToken, domain.ErrInvalid)
	}

	if src.keys, err = spaceKeys(s.Source.Config[cfgSpaces]); err != nil {
		return source{}, fmt.Errorf("source %s: %w", s.Source.Name, err)
	}

	if src.root = strings.TrimSpace(s.Source.Config[cfgRootPage]); src.root != "" && !isID(src.root) {
		return source{}, fmt.Errorf("source %s: %s %q is not a page id: %w", s.Source.Name, cfgRootPage, src.root, domain.ErrInvalid)
	}

	return src, nil
}

// siteHost checks that raw is a site's host, such as acme.atlassian.net, and returns it in lower
// case.
func siteHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))

	u, err := url.Parse("https://" + host)
	if host == "" || err != nil || u.Host != host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("site %q is not a host such as acme.atlassian.net: %w", raw, domain.ErrInvalid)
	}

	return host, nil
}

// spaceKeys parses a config's space keys, separated by commas or new lines, each once.
func spaceKeys(v string) ([]string, error) {
	var keys []string

	for k := range strings.FieldsFuncSeq(v, func(r rune) bool { return r == ',' || r == '\n' }) {
		k = strings.TrimSpace(k)

		switch {
		case k == "":
			continue
		case len(k) > maxSpaceKey || strings.ContainsFunc(k, func(r rune) bool { return !isKeyRune(r) }):
			return nil, fmt.Errorf("%q is not a space key: %w", k, domain.ErrInvalid)
		case !slices.Contains(keys, k):
			keys = append(keys, k)
		}
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("the config names no %s (space keys such as ENG): %w", cfgSpaces, domain.ErrInvalid)
	}

	return keys, nil
}

// isKeyRune reports whether r may be in a space key: letters and digits, and a personal space's
// ~ before its account id.
func isKeyRune(r rune) bool {
	return r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("~_-:", r))
}

// isID reports whether id is a page id: digits only.
func isID(id string) bool {
	return id != "" && len(id) <= maxID && !strings.ContainsFunc(id, func(r rune) bool { return r < '0' || r > '9' })
}

// space is a space of the site.
type space struct {
	ID         json.Number `json:"id"`
	Key        string      `json:"key"`
	HomepageID json.Number `json:"homepageId"`
}

// page is a page of the site; its body only when fetched.
type page struct {
	ID       json.Number `json:"id"`
	Status   string      `json:"status"`
	Title    string      `json:"title"`
	SpaceID  json.Number `json:"spaceId"`
	ParentID json.Number `json:"parentId"`
	Version  struct {
		Number int `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

// item is p of sp under ancestors, top-down, as a source item: its id, its version number and
// ancestors' ids, its title, link and folder.
func (s source) item(sp space, p page, ancestors []page) (domain.SourceItem, error) {
	id := p.ID.String()
	if !isID(id) || p.Version.Number < 1 {
		return domain.SourceItem{}, fmt.Errorf("the site shows page %q without an id and a version", id)
	}

	var folder, ids []string
	if s.root == "" {
		folder = append(folder, sp.Key)
	}

	for _, a := range ancestors {
		folder, ids = append(folder, domain.Slug(a.Title)), append(ids, a.ID.String())
	}

	version := strconv.Itoa(p.Version.Number)
	if len(ids) > 0 {
		version += "@" + strings.Join(ids, "/")
	}

	return domain.SourceItem{
		ID: id, Version: version, Title: p.Title, MIME: domain.MediaMarkdown, Folder: path.Join(folder...),
		URL: "https://" + s.site + "/wiki/spaces/" + url.PathEscape(sp.Key) + "/pages/" + id,
	}, nil
}

// spaces looks the source's spaces up by key, in the config's order; a key the site does not
// show to the account is refused, so its pages are not taken for deleted.
func (r *Reader) spaces(ctx context.Context, s source) ([]space, error) {
	var found []space

	q := url.Values{"keys": {strings.Join(s.keys, ",")}, "limit": {strconv.Itoa(pageLimit)}}
	if err := walk(ctx, r, s, "/spaces", q, func(sp space) bool { found = append(found, sp); return true }); err != nil {
		return nil, fmt.Errorf("the spaces %s: %w", strings.Join(s.keys, ", "), err)
	}

	out := make([]space, 0, len(s.keys))

	for _, k := range s.keys {
		i := slices.IndexFunc(found, func(sp space) bool { return strings.EqualFold(sp.Key, k) })
		if i < 0 {
			return nil, fmt.Errorf("space %s is not on %s, or the connection's account cannot see it", k, s.site)
		}

		out = append(out, found[i])
	}

	return out, nil
}

// walk calls yield with every result of a listing at path with query q, request after request,
// until it returns false. Only the cursor of a next link is used: the next request goes to the
// site whatever host the link names.
func walk[T any](ctx context.Context, r *Reader, s source, path string, q url.Values, yield func(T) bool) error {
	seen := map[string]bool{}

	for {
		var answer struct {
			Results []T `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		if err := r.getJSON(ctx, s, path, q, &answer, maxListingBytes); err != nil {
			return err
		}

		for _, v := range answer.Results {
			if !yield(v) {
				return nil
			}
		}

		if answer.Links.Next == "" {
			return nil
		}

		next, err := url.Parse(answer.Links.Next)
		cursor := ""

		if err == nil {
			cursor = next.Query().Get("cursor")
		}

		// A next link without a new cursor would list the same results forever.
		if cursor == "" || seen[cursor] {
			return fmt.Errorf("the site's next link %q has no new cursor", answer.Links.Next)
		}

		seen[cursor] = true
		q.Set("cursor", cursor)
	}
}

// statusError is the site's answer other than 200.
type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string {
	msg := fmt.Sprintf("Confluence answered %d", e.status)
	if e.status == http.StatusNotFound {
		msg += " (not found, or the connection's account cannot see it)"
	}

	if e.message != "" {
		msg += ": " + e.message
	}

	return msg
}

// getJSON reads the answer of the API's path with query q, at most limit bytes, into out.
func (r *Reader) getJSON(ctx context.Context, s source, path string, q url.Values, out any, limit int64) error {
	body, err := r.send(ctx, s, "https://"+s.site+"/wiki/api/v2"+path+"?"+q.Encode(), limit)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode Confluence's answer: %w", err)
	}

	return nil
}

// send sends one GET with the account's basic authentication and returns a 200's body of at most
// limit bytes; any other answer is a statusError with the site's message, the token scrubbed from
// it.
func (r *Reader) send(ctx context.Context, s source, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "goald")
	req.SetBasicAuth(s.email, s.token)

	res, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, maxProblemBytes))

		return nil, &statusError{status: res.StatusCode, message: s.scrub(problem(raw))}
	}

	tooLarge := fmt.Errorf("the answer is larger than %d bytes: %w", limit, ports.ErrTooLarge)

	// A length announced over the limit is refused before the body is read.
	if res.ContentLength > limit {
		return nil, tooLarge
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read Confluence's answer: %w", err)
	}

	if int64(len(body)) > limit {
		return nil, tooLarge
	}

	return body, nil
}

// problem is the message of a refusal: the API v2's first error, or the gateway's message.
func problem(raw []byte) string {
	var p struct {
		Message string `json:"message"`
		Errors  []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}

	_ = json.Unmarshal(raw, &p)

	if p.Message == "" && len(p.Errors) > 0 {
		p.Message = strings.TrimSuffix(p.Errors[0].Title+": "+p.Errors[0].Detail, ": ")
	}

	return oneLine(p.Message)
}

// scrub takes the account's token, also as the basic credential it is sent in, out of msg.
func (s source) scrub(msg string) string {
	basic := base64.StdEncoding.EncodeToString([]byte(s.email + ":" + s.token))

	return strings.NewReplacer(s.token, "[redacted]", basic, "[redacted]").Replace(msg)
}
