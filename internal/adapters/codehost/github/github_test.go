package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	token   = "ghp_test-token-never-shown" //nolint:gosec // a made-up token for the fake GitHub
	prURL   = "https://github.com/acme/app/pull/7"
	headSHA = "4b825dc6"
	squash  = "squash"
	message = "message"
	merged  = "merged"
	closed  = "closed"
	// mergeSHA is the fake's merge commit.
	mergeSHA = "merge-sha"

	headBranch = "goal/7-add-x"
	mainBranch = "main"
	sharedFile = "docs/shared.md"
	keySHA     = "sha"
)

// connection is a connection of the platform's github kind with values (token and host).
func connection(t *testing.T, values map[string]string) domain.ResolvedConnection {
	t.Helper()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == "github" })
	if i < 0 {
		t.Fatal("the platform has no github kind")
	}

	return domain.ResolvedConnection{Connection: domain.Connection{Name: "github", Kind: "github"}, Kind: kinds[i], Values: values}
}

// dotComConn is a github.com connection with the token.
func dotComConn(t *testing.T) domain.ResolvedConnection {
	t.Helper()

	return connection(t, map[string]string{"token": token})
}

// fakeGitHub is a GitHub REST API with one pull request, acme/app#7, that requires the token.
type fakeGitHub struct {
	mu       sync.Mutex
	pr       map[string]any
	merge    func(body map[string]string) (int, any)
	update   func(body map[string]string) (int, any)
	requests []string
	deleted  []string
	// mergeable, when not empty, is what successive reads of the pull request answer for
	// mergeable, the last one from then on.
	mergeable []any
	// heads are acme/app's branches and their head SHAs; main is its default branch.
	heads map[string]string
	// history is acme/app's linear history, oldest first.
	history []fakeCommit
}

// fakeCommit is one commit of the fake's history.
type fakeCommit struct {
	sha, message string
	files        []string
}

// fastPolls re-reads a pull request whose mergeability is unknown three times, at once.
var fastPolls = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *Client) {
	t.Helper()

	return newFakeGitHubWith(t, Config{})
}

