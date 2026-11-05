// Package gitlab implements ports.CodeHost with GitLab's REST API (v4, merge requests), called
// from the goald host for one project on a GitLab host of the organization's list
// (domain.CodeRepository) and the access token that signs in to it (ADR 0041, decision 7).
//
// It reads merge requests, merges them, rebases them on their target and deletes branches, and
// reads a branch's head and the commits between two commits with their files, nothing else. A project on a host of another kind, and a merge request URL that is not the
// project's (https://<host>/<namespace>/<project>/-/merge_requests/<iid>), are refused before
// anything is sent. The API is the host's domain.CodeHostAPIBase, https://<host>/api/v4. A merge
// names the head SHA it was asked for, so GitLab refuses it if the branch moved, and it is never forced: GitLab's refusals (not mergeable,
// conflicts, head moved) come back as ports.ErrNotMergeable. GitLab's rebase takes no expected
// head: UpdateBranch reads the merge request first and rebases only if its head is the one it
// was given, best effort against a push in between, and never forced; a rebase GitLab refuses
// for conflicts comes back as a *ports.MergeConflictError (without files). Squash merges with squash; merge
// uses the project's own merge method. The token travels only in the PRIVATE-TOKEN header of
// requests to that API; redirects are not followed, and the token is scrubbed from every error.
package gitlab

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

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.CodeHost = (*Client)(nil)

// Config tunes the client. The zero value of each field means its default.
type Config struct {
	// APIURL, when set, is the REST API root for every host instead of the project host's
	// domain.CodeHostAPIBase: a stand-in GitLab in tests.
	APIURL string
	// Timeout bounds one request. Default: 30s.
	Timeout time.Duration
	// Transport carries the requests; nil is http.DefaultTransport. goald passes its connection
	// transport, which dials public addresses only (egress.PublicDialer).
	Transport http.RoundTripper
}

const (
	maxBodyBytes = 1 << 20
	// maxDiffBytes bounds the answers of the compare and commit diff APIs, which carry every
	// changed file's diff.
	maxDiffBytes = 32 << 20
	// maxDiffFiles is the files of one commit Compare reads: GitLab's largest page.
	maxDiffFiles = 100
	methodSquash = "squash"
	methodMerge  = "merge"
)

// Client calls GitLab's REST API. It is safe for concurrent use and keeps no state.
type Client struct {
	api  string
	http *http.Client
}

