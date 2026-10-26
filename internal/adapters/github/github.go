// Package github implements ports.CodeHost with GitHub's REST API, called from the goald host
// with the credential of a connection whose kind's code host is github (spec 4 §5.5).
//
// It reads pull requests, merges them, updates their branches with their base and deletes
// branches, and reads a repository's branch heads and the commits between two commits, nothing
// else. The connection's
// rendered git login says which host it serves and holds the token; a pull request URL on any
// other host is refused before anything is sent, and the repository is the URL's own. The API is
// https://api.github.com for github.com, https://api.<host> for a GitHub Enterprise Cloud host
// (<name>.ghe.com) and https://<host>/api/v3 for a GitHub Enterprise Server. A merge names the
// head SHA it was asked for, so GitHub refuses it if the branch moved, and it is never forced:
// GitHub's refusals (not mergeable, head moved, invalid) come back as ports.ErrNotMergeable. A
// branch update names the expected head SHA the same way; its conflicts come back as a
// *ports.MergeConflictError (without files: GitHub names none). The
// token travels only in the Authorization header of requests to that API; redirects are not
// followed, and the token is scrubbed from every error.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.CodeHost = (*Client)(nil)

// Config tunes the client. The zero value of each field means its default.
type Config struct {
	// APIURL, when set, is the REST API root for every host instead of the one derived from the
	// connection's: a stand-in GitHub in tests.
	APIURL string
	// Timeout bounds one request. Default: 30s.
	Timeout time.Duration
	// Transport carries the requests; nil is http.DefaultTransport. goald passes its connection
	// transport, which dials public addresses only (egress.PublicDialer).
	Transport http.RoundTripper
	// MergeabilityPolls are the waits before each new read of an open pull request whose
	// mergeability GitHub is still computing (it answers mergeable null). Default: about 10s in
	// all, GitHub's documented behavior.
	MergeabilityPolls []time.Duration
	// MergeRetries are the waits before each new try of a merge GitHub refused with 409 "Head
	// branch was modified": the head moved since the caller read it, usually because GitHub's
	// background update-branch pushed its merge commit. Each try merges at the head read again.
	// Default: 3 retries, about 10s in all.
	MergeRetries []time.Duration
}

// defaultMergeRetries add up to 10s.
var defaultMergeRetries = []time.Duration{2 * time.Second, 3 * time.Second, 5 * time.Second}

// defaultMergeabilityPolls add up to 10s.
var defaultMergeabilityPolls = []time.Duration{
	500 * time.Millisecond, time.Second, 1500 * time.Millisecond, 2 * time.Second, 2 * time.Second, 3 * time.Second,
}

const (
	apiVersion   = "2022-11-28"
	maxBodyBytes = 1 << 20
	// maxCommitBytes bounds the answers of the compare and commit APIs, which carry every changed
	// file's patch.
	maxCommitBytes = 32 << 20
	dotCom         = "github.com"
)

// Client calls GitHub's REST API. It is safe for concurrent use and keeps no state.
type Client struct {
	api     string
	http    *http.Client
	polls   []time.Duration
	retries []time.Duration
}