// newFakeGitHubWith is newFakeGitHub with cfg, its APIURL the fake's.
func newFakeGitHubWith(t *testing.T, cfg Config) (*fakeGitHub, *Client) {
	t.Helper()

	f := &fakeGitHub{pr: map[string]any{
		"state": "open", merged: false, "mergeable": true, "merge_commit_sha": "test-merge-sha",
		"head": map[string]any{keySHA: headSHA, "ref": headBranch, "repo": map[string]any{"full_name": "acme/app"}},
		"base": map[string]any{"ref": mainBranch, "repo": map[string]any{"full_name": "acme/app"}},
	}}
	f.merge = func(map[string]string) (int, any) {
		f.pr["state"], f.pr[merged] = closed, true

		return http.StatusOK, map[string]any{keySHA: mergeSHA, "merged": true, message: "Pull Request successfully merged"}
	}

	f.update = func(map[string]string) (int, any) {
		return http.StatusAccepted, map[string]any{message: "Updating pull request branch.", "url": "https://github.com/acme/app/pull/7"}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/app/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		if len(f.mergeable) > 0 {
			f.pr["mergeable"] = f.mergeable[0]
			if len(f.mergeable) > 1 {
				f.mergeable = f.mergeable[1:]
			}
		}
		f.mu.Unlock()

		f.reply(w, http.StatusOK, f.pr)
	})
	mux.HandleFunc("PUT /repos/acme/app/pulls/7/merge", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}

		f.mu.Lock()
		status, res := f.merge(body)
		f.mu.Unlock()

		f.reply(w, status, res)
	})
	mux.HandleFunc("PUT /repos/acme/app/pulls/7/update-branch", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}

		f.mu.Lock()
		status, res := f.update(body)
		f.mu.Unlock()

		f.reply(w, status, res)
	})
	mux.HandleFunc("GET /repos/acme/app", func(w http.ResponseWriter, _ *http.Request) {
		f.reply(w, http.StatusOK, map[string]any{"full_name": "acme/app", "default_branch": mainBranch})
	})
	mux.HandleFunc("GET /repos/acme/app/branches/{branch...}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		sha, ok := f.heads[r.PathValue("branch")]
		f.mu.Unlock()

		if !ok {
			f.reply(w, http.StatusNotFound, map[string]any{message: "Branch not found"})
			return
		}

		f.reply(w, http.StatusOK, map[string]any{"name": r.PathValue("branch"), "commit": map[string]any{keySHA: sha}})
	})
	mux.HandleFunc("GET /repos/acme/app/compare/{spec}", func(w http.ResponseWriter, r *http.Request) {
		status, res := f.compare(r)
		f.reply(w, status, res)
	})
	mux.HandleFunc("GET /repos/acme/app/commits/{sha}", func(w http.ResponseWriter, r *http.Request) {
		i := f.index(r.PathValue("sha"))
		if i < 0 {
			f.reply(w, http.StatusNotFound, map[string]any{message: "No commit found for SHA: " + r.PathValue("sha")})
			return
		}

		f.mu.Lock()
		fc := f.history[i]
		f.mu.Unlock()

		files := []map[string]any{}
		for _, name := range fc.files {
			files = append(files, map[string]any{"filename": name, "status": "modified", "patch": "@@ -1 +1 @@"})
		}

		f.reply(w, http.StatusOK, map[string]any{keySHA: fc.sha, "commit": map[string]any{message: fc.message}, "files": files})
	})
	mux.HandleFunc("DELETE /repos/acme/app/git/refs/heads/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.deleted = append(f.deleted, strings.TrimPrefix(r.URL.EscapedPath(), "/repos/acme/app/git/refs/heads/"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-GitHub-Api-Version") == "" {
			f.reply(w, http.StatusUnauthorized, map[string]any{message: "Bad credentials"})
			return
		}

		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	cfg.APIURL = srv.URL

	return f, New(cfg)
}

func (f *fakeGitHub) reply(w http.ResponseWriter, status int, body any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// index is the position of sha in the fake's history, or -1.
func (f *fakeGitHub) index(sha string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.IndexFunc(f.history, func(c fakeCommit) bool { return c.sha == sha })
}

// compare answers GET compare/{base}...{head} as GitHub does: the commits after base up to head,
// oldest first, paged by per_page and page, with their total and the files of all of them.
func (f *fakeGitHub) compare(r *http.Request) (int, any) {
	base, head, ok := strings.Cut(r.PathValue("spec"), "...")
	from, to := f.index(base), f.index(head)

	if !ok || from < 0 || to < 0 {
		return http.StatusNotFound, map[string]any{message: "Not Found"}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var all []fakeCommit
	if to > from {
		all = f.history[from+1 : to+1]
	}

	perPage, page := 250, 1
	if v := r.URL.Query().Get("per_page"); v != "" {
		perPage, _ = strconv.Atoi(v)
	}

	if v := r.URL.Query().Get("page"); v != "" {
		page, _ = strconv.Atoi(v)
	}

	commits := []map[string]any{}
	files := []map[string]any{}

	for i, fc := range all {
		if i >= (page-1)*perPage && i < page*perPage {
			commits = append(commits, map[string]any{keySHA: fc.sha, "commit": map[string]any{message: fc.message}})
		}

		for _, name := range fc.files {
			files = append(files, map[string]any{"filename": name, "patch": strings.Repeat("+", 4096)})
		}
	}

	return http.StatusOK, map[string]any{
		"status": "ahead", "ahead_by": len(all), "behind_by": 0, "total_commits": len(all), "commits": commits, "files": files,
	}
}

func (f *fakeGitHub) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.requests)
}

