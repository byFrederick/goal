// Package skillssh implements ports.SkillSource: the skills.sh directory for search and
// GitHub's REST API for a skill's files at one commit.
//
// Everything it reads is third-party content that ends up in agent containers, so it trusts
// nothing: it talks only to the two configured hosts (https, or http on a loopback address for
// tests) and follows redirects only within the host it asked; every request has a timeout and
// every response body a size cap; a skill is read only from git objects of the resolved commit,
// whose tree must hold regular files only (a symlink or submodule refuses the skill), within
// domain.MaxSkillFiles and domain.MaxSkillBytes (checked from the tree before any download),
// with valid relative paths, and each downloaded blob must have the size and git object id the
// tree names. Hidden files (a segment starting with a dot) are left out: a definition may not
// hold them. Nothing is executed. Tokens travel only in the Authorization header of their own
// host and are scrubbed from every error.
package skillssh

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha1" //nolint:gosec // git object ids are SHA-1; they check a download against its tree, not a secret
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.SkillSource = (*Source)(nil)

// Config says where skills.sh and GitHub are. The zero value of each field means its default.
type Config struct {
	// DirectoryURL is the skills.sh directory (GOAL_SKILLS_DIRECTORY_URL). Default
	// https://skills.sh.
	DirectoryURL string
	// DirectoryToken, optional, is sent as a bearer token to the directory's API, whose search
	// requires one (GOAL_SKILLS_DIRECTORY_TOKEN).
	DirectoryToken string
	// GitHubAPIURL is GitHub's REST API (GOAL_GITHUB_API_URL). Default https://api.github.com.
	GitHubAPIURL string
	// GitHubToken, optional, raises GitHub's rate limit; public repositories need none.
	GitHubToken string
	// Timeout bounds one request. Default 30s.
	Timeout time.Duration
}

const (
	apiVersion     = "2022-11-28"
	userAgent      = "goal-skills-catalog"
	maxRedirects   = 3
	maxSearchBytes = 4 << 20
	maxTreeBytes   = 16 << 20
	maxCommitBytes = 1 << 20
	// A blob's JSON is its content in base64 (4/3 of it, with line breaks) and a few fields.
	maxBlobBytes = domain.MaxSkillBytes*3/2 + 64<<10
)

var (
	objectID = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// conventional is where skills live by convention (the skillPath of skills-lock.json),
	// tried in order before searching the whole tree.
	conventional = []string{"", "skills/"}
)

// Source reads skills.sh and GitHub. It is safe for concurrent use and keeps no state.
type Source struct {
	dir, api          *url.URL
	dirToken, ghToken string
	http              *http.Client
	redactions        []string
}

// New returns a source over cfg, refusing a URL that is not https (or http on a loopback
// address).
func New(cfg Config) (*Source, error) {
	dir, err := baseURL("skills.sh directory", cmp.Or(cfg.DirectoryURL, "https://skills.sh"))
	if err != nil {
		return nil, err
	}

	api, err := baseURL("GitHub API", cmp.Or(cfg.GitHubAPIURL, "https://api.github.com"))
	if err != nil {
		return nil, err
	}

	timeout := cmp.Or(cfg.Timeout, 30*time.Second)

	s := &Source{
		dir: dir, api: api, dirToken: cfg.DirectoryToken, ghToken: cfg.GitHubToken,
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return errors.New("too many redirects")
				}

				if first := via[0].URL; req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
					return fmt.Errorf("refused a redirect off %s", first.Host)
				}

				return nil
			},
		},
	}

	for _, t := range []string{cfg.DirectoryToken, cfg.GitHubToken} {
		if t != "" {
			s.redactions = append(s.redactions, t)
		}
	}

	return s, nil
}

// baseURL accepts https, or http on a loopback address (a stand-in server in tests).
func baseURL(what, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("%s URL %q is not a base URL", what, raw)
	}

	if u.Scheme == "https" {
		return u, nil
	}

	if ip := net.ParseIP(u.Hostname()); u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, fmt.Errorf("%s URL %q must be https (http only on a loopback address)", what, raw)
	}

	return u, nil
}

// statusError is an answer other than 200.
type statusError struct{ code int }

func (e statusError) Error() string { return "answered " + strconv.Itoa(e.code) }

