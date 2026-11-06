package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	keyName    = "full_name"
	// octo is the account the fake's token signs in as.
	octo = "octo"
	// gheHost is a GitHub Enterprise Server host.
	gheHost = "ghe.example.com"
	// acmeOwner owns the fake's repositories.
	acmeOwner = "acme"
	appName   = "app"
)

// gitHubDotCom is github.com, as the organization's list has it.
var gitHubDotCom = domain.BuiltInCodeHosts()[0]

// repoOf is the repository at raw, a repository's or a pull request's URL, on the built-in hosts
// or ghe.example.com, a GitHub Enterprise host.
func repoOf(t *testing.T, raw string) domain.CodeRepository {
	t.Helper()

	r, err := domain.ParseCodeRepository(domain.ListedCodeHosts([]domain.CodeHost{{Host: gheHost, Kind: domain.CodeHostGitHub}}), raw)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// appRepo is acme/app on github.com, the fake's repository.
func appRepo(t *testing.T) domain.CodeRepository {
	t.Helper()

	return repoOf(t, repoURL)
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
		"head": map[string]any{keySHA: headSHA, "ref": headBranch, "repo": map[string]any{keyName: "acme/app"}},
		"base": map[string]any{"ref": mainBranch, "repo": map[string]any{keyName: "acme/app"}},
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
		f.reply(w, http.StatusOK, map[string]any{keyName: "acme/app", "default_branch": mainBranch})
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
		if got := c.apiFor(domain.CodeHost{Host: host, Kind: domain.CodeHostGitHub}); got != want {
			t.Errorf("API of %s = %s; want %s", host, got, want)
		}
	}

	if got := New(Config{APIURL: "http://127.0.0.1:9/"}).apiFor(domain.CodeHost{Host: gheHost, Kind: domain.CodeHostGitHub}); got != "http://127.0.0.1:9" {
		t.Errorf("API with an override = %s; want the override", got)
	}
}

func TestPullRequest_ReadsStateHeadAndBase(t *testing.T) {
	t.Parallel()

	_, c := newFakeGitHub(t)

	pr, err := c.PullRequest(t.Context(), appRepo(t), token, prURL)
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

			pr, err := c.PullRequest(t.Context(), appRepo(t), token, prURL)
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
	if _, err := c.PullRequest(ctx, appRepo(t), token, prURL); err == nil || ctx.Err() == nil {
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

	if _, err := c.PullRequest(t.Context(), repoOf(t, "https://github.com/acme/docs/pull/2"), token, "https://github.com/acme/docs/pull/2"); !errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("PullRequest of a missing pull request = %v; want ErrNotMergeable", err)
	}

	if got := f.sent(); !slices.Equal(got, []string{"GET /repos/acme/docs/pulls/2"}) {
		t.Errorf("requests %v; want acme/docs#2 read", got)
	}
}

