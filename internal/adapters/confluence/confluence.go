// Package confluence implements ports.SourceReader for the knowledge base's synced sources whose
// connection's kind syncs with confluence (spec 4 §4.8, §5.6): the pages of one or more spaces of
// a Confluence Cloud site, read through its REST API v2 with the connection's Atlassian account.
//
// The connection gives the site (site: its host, such as acme.atlassian.net), the account's
// email and its API token (email, token: the platform kind confluence's fields), sent with basic
// authentication. A source's config names the spaces (spaces: space keys separated by commas or
// new lines, such as ENG, SUP).
//
// A listing is every current page of the spaces (GET /wiki/api/v2/spaces/{id}/pages, 250 a
// request, following the cursor of each answer's next link) with its version number as its
// version, so a sync fetches only the pages edited since and the ingestion loop deletes the
// documents of pages trashed, archived, moved out or no longer visible to the account. A space the
// site does not show fails the listing instead of deleting its documents. A fetch reads one page
// in storage format (GET /wiki/api/v2/pages/{id}?body-format=storage), at most an upload's size
// (knowledge.DefaultMaxUpload), and turns it into Markdown.
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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/knowledge"
	"github.com/byFrederick/goal/internal/ports"
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
	// The connection fields the reader reads: the platform kind confluence's.
	fieldSite  = "site"
	fieldEmail = "email"
	fieldToken = "token"

	// cfgSpaces is the source's config key naming its spaces.
	cfgSpaces = "spaces"

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

// List implements ports.SourceReader: the current pages of the source's spaces, with their
// version numbers as versions.
func (r *Reader) List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	return func(yield func(domain.SourceItem, error) bool) {
		src, err := open(s)
		if err != nil {
			yield(domain.SourceItem{}, err)

			return
		}

		spaces, err := r.spaces(ctx, src)
		if err != nil {
			yield(domain.SourceItem{}, err)

			return
		}

		for _, sp := range spaces {
			var (
				stopped bool
				bad     error
			)

			q := url.Values{"status": {statusCurrent}, "limit": {strconv.Itoa(pageLimit)}}

			err := walk(ctx, r, src, "/spaces/"+sp.ID.String()+"/pages", q, func(p page) bool {
				item, err := src.item(sp, p)
				if err != nil {
					bad = err

					return false
				}

				stopped = !yield(item, nil)

				return !stopped
			})
			if err == nil {
				err = bad
			}

			if stopped {
				return
			}

			if err != nil {
				yield(domain.SourceItem{}, fmt.Errorf("the pages of space %s: %w", sp.Key, err))

				return
			}
		}
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
	if err := r.getJSON(ctx, src, "/pages/"+id, url.Values{"body-format": {"storage"}}, &p, knowledge.DefaultMaxUpload); err != nil {
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

	item, err := src.item(spaces[i], p)
	if err != nil {
		return domain.SourceDoc{}, err
	}

	text, err := toMarkdown(p.Title, p.Body.Storage.Value)
	if err != nil {
		return domain.SourceDoc{}, fmt.Errorf("page %s: %w", id, err)
	}

	return domain.SourceDoc{SourceItem: item, MIME: knowledge.TypeMarkdown, Body: []byte(text)}, nil
}

// source is a confluence source opened: the connection's site and account, and the keys of the
// spaces it reads.
type source struct {
	// site is the host every request goes to.
	site         string
	email, token string
	keys         []string
}

// open reads s: the connection's site and account, and the source's spaces.
func open(s domain.SyncSpec) (source, error) {
	if s.Connection == nil {
		return source{}, fmt.Errorf("source %s has no connection to read: %w", s.Source.Name, domain.ErrInvalid)
	}

	c := s.Connection

	site, err := siteHost(c.Values[fieldSite])
	if err != nil {
		return source{}, fmt.Errorf("connection %s: %w", c.Connection.Name, err)
	}

	src := source{site: site, email: strings.TrimSpace(c.Values[fieldEmail]), token: c.Values[fieldToken]}
	if src.email == "" || src.token == "" {
		return source{}, fmt.Errorf("connection %s has no %s and %s for the site: %w", c.Connection.Name, fieldEmail, fieldToken, domain.ErrInvalid)
	}

	if src.keys, err = spaceKeys(s.Source.Config[cfgSpaces]); err != nil {
		return source{}, fmt.Errorf("source %s: %w", s.Source.Name, err)
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
	ID  json.Number `json:"id"`
	Key string      `json:"key"`
}

// page is a page of the site; its body only when fetched.
type page struct {
	ID      json.Number `json:"id"`
	Status  string      `json:"status"`
	Title   string      `json:"title"`
	SpaceID json.Number `json:"spaceId"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

// item is p of sp as a source item: its id, version number, title and link.
func (s source) item(sp space, p page) (domain.SourceItem, error) {
	id := p.ID.String()
	if !isID(id) || p.Version.Number < 1 {
		return domain.SourceItem{}, fmt.Errorf("the site shows page %q without an id and a version", id)
	}

	return domain.SourceItem{
		ID: id, Version: strconv.Itoa(p.Version.Number), Title: p.Title, MIME: knowledge.TypeMarkdown,
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
