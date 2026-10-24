package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const (
	token   = "ghp_test-token-never-shown" //nolint:gosec // a made-up token for the fake GitHub
	prURL   = "https://github.com/acme/app/pull/7"
	headSHA = "4b825dc6"
	squash  = "squash"
	message = "message"
)

var repo = domain.Repository{URL: "https://github.com/acme/app.git", AuthRef: "env:REPO_TOKEN"}

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

	return f, New(porttest.Secrets{"REPO_TOKEN": token}, Config{APIURL: srv.URL})
}

func (f *fakeGitHub) reply(w http.ResponseWriter, status int, body any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func TestPullRequest_ReadsStateHeadAndBase(t *testing.T) {
	t.Parallel()

	_, c := newFakeGitHub(t)

	pr, err := c.PullRequest(t.Context(), repo, prURL)
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

func TestPullRequest_OnlyOnTheFactorysRepository(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	for _, url := range []string{
		"https://github.com/acme/other/pull/7",
		"https://github.com/evil/app/pull/7",
		"https://code.example.com/acme/app/pull/7",
		"http://github.com/acme/app/pull/7",
		"https://github.com/acme/app/pull/7?x=1",
		"https://github.com/acme/app/issues/7",
	} {
		if _, err := c.PullRequest(t.Context(), repo, url); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("PullRequest(%s) = %v; want ErrNotMergeable", url, err)
		}
	}

	if len(f.requests) != 0 {
		t.Errorf("requests %v; want none for pull requests off the repository", f.requests)
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

	sha, err := c.Merge(t.Context(), repo, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
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

		_, err := c.Merge(t.Context(), repo, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
		if !errors.Is(err, ports.ErrNotMergeable) || !strings.Contains(err.Error(), "Head branch was modified") {
			t.Errorf("merge answered %d: err = %v; want ErrNotMergeable with GitHub's reason", status, err)
		}
	}
}

func TestDeleteBranch_DeletesTheRefAndToleratesAGoneOne(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	if err := c.DeleteBranch(t.Context(), repo, "goal/7-add-x"); err != nil {
		t.Fatal(err)
	}

	if len(f.deleted) != 1 || f.deleted[0] != "goal/7-add-x" {
		t.Errorf("deleted %v; want goal/7-add-x", f.deleted)
	}

	if err := c.DeleteBranch(t.Context(), repo, "../main"); err == nil {
		t.Error("deleting ../main succeeded; want refused")
	}
}

// A version published before a variable was reserved may still name it as the repository's
// token: the install's SecretPolicy refuses it before it is resolved, so GitHub, though trusted
// with the repository token, never receives goald's own secrets.
func TestClient_RefusesAReservedToken(t *testing.T) {
	t.Parallel()

	var calls sync.Map

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Store(r.Method+" "+r.URL.Path, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := New(porttest.Secrets{"GOAL_GATEWAY_KEY": token}, Config{
		APIURL: srv.URL, Policy: domain.SecretPolicy{Reserved: []string{"GOAL_GATEWAY_KEY"}},
	})
	reserved := domain.Repository{URL: repo.URL, AuthRef: "env:GOAL_GATEWAY_KEY"}

	_, perr := c.PullRequest(t.Context(), reserved, prURL)
	_, merr := c.Merge(t.Context(), reserved, prURL, ports.Merge{Method: squash, HeadSHA: headSHA})
	derr := c.DeleteBranch(t.Context(), reserved, "goal/7-add-x")

	for _, err := range []error{perr, merr, derr} {
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("err = %v; want the reserved token refused", err)
		}
	}

	calls.Range(func(k, v any) bool {
		t.Errorf("sent %v with Authorization %q; want nothing sent", k, v)

		return true
	})
}

func TestClient_TokenNeverInErrorsNorSentElsewhere(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitHub(t)

	// GitHub echoing the credential back must not put it in an error.
	f.merge = func(map[string]string) (int, any) {
		return http.StatusInternalServerError, map[string]any{message: "bad token " + token}
	}

	if _, err := c.Merge(t.Context(), repo, prURL, ports.Merge{Method: squash, HeadSHA: headSHA}); err == nil ||
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

	moved := New(porttest.Secrets{"REPO_TOKEN": token}, Config{APIURL: redirect.URL})

	if _, err := moved.PullRequest(t.Context(), repo, prURL); err == nil || leaked {
		t.Errorf("PullRequest via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", err, leaked)
	}
}