// New returns a client. cfg.APIURL, when set, must be an http(s) URL; goald's settings check it.
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	if len(cfg.MergeabilityPolls) == 0 {
		cfg.MergeabilityPolls = defaultMergeabilityPolls
	}

	if len(cfg.MergeRetries) == 0 {
		cfg.MergeRetries = defaultMergeRetries
	}

	return &Client{
		api:   strings.TrimSuffix(cfg.APIURL, "/"),
		polls:   slices.Clone(cfg.MergeabilityPolls),
		retries: slices.Clone(cfg.MergeRetries),
		http: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: cfg.Transport,
			// A credentialed request is never sent anywhere but the connection's API.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// pull is one pull request, with the API and the token that reach it; a repository's own reads
// leave number empty.
type pull struct {
	api, token  string
	owner, repo string
	number      string
}

func (p pull) repoPath(rest string) string {
	return "/repos/" + url.PathEscape(p.owner) + "/" + url.PathEscape(p.repo) + rest
}

var pullPath = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/([1-9][0-9]*)/?$`)

// open checks that prURL is a pull request on the host of c's git login and names it.
func (cl *Client) open(c domain.ResolvedConnection, prURL string) (pull, error) {
	host, token, err := c.GitLogin()
	if err != nil {
		return pull{}, err
	}

	pu, err := url.Parse(prURL)
	if err != nil {
		return pull{}, fmt.Errorf("pull request %q: %w", prURL, ports.ErrNotMergeable)
	}

	m := pullPath.FindStringSubmatch(pu.Path)
	if pu.Scheme != "https" || pu.User != nil || pu.RawQuery != "" || pu.Fragment != "" || m == nil {
		return pull{}, fmt.Errorf("%q is not a pull request URL: %w", prURL, ports.ErrNotMergeable)
	}

	if !strings.EqualFold(pu.Host, host) {
		return pull{}, fmt.Errorf("%s is not on %s, the host of connection %s: %w", prURL, host, c.Connection.Name, ports.ErrNotMergeable)
	}

	return pull{api: cl.apiFor(host), token: token, owner: m[1], repo: m[2], number: m[3]}, nil
}

var repoPath = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// openRepo checks that repoURL is a repository on the host of c's git login and names it.
func (cl *Client) openRepo(c domain.ResolvedConnection, repoURL string) (pull, error) {
	host, token, err := c.GitLogin()
	if err != nil {
		return pull{}, err
	}

	u, err := url.Parse(repoURL)
	if err != nil {
		return pull{}, fmt.Errorf("repository %q: %w", repoURL, err)
	}

	m := repoPath.FindStringSubmatch(u.Path)
	if u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || m == nil {
		return pull{}, fmt.Errorf("%q is not a repository URL", repoURL)
	}

	if !strings.EqualFold(u.Host, host) {
		return pull{}, fmt.Errorf("%s is not on %s, the host of connection %s", repoURL, host, c.Connection.Name)
	}

	return pull{api: cl.apiFor(host), token: token, owner: m[1], repo: m[2]}, nil
}

// apiFor is the REST API root of host.
func (cl *Client) apiFor(host string) string {
	switch {
	case cl.api != "":
		return cl.api
	case host == dotCom || strings.HasSuffix(host, ".ghe.com"):
		return "https://api." + host
	default:
		return "https://" + host + "/api/v3"
	}
}

// pullJSON is the part of GitHub's pull request object goald reads.
type pullJSON struct {
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Head           struct {
		SHA  string    `json:"sha"`
		Ref  string    `json:"ref"`
		Repo *repoJSON `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref  string    `json:"ref"`
		Repo *repoJSON `json:"repo"`
	} `json:"base"`
}

type repoJSON struct {
	FullName string `json:"full_name"`
}

// read gets the pull request once.
func (cl *Client) read(ctx context.Context, p pull, prURL string) (pullJSON, error) {
	var pj pullJSON

	status, _, err := cl.call(ctx, p, http.MethodGet, p.repoPath("/pulls/"+p.number), nil, &pj)

	switch {
	case err != nil:
		return pullJSON{}, fmt.Errorf("pull request %s: %w", prURL, err)
	case status == http.StatusNotFound:
		return pullJSON{}, fmt.Errorf("pull request %s does not exist: %w", prURL, ports.ErrNotMergeable)
	case status != http.StatusOK:
		return pullJSON{}, fmt.Errorf("pull request %s: GitHub answered %d", prURL, status)
	}

	return pj, nil
}