// get reads one JSON document of at most limit bytes from base's host.
func (s *Source) get(ctx context.Context, base *url.URL, token, p string, query url.Values, limit int64, out any) error {
	u := *base
	u.Path = strings.TrimSuffix(base.Path, "/") + p
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	if base == s.api {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", apiVersion)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := s.http.Do(req)
	if err != nil {
		return s.scrub(fmt.Errorf("GET %s%s: %w", u.Host, u.Path, err))
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s%s: %w", u.Host, u.Path, statusError{res.StatusCode})
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return s.scrub(fmt.Errorf("GET %s%s: %w", u.Host, u.Path, err))
	}

	if int64(len(body)) > limit {
		return fmt.Errorf("GET %s%s: the answer is over %d bytes: %w", u.Host, u.Path, limit, ports.ErrTooLarge)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s%s: %w", u.Host, u.Path, err)
	}

	return nil
}

// scrub removes the tokens from err's text.
func (s *Source) scrub(err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()
	for _, t := range s.redactions {
		msg = strings.ReplaceAll(msg, t, "[redacted]")
	}

	if msg == err.Error() {
		return err
	}

	return errors.New(msg)
}

func statusOf(err error) int {
	if st, ok := errors.AsType[statusError](err); ok {
		return st.code
	}

	return 0
}

// searchJSON is the part of the directory's search answer the catalog reads.
type searchJSON struct {
	Data []struct {
		Slug       string `json:"slug"`
		Source     string `json:"source"`
		SourceType string `json:"sourceType"`
		Installs   int64  `json:"installs"`
	} `json:"data"`
}

// Search implements ports.SkillSource with the directory's GET /api/v1/skills/search. It keeps
// only GitHub-hosted skills with valid names.
func (s *Source) Search(ctx context.Context, query string, limit int) ([]ports.SkillListing, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, fmt.Errorf("search %q: a search term has at least 2 characters", query)
	}

	limit = min(max(limit, 1), 200)

	var res searchJSON

	err := s.get(ctx, s.dir, s.dirToken, "/api/v1/skills/search", url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}},
		maxSearchBytes, &res)
	if code := statusOf(err); code == http.StatusUnauthorized || code == http.StatusForbidden {
		return nil, fmt.Errorf("search %q: the skills.sh API needs a token (GOAL_SKILLS_DIRECTORY_TOKEN): %w", query, err)
	}

	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}

	var out []ports.SkillListing

	for _, d := range res.Data {
		owner, repo, ok := strings.Cut(d.Source, "/")
		ref := ports.SkillRef{Owner: owner, Repo: repo, Skill: d.Slug}

		if !ok || d.SourceType != "github" || !(domain.SkillKey{Owner: owner, Repo: repo, Skill: d.Slug}).Valid() ||
			slices.ContainsFunc(out, func(l ports.SkillListing) bool { return l.Ref == ref }) {
			continue
		}

		out = append(out, ports.SkillListing{Ref: ref, Installs: max(d.Installs, 0)})
	}

	slices.SortStableFunc(out, func(a, b ports.SkillListing) int { return cmp.Compare(b.Installs, a.Installs) })

	return out[:min(limit, len(out))], nil
}

// treeEntry is one entry of a git tree.
type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type treeJSON struct {
	SHA       string      `json:"sha"`
	Truncated bool        `json:"truncated"`
	Tree      []treeEntry `json:"tree"`
}

// repo is one repository of the GitHub API.
type repo struct {
	s           *Source
	owner, name string
}

func (r repo) get(ctx context.Context, p string, query url.Values, limit int64, out any) error {
	return r.s.get(ctx, r.s.api, r.s.ghToken, "/repos/"+url.PathEscape(r.owner)+"/"+url.PathEscape(r.name)+p, query, limit, out)
}

func (r repo) tree(ctx context.Context, sha string, recursive bool) (treeJSON, error) {
	q := url.Values{}
	if recursive {
		q.Set("recursive", "1")
	}

	var t treeJSON

	err := r.get(ctx, "/git/trees/"+sha, q, maxTreeBytes, &t)

	return t, err
}

