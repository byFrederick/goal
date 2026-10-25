package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	token   = "ghp_test-token-never-shown" //nolint:gosec // a made-up token for the fake GitHub
	prURL   = "https://github.com/acme/app/pull/7"
	headSHA = "4b825dc6"
	squash  = "squash"
	message = "message"
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
	requests []string
	deleted  []string
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *Client) {
	t.Helper()

	f := &fakeGitHub{pr: map[string]any{
		"state": "open", "merged": false, "mergeable": true, "merge_commit_sha": "test-merge-sha",
		"head": map[string]any{"sha": headSHA, "ref": "goal/7-add-x", "repo": map[string]any{"full_name": "acme/app"}},
		"base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "acme/app"}},
	}}
	f.merge = func(map[string]string) (int, any) {
		f.pr["state"], f.pr["merged"] = "closed", true

		return http.StatusOK, map[string]any{"sha": "merge-sha", "merged": true, message: "Pull Request successfully merged"}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/app/pulls/7", func(w http.ResponseWriter, _ *http.Request) { f.reply(w, http.StatusOK, f.pr) })
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

	return f, New(Config{APIURL: srv.URL})
}

func (f *fakeGitHub) reply(w http.ResponseWriter, status int, body any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
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
		URL: prURL, Open: true, Mergeable: true, HeadSHA: headSHA, HeadBranch: "goal/7-add-x", SameRepository: true, BaseBranch: "main",
	}
	if pr != want {
		t.Errorf("pull request = %+v; want %+v", pr, want)
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

		if err := c.DeleteBranch(t.Context(), tt.conn, tt.url, "goal/7-add-x"); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("DeleteBranch(%s) = %v; want ErrNotMergeable", tt.url, err)
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

		return http.StatusOK, map[string]any{"sha": "merge-sha", "merged": true}
	}

	sha, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
	if err != nil || sha != "merge-sha" {
		t.Fatalf("Merge = %q, %v; want merge-sha", sha, err)
	}

	if asked["merge_method"] != squash || asked["sha"] != headSHA {
		t.Errorf("merge request %v; want squash of head 4b825dc6", asked)
	}
}

func TestMerge_GitHubRefusalsAreNotMergeable(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusConflict, http.StatusUnprocessableEntity} {
		f, c := newFakeGitHub(t)
		f.merge = func(map[string]string) (int, any) {
			return status, map[string]any{message: "Head branch was modified. Review and try the merge again."}
		}

		_, err := c.Merge(t.Context(), dotComConn(t), prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
		if !errors.Is(err, ports.ErrNotMergeable) || !strings.Contains(err.Error(), "Head branch was modified") {
			t.Errorf("merge answered %d: err = %v; want ErrNotMergeable with GitHub's reason", status, err)
		}
	}
}

func TestDeleteBranch_DeletesTheRefAndToleratesAGoneOne(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	if err := c.DeleteBranch(t.Context(), dotComConn(t), prURL, "goal/7-add-x"); err != nil {
		t.Fatal(err)
	}

	if len(f.deleted) != 1 || f.deleted[0] != "goal/7-add-x" {
		t.Errorf("deleted %v; want goal/7-add-x", f.deleted)
	}

	if err := c.DeleteBranch(t.Context(), dotComConn(t), prURL, "../main"); err == nil {
		t.Error("deleting ../main succeeded; want refused")
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

	if _, err := moved.PullRequest(t.Context(), dotComConn(t), prURL); err == nil || leaked {
		t.Errorf("PullRequest via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", err, leaked)
	}
}