// New returns a client.
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Client{
		api: strings.TrimSuffix(cfg.APIURL, "/"),
		http: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: cfg.Transport,
			// A credentialed request is never sent anywhere but the project host's API.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// mergeRequest is one merge request, with the API and the token that reach it.
type mergeRequest struct {
	api, token string
	// project is the project's full path, URL-encoded as the API's project id.
	project string
	iid     string
}

func (m mergeRequest) path(rest string) string {
	return "/projects/" + m.project + rest
}

// mrPath is a merge request's path: a namespace (groups and subgroups), a project, its iid.
var mrPath = regexp.MustCompile(`^/((?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+)/-/merge_requests/([1-9][0-9]*)/?$`)

// open checks repo and token (check) and that mrURL is a merge request of repo, and names it.
func (cl *Client) open(repo domain.CodeRepository, token, mrURL string) (mergeRequest, error) {
	m, err := cl.check(repo, token)
	if err != nil {
		return mergeRequest{}, fmt.Errorf("merge request %s: %w: %w", mrURL, err, ports.ErrNotMergeable)
	}

	u, err := url.Parse(mrURL)
	if err != nil {
		return mergeRequest{}, fmt.Errorf("merge request %q: %w", mrURL, ports.ErrNotMergeable)
	}

	mm := mrPath.FindStringSubmatch(u.Path)
	if u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || mm == nil {
		return mergeRequest{}, fmt.Errorf("%q is not a merge request URL: %w", mrURL, ports.ErrNotMergeable)
	}

	if !strings.EqualFold(u.Host, repo.Host.Host) || !strings.EqualFold(mm[1], repo.Owner+"/"+repo.Name) {
		return mergeRequest{}, fmt.Errorf("%s is not a merge request of %s: %w", mrURL, repo, ports.ErrNotMergeable)
	}

	m.iid = mm[2]

	return m, nil
}

// projectSegment is one segment of a project's path: a group, a subgroup or the project.
var projectSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// check refuses, before anything is sent, a project that is not on a GitLab host or names no
// namespace or name, and an empty token, each wrapping domain.ErrInvalid; it names the project
// with the API that reaches it.
func (cl *Client) check(repo domain.CodeRepository, token string) (mergeRequest, error) {
	segs := append(strings.Split(repo.Owner, "/"), repo.Name)

	switch {
	case repo.Host.Kind != domain.CodeHostGitLab || repo.Host.Host == "":
		return mergeRequest{}, fmt.Errorf("%s is not on a GitLab host: %w", repo, domain.ErrInvalid)
	case slices.ContainsFunc(segs, func(s string) bool { return !projectSegment.MatchString(s) || strings.Trim(s, ".") == "" }):
		return mergeRequest{}, fmt.Errorf("%s is not a GitLab project: %w", repo, domain.ErrInvalid)
	case token == "":
		return mergeRequest{}, fmt.Errorf("%s: no token: %w", repo, domain.ErrInvalid)
	}

	api := cl.api
	if api == "" {
		api = domain.CodeHostAPIBase(repo.Host)
	}

	return mergeRequest{api: api, token: token, project: url.PathEscape(repo.Owner + "/" + repo.Name)}, nil
}

// mrJSON is the part of GitLab's merge request object goald reads.
type mrJSON struct {
	State               string `json:"state"` // opened, closed, locked or merged
	SHA                 string `json:"sha"`
	SourceBranch        string `json:"source_branch"`
	TargetBranch        string `json:"target_branch"`
	SourceProjectID     int64  `json:"source_project_id"`
	TargetProjectID     int64  `json:"target_project_id"`
	DetailedMergeStatus string `json:"detailed_merge_status"`
	MergeStatus         string `json:"merge_status"` // GitLab before 15.6
	MergeCommitSHA      string `json:"merge_commit_sha"`
	SquashCommitSHA     string `json:"squash_commit_sha"`
}

// mergeSHA is the commit a merged merge request left on its target: its merge commit, its squash
// commit when fast-forwarded, or its head when fast-forwarded without squashing.
func (mr mrJSON) mergeSHA() string {
	switch {
	case mr.MergeCommitSHA != "":
		return mr.MergeCommitSHA
	case mr.SquashCommitSHA != "":
		return mr.SquashCommitSHA
	default:
		return mr.SHA
	}
}

// read gets the merge request.
func (cl *Client) read(ctx context.Context, m mergeRequest, mrURL string) (mrJSON, error) {
	var mr mrJSON

	status, _, err := cl.call(ctx, m, http.MethodGet, m.path("/merge_requests/"+m.iid), nil, &mr)

	switch {
	case err != nil:
		return mrJSON{}, fmt.Errorf("merge request %s: %w", mrURL, err)
	case status == http.StatusNotFound:
		return mrJSON{}, fmt.Errorf("merge request %s does not exist: %w", mrURL, ports.ErrNotMergeable)
	case status != http.StatusOK:
		return mrJSON{}, fmt.Errorf("merge request %s: GitLab answered %d", mrURL, status)
	}

	return mr, nil
}

// PullRequest implements ports.CodeHost.
func (cl *Client) PullRequest(ctx context.Context, repo domain.CodeRepository, token, mrURL string) (ports.PullRequest, error) {
	m, err := cl.open(repo, token, mrURL)
	if err != nil {
		return ports.PullRequest{}, err
	}

	mr, err := cl.read(ctx, m, mrURL)
	if err != nil {
		return ports.PullRequest{}, err
	}

	mergeable := mr.DetailedMergeStatus == "mergeable" || (mr.DetailedMergeStatus == "" && mr.MergeStatus == "can_be_merged")

	pr := ports.PullRequest{
		URL: mrURL, Open: mr.State == "opened", Merged: mr.State == "merged", Mergeable: mr.State == "opened" && mergeable,
		HeadSHA: mr.SHA, HeadBranch: mr.SourceBranch, BaseBranch: mr.TargetBranch,
		SameRepository: mr.SourceProjectID != 0 && mr.SourceProjectID == mr.TargetProjectID,
	}

	if pr.Merged {
		pr.MergeSHA = mr.mergeSHA()
	}

	return pr, nil
}

// Merge implements ports.CodeHost.
func (cl *Client) Merge(ctx context.Context, repo domain.CodeRepository, token, mrURL string, mm ports.Merge) (string, error) {
	m, err := cl.open(repo, token, mrURL)
	if err != nil {
		return "", err
	}

	if mm.HeadSHA == "" {
		return "", fmt.Errorf("merge %s: the head SHA to merge is required", mrURL)
	}

	if mm.Method != methodSquash && mm.Method != methodMerge {
		return "", fmt.Errorf("merge %s: GitLab merges with squash or the project's merge method, not %q: %w", mrURL, mm.Method, ports.ErrNotMergeable)
	}

	var mr mrJSON

	status, msg, err := cl.call(ctx, m, http.MethodPut, m.path("/merge_requests/"+m.iid+"/merge"), map[string]any{
		"sha": mm.HeadSHA, "squash": mm.Method == methodSquash,
	}, &mr)

	switch {
	case err != nil:
		return "", fmt.Errorf("merge %s: %w", mrURL, err)
	case status == http.StatusOK:
		return mr.mergeSHA(), nil
	case status == http.StatusMethodNotAllowed, status == http.StatusNotAcceptable, status == http.StatusConflict,
		status == http.StatusUnprocessableEntity:
		// Not mergeable (draft, closed, blocked), conflicts, or the head moved.
		return "", fmt.Errorf("merge %s: GitLab refused (%d): %s: %w", mrURL, status, msg, ports.ErrNotMergeable)
	default:
		return "", fmt.Errorf("merge %s: GitLab answered %d: %s", mrURL, status, msg)
	}
}

// UpdateBranch implements ports.CodeHost: GitLab rebases the source branch on the target, in
// the background once it answers 202.
func (cl *Client) UpdateBranch(ctx context.Context, repo domain.CodeRepository, token, mrURL, headSHA string) error {
	m, err := cl.open(repo, token, mrURL)
	if err != nil {
		return err
	}

	if headSHA == "" {
		return fmt.Errorf("rebase %s: the expected head SHA is required", mrURL)
	}

	mr, err := cl.read(ctx, m, mrURL)

	switch {
	case err != nil:
		return err
	case mr.State != "opened":
		return fmt.Errorf("rebase %s: the merge request is %s: %w", mrURL, mr.State, ports.ErrNotMergeable)
	case mr.SHA != headSHA:
		return fmt.Errorf("rebase %s: head moved to %s: %w", mrURL, mr.SHA, ports.ErrNotMergeable)
	}

	status, msg, err := cl.call(ctx, m, http.MethodPut, m.path("/merge_requests/"+m.iid+"/rebase"), nil, nil)

	switch {
	case err != nil:
		return fmt.Errorf("rebase %s: %w", mrURL, err)
	case status == http.StatusAccepted:
		return nil
	case status == http.StatusConflict, status >= http.StatusBadRequest && strings.Contains(strings.ToLower(msg), "conflict"):
		return fmt.Errorf("rebase %s: GitLab refused (%d): %s: %w", mrURL, status, msg, &ports.MergeConflictError{URL: mrURL})
	case status == http.StatusForbidden, status == http.StatusMethodNotAllowed, status == http.StatusNotFound:
		// No permission to push to the source branch, or a merge request that cannot be rebased.
		return fmt.Errorf("rebase %s: GitLab refused (%d): %s: %w", mrURL, status, msg, ports.ErrNotMergeable)
	default:
		return fmt.Errorf("rebase %s: GitLab answered %d: %s", mrURL, status, msg)
	}
}

// DeleteBranch implements ports.CodeHost.
func (cl *Client) DeleteBranch(ctx context.Context, repo domain.CodeRepository, token, mrURL, branch string) error {
	m, err := cl.open(repo, token, mrURL)
	if err != nil {
		return err
	}

	if branch == "" || strings.Contains(branch, "..") {
		return fmt.Errorf("delete branch %q: not a branch name", branch)
	}

	status, msg, err := cl.call(ctx, m, http.MethodDelete, m.path("/repository/branches/"+url.PathEscape(branch)), nil, nil)

	switch {
	case err != nil:
		return fmt.Errorf("delete branch %s: %w", branch, err)
	case status == http.StatusNoContent, status == http.StatusNotFound:
		// Deleted, or already gone.
		return nil
	default:
		return fmt.Errorf("delete branch %s: GitLab answered %d: %s", branch, status, msg)
	}
}

// call sends one API request with the project's token and decodes a successful JSON answer
// into out (when out is not nil). It returns the status and GitLab's message, if any; err is
// only for requests that got no answer or an unreadable one. The token is scrubbed from the
// message and from errors.
func (cl *Client) call(ctx context.Context, m mergeRequest, method, path string, body, out any) (int, string, error) {
	return cl.exchange(ctx, m, method, path, body, out, maxBodyBytes)
}

// exchange is call with its answer bounded by limit bytes; a longer answer is an error.
func (cl *Client) exchange(ctx context.Context, m mergeRequest, method, path string, body, out any, limit int64) (int, string, error) {
	status, msg, err := cl.send(ctx, m, method, path, body, out, limit)

	scrub := func(s string) string { return strings.ReplaceAll(s, m.token, "[redacted]") }

	if err != nil {
		err = errors.New(scrub(err.Error()))
	}

	return status, scrub(msg), err
}

func (cl *Client) send(ctx context.Context, m mergeRequest, method, path string, body, out any, limit int64) (int, string, error) {
	var reader io.Reader

	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, "", err
		}

		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, m.api+path, reader)
	if err != nil {
		return 0, "", err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("PRIVATE-TOKEN", m.token)
	req.Header.Set("User-Agent", "goald")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := cl.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return 0, "", fmt.Errorf("read answer: %w", err)
	}

	if int64(len(raw)) > limit {
		return 0, "", fmt.Errorf("read answer (%d): longer than %d bytes", res.StatusCode, limit)
	}

	if len(bytes.TrimSpace(raw)) == 0 || !strings.Contains(res.Header.Get("Content-Type"), "json") {
		return res.StatusCode, "", nil
	}

	if res.StatusCode >= http.StatusMultipleChoices {
		return res.StatusCode, problem(raw), nil
	}

	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return 0, "", fmt.Errorf("decode answer (%d): %w", res.StatusCode, err)
		}
	}

	return res.StatusCode, "", nil
}