// readKnown reads the pull request, and again after each of cl.polls while GitHub is still
// computing its mergeability in the background (it answers null until it knows). Merged and
// closed pull requests always answer null: they are read once.
func (cl *Client) readKnown(ctx context.Context, p pull, prURL string) (pullJSON, error) {
	pj, err := cl.read(ctx, p, prURL)

	for _, wait := range cl.polls {
		if err != nil || pj.Mergeable != nil || pj.State != "open" || pj.Merged {
			break
		}

		if err := sleep(ctx, wait); err != nil {
			return pullJSON{}, fmt.Errorf("pull request %s: waiting for GitHub to compute its mergeability: %w", prURL, err)
		}

		pj, err = cl.read(ctx, p, prURL)
	}

	return pj, err
}

// sleep waits d, or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// PullRequest implements ports.CodeHost.
func (cl *Client) PullRequest(ctx context.Context, c domain.ResolvedConnection, prURL string) (ports.PullRequest, error) {
	p, err := cl.open(c, prURL)
	if err != nil {
		return ports.PullRequest{}, err
	}

	pj, err := cl.readKnown(ctx, p, prURL)
	if err != nil {
		return ports.PullRequest{}, err
	}

	unmerged := pj.State == "open" && !pj.Merged
	pr := ports.PullRequest{
		URL: prURL, Open: pj.State == "open", Merged: pj.Merged, Mergeable: pj.Mergeable != nil && *pj.Mergeable,
		MergeabilityPending: unmerged && pj.Mergeable == nil,
		HeadSHA:             pj.Head.SHA, HeadBranch: pj.Head.Ref, BaseBranch: pj.Base.Ref,
		SameRepository: pj.Head.Repo != nil && pj.Base.Repo != nil && strings.EqualFold(pj.Head.Repo.FullName, pj.Base.Repo.FullName),
	}

	if pj.Merged {
		pr.MergeSHA = pj.MergeCommitSHA
	}

	return pr, nil
}

// headWasModified is GitHub's 409 answer to a merge at a SHA that is no longer the head ("Head
// branch was modified. Review and try the merge again."), matched case-insensitively.
const headWasModified = "head branch was modified"

// Merge implements ports.CodeHost. GitHub updates a branch in the background after it answers an
// update-branch, so a merge right after one can carry a head that is already stale: GitHub then
// refuses with 409 "Head branch was modified". Merge reads the pull request again after each of
// cl.retries and merges at the head it reports now; a pull request found merged meanwhile is not
// merged twice. Any other refusal, or the last 409, is ErrNotMergeable.
func (cl *Client) Merge(ctx context.Context, c domain.ResolvedConnection, prURL string, m ports.Merge) (string, error) {
	p, err := cl.open(c, prURL)
	if err != nil {
		return "", err
	}

	if m.HeadSHA == "" {
		return "", fmt.Errorf("merge %s: the head SHA to merge is required", prURL)
	}

	head := m.HeadSHA

	for try := 0; ; try++ {
		sha, status, msg, err := cl.mergeOnce(ctx, p, prURL, m.Method, head)
		if err != nil || status == http.StatusOK {
			return sha, err
		}

		if status != http.StatusConflict || !strings.Contains(strings.ToLower(msg), headWasModified) || try == len(cl.retries) {
			return "", mergeRefusal(prURL, status, msg)
		}

		if err := sleep(ctx, cl.retries[try]); err != nil {
			return "", fmt.Errorf("merge %s: waiting to retry after GitHub said the head was modified: %w", prURL, err)
		}

		pj, err := cl.read(ctx, p, prURL)

		switch {
		case err != nil:
			return "", fmt.Errorf("merge %s: %w", prURL, err)
		case pj.Merged:
			return pj.MergeCommitSHA, nil
		case pj.State != "open":
			return "", fmt.Errorf("merge %s: closed while retrying: %w", prURL, ports.ErrNotMergeable)
		}

		head = pj.Head.SHA
	}
}