func TestClient_APIForTheConnectionsHost(t *testing.T) {
	t.Parallel()

	c := New(Config{})

	for host, want := range map[string]string{
		"github.com":      "https://api.github.com",
		"acme.ghe.com":    "https://api.acme.ghe.com",
		"ghe.example.com": "https://ghe.example.com/api/v3",
	} {
		if got := c.apiFor(host); got != want {
			t.Errorf("API of %s = %s; want %s", host, got, want)
		}
	}

	if got := New(Config{APIURL: "http://127.0.0.1:9/"}).apiFor("ghe.example.com"); got != "http://127.0.0.1:9" {
		t.Errorf("API with an override = %s; want the override", got)
	}
}

func TestPullRequest_ReadsStateHeadAndBase(t *testing.T) {
	t.Parallel()

	_, c := newFakeGitHub(t)

	pr, err := c.PullRequest(t.Context(), dotComConn(t), prURL)
	if err != nil {
		t.Fatal(err)
	}

	want := ports.PullRequest{
		URL: prURL, Open: true, Mergeable: true, HeadSHA: headSHA, HeadBranch: headBranch, SameRepository: true, BaseBranch: mainBranch,
	}
	if pr != want {
		t.Errorf("pull request = %+v; want %+v", pr, want)
	}
}

// GitHub answers mergeable null while it computes it: the client reads the pull request again
// until it knows, and says so when it never does. A known answer, or a pull request no longer
// open (merged and closed ones always answer null), is not read again.
func TestPullRequest_WaitsForMergeability(t *testing.T) {
	t.Parallel()

	const get = "GET /repos/acme/app/pulls/7"

	tests := []struct {
		name          string
		pr            map[string]any // fields replacing the fake's
		mergeable     []any
		wantMergeable bool
		wantPending   bool
		wantGets      int
	}{
		{name: "null then true", mergeable: []any{nil, nil, true}, wantMergeable: true, wantGets: 3},
		{name: "always null", mergeable: []any{nil}, wantPending: true, wantGets: 1 + len(fastPolls)},
		{name: "false", mergeable: []any{false, true}, wantGets: 1},
		{name: merged, pr: map[string]any{"state": closed, merged: true}, mergeable: []any{nil, true}, wantGets: 1},
		{name: closed, pr: map[string]any{"state": closed}, mergeable: []any{nil, true}, wantGets: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, c := newFakeGitHubWith(t, Config{MergeabilityPolls: fastPolls})
			f.mergeable = tt.mergeable
			maps.Copy(f.pr, tt.pr)

			pr, err := c.PullRequest(t.Context(), dotComConn(t), prURL)
			if err != nil {
				t.Fatal(err)
			}

			if pr.Mergeable != tt.wantMergeable || pr.MergeabilityPending != tt.wantPending {
				t.Errorf("mergeable %v, pending %v; want %v, %v", pr.Mergeable, pr.MergeabilityPending, tt.wantMergeable, tt.wantPending)
			}

			if got := f.sent(); len(got) != tt.wantGets || slices.ContainsFunc(got, func(r string) bool { return r != get }) {
				t.Errorf("requests %v; want %d reads of the pull request", got, tt.wantGets)
			}
		})
	}
}

// The wait ends with the context, with its error.
func TestPullRequest_WaitStopsWithTheContext(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHubWith(t, Config{MergeabilityPolls: []time.Duration{time.Hour}})
	f.mergeable = []any{nil}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	// The deadline may also end the first read, whose transport error does not wrap it.
	if _, err := c.PullRequest(ctx, dotComConn(t), prURL); err == nil || ctx.Err() == nil {
		t.Errorf("PullRequest = %v, context %v; want an error once the context ended", err, ctx.Err())
	} else if got := f.sent(); len(got) > 1 {
		t.Errorf("requests %v; want no read after the context ended", got)
	}
}

// The repository is the URL's: any repository on the connection's host, which the token's own
// scope limits.
func TestPullRequest_TheRepositoryIsTheURLs(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	if _, err := c.PullRequest(t.Context(), dotComConn(t), "https://github.com/acme/docs/pull/2"); !errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("PullRequest of a missing pull request = %v; want ErrNotMergeable", err)
	}

	if got := f.sent(); !slices.Equal(got, []string{"GET /repos/acme/docs/pulls/2"}) {
		t.Errorf("requests %v; want acme/docs#2 read", got)
	}
}