// A pull request URL outside the repository it is called with (another host, another repository,
// not a pull request's), a repository on a host of another kind, and an empty token are refused
// before anything is sent.
func TestPullRequest_OnlyOnTheConnectionsHost(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	ghe := repoOf(t, "https://"+gheHost+"/acme/app")
	onGitLab := domain.CodeRepository{Host: domain.BuiltInCodeHosts()[1], Owner: acmeOwner, Name: appName}

	for _, tt := range []struct {
		repo domain.CodeRepository
		url  string
	}{
		{appRepo(t), "https://code.example.com/acme/app/pull/7"},
		{appRepo(t), "https://gitlab.com/acme/app/-/merge_requests/7"},
		{ghe, prURL},
		{appRepo(t), "https://github.com/acme/docs/pull/7"},
		{appRepo(t), "https://github.com/octo/app/pull/7"},
		{appRepo(t), "http://github.com/acme/app/pull/7"},
		{appRepo(t), "https://user@github.com/acme/app/pull/7"},
		{appRepo(t), "https://github.com/acme/app/pull/7?x=1"},
		{appRepo(t), "https://github.com/acme/app/issues/7"},
		{onGitLab, "https://gitlab.com/acme/app/pull/7"},
		{domain.CodeRepository{}, prURL},
	} {
		if _, err := c.PullRequest(t.Context(), tt.repo, token, tt.url); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("PullRequest(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
		}

		if err := c.DeleteBranch(t.Context(), tt.repo, token, tt.url, headBranch); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("DeleteBranch(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
		}

		if err := c.UpdateBranch(t.Context(), tt.repo, token, tt.url, headSHA); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("UpdateBranch(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
		}

		if _, err := c.Merge(t.Context(), tt.repo, token, tt.url, ports.Merge{Method: squash, HeadSHA: headSHA}); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("Merge(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
		}
	}

	// No token: nothing is sent either.
	if _, err := c.PullRequest(t.Context(), appRepo(t), "", prURL); err == nil {
		t.Error("PullRequest without a token succeeded; want an error")
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

	sha, err := c.Merge(t.Context(), appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
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

		_, err := c.Merge(t.Context(), appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
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

	sha, err := c.Merge(t.Context(), appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
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

	_, err := c.Merge(t.Context(), appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
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

	if _, err := c.Merge(ctx, appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA}); !errors.Is(err, context.Canceled) {
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

	sha, err := c.Merge(t.Context(), appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
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

	if _, err := c.Merge(t.Context(), appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA}); !errors.Is(err, ports.ErrNotMergeable) {
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

	if err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA); err != nil {
		t.Fatalf("UpdateBranch = %v; want accepted", err)
	}

	if len(asked) != 1 || asked["expected_head_sha"] != headSHA {
		t.Errorf("update request %v; want only expected_head_sha 4b825dc6", asked)
	}

	// Without a head to expect, GitHub would update whatever the branch holds: nothing is sent.
	before := len(f.sent())
	if err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, ""); err == nil || len(f.sent()) != before {
		t.Errorf("UpdateBranch without a head = %v, sent %v; want refused before sending", err, f.sent()[before:])
	}
}

func TestUpdateBranch_ConflictIsMergeConflict(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.update = func(map[string]string) (int, any) {
		return http.StatusUnprocessableEntity, map[string]any{message: "merge conflict between base and head"}
	}

	err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA)

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

	if err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA); err != nil {
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

	err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA)

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

	err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA)
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

	err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA)
	if !errors.Is(err, ports.ErrNotMergeable) || errors.Is(err, ports.ErrMergeConflict) {
		t.Errorf("UpdateBranch on a moved head = %v; want ErrNotMergeable, not a conflict", err)
	}
}

func TestDeleteBranch_DeletesTheRefAndToleratesAGoneOne(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	if err := c.DeleteBranch(t.Context(), appRepo(t), token, prURL, headBranch); err != nil {
		t.Fatal(err)
	}

	if len(f.deleted) != 1 || f.deleted[0] != headBranch {
		t.Errorf("deleted %v; want goal/7-add-x", f.deleted)
	}

	if err := c.DeleteBranch(t.Context(), appRepo(t), token, prURL, "../main"); err == nil {
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
		got, err := c.BranchHead(t.Context(), repoOf(t, tt.repo), token, tt.branch)
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
		if _, err := c.BranchHead(t.Context(), repoOf(t, tt.repo), token, tt.branch); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("BranchHead(%s, %q) = %v; want ErrNotFound", tt.repo, tt.branch, err)
		}
	}
}