// mergeOnce asks GitHub to merge the pull request at head once. It answers the merge SHA and 200
// when GitHub merged it, or GitHub's refusal status and message.
func (cl *Client) mergeOnce(ctx context.Context, p pull, prURL, method, head string) (string, int, string, error) {
	var res struct {
		SHA    string `json:"sha"`
		Merged bool   `json:"merged"`
	}

	status, msg, err := cl.call(ctx, p, http.MethodPut, p.repoPath("/pulls/"+p.number+"/merge"), map[string]string{
		"merge_method": method, "sha": head,
	}, &res)

	switch {
	case err != nil:
		return "", 0, "", fmt.Errorf("merge %s: %w", prURL, err)
	case status == http.StatusOK && res.Merged:
		return res.SHA, http.StatusOK, "", nil
	case status == http.StatusOK:
		return "", 0, "", fmt.Errorf("merge %s: GitHub answered 200 without merging: %s", prURL, msg)
	default:
		return "", status, msg, nil
	}
}

// mergeRefusal is what GitHub's refusal status and msg of a merge of prURL mean.
func mergeRefusal(prURL string, status int, msg string) error {
	switch status {
	case http.StatusMethodNotAllowed, http.StatusConflict, http.StatusUnprocessableEntity:
		// Not mergeable, head moved, or a merge GitHub rejects as invalid.
		return fmt.Errorf("merge %s: GitHub refused (%d): %s: %w", prURL, status, msg, ports.ErrNotMergeable)
	default:
		return fmt.Errorf("merge %s: GitHub answered %d: %s", prURL, status, msg)
	}
}

// GitHub's 422 answers to an update-branch, matched case-insensitively on their message:
//   - headMoved: expected_head_sha is not the branch's head ("expected head sha didn't match
//     current head ref.");
//   - upToDate: the head already holds the base ("There are no new commits on the base branch.");
//   - conflicted: the base does not merge into the head cleanly ("merge conflict between base and
//     head").
const (
	headMoved  = "expected head sha"
	upToDate   = "no new commits"
	conflicted = "merge conflict"
)

// dirty is the mergeable_state of a pull request that conflicts with its base.
const dirty = "dirty"

// UpdateBranch implements ports.CodeHost: GitHub merges the base into the head branch, in the
// background once it answers 202. The pull request is read once first: a dirty one is a conflict
// without asking. A branch already up to date is a success; a 422 is a conflict only when GitHub
// names one, and any other refusal is ErrNotMergeable.
func (cl *Client) UpdateBranch(ctx context.Context, c domain.ResolvedConnection, prURL, headSHA string) error {
	p, err := cl.open(c, prURL)
	if err != nil {
		return err
	}

	if headSHA == "" {
		return fmt.Errorf("update branch of %s: the expected head SHA is required", prURL)
	}

	pj, err := cl.read(ctx, p, prURL)
	if err != nil {
		return fmt.Errorf("update branch of %s: %w", prURL, err)
	}

	if pj.MergeableState == dirty {
		return fmt.Errorf("update branch of %s: GitHub reports it dirty: %w", prURL, &ports.MergeConflictError{URL: prURL})
	}

	status, msg, err := cl.call(ctx, p, http.MethodPut, p.repoPath("/pulls/"+p.number+"/update-branch"), map[string]string{
		"expected_head_sha": headSHA,
	}, nil)
	if err != nil {
		return fmt.Errorf("update branch of %s: %w", prURL, err)
	}

	return updateAnswer(prURL, status, msg)
}

// updateAnswer is what GitHub's answer status and msg to an update-branch of prURL mean.
func updateAnswer(prURL string, status int, msg string) error {
	lower := strings.ToLower(msg)

	switch {
	case status == http.StatusAccepted:
		return nil
	case status == http.StatusUnprocessableEntity && strings.Contains(lower, upToDate):
		return nil
	case status == http.StatusUnprocessableEntity && strings.Contains(lower, conflicted):
		return fmt.Errorf("update branch of %s: GitHub refused: %s: %w", prURL, msg, &ports.MergeConflictError{URL: prURL})
	case status == http.StatusUnprocessableEntity && strings.Contains(lower, headMoved):
		return fmt.Errorf("update branch of %s: the head moved: GitHub refused (%d): %s: %w", prURL, status, msg, ports.ErrNotMergeable)
	case status == http.StatusUnprocessableEntity, status == http.StatusNotFound, status == http.StatusForbidden:
		// A refusal GitHub does not explain: never forced, nothing changed, the item stays.
		return fmt.Errorf("update branch of %s: GitHub refused (%d): %s: %w", prURL, status, msg, ports.ErrNotMergeable)
	default:
		return fmt.Errorf("update branch of %s: GitHub answered %d: %s", prURL, status, msg)
	}
}