// Fetch implements ports.SkillSource: it resolves ref's commit, finds the skill's folder in that
// commit's tree (<skill>/, skills/<skill>/, else the one folder named <skill> with a SKILL.md)
// and downloads its files.
func (s *Source) Fetch(ctx context.Context, ref ports.SkillRef) (ports.SkillTree, error) {
	if !(domain.SkillKey{Owner: ref.Owner, Repo: ref.Repo, Skill: ref.Skill}).Valid() ||
		(ref.Commit != "" && !objectID.MatchString(ref.Commit)) {
		return ports.SkillTree{}, fmt.Errorf("%s is not a valid skill reference", ref)
	}

	r := repo{s: s, owner: ref.Owner, name: ref.Repo}

	commit, root, err := r.resolve(ctx, ref.Commit)
	if err != nil {
		return ports.SkillTree{}, fmt.Errorf("skill %s: %w", ref, err)
	}

	dir, err := r.findSkill(ctx, root, ref.Skill)
	if err != nil {
		return ports.SkillTree{}, fmt.Errorf("skill %s at %s: %w", ref, commit, err)
	}

	files, err := r.download(ctx, dir)
	if err != nil {
		return ports.SkillTree{}, fmt.Errorf("skill %s at %s: %w", ref, commit, err)
	}

	resolved := ref
	resolved.Commit = commit

	return ports.SkillTree{Ref: resolved, Files: files}, nil
}

// resolve returns the commit (the default branch's head when commit is empty) and its root
// tree.
func (r repo) resolve(ctx context.Context, commit string) (string, string, error) {
	var c struct {
		SHA    string `json:"sha"`
		Commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}

	err := r.get(ctx, "/commits/"+cmp.Or(commit, "HEAD"), nil, maxCommitBytes, &c)
	if code := statusOf(err); code == http.StatusNotFound || code == http.StatusUnprocessableEntity {
		return "", "", fmt.Errorf("no such repository or commit: %w", ports.ErrNotFound)
	}

	if err != nil {
		return "", "", err
	}

	if !objectID.MatchString(c.SHA) || !objectID.MatchString(c.Commit.Tree.SHA) || (commit != "" && c.SHA != commit) {
		return "", "", fmt.Errorf("GitHub answered commit %q for %q", c.SHA, commit)
	}

	return c.SHA, c.Commit.Tree.SHA, nil
}

// findSkill returns the tree sha of the skill's folder.
func (r repo) findSkill(ctx context.Context, root, skill string) (string, error) {
	top, err := r.tree(ctx, root, false)
	if err != nil {
		return "", err
	}

	for _, prefix := range conventional {
		if sha, ok := r.walk(ctx, top, prefix+skill); ok {
			return sha, nil
		}
	}

	all, err := r.tree(ctx, root, true)
	if err != nil {
		return "", err
	}

	return search(all, skill)
}

// search finds the one folder named skill with a SKILL.md in a recursive tree listing.
func search(all treeJSON, skill string) (string, error) {
	var dirs []string

	for _, e := range all.Tree {
		if e.Type == "blob" && path.Base(e.Path) == domain.SkillFile && path.Base(path.Dir(e.Path)) == skill {
			dirs = append(dirs, path.Dir(e.Path))
		}
	}

	switch {
	case len(dirs) > 1:
		return "", fmt.Errorf("%d folders are named %s (%s); pin the skill in another way", len(dirs), skill, strings.Join(dirs, ", "))
	case len(dirs) == 0 && all.Truncated:
		return "", fmt.Errorf("no folder %s/ or skills/%s/ with a %s, and the repository is too large to search", skill, skill, domain.SkillFile)
	case len(dirs) == 0:
		return "", fmt.Errorf("no folder %s with a %s: %w", skill, domain.SkillFile, ports.ErrNotFound)
	}

	i := slices.IndexFunc(all.Tree, func(e treeEntry) bool { return e.Type == "tree" && e.Path == dirs[0] })
	if i < 0 {
		return "", fmt.Errorf("the tree lists %s without its folder", dirs[0])
	}

	return all.Tree[i].SHA, nil
}