// A pull request on another host than the connection's, or a URL that is not a pull request's,
// is refused before anything is sent.
func TestPullRequest_OnlyOnTheConnectionsHost(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	ghe := connection(t, map[string]string{"token": token, "host": "ghe.example.com"})

	for _, tt := range []struct {
		conn domain.ResolvedConnection
		url  string
	}{
		{dotComConn(t), "https://code.example.com/acme/app/pull/7"},
		{dotComConn(t), "https://gitlab.com/acme/app/-/merge_requests/7"},
		{ghe, prURL},
		{dotComConn(t), "http://github.com/acme/app/pull/7"},
		{dotComConn(t), "https://user@github.com/acme/app/pull/7"},
		{dotComConn(t), "https://github.com/acme/app/pull/7?x=1"},
		{dotComConn(t), "https://github.com/acme/app/issues/7"},
	} {
		if _, err := c.PullRequest(t.Context(), tt.conn, tt.url); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("PullRequest(%s) = %v; want ErrNotMergeable", tt.url, err)
		}

		if err := c.DeleteBranch(t.Context(), tt.conn, tt.url, headBranch); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("DeleteBranch(%s) = %v; want ErrNotMergeable", tt.url, err)
		}

		if err := c.UpdateBranch(t.Context(), tt.conn, tt.url, headSHA); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("UpdateBranch(%s) = %v; want ErrNotMergeable", tt.url, err)
		}
	}

	// A connection without a token has no git login: nothing is sent either.
	if _, err := c.PullRequest(t.Context(), connection(t, map[string]string{}), prURL); err == nil {
		t.Error("PullRequest with a connection without a token succeeded; want an error")
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}

func TestMerge_SquashesTheHeadItWasGiven(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	var asked map[string]string

	f.merge = func(body map[string]string) (int, any) {
		asked = body

		return http.StatusOK, map[string]any{keySHA: mergeSHA, "merged": true}
	}

	sha, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
	if err != nil || sha != mergeSHA {
		t.Fatalf("Merge = %q, %v; want merge-sha", sha, err)
	}

	if asked["merge_method"] != squash || asked["sha"] != headSHA {
		t.Errorf("merge request %v; want squash of head 4b825dc6", asked)
	}
}

func TestMerge_GitHubRefusalsAreNotMergeable(t *testing.T) {
	t.Parallel()

	for _, refusal := range []struct {
		status int
		msg    string
	}{
		{http.StatusMethodNotAllowed, "Pull Request is not mergeable"},
		{http.StatusUnprocessableEntity, headModified},
		{http.StatusConflict, "Merge conflict"},
	} {
		f, c := newFakeGitHubWith(t, Config{MergeRetries: fastPolls})
		f.merge = func(map[string]string) (int, any) {
			return refusal.status, map[string]any{message: refusal.msg}
		}

		_, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
		if !errors.Is(err, ports.ErrNotMergeable) || !strings.Contains(err.Error(), refusal.msg) {
			t.Errorf("merge answered %d %q: err = %v; want ErrNotMergeable with GitHub's reason", refusal.status, refusal.msg, err)
		}

		if got := mergeRequests(f); got != 1 {
			t.Errorf("merge answered %d %q: %d merge requests; want 1, refused at once", refusal.status, refusal.msg, got)
		}
	}
}

// headModified is GitHub's 409 answer to a merge at a head that is no longer the branch's.
const headModified = "Head branch was modified. Review and try the merge again."

// mergeRequests counts the merge requests f received.
func mergeRequests(f *fakeGitHub) int {
	n := 0

	for _, r := range f.sent() {
		if r == "PUT /repos/acme/app/pulls/7/merge" {
			n++
		}
	}

	return n
}