// problem is the reason GitLab gives in an error answer: its message (a string, or an object of
// field errors), else its error.
func problem(raw []byte) string {
	var p struct {
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
	}

	if json.Unmarshal(raw, &p) != nil {
		return ""
	}

	var s string
	if json.Unmarshal(p.Message, &s) == nil {
		return s
	}

	if len(p.Message) > 0 {
		return string(p.Message)
	}

	return p.Error
}

// get reads path into out, its answer bounded by limit: a 404 is ports.ErrNotFound.
func (cl *Client) get(ctx context.Context, m mergeRequest, path string, out any, limit int64) error {
	status, msg, err := cl.exchange(ctx, m, http.MethodGet, path, nil, out, limit)

	switch {
	case err != nil:
		return err
	case status == http.StatusNotFound:
		return fmt.Errorf("GitLab answered 404: %s: %w", msg, ports.ErrNotFound)
	case status != http.StatusOK:
		return fmt.Errorf("GitLab answered %d: %s", status, msg)
	}

	return nil
}

// BranchHead implements ports.CodeHost with GET /projects/:id/repository/branches/:branch, the
// branch escaped as one path segment. An empty branch is the project's default_branch, read
// first with GET /projects/:id.
func (cl *Client) BranchHead(ctx context.Context, repo domain.CodeRepository, token, branch string) (string, error) {
	m, err := cl.check(repo, token)
	if err != nil {
		return "", err
	}

	if strings.Contains(branch, "..") {
		return "", fmt.Errorf("branch head %q: not a branch name", branch)
	}

	if branch == "" {
		var p struct {
			DefaultBranch string `json:"default_branch"`
		}

		if err := cl.get(ctx, m, m.path(""), &p, maxBodyBytes); err != nil {
			return "", fmt.Errorf("default branch of %s: %w", repo, err)
		}

		if p.DefaultBranch == "" {
			return "", fmt.Errorf("default branch of %s: GitLab named none", repo)
		}

		branch = p.DefaultBranch
	}

	var b struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}

	if err := cl.get(ctx, m, m.path("/repository/branches/"+url.PathEscape(branch)), &b, maxBodyBytes); err != nil {
		return "", fmt.Errorf("head of %s in %s: %w", branch, repo, err)
	}

	if b.Commit.ID == "" {
		return "", fmt.Errorf("head of %s in %s: GitLab named no commit", branch, repo)
	}

	return b.Commit.ID, nil
}