// walk follows a slash-separated folder path from the root tree, one tree at a time, and
// reports whether it exists with a SKILL.md in it.
func (r repo) walk(ctx context.Context, top treeJSON, dir string) (string, bool) {
	cur := top

	segs := strings.Split(dir, "/")
	for i, seg := range segs {
		j := slices.IndexFunc(cur.Tree, func(e treeEntry) bool { return e.Type == "tree" && e.Path == seg })
		if j < 0 {
			return "", false
		}

		next, err := r.tree(ctx, cur.Tree[j].SHA, false)
		if err != nil {
			return "", false
		}

		if i == len(segs)-1 {
			return cur.Tree[j].SHA, slices.ContainsFunc(next.Tree, func(e treeEntry) bool {
				return e.Type == "blob" && e.Path == domain.SkillFile
			})
		}

		cur = next
	}

	return "", false
}

// download reads every file of the folder with tree sha dir, after checking the tree.
func (r repo) download(ctx context.Context, dir string) (map[string][]byte, error) {
	t, err := r.tree(ctx, dir, true)
	if err != nil {
		return nil, err
	}

	if t.Truncated {
		return nil, fmt.Errorf("the folder's tree is truncated: %w", ports.ErrTooLarge)
	}

	blobs, err := checkTree(t.Tree)
	if err != nil {
		return nil, err
	}

	files := make(map[string][]byte, len(blobs))

	for _, e := range blobs {
		body, err := r.blob(ctx, e)
		if err != nil {
			return nil, err
		}

		files[e.Path] = body
	}

	return files, nil
}

// checkTree keeps the folder's regular files that are not hidden and enforces the limits and the
// path rules before anything is downloaded.
func checkTree(entries []treeEntry) ([]treeEntry, error) {
	var (
		blobs []treeEntry
		total int64
	)

	for _, e := range entries {
		switch {
		case e.Type == "tree":
			continue
		case e.Type != "blob" || (e.Mode != "100644" && e.Mode != "100755"):
			return nil, fmt.Errorf("%s is not a regular file (mode %s, %s); skills hold regular files only", e.Path, e.Mode, e.Type)
		case escapes(e.Path):
			return nil, fmt.Errorf("path %q leaves the skill's folder", e.Path)
		case domain.HiddenSkillPath(e.Path):
			continue
		case !domain.ValidSkillPath(e.Path):
			return nil, fmt.Errorf("path %q is not a valid skill file path", e.Path)
		case e.Size < 0:
			return nil, fmt.Errorf("%s has a negative size", e.Path)
		}

		blobs = append(blobs, e)
		total += e.Size
	}

	if len(blobs) > domain.MaxSkillFiles {
		return nil, fmt.Errorf("%d files, at most %d: %w", len(blobs), domain.MaxSkillFiles, ports.ErrTooLarge)
	}

	if total > domain.MaxSkillBytes {
		return nil, fmt.Errorf("%d bytes, at most %d: %w", total, domain.MaxSkillBytes, ports.ErrTooLarge)
	}

	if !slices.ContainsFunc(blobs, func(e treeEntry) bool { return e.Path == domain.SkillFile }) {
		return nil, fmt.Errorf("no %s at the folder's root: %w", domain.SkillFile, ports.ErrNotFound)
	}

	return blobs, nil
}

// escapes reports whether p is absolute or has an empty, . or .. segment.
func escapes(p string) bool {
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return true
	}

	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return true
		}
	}

	return false
}

// blob downloads one file and checks it against its tree entry: size and git object id.
func (r repo) blob(ctx context.Context, e treeEntry) ([]byte, error) {
	var b struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}

	if !objectID.MatchString(e.SHA) {
		return nil, fmt.Errorf("%s has an invalid object id", e.Path)
	}

	if err := r.get(ctx, "/git/blobs/"+e.SHA, nil, maxBlobBytes, &b); err != nil {
		return nil, err
	}

	if b.Encoding != "base64" {
		return nil, fmt.Errorf("%s came in encoding %q", e.Path, b.Encoding)
	}

	body, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(b.Content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.Path, err)
	}

	if int64(len(body)) != e.Size || gitBlobID(body) != e.SHA {
		return nil, fmt.Errorf("%s does not match its tree entry", e.Path)
	}

	return body, nil
}

// gitBlobID is git's object id of a blob.
func gitBlobID(body []byte) string {
	h := sha1.New() //nolint:gosec // git object ids are SHA-1; see the import
	_, _ = h.Write(bytes.Join([][]byte{[]byte("blob " + strconv.Itoa(len(body))), body}, []byte{0}))

	return hex.EncodeToString(h.Sum(nil))
}
