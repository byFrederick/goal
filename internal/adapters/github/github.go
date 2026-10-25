// Package github implements ports.CodeHost with GitHub's REST API, called from the goald host
// with the credential of a connection whose kind's code host is github (spec 4 §5.5).
//
// It reads pull requests, merges them and deletes branches, nothing else. The connection's
// rendered git login says which host it serves and holds the token; a pull request URL on any
// other host is refused before anything is sent, and the repository is the URL's own. The API is
// https://api.github.com for github.com, https://api.<host> for a GitHub Enterprise Cloud host
// (<name>.ghe.com) and https://<host>/api/v3 for a GitHub Enterprise Server. A merge names the
// head SHA it was asked for, so GitHub refuses it if the branch moved, and it is never forced:
// GitHub's refusals (not mergeable, head moved, invalid) come back as ports.ErrNotMergeable. The
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
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.CodeHost = (*Client)(nil)

// Config tunes the client. The zero value of each field means its default.
type Config struct {
	// APIURL, when set, is the REST API root for every host instead of the one derived from the
	// connection's: goald's GOAL_GITHUB_API_URL until runs get connections (spec 4 PR 4-14), a
	// stand-in GitHub in tests.
	APIURL string
	// Timeout bounds one request. Default: 30s.
	Timeout time.Duration
}

const (
	apiVersion   = "2022-11-28"
	maxBodyBytes = 1 << 20
	dotCom       = "github.com"
)

// Client calls GitHub's REST API. It is safe for concurrent use and keeps no state.
type Client struct {
	api  string
	http *http.Client
}

// New returns a client. cfg.APIURL, when set, must be an http(s) URL; goald's settings check it.
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Client{
		api: strings.TrimSuffix(cfg.APIURL, "/"),
		http: &http.Client{
			Timeout: cfg.Timeout,
			// A credentialed request is never sent anywhere but the connection's API.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// pull is one pull request, with the API and the token that reach it.
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

// PullRequest implements ports.CodeHost.
func (cl *Client) PullRequest(ctx context.Context, c domain.ResolvedConnection, prURL string) (ports.PullRequest, error) {
	p, err := cl.open(c, prURL)
	if err != nil {
		return ports.PullRequest{}, err
	}

	var pj pullJSON

	status, _, err := cl.call(ctx, p, http.MethodGet, p.repoPath("/pulls/"+p.number), nil, &pj)

	switch {
	case err != nil:
		return ports.PullRequest{}, fmt.Errorf("pull request %s: %w", prURL, err)
	case status == http.StatusNotFound:
		return ports.PullRequest{}, fmt.Errorf("pull request %s does not exist: %w", prURL, ports.ErrNotMergeable)
	case status != http.StatusOK:
		return ports.PullRequest{}, fmt.Errorf("pull request %s: GitHub answered %d", prURL, status)
	}

	pr := ports.PullRequest{
		URL: prURL, Open: pj.State == "open", Merged: pj.Merged, Mergeable: pj.Mergeable != nil && *pj.Mergeable,
		HeadSHA: pj.Head.SHA, HeadBranch: pj.Head.Ref, BaseBranch: pj.Base.Ref,
		SameRepository: pj.Head.Repo != nil && pj.Base.Repo != nil && strings.EqualFold(pj.Head.Repo.FullName, pj.Base.Repo.FullName),
	}

	if pj.Merged {
		pr.MergeSHA = pj.MergeCommitSHA
	}

	return pr, nil
}

// Merge implements ports.CodeHost.
func (cl *Client) Merge(ctx context.Context, c domain.ResolvedConnection, prURL string, m ports.Merge) (string, error) {
	p, err := cl.open(c, prURL)
	if err != nil {
		return "", err
	}

	if m.HeadSHA == "" {
		return "", fmt.Errorf("merge %s: the head SHA to merge is required", prURL)
	}

	var res struct {
		SHA    string `json:"sha"`
		Merged bool   `json:"merged"`
	}

	status, msg, err := cl.call(ctx, p, http.MethodPut, p.repoPath("/pulls/"+p.number+"/merge"), map[string]string{
		"merge_method": m.Method, "sha": m.HeadSHA,
	}, &res)

	switch {
	case err != nil:
		return "", fmt.Errorf("merge %s: %w", prURL, err)
	case status == http.StatusOK && res.Merged:
		return res.SHA, nil
	case status == http.StatusMethodNotAllowed, status == http.StatusConflict, status == http.StatusUnprocessableEntity:
		// Not mergeable, head moved, or a merge GitHub rejects as invalid.
		return "", fmt.Errorf("merge %s: GitHub refused (%d): %s: %w", prURL, status, msg, ports.ErrNotMergeable)
	default:
		return "", fmt.Errorf("merge %s: GitHub answered %d: %s", prURL, status, msg)
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

	segments := strings.Split(branch, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}

	status, msg, err := cl.call(ctx, p, http.MethodDelete, p.repoPath("/git/refs/heads/"+strings.Join(segments, "/")), nil, nil)

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

// call sends one API request with the connection's token and decodes a successful JSON answer
// into out (when out is not nil). It returns the status and GitHub's message, if any; err is
// only for requests that got no answer or an unreadable one. The token is scrubbed from the
// message and from errors.
func (cl *Client) call(ctx context.Context, p pull, method, path string, body, out any) (int, string, error) {
	status, msg, err := cl.send(ctx, p, method, path, body, out)

	scrub := func(s string) string { return strings.ReplaceAll(s, p.token, "[redacted]") }

	if err != nil {
		err = errors.New(scrub(err.Error()))
	}

	return status, scrub(msg), err
}

func (cl *Client) send(ctx context.Context, p pull, method, path string, body, out any) (int, string, error) {
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

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
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