// Compare implements ports.CodeHost with GET /projects/:id/repository/compare (from base to
// head, from their merge base: straight=false), which lists every commit in one answer. GitLab
// lists them oldest first (it walks the range in reverse), though its API docs do not say so;
// Compare keeps that order and the first ports.MaxCompareCommits of them. The compare answer
// carries the diffs of all commits together, so each kept commit's diff is read once more
// (GET /projects/:id/repository/commits/:sha/diff, its first 100 files, not paged past): up to
// ports.MaxCompareCommits+1 requests in all, and any failure fails the whole call.
func (cl *Client) Compare(ctx context.Context, repo domain.CodeRepository, token, base, head string) (ports.Comparison, error) {
	m, err := cl.check(repo, token)
	if err != nil {
		return ports.Comparison{}, err
	}

	for _, ref := range []string{base, head} {
		if ref == "" || strings.Contains(ref, "..") {
			return ports.Comparison{}, fmt.Errorf("compare %q...%q in %s: not a commit", base, head, repo)
		}
	}

	var cmp struct {
		Commits []struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"commits"`
	}

	query := url.Values{"from": {base}, "to": {head}, "straight": {"false"}}
	if err := cl.get(ctx, m, m.path("/repository/compare?"+query.Encode()), &cmp, maxDiffBytes); err != nil {
		return ports.Comparison{}, fmt.Errorf("compare %s...%s in %s: %w", base, head, repo, err)
	}

	commits := cmp.Commits[:min(len(cmp.Commits), ports.MaxCompareCommits)]
	out := ports.Comparison{Commits: make([]ports.Commit, 0, len(commits)), Truncated: len(cmp.Commits) > len(commits)}

	for _, cj := range commits {
		var diffs []struct {
			OldPath string `json:"old_path"`
			NewPath string `json:"new_path"`
		}

		path := m.path("/repository/commits/" + url.PathEscape(cj.ID) + "/diff?per_page=" + strconv.Itoa(maxDiffFiles))
		if err := cl.get(ctx, m, path, &diffs, maxDiffBytes); err != nil {
			return ports.Comparison{}, fmt.Errorf("compare %s...%s in %s: files of %s: %w", base, head, repo, cj.ID, err)
		}

		// A rename or a delete changed its old path too.
		files := make([]string, 0, len(diffs))
		for _, d := range diffs {
			for _, f := range []string{d.NewPath, d.OldPath} {
				if f != "" && !slices.Contains(files, f) {
					files = append(files, f)
				}
			}
		}

		subject, _, _ := strings.Cut(cj.Message, "\n")
		out.Commits = append(out.Commits, ports.Commit{SHA: cj.ID, Subject: strings.TrimSpace(subject), Files: files})
	}

	return out, nil
}

// Repositories implements ports.CodeHost: GitLab does not support it yet, and nothing is sent.
func (cl *Client) Repositories(context.Context, domain.CodeHost, string, string) (ports.RepositoryList, error) {
	return ports.RepositoryList{}, fmt.Errorf("gitlab: repositories: %w", errors.ErrUnsupported)
}

// accessLevels are GitLab's role names by access level.
var accessLevels = map[int]string{10: "Guest", 15: "Planner", 20: "Reporter", 30: "Developer", 40: "Maintainer", 50: "Owner"}

// pushLevel is the least access level that can push: Developer.
const pushLevel = 30

// RepositoryAccess implements ports.CodeHost with GET /projects/:id: a project GitLab shows the
// token can be read, and it can be pushed to when the higher of its project and group access
// levels is Developer (30) or more. A refusal (401, 403, 404) is a failed read with GitLab's
// message; a 5xx or no answer is an error.
func (cl *Client) RepositoryAccess(ctx context.Context, repo domain.CodeRepository, token string) (ports.RepositoryAccess, error) {
	mr, err := cl.check(repo, token)
	if err != nil {
		return ports.RepositoryAccess{}, fmt.Errorf("repository access: %w", err)
	}

	project := repo.Owner + "/" + repo.Name

	type access struct {
		Level int `json:"access_level"`
	}

	var p struct {
		Permissions struct {
			Project *access `json:"project_access"`
			Group   *access `json:"group_access"`
		} `json:"permissions"`
	}

	status, msg, err := cl.call(ctx, mr, http.MethodGet, mr.path(""), nil, &p)

	switch {
	case err != nil:
		return ports.RepositoryAccess{}, fmt.Errorf("repository access of %s: %w", project, err)
	case status >= http.StatusInternalServerError:
		return ports.RepositoryAccess{}, fmt.Errorf("repository access of %s: GitLab answered %d %s", project, status, msg)
	case status != http.StatusOK:
		return ports.RepositoryAccess{
			Read: ports.AccessCheck{Reason: strings.TrimSpace(fmt.Sprintf("GitLab answered %d: %s", status, msg))},
			Push: ports.AccessCheck{Reason: "not checked: the token cannot read " + project},
		}, nil
	}

	level := 0

	for _, a := range []*access{p.Permissions.Project, p.Permissions.Group} {
		if a != nil {
			level = max(level, a.Level)
		}
	}

	return ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "GitLab shows " + project + " to the token"},
		Push: pushCheck(level, project),
	}, nil
}

// pushCheck is whether access level can push to project: Developer (30) or more.
func pushCheck(level int, project string) ports.AccessCheck {
	role, ok := accessLevels[level]
	if !ok {
		role = "custom"
	}

	switch {
	case level >= pushLevel:
		return ports.AccessCheck{OK: true, Reason: fmt.Sprintf("the token has %s access (%d) to %s", role, level, project)}
	case level == 0:
		return ports.AccessCheck{Reason: "the token is no member of " + project + ", so it cannot push"}
	default:
		return ports.AccessCheck{Reason: fmt.Sprintf("the token has %s access (%d) to %s; pushing needs Developer (30)", role, level, project)}
	}
}