// A repository on a host of another kind, one that names no owner or name, and an empty token are
// refused before anything is sent.
func TestBranchHead_OnlyOnTheConnectionsHost(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	dotCom := domain.BuiltInCodeHosts()

	for _, tt := range []struct {
		repo  domain.CodeRepository
		token string
	}{
		{domain.CodeRepository{Host: dotCom[1], Owner: acmeOwner, Name: appName}, token},
		{domain.CodeRepository{Host: domain.CodeHost{Host: gheHost}, Owner: acmeOwner, Name: appName}, token},
		{domain.CodeRepository{Host: dotCom[0], Owner: acmeOwner}, token},
		{domain.CodeRepository{Host: dotCom[0], Owner: "..", Name: appName}, token},
		{domain.CodeRepository{Host: dotCom[0], Owner: "acme/team", Name: appName}, token},
		{domain.CodeRepository{}, token},
		{appRepo(t), ""},
	} {
		if _, err := c.BranchHead(t.Context(), tt.repo, tt.token, ""); err == nil {
			t.Errorf("BranchHead(%+v) succeeded; want refused", tt.repo)
		}

		if _, err := c.Compare(t.Context(), tt.repo, tt.token, "c1", "c2"); err == nil {
			t.Errorf("Compare(%+v) succeeded; want refused", tt.repo)
		}

		if _, err := c.RepositoryAccess(t.Context(), tt.repo, tt.token); err == nil {
			t.Errorf("RepositoryAccess(%+v) succeeded; want refused", tt.repo)
		}
	}

	if _, err := c.Repositories(t.Context(), dotCom[1], token, ""); err == nil {
		t.Error("Repositories on a GitLab host succeeded; want refused")
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}

func TestCompare_CommitsOldestFirstWithFiles(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)
	f.seedHistory(4)

	got, err := c.Compare(t.Context(), appRepo(t), token, "c1", "c4")
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
	if got, err := c.Compare(t.Context(), appRepo(t), token, "c4", "c4"); err != nil || len(got.Commits) != 0 || got.Truncated {
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
		if _, err := c.Compare(t.Context(), repoOf(t, tt.repo), token, tt.base, tt.head); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("Compare(%s, %s, %s) = %v; want ErrNotFound", tt.repo, tt.base, tt.head, err)
		}
	}

	for _, tt := range []struct{ base, head string }{{"", "c2"}, {"c1", ""}, {"c1..c0", "c2"}} {
		if _, err := c.Compare(t.Context(), appRepo(t), token, tt.base, tt.head); err == nil {
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

	got, err := c.Compare(t.Context(), appRepo(t), token, "c1", "c"+strconv.Itoa(ports.MaxCompareCommits+30))
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
	if got, err := c.Compare(t.Context(), appRepo(t), token, "c1", "c101"); err != nil || got.Truncated || len(got.Commits) != 100 {
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

	if _, err := c.Merge(t.Context(), appRepo(t), token, prURL, ports.Merge{Method: squash, HeadSHA: headSHA}); err == nil ||
		strings.Contains(err.Error(), token) {
		t.Errorf("err = %v; want an error without the token", err)
	}

	f.update = func(map[string]string) (int, any) {
		return http.StatusUnprocessableEntity, map[string]any{message: "bad token " + token}
	}

	if err := c.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("UpdateBranch err = %v; want an error without the token", err)
	}

	// Nor in an error of a listing GitHub refuses with it.
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.reply(w, http.StatusForbidden, map[string]any{message: "token " + token + " lacks a scope"})
	}))
	t.Cleanup(echo.Close)

	if _, err := New(Config{APIURL: echo.URL}).Repositories(t.Context(), gitHubDotCom, token, ""); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("Repositories err = %v; want an error without the token", err)
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
			_, err := moved.PullRequest(t.Context(), appRepo(t), token, prURL)
			return err
		},
		"UpdateBranch": func() error { return moved.UpdateBranch(t.Context(), appRepo(t), token, prURL, headSHA) },
		"BranchHead": func() error {
			_, err := moved.BranchHead(t.Context(), appRepo(t), token, "")
			return err
		},
		"Compare": func() error {
			_, err := moved.Compare(t.Context(), appRepo(t), token, "c1", "c2")
			return err
		},
		"Repositories": func() error {
			_, err := moved.Repositories(t.Context(), gitHubDotCom, token, "")
			return err
		},
	} {
		if err := call(); err == nil || leaked {
			t.Errorf("%s via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", name, err, leaked)
		}
	}
}