// DeleteBranch implements ports.CodeHost.
func (cl *Client) DeleteBranch(ctx context.Context, c domain.ResolvedConnection, prURL, branch string) error {
	p, err := cl.open(c, prURL)
	if err != nil {
		return err
	}

	if branch == "" || strings.Contains(branch, "..") {
		return fmt.Errorf("delete branch %q: not a branch name", branch)
	}

	status, msg, err := cl.call(ctx, p, http.MethodDelete, p.repoPath("/git/refs/heads/"+refPath(branch)), nil, nil)

	switch {
	case err != nil:
		return fmt.Errorf("delete branch %s: %w", branch, err)
	case status == http.StatusNoContent, status == http.StatusNotFound, status == http.StatusUnprocessableEntity:
		// Deleted, or already gone (GitHub answers 422 "Reference does not exist").
		return nil
	default:
		return fmt.Errorf("delete branch %s: GitHub answered %d: %s", branch, status, msg)
	}
}

// BranchHead implements ports.CodeHost. An empty branch is the repository's default_branch.
func (cl *Client) BranchHead(ctx context.Context, c domain.ResolvedConnection, repoURL, branch string) (string, error) {
	p, err := cl.openRepo(c, repoURL)
	if err != nil {
		return "", err
	}

	if strings.Contains(branch, "..") {
		return "", fmt.Errorf("branch head %q: not a branch name", branch)
	}

	if branch == "" {
		var repo struct {
			DefaultBranch string `json:"default_branch"`
		}

		if err := cl.get(ctx, p, p.repoPath(""), &repo, maxBodyBytes); err != nil {
			return "", fmt.Errorf("default branch of %s: %w", repoURL, err)
		}

		branch = repo.DefaultBranch
	}

	var b struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	if err := cl.get(ctx, p, p.repoPath("/branches/"+refPath(branch)), &b, maxBodyBytes); err != nil {
		return "", fmt.Errorf("head of %s in %s: %w", branch, repoURL, err)
	}

	if b.Commit.SHA == "" {
		return "", fmt.Errorf("head of %s in %s: GitHub named no commit", branch, repoURL)
	}

	return b.Commit.SHA, nil
}

// commitJSON is the part of GitHub's commit object goald reads.
type commitJSON struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
	Files []struct {
		Filename string `json:"filename"`
	} `json:"files"`
}

