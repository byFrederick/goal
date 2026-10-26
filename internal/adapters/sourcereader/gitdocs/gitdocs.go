// Package gitdocs implements ports.SourceReader for the knowledge base's synced sources whose
// connection's kind syncs with gitdocs (spec 4 §4.8, §5.6): the documents of one repository on
// GitHub or GitLab, read through the code host's REST API with the connection's token.
//
// A source's config names the repository (repository: its https URL on the connection's git host,
// such as https://github.com/acme/docs), the branch (branch; the repository's default branch when
// empty) and the files (include and exclude: path globs separated by commas or new lines, include
// **/*.md by default; a file is read when it matches an include and no exclude). In a glob, ** is
// any number of directories, * and ? any characters of one name but /, and [a-z] a class.
//
// A listing is the branch's tree (one request on GitHub, pages of 100 on GitLab) with each file's
// git blob id as its version, so a sync fetches only the files whose blob changed and the
// ingestion loop deletes the documents of files no longer listed. Symbolic links and submodules
// are not documents. A fetch reads one file raw, at most an upload's size
// (domain.DefaultMaxUpload). The token travels only in the API's own header of requests to the
// connection's host; a repository on any other host is refused before anything is sent, redirects
// are not followed and no error carries the token. goald gives it a transport that dials public
// addresses only (egress.PublicDialer).
package gitdocs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"path"
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
	// defaultInclude is the files a source without include globs reads.
	defaultInclude = "**/*.md"
	// maxListingBytes bounds one listing answer: GitHub cuts a tree at 7 MB.
	maxListingBytes = 32 << 20
	// maxProblemBytes bounds the part of a refusal read for its message.
	maxProblemBytes = 4 << 10
)

// Reader reads gitdocs sources. It is safe for concurrent use and keeps no state.
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
		// A credentialed request is never sent anywhere but the connection's API.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// List implements ports.SourceReader: the files of the source's branch its globs choose, with
// their blob ids as versions.
func (r *Reader) List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	return func(yield func(domain.SourceItem, error) bool) {
		src, err := open(s)
		if err != nil {
			yield(domain.SourceItem{}, err)

			return
		}

		branch := src.branch
		if branch == "" {
			if branch, err = src.host.defaultBranch(ctx, r, src); err != nil {
				yield(domain.SourceItem{}, fmt.Errorf("%s's default branch: %w", src.repo, err))

				return
			}
		}

		err = src.host.files(ctx, r, src, branch, func(f file) bool {
			if !src.wants(f.path) {
				return true
			}

			return yield(domain.SourceItem{ID: f.path, Version: f.blob, Title: f.path, URL: src.host.webURL(src, branch, f.path)}, nil)
		})
		if err != nil {
			yield(domain.SourceItem{}, fmt.Errorf("%s at %s: %w", src.repo, branch, err))
		}
	}
}

// Fetch implements ports.SourceReader: one file of the source's branch, raw, with its type. A
// path outside the source's globs is refused.
func (r *Reader) Fetch(ctx context.Context, s domain.SyncSpec, id string) (domain.SourceDoc, error) {
	src, err := open(s)
	if err != nil {
		return domain.SourceDoc{}, err
	}

	if !src.wants(id) {
		return domain.SourceDoc{}, fmt.Errorf("%q is not a file of source %s: %w", id, s.Source.Name, domain.ErrInvalid)
	}

	u, accept := src.host.raw(src, id)

	_, body, err := r.send(ctx, src, u, accept, domain.DefaultMaxUpload)
	if err != nil {
		return domain.SourceDoc{}, fmt.Errorf("%s: %w", id, err)
	}

	// A type it cannot index stays "", which extraction refuses with the reason.
	mime, _ := domain.DocumentType(id, body)

	return domain.SourceDoc{SourceItem: domain.SourceItem{ID: id, MIME: mime}, Body: body}, nil
}

// source is a gitdocs source opened: its repository on its code host with the connection's token
// and the files it reads.
type source struct {
	host codeHost
	// api is the code host's REST API root; web the repository's web URL.
	api, web string
	// repo is the repository's path: owner/name on GitHub, namespace/project on GitLab.
	repo  string
	token string
	// branch "" is the default branch.
	branch           string
	include, exclude [][]string
}

// open reads s: the connection's code host and git login, and the source's config.
func open(s domain.SyncSpec) (source, error) {
	if s.Connection == nil {
		return source{}, fmt.Errorf("source %s has no connection to read: %w", s.Source.Name, domain.ErrInvalid)
	}

	c := s.Connection

	var host codeHost

	switch c.Kind.Spec.CodeHost {
	case "github":
		host = github{}
	case "gitlab":
		host = gitlab{}
	default:
		return source{}, fmt.Errorf("gitdocs reads repositories on GitHub or GitLab, and kind %s of connection %s has neither as its code host: %w",
			c.Kind.Name, c.Connection.Name, domain.ErrInvalid)
	}

	login, token, err := c.GitLogin()
	if err != nil {
		return source{}, err
	}

	cfg := s.Source.Config
	src := source{host: host, token: token, branch: strings.TrimSpace(cfg["branch"])}

	if src.web, src.repo, err = repository(cfg["repository"], login, host); err != nil {
		return source{}, fmt.Errorf("source %s: %w", s.Source.Name, err)
	}

	src.api = host.api(login)

	include := cfg["include"]
	if strings.TrimSpace(include) == "" {
		include = defaultInclude
	}

	if src.include, err = globs(include); err != nil {
		return source{}, fmt.Errorf("source %s's include: %w", s.Source.Name, err)
	}

	if src.exclude, err = globs(cfg["exclude"]); err != nil {
		return source{}, fmt.Errorf("source %s's exclude: %w", s.Source.Name, err)
	}

	return src, nil
}