// RepositoryAccess answers a redirect as a failed read, and sends the token nowhere else.
func TestRepositoryAccess_FollowsNoRedirect(t *testing.T) {
	t.Parallel()

	var leaked atomic.Bool

	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked.Store(leaked.Load() || r.Header.Get("Authorization") != "")
	}))
	t.Cleanup(elsewhere.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // the redirect under test
	}))
	t.Cleanup(redirect.Close)

	got, err := New(Config{APIURL: redirect.URL}).RepositoryAccess(t.Context(), appRepo(t), token)
	if err != nil || got.Read.OK || leaked.Load() {
		t.Errorf("RepositoryAccess via a redirect = %+v, %v, token sent elsewhere: %v; want a failed read and no token sent", got, err, leaked.Load())
	}
}

// fakeRepo is one repository of the fake's account or organization listings.
type fakeRepo struct {
	fullName  string
	updatedAt string
	languages string // the languages answer, a JSON object of bytes by language
}

// serveRepositories adds GitHub's account, organization and repository listings to the fake's
// API: octo signs in, belongs to acme, owns mine and acme owns many; acme's first page ends with
// a Link to a next one. Each listing must be asked by most recently updated, 100 a page.
func serveRepositories(t *testing.T, f *fakeGitHub, mux *http.ServeMux, mine, acme []fakeRepo) {
	t.Helper()

	list := func(repos []fakeRepo, next bool, want map[string]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			for k, v := range want {
				if got := r.URL.Query().Get(k); got != v {
					t.Errorf("%s %s=%q; want %q", r.URL.Path, k, got, v)
				}
			}

			out := []map[string]any{}
			for _, fr := range repos {
				out = append(out, map[string]any{
					keyName: fr.fullName, "clone_url": "https://github.com/" + fr.fullName + ".git", "updated_at": fr.updatedAt,
					"private": false,
				})
			}

			if next {
				w.Header().Set("Link", `<https://api.github.com/organizations/1/repos?page=2>; rel="next", <https://api.github.com/organizations/1/repos?page=3>; rel="last"`)
			}

			f.reply(w, http.StatusOK, out)
		}
	}

	mux.HandleFunc("GET /user", func(w http.ResponseWriter, _ *http.Request) {
		f.reply(w, http.StatusOK, map[string]any{"login": octo, "type": "User"})
	})
	mux.HandleFunc("GET /user/orgs", func(w http.ResponseWriter, _ *http.Request) {
		f.reply(w, http.StatusOK, []map[string]any{{"login": "acme"}})
	})
	mux.HandleFunc("GET /user/repos", list(mine, false, map[string]string{"affiliation": "owner", "sort": "updated", "per_page": "100"}))
	mux.HandleFunc("GET /orgs/acme/repos", list(acme, true, map[string]string{"type": "all", "sort": "updated", "per_page": "100"}))
	mux.HandleFunc("GET /orgs/{org}/repos", func(w http.ResponseWriter, _ *http.Request) {
		f.reply(w, http.StatusNotFound, map[string]any{message: "Not Found"})
	})
	mux.HandleFunc("GET /repos/{owner}/{name}/languages", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("owner") + "/" + r.PathValue("name")
		for _, fr := range slices.Concat(mine, acme) {
			if fr.fullName == name {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, fr.languages)

				return
			}
		}

		f.reply(w, http.StatusNotFound, map[string]any{message: "Not Found"})
	})
}

// newRepositoriesGitHub is a fake GitHub serving the listings of serveRepositories: octo owns
// mine, and acme owns 100 on a first page of more.
func newRepositoriesGitHub(t *testing.T, mine []fakeRepo) (*fakeGitHub, *Client) {
	t.Helper()

	acme := make([]fakeRepo, 0, 100)
	for i := range 100 {
		acme = append(acme, fakeRepo{fullName: "acme/app-" + strconv.Itoa(i), updatedAt: "2026-09-01T00:00:00Z", languages: `{"Python":1}`})
	}

	f := &fakeGitHub{}
	mux := http.NewServeMux()
	serveRepositories(t, f, mux, mine, acme)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer "+token {
			f.reply(w, http.StatusUnauthorized, map[string]any{message: "Bad credentials"})
			return
		}

		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return f, New(Config{APIURL: srv.URL})
}