// Compare implements ports.CodeHost with the compare API (base...head, from their merge base),
// whose first page of ports.MaxCompareCommits holds the oldest commits; total_commits says how
// many there are. The compare answer lists the files of all commits together, so each commit is
// read once more for its own (up to ports.MaxCompareCommits+1 requests in all); any failure
// fails the whole call. GitHub lists at most 300 files of one commit, and Compare does not page
// past them.
func (cl *Client) Compare(ctx context.Context, c domain.ResolvedConnection, repoURL, base, head string) (ports.Comparison, error) {
	p, err := cl.openRepo(c, repoURL)
	if err != nil {
		return ports.Comparison{}, err
	}

	for _, ref := range []string{base, head} {
		if ref == "" || strings.Contains(ref, "..") {
			return ports.Comparison{}, fmt.Errorf("compare %q...%q in %s: not a commit", base, head, repoURL)
		}
	}

	var cmp struct {
		TotalCommits int          `json:"total_commits"`
		Commits      []commitJSON `json:"commits"`
	}

	path := p.repoPath("/compare/"+refPath(base)+"..."+refPath(head)) + "?per_page=" + strconv.Itoa(ports.MaxCompareCommits) + "&page=1"
	if err := cl.get(ctx, p, path, &cmp, maxCommitBytes); err != nil {
		return ports.Comparison{}, fmt.Errorf("compare %s...%s in %s: %w", base, head, repoURL, err)
	}

	commits := cmp.Commits[:min(len(cmp.Commits), ports.MaxCompareCommits)]
	out := ports.Comparison{
		Commits:   make([]ports.Commit, 0, len(commits)),
		Truncated: cmp.TotalCommits > len(commits) || len(cmp.Commits) > len(commits),
	}

	for _, cj := range commits {
		var full commitJSON
		if err := cl.get(ctx, p, p.repoPath("/commits/"+url.PathEscape(cj.SHA)), &full, maxCommitBytes); err != nil {
			return ports.Comparison{}, fmt.Errorf("compare %s...%s in %s: files of %s: %w", base, head, repoURL, cj.SHA, err)
		}

		files := make([]string, 0, len(full.Files))
		for _, f := range full.Files {
			files = append(files, f.Filename)
		}

		subject, _, _ := strings.Cut(cj.Commit.Message, "\n")
		out.Commits = append(out.Commits, ports.Commit{SHA: cj.SHA, Subject: strings.TrimSpace(subject), Files: files})
	}

	return out, nil
}

// get reads path into out, its answer bounded by limit: a 404 is ports.ErrNotFound.
func (cl *Client) get(ctx context.Context, p pull, path string, out any, limit int64) error {
	status, msg, err := cl.callLimit(ctx, p, http.MethodGet, path, nil, out, limit)

	switch {
	case err != nil:
		return err
	case status == http.StatusNotFound:
		return fmt.Errorf("GitHub answered 404: %s: %w", msg, ports.ErrNotFound)
	case status != http.StatusOK:
		return fmt.Errorf("GitHub answered %d: %s", status, msg)
	}

	return nil
}

// refPath is ref as URL path segments, each escaped.
func refPath(ref string) string {
	segments := strings.Split(ref, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}

	return strings.Join(segments, "/")
}

// call sends one API request with the connection's token and decodes a successful JSON answer
// into out (when out is not nil). It returns the status and GitHub's message, if any; err is
// only for requests that got no answer or an unreadable one. The token is scrubbed from the
// message and from errors.
func (cl *Client) call(ctx context.Context, p pull, method, path string, body, out any) (int, string, error) {
	return cl.callLimit(ctx, p, method, path, body, out, maxBodyBytes)
}

// callLimit is call with the answer bounded by limit bytes.
func (cl *Client) callLimit(ctx context.Context, p pull, method, path string, body, out any, limit int64) (int, string, error) {
	status, msg, err := cl.send(ctx, p, method, path, body, out, limit)

	scrub := func(s string) string { return strings.ReplaceAll(s, p.token, "[redacted]") }

	if err != nil {
		err = errors.New(scrub(err.Error()))
	}

	return status, scrub(msg), err
}

func (cl *Client) send(ctx context.Context, p pull, method, path string, body, out any, limit int64) (int, string, error) {
	var reader io.Reader

	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, "", err
		}

		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.api+path, reader)
	if err != nil {
		return 0, "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "goald")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := cl.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return 0, "", fmt.Errorf("read answer: %w", err)
	}

	if len(bytes.TrimSpace(raw)) == 0 || !strings.Contains(res.Header.Get("Content-Type"), "json") {
		return res.StatusCode, "", nil
	}

	var problem struct {
		Message string `json:"message"`
	}

	if err := json.Unmarshal(raw, &problem); err != nil {
		return 0, "", fmt.Errorf("decode answer (%d): %w", res.StatusCode, err)
	}

	if out != nil && res.StatusCode < http.StatusMultipleChoices {
		if err := json.Unmarshal(raw, out); err != nil {
			return 0, "", fmt.Errorf("decode answer (%d): %w", res.StatusCode, err)
		}
	}

	return res.StatusCode, problem.Message, nil
}