// repository checks that raw is an https URL of a repository on host (the connection's git
// host) and returns its web URL and path.
func repository(raw, host string, h codeHost) (web, repo string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("the config names no repository: %w", domain.ErrInvalid)
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("repository %q is not an https URL without credentials, query or fragment: %w", raw, domain.ErrInvalid)
	}

	if !strings.EqualFold(u.Host, host) {
		return "", "", fmt.Errorf("repository %s is not on %s, the connection's host: %w", raw, host, domain.ErrInvalid)
	}

	repo = strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if err := h.checkRepo(repo); err != nil {
		return "", "", fmt.Errorf("repository %s: %w", raw, err)
	}

	return "https://" + u.Host + "/" + repo, repo, nil
}

// wants reports whether the file at p is one the source reads: a clean path in the repository
// that matches an include and no exclude.
func (s source) wants(p string) bool {
	if p == "" || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || p == ".." {
		return false
	}

	name := strings.Split(p, "/")

	return matchAny(s.include, name) && !matchAny(s.exclude, name)
}

// file is a file of a branch's tree with its blob id.
type file struct {
	path, blob string
}

// codeHost is one code host's REST API: how its repositories are named, listed and read.
type codeHost interface {
	// name is how errors call it.
	name() string
	// api is the REST API root of host.
	api(host string) string
	// checkRepo refuses a repository path the host has no such repository for.
	checkRepo(repo string) error
	// authorize puts token in the header the host reads it from.
	authorize(h http.Header, token string)
	defaultBranch(ctx context.Context, r *Reader, s source) (string, error)
	// files calls yield with every file of branch's tree (blobs, no links or submodules) until
	// it returns false.
	files(ctx context.Context, r *Reader, s source, branch string, yield func(file) bool) error
	// raw is where the file at p of the source's branch is read as it is, and the media type
	// to accept ("" for any).
	raw(s source, p string) (u, accept string)
	// webURL links to the file at p of branch.
	webURL(s source, branch, p string) string
}

// mode of a symbolic link in a git tree; a submodule is a "commit" entry.
const modeSymlink = "120000"

// statusError is a code host's answer other than 200.
type statusError struct {
	host    string
	status  int
	message string
}

func (e *statusError) Error() string {
	msg := fmt.Sprintf("%s answered %d", e.host, e.status)
	if e.status == http.StatusNotFound {
		msg += " (not found, or the connection's token cannot read it)"
	}

	if e.message != "" {
		msg += ": " + e.message
	}

	return msg
}

// getJSON reads the answer of url into out.
func (r *Reader) getJSON(ctx context.Context, s source, url string, out any) (http.Header, error) {
	header, body, err := r.send(ctx, s, url, "application/json", maxListingBytes)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("decode %s's answer: %w", s.host.name(), err)
	}

	return header, nil
}

// send sends one GET with the connection's token and returns a 200's header and body of at most
// limit bytes; any other answer is a statusError with the host's message, the token scrubbed
// from it.
func (r *Reader) send(ctx context.Context, s source, url, accept string, limit int64) (http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	req.Header.Set("User-Agent", "goald")
	s.host.authorize(req.Header, s.token)

	res, err := r.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, maxProblemBytes))

		var problem struct {
			Message any `json:"message"`
			Error   any `json:"error"`
		}

		_ = json.Unmarshal(raw, &problem)

		msg := fmt.Sprint(firstOf(problem.Message, problem.Error))

		return nil, nil, &statusError{host: s.host.name(), status: res.StatusCode, message: strings.ReplaceAll(msg, s.token, "[redacted]")}
	}

	tooLarge := fmt.Errorf("%s's answer is larger than %d bytes: %w", s.host.name(), limit, ports.ErrTooLarge)

	// A length announced over the limit is refused before the body is read.
	if res.ContentLength > limit {
		return nil, nil, tooLarge
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s's answer: %w", s.host.name(), err)
	}

	if int64(len(body)) > limit {
		return nil, nil, tooLarge
	}

	return res.Header, body, nil
}

// firstOf is the first of vs that is set, "" when none is.
func firstOf(vs ...any) any {
	for _, v := range vs {
		if v != nil && v != "" {
			return v
		}
	}

	return ""
}

// errNoDefaultBranch refuses a repository without a default branch: an empty one.
var errNoDefaultBranch = errors.New("the repository has no default branch (is it empty?)")