// fakeOwners are the owners octo's listings name.
var fakeOwners = []ports.RepositoryOwner{{Login: octo, Kind: ports.RepositoryOwnerUser}, {Login: "acme", Kind: ports.RepositoryOwnerOrganization}}

func TestClient_Repositories(t *testing.T) {
	t.Parallel()

	_, c := newRepositoriesGitHub(t, []fakeRepo{
		{fullName: "octo/goal", updatedAt: "2026-10-27T09:00:00Z", languages: `{"Go":9000,"TypeScript":4000,"Shell":30,"HCL":200}`},
		{fullName: "octo/notes", updatedAt: "2026-10-01T09:00:00Z", languages: `{}`},
	})

	// The account's, when no owner is asked.
	got, err := c.Repositories(t.Context(), gitHubDotCom, token, "")
	if err != nil {
		t.Fatal(err)
	}

	want := ports.RepositoryList{
		Login: octo, Owners: fakeOwners, Owner: octo,
		Repositories: []ports.Repository{
			{
				FullName: "octo/goal", CloneURL: "https://github.com/octo/goal.git", Languages: []string{"Go", "TypeScript", "HCL"},
				UpdatedAt: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC),
			},
			{FullName: "octo/notes", CloneURL: "https://github.com/octo/notes.git", Languages: []string{}, UpdatedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Repositories(\"\") = %+v; want %+v", got, want)
	}

	// The account named as the owner is the account's listing.
	if got, err := c.Repositories(t.Context(), gitHubDotCom, token, octo); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Repositories(octo) = %+v, %v; want %+v", got, err, want)
	}
}

// An organization's listing is its first page of 100, truncated when GitHub links a next one.
func TestClient_RepositoriesOfAnOrganizationTruncated(t *testing.T) {
	t.Parallel()

	_, c := newRepositoriesGitHub(t, nil)

	got, err := c.Repositories(t.Context(), gitHubDotCom, token, "acme")
	if err != nil {
		t.Fatal(err)
	}

	if got.Owner != "acme" || len(got.Repositories) != ports.MaxRepositories || !got.Truncated ||
		!slices.Equal(got.Repositories[99].Languages, []string{"Python"}) || !reflect.DeepEqual(got.Owners, fakeOwners) {
		t.Errorf("Repositories(acme) = owner %s, %d repositories, truncated %v; want acme, 100, truncated", got.Owner, len(got.Repositories), got.Truncated)
	}
}

// An owner GitHub does not know is not found; a name that is not a login is refused unsent.
func TestClient_RepositoriesRefusals(t *testing.T) {
	t.Parallel()

	f, c := newRepositoriesGitHub(t, nil)

	if _, err := c.Repositories(t.Context(), gitHubDotCom, token, "nobody"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Repositories(nobody) = %v; want ErrNotFound", err)
	}

	before := len(f.sent())
	if _, err := c.Repositories(t.Context(), gitHubDotCom, token, "../user"); err == nil || len(f.sent()) != before {
		t.Errorf("Repositories(../user) = %v after %d requests; want an error and none sent", err, len(f.sent())-before)
	}
}

// Names of the repository access tests.
const (
	repoApp        = "acme/app"
	repoDocs       = "acme/docs"
	badCredentials = "Bad credentials"
	notFound       = "Not Found"
	keyPermissions = "permissions"
)

// accessCase is one RepositoryAccess call and what it must answer: the checks and a word of the
// failed check's reason.
type accessCase struct {
	name, repo, token string
	read, push        bool
	reason            string
}

