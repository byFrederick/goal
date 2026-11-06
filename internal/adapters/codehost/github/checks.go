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
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	// maxCheckRunPages caps the pages of check runs Checks reads: 1,000 check runs on one commit.
	maxCheckRunPages = 10
	// ciPermission is what a token that cannot see a repository's CI on GitHub needs.
	ciPermission = "read access to Checks and Commit statuses (a fine-grained token needs both permissions)"
	// defaultRateLimitWait is how long a rate-limited answer that names no time is waited out.
	defaultRateLimitWait = time.Minute
	// runCompleted is the status of a check run that has finished.
	runCompleted = "completed"
)

// commitSHA is a commit id as Checks accepts it.
var commitSHA = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// etagged is the last answer of a URL with its ETag, for a conditional request. It keeps the
// answer's Link too, since a 304 need not repeat it and the next page is read from it.
type etagged struct {
	etag string
	body []byte
	link string
}

type checkRunsJSON struct {
	CheckRuns []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"check_runs"`
}

type combinedStatusJSON struct {
	// State is not read: GitHub answers pending for a commit with no statuses at all.
	Statuses []struct {
		Context   string `json:"context"`
		State     string `json:"state"`
		TargetURL string `json:"target_url"`
	} `json:"statuses"`
}

// Checks implements ports.CodeHost with the commit's check runs (GET
// /repos/{o}/{r}/commits/{sha}/check-runs?filter=latest, every page up to maxCheckRunPages) and
// then its commit statuses (the statuses list of GET /repos/{o}/{r}/commits/{sha}/status, never
// its combined state). With url, it first reads the pull request and uses its head. Each read is
// conditional: a 304 gives the URL's last answer again, which GitHub does not count against the
// rate limit. A 403 is a *ports.CINotReadableError; a 429, or a 403 with no requests left, is a
// *ports.RateLimitedError. FailedJobs stays empty.
func (cl *Client) Checks(ctx context.Context, repo domain.CodeRepository, token, sha, prURL string) (ports.CommitChecks, error) {
	p, head, err := cl.checksHead(ctx, repo, token, sha, prURL)
	if err != nil {
		return ports.CommitChecks{}, err
	}

	commit := p.repoPath("/commits/" + url.PathEscape(head))
	out := ports.CommitChecks{Head: head}

	for page := 1; ; page++ {
		if page > maxCheckRunPages {
			return ports.CommitChecks{}, fmt.Errorf("checks of %s at %s: more than %d pages of check runs", repo, head, maxCheckRunPages)
		}

		var runs checkRunsJSON

		next, err := cl.getCI(ctx, p, commit+"/check-runs?filter=latest&per_page=100&page="+strconv.Itoa(page), &runs)
		if err != nil {
			return ports.CommitChecks{}, fmt.Errorf("check runs of %s at %s: %w", repo, head, err)
		}

		for _, r := range runs.CheckRuns {
			out.Checks = append(out.Checks, checkRunCheck(r.Name, r.Status, r.Conclusion, r.HTMLURL))
		}

		if !next {
			break
		}
	}

	var status combinedStatusJSON

	if _, err := cl.getCI(ctx, p, commit+"/status?per_page=100", &status); err != nil {
		return ports.CommitChecks{}, fmt.Errorf("commit statuses of %s at %s: %w", repo, head, err)
	}

	for _, s := range status.Statuses {
		out.Checks = append(out.Checks, statusCheck(s.Context, s.State, s.TargetURL))
	}

	return out, nil
}

// checksHead checks Checks' arguments before anything is sent and names the commit to read:
// sha, or the current head of the pull request prURL when it is set.
func (cl *Client) checksHead(ctx context.Context, repo domain.CodeRepository, token, sha, prURL string) (pull, string, error) {
	if prURL == "" {
		p, err := cl.check(repo, token)
		if err != nil {
			return pull{}, "", fmt.Errorf("checks: %w", err)
		}

		if !commitSHA.MatchString(sha) {
			return pull{}, "", fmt.Errorf("checks of %s: %q is not a commit: %w", repo, sha, domain.ErrInvalid)
		}

		return p, sha, nil
	}

	p, err := cl.open(repo, token, prURL)
	if err != nil {
		return pull{}, "", fmt.Errorf("checks: %w", err)
	}

	var pj pullJSON

	if _, err := cl.getCached(ctx, p, p.repoPath("/pulls/"+p.number), &pj, false); err != nil {
		return pull{}, "", fmt.Errorf("checks of pull request %s: %w", prURL, err)
	}

	if !commitSHA.MatchString(pj.Head.SHA) {
		return pull{}, "", fmt.Errorf("checks of pull request %s: GitHub answered no head commit", prURL)
	}

	return p, pj.Head.SHA, nil
}

// checkRunCheck maps a check run (ADR 0044, decision 6). A status GitHub adds later is pending,
// and a conclusion it adds later is failed: neither passes.
func checkRunCheck(name, status, conclusion, link string) ports.Check {
	c := ports.Check{Name: name, URL: link}

	switch status {
	case "in_progress":
		c.State = ports.CheckRunning
	case runCompleted:
		c.State = ports.CheckFinished

		switch conclusion {
		case "success":
			c.Result = ports.CheckPassed
		case "neutral", "skipped", "stale":
			c.Result = ports.CheckNeutral
		default:
			c.Result = ports.CheckFailed
		}
	default:
		c.State = ports.CheckPending
	}

	return c
}

// statusCheck maps a commit status; a state GitHub adds later is pending.
func statusCheck(name, state, link string) ports.Check {
	c := ports.Check{Name: name, URL: link, State: ports.CheckFinished}

	switch state {
	case "success":
		c.Result = ports.CheckPassed
	case "failure", "error":
		c.Result = ports.CheckFailed
	default:
		c.State = ports.CheckPending
	}

	return c
}

// getCI is getCached for a CI read, where a 403 means the token cannot see CI.
func (cl *Client) getCI(ctx context.Context, p pull, path string, out any) (bool, error) {
	return cl.getCached(ctx, p, path, out, true)
}

// getCached reads path into out with a conditional request, and reports whether GitHub links a
// next page. A rate limit is a *ports.RateLimitedError, and with ci a 403 is a
// *ports.CINotReadableError; any other answer but 200 or 304 is an error.
func (cl *Client) getCached(ctx context.Context, p pull, path string, out any, ci bool) (bool, error) {
	status, body, header, err := cl.conditional(ctx, p, path)

	scrub := func(s string) string { return strings.ReplaceAll(s, p.token, "[redacted]") }

	switch {
	case err != nil:
		return false, errors.New(scrub(err.Error()))
	case rateLimited(status, header):
		return false, &ports.RateLimitedError{RetryAt: retryAt(header, time.Now())}
	case ci && status == http.StatusForbidden:
		return false, &ports.CINotReadableError{Permission: ciPermission}
	case status == http.StatusNotFound:
		return false, fmt.Errorf("GitHub answered 404: %w", ports.ErrNotFound)
	case status != http.StatusOK:
		msg, _ := messageOf(body)

		return false, fmt.Errorf("GitHub answered %d: %s", status, scrub(msg))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("decode answer: %w", err)
	}

	return strings.Contains(header.Get("Link"), `rel="next"`), nil
}

// conditional sends GET path with the ETag of its last answer, and answers a 304 with that
// answer's body and Link as a 200. It keeps each 200's ETag, body and Link by URL.
func (cl *Client) conditional(ctx context.Context, p pull, path string) (int, []byte, http.Header, error) {
	key := p.api + path

	cl.etagsMu.Lock()
	last, cached := cl.etags[key]
	cl.etagsMu.Unlock()

	req, err := newRequest(ctx, p, http.MethodGet, path, nil)
	if err != nil {
		return 0, nil, nil, err
	}

	if cached {
		req.Header.Set("If-None-Match", last.etag)
	}

	res, err := cl.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxListBytes))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("read answer: %w", err)
	}

	switch {
	case res.StatusCode == http.StatusNotModified && cached:
		header := res.Header.Clone()
		header.Del("Link")

		if last.link != "" {
			header.Set("Link", last.link)
		}

		return http.StatusOK, last.body, header, nil
	case res.StatusCode == http.StatusOK && len(bytes.TrimSpace(body)) == 0:
		return 0, nil, nil, errors.New("GitHub answered 200 with no body")
	case res.StatusCode == http.StatusOK:
		if etag := res.Header.Get("ETag"); etag != "" {
			cl.etagsMu.Lock()
			cl.etags[key] = etagged{etag: etag, body: body, link: res.Header.Get("Link")}
			cl.etagsMu.Unlock()
		}
	}

	return res.StatusCode, body, res.Header, nil
}

// rateLimited reports whether GitHub refused a request for its rate limit: a 429, or a 403 with
// no requests left.
func rateLimited(status int, header http.Header) bool {
	return status == http.StatusTooManyRequests ||
		(status == http.StatusForbidden && header.Get("X-Ratelimit-Remaining") == "0")
}

// retryAt is when a rate-limited request may be sent again: now plus Retry-After when it is set,
// else the x-ratelimit-reset epoch, else now plus defaultRateLimitWait.
func retryAt(header http.Header, now time.Time) time.Time {
	if s, err := strconv.Atoi(header.Get("Retry-After")); err == nil && s >= 0 {
		return now.Add(time.Duration(s) * time.Second)
	}

	if epoch, err := strconv.ParseInt(header.Get("X-Ratelimit-Reset"), 10, 64); err == nil && epoch > 0 {
		return time.Unix(epoch, 0)
	}

	return now.Add(defaultRateLimitWait)
}
