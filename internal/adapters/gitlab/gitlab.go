// Package gitlab implements ports.CodeHost with GitLab's REST API (v4, merge requests), called
// from the goald host with the credential of a connection whose kind's code host is gitlab (spec
// 4 §5.5).
//
// It reads merge requests, merges them and deletes branches, nothing else. The connection's
// rendered git login says which host it serves (gitlab.com or a self-managed GitLab) and holds
// the access token; a merge request URL on any other host is refused before anything is sent,
// and the project is the URL's own (https://<host>/<namespace>/<project>/-/merge_requests/<iid>).
// The API is https://<host>/api/v4. A merge names the head SHA it was asked for, so GitLab
// refuses it if the branch moved, and it is never forced: GitLab's refusals (not mergeable,
// conflicts, head moved) come back as ports.ErrNotMergeable. Squash merges with squash; merge
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
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.CodeHost = (*Client)(nil)

// Config tunes the client. The zero value of each field means its default.
type Config struct {
	// APIURL, when set, is the REST API root for every host instead of https://<host>/api/v4: a
	// stand-in GitLab in tests.
	APIURL string
	// Timeout bounds one request. Default: 30s.
	Timeout time.Duration
	// Transport carries the requests; nil is http.DefaultTransport. goald passes its connection
	// transport, which dials public addresses only (egress.PublicDialer).
	Transport http.RoundTripper
}

const (
	maxBodyBytes = 1 << 20
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
			// A credentialed request is never sent anywhere but the connection's API.
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

// open checks that mrURL is a merge request on the host of c's git login and names it.
func (cl *Client) open(c domain.ResolvedConnection, mrURL string) (mergeRequest, error) {
	host, token, err := c.GitLogin()
	if err != nil {
		return mergeRequest{}, err
	}

	u, err := url.Parse(mrURL)
	if err != nil {
		return mergeRequest{}, fmt.Errorf("merge request %q: %w", mrURL, ports.ErrNotMergeable)
	}

	m := mrPath.FindStringSubmatch(u.Path)
	if u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || m == nil {
		return mergeRequest{}, fmt.Errorf("%q is not a merge request URL: %w", mrURL, ports.ErrNotMergeable)
	}

	if !strings.EqualFold(u.Host, host) {
		return mergeRequest{}, fmt.Errorf("%s is not on %s, the host of connection %s: %w", mrURL, host, c.Connection.Name, ports.ErrNotMergeable)
	}

	api := cl.api
	if api == "" {
		api = "https://" + host + "/api/v4"
	}

	return mergeRequest{api: api, token: token, project: url.PathEscape(m[1]), iid: m[2]}, nil
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

// PullRequest implements ports.CodeHost.
func (cl *Client) PullRequest(ctx context.Context, c domain.ResolvedConnection, mrURL string) (ports.PullRequest, error) {
	m, err := cl.open(c, mrURL)
	if err != nil {
		return ports.PullRequest{}, err
	}

	var mr mrJSON

	status, _, err := cl.call(ctx, m, http.MethodGet, m.path("/merge_requests/"+m.iid), nil, &mr)

	switch {
	case err != nil:
		return ports.PullRequest{}, fmt.Errorf("merge request %s: %w", mrURL, err)
	case status == http.StatusNotFound:
		return ports.PullRequest{}, fmt.Errorf("merge request %s does not exist: %w", mrURL, ports.ErrNotMergeable)
	case status != http.StatusOK:
		return ports.PullRequest{}, fmt.Errorf("merge request %s: GitLab answered %d", mrURL, status)
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
func (cl *Client) Merge(ctx context.Context, c domain.ResolvedConnection, mrURL string, mm ports.Merge) (string, error) {
	m, err := cl.open(c, mrURL)
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

// DeleteBranch implements ports.CodeHost.
func (cl *Client) DeleteBranch(ctx context.Context, c domain.ResolvedConnection, mrURL, branch string) error {
	m, err := cl.open(c, mrURL)
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

// call sends one API request with the connection's token and decodes a successful JSON answer
// into out (when out is not nil). It returns the status and GitLab's message, if any; err is
// only for requests that got no answer or an unreadable one. The token is scrubbed from the
// message and from errors.
func (cl *Client) call(ctx context.Context, m mergeRequest, method, path string, body, out any) (int, string, error) {
	status, msg, err := cl.send(ctx, m, method, path, body, out)

	scrub := func(s string) string { return strings.ReplaceAll(s, m.token, "[redacted]") }

	if err != nil {
		err = errors.New(scrub(err.Error()))
	}

	return status, scrub(msg), err
}

func (cl *Client) send(ctx context.Context, m mergeRequest, method, path string, body, out any) (int, string, error) {
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

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return 0, "", fmt.Errorf("read answer: %w", err)
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