// check calls RepositoryAccess on c and checks the answer against tc.
func (tc accessCase) check(t *testing.T, c *Client) {
	t.Helper()

	got, err := c.RepositoryAccess(t.Context(), repoOf(t, tc.repo), tc.token)
	if err != nil {
		t.Fatalf("%s: err = %v", tc.name, err)
	}

	if got.Read.OK != tc.read || got.Push.OK != tc.push || got.Read.Reason == "" || got.Push.Reason == "" {
		t.Errorf("%s: access = %+v; want read %v, push %v, each with a reason", tc.name, got, tc.read, tc.push)
	}

	failed := got.Push.Reason
	if !tc.read {
		failed = got.Read.Reason
	}

	if tc.reason != "" && !strings.Contains(failed, tc.reason) {
		t.Errorf("%s: reason %q; want it to say %q", tc.name, failed, tc.reason)
	}
}

// accessGitHub is a stand-in GitHub that answers GET /repos/<repo> for the token with its push
// permission in pushes, any other repository 404 and another token 401. It records each request.
func accessGitHub(t *testing.T, pushes map[string]bool) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu    sync.Mutex
		paths []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		paths = append(paths, r.Method+" "+r.URL.Path)

		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		push, ok := pushes[strings.TrimPrefix(r.URL.Path, "/repos/")]

		switch {
		case r.Header.Get("Authorization") != "Bearer "+token:
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{message: badCredentials})
		case !ok:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{message: notFound})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{keyPermissions: map[string]bool{"pull": true, "push": push}})
		}
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return slices.Clone(paths)
	}
}

// A token that GitHub shows the repository to reads it, and pushes when permissions.push is
// true; an unknown repository and a bad token fail the read with GitHub's message. Only the
// repository is read.
func TestRepositoryAccess(t *testing.T) {
	t.Parallel()

	srv, paths := accessGitHub(t, map[string]bool{repoApp: true, repoDocs: false})
	c := New(Config{APIURL: srv.URL})

	for _, tc := range []accessCase{
		{name: "read and push", repo: "https://github.com/" + repoApp + ".git", token: token, read: true, push: true},
		{name: "read only", repo: "https://github.com/" + repoDocs, token: token, read: true, reason: "push"},
		{name: "unknown repository", repo: "https://github.com/acme/gone", token: token, reason: notFound},
		{name: "bad token", repo: "https://github.com/" + repoApp, token: "ghp_wrong", reason: badCredentials},
	} {
		tc.check(t, c)
	}

	for _, p := range paths() {
		if !strings.HasPrefix(p, "GET /repos/") {
			t.Errorf("sent %s; want only reads of the repository", p)
		}
	}
}

// A URL that is no repository's is refused before anything is sent, a token GitHub echoes is in
// no reason, and a host that does not answer is an error, not a failed check.
func TestRepositoryAccess_Refusals(t *testing.T) {
	t.Parallel()

	srv, paths := accessGitHub(t, map[string]bool{})
	c := New(Config{APIURL: srv.URL})

	for _, bad := range []domain.CodeRepository{
		{}, {Host: gitHubDotCom, Owner: acmeOwner}, {Host: gitHubDotCom, Owner: acmeOwner, Name: "a b"}, {Host: domain.BuiltInCodeHosts()[1], Owner: acmeOwner, Name: appName},
	} {
		if _, err := c.RepositoryAccess(t.Context(), bad, token); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("RepositoryAccess(%+v) err = %v; want domain.ErrInvalid", bad, err)
		}
	}

	if n := len(paths()); n != 0 {
		t.Errorf("sent %d requests; want none", n)
	}

	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{message: "token " + token + " lacks a scope"})
	}))
	t.Cleanup(echo.Close)

	got, err := New(Config{APIURL: echo.URL}).RepositoryAccess(t.Context(), repoOf(t, "https://github.com/"+repoApp), token)
	if err != nil || got.Read.OK || strings.Contains(got.Read.Reason+got.Push.Reason, token) {
		t.Errorf("access = %+v, err = %v; want read failed without the token", got, err)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()

	if _, err := New(Config{APIURL: dead.URL}).RepositoryAccess(t.Context(), repoOf(t, "https://github.com/"+repoApp), token); err == nil {
		t.Error("a host that does not answer: err = nil; want an error")
	}
}