func TestMerge_RetriesAtTheNewHeadWhenGitHubSaysTheHeadWasModified(t *testing.T) {
	t.Parallel()

	const newHead = "9c1f2e7a"

	f, c := newFakeGitHubWith(t, Config{MergeRetries: fastPolls})

	var asked []string

	f.merge = func(body map[string]string) (int, any) {
		asked = append(asked, body["sha"])

		if body["sha"] != newHead {
			// GitHub's background update has pushed newHead since the caller read the head.
			head, _ := f.pr["head"].(map[string]any)
			head[keySHA] = newHead

			return http.StatusConflict, map[string]any{message: headModified}
		}

		f.pr["state"], f.pr[merged] = closed, true

		return http.StatusOK, map[string]any{keySHA: mergeSHA, "merged": true}
	}

	sha, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
	if err != nil || sha != mergeSHA {
		t.Fatalf("Merge = %q, %v; want merge-sha", sha, err)
	}

	if !slices.Equal(asked, []string{headSHA, newHead}) {
		t.Errorf("merge requests at %v; want %v", asked, []string{headSHA, newHead})
	}
}

func TestMerge_HeadModifiedRetriesEndNotMergeable(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHubWith(t, Config{MergeRetries: fastPolls})
	f.merge = func(map[string]string) (int, any) {
		return http.StatusConflict, map[string]any{message: headModified}
	}

	_, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
	if !errors.Is(err, ports.ErrNotMergeable) || errors.Is(err, ports.ErrMergeConflict) || !strings.Contains(err.Error(), "Head branch was modified") {
		t.Errorf("err = %v; want ErrNotMergeable, not a conflict, with GitHub's reason", err)
	}

	if got, want := mergeRequests(f), len(fastPolls)+1; got != want {
		t.Errorf("%d merge requests; want %d", got, want)
	}

	// A cancelled context stops the waiting.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	f, c = newFakeGitHubWith(t, Config{MergeRetries: []time.Duration{time.Hour}, Transport: cancelAfter(cancel)})
	f.merge = func(map[string]string) (int, any) {
		return http.StatusConflict, map[string]any{message: headModified}
	}

	if _, err := c.Merge(ctx, dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: err = %v; want context.Canceled", err)
	}
}

// cancelAfter is a transport that calls cancel once it has read a whole answer.
func cancelAfter(cancel context.CancelFunc) http.RoundTripper {
	return roundTripper(func(r *http.Request) (*http.Response, error) {
		res, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()

		res.Body = io.NopCloser(bytes.NewReader(body))

		cancel()

		return res, err
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMerge_HeadModifiedThenMergedIsNotMergedTwice(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHubWith(t, Config{MergeRetries: fastPolls})
	f.merge = func(map[string]string) (int, any) {
		// Someone else merged it meanwhile.
		f.pr["state"], f.pr[merged], f.pr["merge_commit_sha"] = closed, true, "their-merge-sha"

		return http.StatusConflict, map[string]any{message: headModified}
	}

	sha, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
	if err != nil || sha != "their-merge-sha" {
		t.Errorf("Merge = %q, %v; want their-merge-sha", sha, err)
	}

	if got := mergeRequests(f); got != 1 {
		t.Errorf("%d merge requests; want 1", got)
	}

	f, c = newFakeGitHubWith(t, Config{MergeRetries: fastPolls})
	f.merge = func(map[string]string) (int, any) {
		f.pr["state"] = closed

		return http.StatusConflict, map[string]any{message: headModified}
	}

	if _, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA}); !errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("closed: err = %v; want ErrNotMergeable", err)
	}

	if got := mergeRequests(f); got != 1 {
		t.Errorf("closed: %d merge requests; want 1", got)
	}
}

func TestUpdateBranch_SendsTheExpectedHead(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	var asked map[string]string

	f.update = func(body map[string]string) (int, any) {
		asked = body

		return http.StatusAccepted, map[string]any{message: "Updating pull request branch."}
	}

	if err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA); err != nil {
		t.Fatalf("UpdateBranch = %v; want accepted", err)
	}

	if len(asked) != 1 || asked["expected_head_sha"] != headSHA {
		t.Errorf("update request %v; want only expected_head_sha 4b825dc6", asked)
	}

	// Without a head to expect, GitHub would update whatever the branch holds: nothing is sent.
	before := len(f.sent())
	if err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, ""); err == nil || len(f.sent()) != before {
		t.Errorf("UpdateBranch without a head = %v, sent %v; want refused before sending", err, f.sent()[before:])
	}
}

