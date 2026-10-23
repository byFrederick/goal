// Package github implements ports.CodeHost with GitHub's REST API, called from the goald host
// with the factory repository's token (resolved through ports.Secrets for every call).
//
// It reads pull requests, merges them and deletes branches, nothing else. A pull request must
// live on the factory's own repository. A merge names the head SHA it was asked for, so GitHub
// refuses it if the branch moved, and it is never forced: GitHub's refusals (not mergeable,
// head moved, invalid) come back as ports.ErrNotMergeable. The token travels only in the
// Authorization header of requests to the configured API; redirects are not followed, and the
// token is scrubbed from every error.
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

// Config says where GitHub is. The zero value of each field means its default.
type Config struct {
	// APIURL is the REST API root. Default: https://api.github.com.
	APIURL string
	// WebHost is the host of repository and pull request URLs. Default: github.com.
	WebHost string
	// Timeout bounds one request. Default: 30s.
	Timeout time.Duration
}

const (
	apiVersion   = "2022-11-28"
	maxBodyBytes = 1 << 20
)

// Client calls GitHub's REST API. It is safe for concurrent use and keeps no state.
type Client struct {
	secrets ports.Secrets
	api     string
	web     string
	http    *http.Client
}

// New returns a client that resolves repository tokens through secrets. cfg.APIURL must be an
// http(s) URL; goald's settings check it.
func New(secrets ports.Secrets, cfg Config) *Client {
	if cfg.APIURL == "" {
		cfg.APIURL = "https://api.github.com"
	}

	if cfg.WebHost == "" {
		cfg.WebHost = "github.com"
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Client{
		secrets: secrets, api: strings.TrimSuffix(cfg.APIURL, "/"), web: strings.ToLower(cfg.WebHost),
		http: &http.Client{
			Timeout: cfg.Timeout,
			// A credentialed request is never sent anywhere but the configured API.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// pull is one pull request on a repository.
type pull struct {
	owner, repo string
	number      string
}

func (p pull) path(rest string) string {
	return "/repos/" + url.PathEscape(p.owner) + "/" + url.PathEscape(p.repo) + "/pulls/" + p.number + rest
}

var (
	repoPath = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)
	pullPath = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/([1-9][0-9]*)/?$`)
)

// parse checks that prURL is a pull request of repo on the web host and names it.
func (c *Client) parse(repo domain.Repository, prURL string) (pull, error) {
	ru, err := url.Parse(repo.URL)
	if err != nil {
		return pull{}, fmt.Errorf("repository URL: %w", err)
	}

	rm := repoPath.FindStringSubmatch(ru.Path)
	if ru.Scheme != "https" || strings.ToLower(ru.Host) != c.web || rm == nil {
		return pull{}, fmt.Errorf("repository %s is not a %s repository", repo.URL, c.web)
	}

	pu, err := url.Parse(prURL)
	if err != nil {
		return pull{}, fmt.Errorf("pull request %q: %w", prURL, ports.ErrNotMergeable)
	}

	pm := pullPath.FindStringSubmatch(pu.Path)
	if pu.Scheme != "https" || strings.ToLower(pu.Host) != c.web || pm == nil || pu.RawQuery != "" || pu.Fragment != "" {
		return pull{}, fmt.Errorf("%q is not a pull request URL: %w", prURL, ports.ErrNotMergeable)
	}

	if !strings.EqualFold(pm[1], rm[1]) || !strings.EqualFold(pm[2], rm[2]) {
		return pull{}, fmt.Errorf("%s is not on the factory's repository %s/%s: %w", prURL, rm[1], rm[2], ports.ErrNotMergeable)
	}

	return pull{owner: rm[1], repo: rm[2], number: pm[3]}, nil
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
func (c *Client) PullRequest(ctx context.Context, repo domain.Repository, prURL string) (ports.PullRequest, error) {
	p, err := c.parse(repo, prURL)
	if err != nil {
		return ports.PullRequest{}, err
	}

	var pj pullJSON

	status, _, err := c.call(ctx, repo, http.MethodGet, p.path(""), nil, &pj)

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
func (c *Client) Merge(ctx context.Context, repo domain.Repository, prURL string, m ports.Merge) (string, error) {
	p, err := c.parse(repo, prURL)
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

	status, msg, err := c.call(ctx, repo, http.MethodPut, p.path("/merge"), map[string]string{
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
func (c *Client) DeleteBranch(ctx context.Context, repo domain.Repository, branch string) error {
	ru, err := url.Parse(repo.URL)
	if err != nil {
		return fmt.Errorf("repository URL: %w", err)
	}

	rm := repoPath.FindStringSubmatch(ru.Path)
	if rm == nil || strings.ToLower(ru.Host) != c.web {
		return fmt.Errorf("repository %s is not a %s repository", repo.URL, c.web)
	}

	if branch == "" || strings.Contains(branch, "..") {
		return fmt.Errorf("delete branch %q: not a branch name", branch)
	}

	segments := strings.Split(branch, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}

	path := "/repos/" + url.PathEscape(rm[1]) + "/" + url.PathEscape(rm[2]) + "/git/refs/heads/" + strings.Join(segments, "/")

	status, msg, err := c.call(ctx, repo, http.MethodDelete, path, nil, nil)

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

// call sends one API request with the repository's token and decodes a successful JSON answer
// into out (when out is not nil). It returns the status and GitHub's message, if any; err is
// only for requests that got no answer or an unreadable one. The token is scrubbed from the
// message and from errors.
func (c *Client) call(ctx context.Context, repo domain.Repository, method, path string, body, out any) (int, string, error) {
	if repo.AuthRef == "" {
		return 0, "", errors.New("the repository has no token (runtime.repository.token)")
	}

	token, err := c.secrets.Resolve(ctx, repo.AuthRef)
	if err != nil {
		return 0, "", fmt.Errorf("repository token: %w", err)
	}

	status, msg, err := c.send(ctx, token, method, path, body, out)

	scrub := func(s string) string {
		if token == "" {
			return s
		}

		return strings.ReplaceAll(s, token, "[redacted]")
	}

	if err != nil {
		err = errors.New(scrub(err.Error()))
	}

	return status, scrub(msg), err
}

func (c *Client) send(ctx context.Context, token, method, path string, body, out any) (int, string, error) {
	var reader io.Reader

	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, "", err
		}

		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.api+path, reader)
	if err != nil {
		return 0, "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "goald")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
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