// ownerRepo is one repository of the token's listing in ownerGitHub: its owner, and whether the
// token can push to it.
type ownerRepo struct {
	owner string
	push  bool
}

// ownerRepos are n repositories of owner, the first pushable of them pushable.
func ownerRepos(owner string, n, pushable int) []ownerRepo {
	out := make([]ownerRepo, n)
	for i := range out {
		out[i] = ownerRepo{owner: owner, push: i < pushable}
	}

	return out
}

// ownerGitHub is a stand-in GitHub answering GET /user/repos for the token with repos, 100 a
// page, linking a next page while there are more, and any other token 401. It checks the
// affiliation asked for and records each request.
func ownerGitHub(t *testing.T, repos []ownerRepo) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu    sync.Mutex
		paths []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		paths = append(paths, r.Method+" "+r.URL.Path)

		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{message: badCredentials})

			return
		}

		q := r.URL.Query()
		if got := q.Get("affiliation"); got != "owner,collaborator,organization_member" || q.Get("per_page") != "100" {
			t.Errorf("GET /user/repos affiliation=%q per_page=%q; want every affiliation, 100 a page", got, q.Get("per_page"))
		}

		page, err := strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 {
			page = 1
		}

		from, to := min((page-1)*100, len(repos)), min(page*100, len(repos))

		out := []map[string]any{}
		for i, or := range repos[from:to] {
			out = append(out, map[string]any{
				keyName: fmt.Sprintf("%s/repo-%d", or.owner, from+i), "owner": map[string]string{"login": or.owner},
				keyPermissions: map[string]bool{"pull": true, "push": or.push},
			})
		}

		if to < len(repos) {
			w.Header().Set("Link", fmt.Sprintf(`<https://api.github.com/user/repos?page=%d>; rel="next"`, page+1))
		}

		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return slices.Clone(paths)
	}
}

// OwnerAccess counts the token's repositories of the owner, ignoring case, and those it can push
// to, across pages; it stops at ports.MaxOwnerRepositories and says so. An owner the token has no
// repositories of, and a bad token, are refusals in the result. It only lists the token's
// repositories.
func TestOwnerAccess(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, owner, token string
		repos              []ownerRepo
		want               ports.OwnerAccess
		reason             string
	}{
		{
			name: "read and push", owner: "Acme", token: token,
			repos: slices.Concat(ownerRepos(acmeOwner, 3, 2), ownerRepos(octo, 4, 4)),
			want:  ports.OwnerAccess{Readable: 3, Pushable: 2}, reason: "3",
		},
		{
			name: "read only, across pages", owner: acmeOwner, token: token,
			repos: slices.Concat(ownerRepos(octo, 120, 120), ownerRepos(acmeOwner, 130, 0)),
			want:  ports.OwnerAccess{Readable: 130},
		},
		{
			name: "truncated", owner: acmeOwner, token: token,
			repos: ownerRepos(acmeOwner, ports.MaxOwnerRepositories+50, 1),
			want:  ports.OwnerAccess{Readable: ports.MaxOwnerRepositories, Pushable: 1, Truncated: true},
		},
		{name: "no repositories of the owner", owner: "nobody", token: token, repos: ownerRepos(acmeOwner, 2, 2), reason: "nobody"},
		{name: "bad token", owner: acmeOwner, token: "ghp_wrong", repos: ownerRepos(acmeOwner, 2, 2), reason: badCredentials},
	} {
		srv, paths := ownerGitHub(t, tc.repos)

		got, err := New(Config{APIURL: srv.URL}).OwnerAccess(t.Context(), gitHubDotCom, tc.token, tc.owner)
		if err != nil {
			t.Fatalf("%s: err = %v", tc.name, err)
		}

		if reason := got.Reason; reason == "" || !strings.Contains(reason, tc.reason) {
			t.Errorf("%s: reason %q; want one that says %q", tc.name, reason, tc.reason)
		}

		got.Reason = ""
		if got != tc.want {
			t.Errorf("%s: OwnerAccess = %+v; want %+v", tc.name, got, tc.want)
		}

		for _, p := range paths() {
			if p != "GET /user/repos" {
				t.Errorf("%s: sent %s; want only GET /user/repos", tc.name, p)
			}
		}

		if n := len(paths()); n > ports.MaxOwnerRepositories/100 {
			t.Errorf("%s: sent %d requests; want at most %d", tc.name, n, ports.MaxOwnerRepositories/100)
		}
	}
}