func TestUpdateBranch_ConflictIsMergeConflict(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.update = func(map[string]string) (int, any) {
		return http.StatusUnprocessableEntity, map[string]any{message: "merge conflict between base and head"}
	}

	err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA)

	var mc *ports.MergeConflictError
	if !errors.Is(err, ports.ErrMergeConflict) || !errors.As(err, &mc) || len(mc.Files) != 0 || errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("UpdateBranch on a conflict = %v; want a *MergeConflictError without files, not ErrNotMergeable", err)
	}
}

func TestUpdateBranch_UpToDateIsNotAConflict(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.pr["mergeable_state"] = "clean"
	f.update = func(map[string]string) (int, any) {
		// GitHub's answer when the head branch already holds every commit of the base.
		return http.StatusUnprocessableEntity, map[string]any{message: "There are no new commits on the base branch."}
	}

	if err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA); err != nil {
		t.Errorf("UpdateBranch on an up-to-date branch = %v; want nil", err)
	}
}

func TestUpdateBranch_DirtyIsMergeConflictWithoutUpdating(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.pr["mergeable"], f.pr["mergeable_state"] = false, "dirty"

	updated := false
	f.update = func(map[string]string) (int, any) {
		updated = true

		return http.StatusAccepted, map[string]any{message: "Updating pull request branch."}
	}

	err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA)

	var mc *ports.MergeConflictError
	if !errors.As(err, &mc) || errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("UpdateBranch on a dirty pull request = %v; want a *MergeConflictError", err)
	}

	if updated {
		t.Error("UpdateBranch asked GitHub to update a dirty branch; want no update-branch call")
	}
}

func TestUpdateBranch_UnexplainedRefusalIsNotMergeable(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.update = func(map[string]string) (int, any) {
		return http.StatusUnprocessableEntity, map[string]any{message: "Validation Failed"}
	}

	err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA)
	if !errors.Is(err, ports.ErrNotMergeable) || errors.Is(err, ports.ErrMergeConflict) {
		t.Errorf("UpdateBranch on an unexplained 422 = %v; want ErrNotMergeable, not a conflict", err)
	}
}

func TestUpdateBranch_MovedHeadIsNotMergeable(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.update = func(map[string]string) (int, any) {
		// GitHub's answer when expected_head_sha is not the branch's head.
		return http.StatusUnprocessableEntity, map[string]any{message: "expected head sha didn't match current head ref."}
	}

	err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA)
	if !errors.Is(err, ports.ErrNotMergeable) || errors.Is(err, ports.ErrMergeConflict) {
		t.Errorf("UpdateBranch on a moved head = %v; want ErrNotMergeable, not a conflict", err)
	}
}

func TestDeleteBranch_DeletesTheRefAndToleratesAGoneOne(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	if err := c.DeleteBranch(t.Context(), dotComConn(t), prURL, headBranch); err != nil {
		t.Fatal(err)
	}

	if len(f.deleted) != 1 || f.deleted[0] != headBranch {
		t.Errorf("deleted %v; want goal/7-add-x", f.deleted)
	}

	if err := c.DeleteBranch(t.Context(), dotComConn(t), prURL, "../main"); err == nil {
		t.Error("deleting ../main succeeded; want refused")
	}
}

const repoURL = "https://github.com/acme/app"

// seedHistory gives the fake acme/app n commits, c1 to cn, each changing its own file and
// docs/shared.md, with main at the last one.
func (f *fakeGitHub) seedHistory(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.history = nil
	for i := 1; i <= n; i++ {
		f.history = append(f.history, fakeCommit{
			sha:     "c" + strconv.Itoa(i),
			message: "change " + strconv.Itoa(i) + "\n\nThe body, not the subject.",
			files:   []string{"internal/f" + strconv.Itoa(i) + ".go", sharedFile},
		})
	}

	f.heads = map[string]string{mainBranch: "c" + strconv.Itoa(n), headBranch: headSHA}
}

func TestBranchHead_NamedAndDefaultBranch(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.seedHistory(3)

	for _, tt := range []struct{ repo, branch, want string }{
		{repoURL, "", "c3"},
		{repoURL + ".git", "", "c3"},
		{repoURL + "/", mainBranch, "c3"},
		{repoURL, headBranch, headSHA},
	} {
		got, err := c.BranchHead(t.Context(), dotComConn(t), tt.repo, tt.branch)
		if err != nil || got != tt.want {
			t.Errorf("BranchHead(%s, %q) = %q, %v; want %q", tt.repo, tt.branch, got, err, tt.want)
		}
	}

	if got := f.sent(); !slices.Contains(got, "GET /repos/acme/app") || !slices.Contains(got, "GET /repos/acme/app/branches/goal/7-add-x") {
		t.Errorf("requests %v; want the repository read for its default branch and the named branch read", got)
	}

	for _, tt := range []struct{ repo, branch string }{
		{repoURL, "gone"},
		{"https://github.com/acme/other", ""},
	} {
		if _, err := c.BranchHead(t.Context(), dotComConn(t), tt.repo, tt.branch); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("BranchHead(%s, %q) = %v; want ErrNotFound", tt.repo, tt.branch, err)
		}
	}
}

// A repository on another host than the connection's, or a URL that is not a repository's, is
// refused before anything is sent.
func TestBranchHead_OnlyOnTheConnectionsHost(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	ghe := connection(t, map[string]string{"token": token, "host": "ghe.example.com"})

	for _, tt := range []struct {
		conn domain.ResolvedConnection
		repo string
	}{
		{dotComConn(t), "https://code.example.com/acme/app"},
		{ghe, repoURL},
		{dotComConn(t), "http://github.com/acme/app"},
		{dotComConn(t), "https://user@github.com/acme/app"},
		{dotComConn(t), "https://github.com/acme/app?x=1"},
		{dotComConn(t), "https://github.com/acme/app#x"},
		{dotComConn(t), "https://github.com/acme"},
		{dotComConn(t), prURL},
	} {
		if _, err := c.BranchHead(t.Context(), tt.conn, tt.repo, ""); err == nil {
			t.Errorf("BranchHead(%s) succeeded; want refused", tt.repo)
		}

		if _, err := c.Compare(t.Context(), tt.conn, tt.repo, "c1", "c2"); err == nil {
			t.Errorf("Compare(%s) succeeded; want refused", tt.repo)
		}
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}

func TestCompare_CommitsOldestFirstWithFiles(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.seedHistory(4)

	got, err := c.Compare(t.Context(), dotComConn(t), repoURL, "c1", "c4")
	if err != nil {
		t.Fatal(err)
	}

	want := ports.Comparison{Commits: []ports.Commit{
		{SHA: "c2", Subject: "change 2", Files: []string{"internal/f2.go", sharedFile}},
		{SHA: "c3", Subject: "change 3", Files: []string{"internal/f3.go", sharedFile}},
		{SHA: "c4", Subject: "change 4", Files: []string{"internal/f4.go", sharedFile}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Compare = %+v; want %+v", got, want)
	}

	if sent := f.sent(); !slices.Contains(sent, "GET /repos/acme/app/compare/c1...c4") {
		t.Errorf("requests %v; want the compare API asked", sent)
	}

	// Nothing since: no commits, not truncated.
	if got, err := c.Compare(t.Context(), dotComConn(t), repoURL, "c4", "c4"); err != nil || len(got.Commits) != 0 || got.Truncated {
		t.Errorf("Compare(c4, c4) = %+v, %v; want no commits", got, err)
	}
}

func TestCompare_UnknownCommitIsNotFound(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.seedHistory(2)

	for _, tt := range []struct{ repo, base, head string }{
		{repoURL, "gone", "c2"},
		{repoURL, "c1", "gone"},
		{"https://github.com/acme/other", "c1", "c2"},
	} {
		if _, err := c.Compare(t.Context(), dotComConn(t), tt.repo, tt.base, tt.head); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("Compare(%s, %s, %s) = %v; want ErrNotFound", tt.repo, tt.base, tt.head, err)
		}
	}

	for _, tt := range []struct{ base, head string }{{"", "c2"}, {"c1", ""}, {"c1..c0", "c2"}} {
		if _, err := c.Compare(t.Context(), dotComConn(t), repoURL, tt.base, tt.head); err == nil {
			t.Errorf("Compare(%q, %q) succeeded; want refused", tt.base, tt.head)
		}
	}
}

// Past 100 commits, Compare returns the oldest 100 and says it truncated. The compare answer
// carries every file's patch, far over the 1 MiB other answers are held to.
func TestCompare_TruncatesPast100(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.seedHistory(ports.MaxCompareCommits + 30)

	got, err := c.Compare(t.Context(), dotComConn(t), repoURL, "c1", "c"+strconv.Itoa(ports.MaxCompareCommits+30))
	if err != nil {
		t.Fatal(err)
	}

	if !got.Truncated || len(got.Commits) != ports.MaxCompareCommits {
		t.Fatalf("Compare = %d commits, truncated %v; want %d and truncated", len(got.Commits), got.Truncated, ports.MaxCompareCommits)
	}

	if first, last := got.Commits[0].SHA, got.Commits[len(got.Commits)-1].SHA; first != "c2" || last != "c101" {
		t.Errorf("commits %s to %s; want c2 to c101, the oldest", first, last)
	}

	if sent := f.sent(); !slices.Contains(sent, "GET /repos/acme/app/compare/c1...c130") || len(sent) != 1+ports.MaxCompareCommits {
		t.Errorf("%d requests, first %v; want the compare API asked, then each of the 100 commits", len(sent), sent[:1])
	}

	// Exactly 100 is not truncated.
	if got, err := c.Compare(t.Context(), dotComConn(t), repoURL, "c1", "c101"); err != nil || got.Truncated || len(got.Commits) != 100 {
		t.Errorf("Compare(c1, c101) = %d commits, truncated %v, %v; want 100, not truncated", len(got.Commits), got.Truncated, err)
	}
}

func TestClient_TokenNeverInErrorsNorSentElsewhere(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	// GitHub echoing the credential back must not put it in an error.
	f.merge = func(map[string]string) (int, any) {
		return http.StatusInternalServerError, map[string]any{message: "bad token " + token}
	}

	if _, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA}); err == nil ||
		strings.Contains(err.Error(), token) {
		t.Errorf("err = %v; want an error without the token", err)
	}

	f.update = func(map[string]string) (int, any) {
		return http.StatusUnprocessableEntity, map[string]any{message: "bad token " + token}
	}

	if err := c.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("UpdateBranch err = %v; want an error without the token", err)
	}

	// A redirect is not followed: the token would travel to another host.
	var leaked bool

	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = leaked || r.Header.Get("Authorization") != ""
	}))
	t.Cleanup(elsewhere.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // the redirect under test
	}))
	t.Cleanup(redirect.Close)

	moved := New(Config{APIURL: redirect.URL})

	for name, call := range map[string]func() error{
		"PullRequest": func() error {
			_, err := moved.PullRequest(t.Context(), dotComConn(t), prURL)
			return err
		},
		"UpdateBranch": func() error { return moved.UpdateBranch(t.Context(), dotComConn(t), prURL, headSHA) },
		"BranchHead": func() error {
			_, err := moved.BranchHead(t.Context(), dotComConn(t), repoURL, "")
			return err
		},
		"Compare": func() error {
			_, err := moved.Compare(t.Context(), dotComConn(t), repoURL, "c1", "c2")
			return err
		},
	} {
		if err := call(); err == nil || leaked {
			t.Errorf("%s via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", name, err, leaked)
		}
	}
}