// An empty token or owner, an owner that is not a login and a host of another kind are refused
// before anything is sent, and a token GitHub echoes is in no reason.
func TestOwnerAccess_Refusals(t *testing.T) {
	t.Parallel()

	srv, paths := ownerGitHub(t, ownerRepos(acmeOwner, 1, 1))
	c := New(Config{APIURL: srv.URL})

	for _, bad := range []struct {
		host         domain.CodeHost
		token, owner string
	}{
		{gitHubDotCom, "", acmeOwner},
		{gitHubDotCom, token, ""},
		{gitHubDotCom, token, "../user"},
		{domain.BuiltInCodeHosts()[1], token, acmeOwner},
		{domain.CodeHost{Kind: domain.CodeHostGitHub}, token, acmeOwner},
	} {
		if _, err := c.OwnerAccess(t.Context(), bad.host, bad.token, bad.owner); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("OwnerAccess(%+v, %q) err = %v; want domain.ErrInvalid", bad.host, bad.owner, err)
		}
	}

	if n := len(paths()); n != 0 {
		t.Errorf("sent %d requests; want none", n)
	}

	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{message: "token " + token + " lacks a scope"})
	}))
	t.Cleanup(echo.Close)

	got, err := New(Config{APIURL: echo.URL}).OwnerAccess(t.Context(), gitHubDotCom, token, acmeOwner)
	if err != nil || got.Readable != 0 || got.Reason == "" || strings.Contains(got.Reason, token) {
		t.Errorf("OwnerAccess with an echoed token = %+v, %v; want a refusal without the token", got, err)
	}
}

// A redirect is not followed and is a refusal; a host that does not answer, or answers 5xx, is
// an error.
func TestOwnerAccess_RedirectsAndFailures(t *testing.T) {
	t.Parallel()

	var leaked atomic.Bool

	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked.Store(leaked.Load() || r.Header.Get("Authorization") != "")
	}))
	t.Cleanup(elsewhere.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // the redirect under test
	}))
	t.Cleanup(redirect.Close)

	got, err := New(Config{APIURL: redirect.URL}).OwnerAccess(t.Context(), gitHubDotCom, token, acmeOwner)
	if err != nil || got.Readable != 0 || leaked.Load() {
		t.Errorf("OwnerAccess via a redirect = %+v, %v, token sent elsewhere: %v; want a refusal and no token sent", got, err, leaked.Load())
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(failing.Close)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()

	for name, api := range map[string]string{"502": failing.URL, "no answer": dead.URL} {
		if _, err := New(Config{APIURL: api}).OwnerAccess(t.Context(), gitHubDotCom, token, acmeOwner); err == nil {
			t.Errorf("a host with %s: err = nil; want an error", name)
		}
	}
}

// Until the github part of ADR 0044 lands, Checks reads nothing and says it cannot.
func TestChecks_Unsupported(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("Checks sent %s %s; want no request", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	_, err := New(Config{APIURL: srv.URL}).Checks(t.Context(), appRepo(t), token, headSHA, prURL)
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("err = %v; want errors.ErrUnsupported", err)
	}
}
