package gitlab

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	token   = "glpat-test-token-never-shown"
	mrURL   = "https://gitlab.com/acme/platform/web/-/merge_requests/3"
	project = "/projects/acme%2Fplatform%2Fweb"
	headSHA = "4b825dc6"
	branch  = "goal/3-add-x"

	fieldToken  = "token"
	keyState    = "state"
	stateClosed = "closed"
	keyStatus   = "detailed_merge_status"
	keyMessage  = "message"
	squashSHA   = "squash-sha"
	cannotMerge = "Branch cannot be merged"
)

// connection is a connection of the platform's gitlab kind with values (token and host).
func connection(t *testing.T, values map[string]string) domain.ResolvedConnection {
	t.Helper()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == "gitlab" })
	if i < 0 {
		t.Fatal("the platform has no gitlab kind")
	}

	return domain.ResolvedConnection{Connection: domain.Connection{Name: "gitlab", Kind: "gitlab"}, Kind: kinds[i], Values: values}
}

// dotComConn is a gitlab.com connection with the token.
func dotComConn(t *testing.T) domain.ResolvedConnection {
	t.Helper()

	return connection(t, map[string]string{fieldToken: token})
}

// fakeGitLab is a GitLab REST API with one merge request, acme/platform/web!3, that requires the
// token. Its routes are matched on the escaped path, as GitLab reads a project id.
type fakeGitLab struct {
	mu       sync.Mutex
	mr       map[string]any
	merge    func(body map[string]any) (int, any)
	rebase   func() (int, any)
	requests []string
	deleted  []string
}

func newFakeGitLab(t *testing.T) (*fakeGitLab, *Client) {
	t.Helper()

	f := &fakeGitLab{mr: map[string]any{
		"iid": 3, keyState: "opened", "sha": headSHA, "source_branch": branch, "target_branch": "main",
		"source_project_id": 42, "target_project_id": 42, keyStatus: "mergeable",
		"merge_commit_sha": nil, "squash_commit_sha": nil,
	}}
	f.merge = func(map[string]any) (int, any) {
		f.mr[keyState], f.mr["squash_commit_sha"] = "merged", squashSHA

		return http.StatusOK, f.mr
	}

	f.rebase = func() (int, any) { return http.StatusAccepted, map[string]any{"rebase_in_progress": true} }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.Method + " " + r.URL.EscapedPath()

		f.mu.Lock()
		f.requests = append(f.requests, route)
		f.mu.Unlock()

		if r.Header.Get("PRIVATE-TOKEN") != token {
			f.reply(w, http.StatusUnauthorized, map[string]any{keyMessage: "401 Unauthorized"})
			return
		}

		switch {
		case route == "GET "+project+"/merge_requests/3":
			f.reply(w, http.StatusOK, f.mr)
		case route == "PUT "+project+"/merge_requests/3/merge":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}

			f.mu.Lock()
			status, res := f.merge(body)
			f.mu.Unlock()

			f.reply(w, status, res)
		case route == "PUT "+project+"/merge_requests/3/rebase":
			f.mu.Lock()
			status, res := f.rebase()
			f.mu.Unlock()

			f.reply(w, status, res)
		case strings.HasPrefix(route, "DELETE "+project+"/repository/branches/"):
			f.mu.Lock()
			f.deleted = append(f.deleted, strings.TrimPrefix(route, "DELETE "+project+"/repository/branches/"))
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			f.reply(w, http.StatusNotFound, map[string]any{keyMessage: "404 Not Found"})
		}
	}))
	t.Cleanup(srv.Close)

	return f, New(Config{APIURL: srv.URL})
}

func (f *fakeGitLab) reply(w http.ResponseWriter, status int, body any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeGitLab) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.requests)
}

// The API is the connection's host's own, gitlab.com or a self-managed GitLab.
func TestClient_APIForTheConnectionsHost(t *testing.T) {
	t.Parallel()

	c := New(Config{})

	tests := []struct {
		values   map[string]string
		url, api string
	}{
		{map[string]string{fieldToken: token}, mrURL, "https://gitlab.com/api/v4"},
		{
			map[string]string{fieldToken: token, "host": "gitlab.example.com"},
			"https://gitlab.example.com/acme/platform/web/-/merge_requests/3", "https://gitlab.example.com/api/v4",
		},
	}

	for _, tt := range tests {
		m, err := c.open(connection(t, tt.values), tt.url)
		if err != nil || m.api != tt.api || m.project != "acme%2Fplatform%2Fweb" || m.iid != "3" {
			t.Errorf("open(%s) = %+v, %v; want the API %s, project acme/platform/web and iid 3", tt.url, m, err, tt.api)
		}
	}
}

func TestPullRequest_ReadsStateHeadAndBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mr   map[string]any
		want ports.PullRequest
	}{
		{
			name: "open and mergeable", mr: map[string]any{},
			want: ports.PullRequest{Open: true, Mergeable: true},
		},
		{
			name: "mergeability being computed", mr: map[string]any{keyStatus: "checking"},
			want: ports.PullRequest{Open: true},
		},
		{
			name: "conflicts", mr: map[string]any{keyStatus: "conflict"},
			want: ports.PullRequest{Open: true},
		},
		{
			name: "a GitLab before detailed_merge_status", mr: map[string]any{keyStatus: nil, "merge_status": "can_be_merged"},
			want: ports.PullRequest{Open: true, Mergeable: true},
		},
		{
			name: "from a fork", mr: map[string]any{"source_project_id": 7},
			want: ports.PullRequest{Open: true, Mergeable: true},
		},
		{
			name: "merged with a merge commit", mr: map[string]any{keyState: "merged", "merge_commit_sha": "merge-sha"},
			want: ports.PullRequest{Merged: true, MergeSHA: "merge-sha"},
		},
		{
			name: "squashed and fast-forwarded", mr: map[string]any{keyState: "merged", "squash_commit_sha": squashSHA},
			want: ports.PullRequest{Merged: true, MergeSHA: squashSHA},
		},
		{
			name: stateClosed, mr: map[string]any{keyState: stateClosed},
			want: ports.PullRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, c := newFakeGitLab(t)
			maps.Copy(f.mr, tt.mr)

			pr, err := c.PullRequest(t.Context(), dotComConn(t), mrURL)
			if err != nil {
				t.Fatal(err)
			}

			want := tt.want
			want.URL, want.HeadSHA, want.HeadBranch, want.BaseBranch = mrURL, headSHA, branch, "main"
			// Only the fork case moves the source project away from the target's.
			want.SameRepository = tt.mr["source_project_id"] == nil

			if pr != want {
				t.Errorf("merge request = %+v; want %+v", pr, want)
			}
		})
	}
}

// A merge request on another host than the connection's, or a URL that is not a merge
// request's, is refused before anything is sent.
func TestPullRequest_OnlyOnTheConnectionsHost(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	selfManaged := connection(t, map[string]string{fieldToken: token, "host": "gitlab.example.com"})

	for _, tt := range []struct {
		conn domain.ResolvedConnection
		url  string
	}{
		{dotComConn(t), "https://gitlab.example.com/acme/web/-/merge_requests/3"},
		{dotComConn(t), "https://github.com/acme/web/pull/3"},
		{selfManaged, mrURL},
		{dotComConn(t), "http://gitlab.com/acme/web/-/merge_requests/3"},
		{dotComConn(t), "https://gitlab.com/acme/web/-/merge_requests/3?x=1"},
		{dotComConn(t), "https://gitlab.com/acme/web/-/issues/3"},
		{dotComConn(t), "https://gitlab.com/web/-/merge_requests/3"},
	} {
		if _, err := c.PullRequest(t.Context(), tt.conn, tt.url); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("PullRequest(%s) = %v; want ErrNotMergeable", tt.url, err)
		}

		if err := c.DeleteBranch(t.Context(), tt.conn, tt.url, branch); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("DeleteBranch(%s) = %v; want ErrNotMergeable", tt.url, err)
		}

		if err := c.UpdateBranch(t.Context(), tt.conn, tt.url, headSHA); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("UpdateBranch(%s) = %v; want ErrNotMergeable", tt.url, err)
		}
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}

func TestMerge_AtTheHeadItWasGiven(t *testing.T) {
	t.Parallel()

	for _, method := range []string{methodSquash, methodMerge} {
		f, c := newFakeGitLab(t)

		var asked map[string]any

		merge := f.merge
		f.merge = func(body map[string]any) (int, any) {
			asked = body

			return merge(body)
		}

		sha, err := c.Merge(t.Context(), dotComConn(t), mrURL, ports.Merge{Method: method, HeadSHA: headSHA})
		if err != nil || sha != squashSHA {
			t.Fatalf("Merge(%s) = %q, %v; want squash-sha", method, sha, err)
		}

		if asked["sha"] != headSHA || asked["squash"] != (method == methodSquash) {
			t.Errorf("merge (%s) request %v; want head 4b825dc6, squash %v", method, asked, method == methodSquash)
		}
	}

	// GitLab cannot rebase in one call: refused, nothing sent.
	f, c := newFakeGitLab(t)
	if _, err := c.Merge(t.Context(), dotComConn(t), mrURL, ports.Merge{Method: "rebase", HeadSHA: headSHA}); !errors.Is(err, ports.ErrNotMergeable) ||
		len(f.sent()) != 0 {
		t.Errorf("Merge with rebase = %v after %v; want ErrNotMergeable and nothing sent", err, f.sent())
	}
}

func TestMerge_GitLabRefusalsAreNotMergeable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		body   any
		reason string
	}{
		{http.StatusMethodNotAllowed, map[string]any{keyMessage: "405 Method Not Allowed"}, "405 Method Not Allowed"},
		{http.StatusNotAcceptable, map[string]any{keyMessage: cannotMerge}, cannotMerge},
		{http.StatusConflict, map[string]any{keyMessage: "SHA does not match HEAD of source branch: 1a2b3c"}, "SHA does not match"},
		{http.StatusUnprocessableEntity, map[string]any{keyMessage: map[string]any{"base": []string{cannotMerge}}}, cannotMerge},
	}

	for _, tt := range tests {
		f, c := newFakeGitLab(t)
		f.merge = func(map[string]any) (int, any) { return tt.status, tt.body }

		_, err := c.Merge(t.Context(), dotComConn(t), mrURL, ports.Merge{Method: methodSquash, HeadSHA: headSHA})
		if !errors.Is(err, ports.ErrNotMergeable) || !strings.Contains(err.Error(), tt.reason) {
			t.Errorf("merge answered %d: err = %v; want ErrNotMergeable with GitLab's reason", tt.status, err)
		}
	}
}

func TestUpdateBranch_RebasesAtTheHeadItWasGiven(t *testing.T) {
	t.Parallel()

	const (
		read   = "GET " + project + "/merge_requests/3"
		rebase = "PUT " + project + "/merge_requests/3/rebase"
	)

	f, c := newFakeGitLab(t)

	if err := c.UpdateBranch(t.Context(), dotComConn(t), mrURL, headSHA); err != nil {
		t.Fatalf("UpdateBranch = %v; want accepted", err)
	}

	if got := f.sent(); !slices.Equal(got, []string{read, rebase}) {
		t.Errorf("requests %v; want the read, then the rebase", got)
	}

	// A head that moved since the caller read it, a closed merge request or no head: no rebase.
	for name, change := range map[string]func(*fakeGitLab){
		"moved":     func(f *fakeGitLab) { f.mr["sha"] = "1a2b3c" },
		stateClosed: func(f *fakeGitLab) { f.mr[keyState] = stateClosed },
	} {
		f, c := newFakeGitLab(t)
		change(f)

		if err := c.UpdateBranch(t.Context(), dotComConn(t), mrURL, headSHA); !errors.Is(err, ports.ErrNotMergeable) ||
			slices.Contains(f.sent(), rebase) {
			t.Errorf("%s: UpdateBranch = %v after %v; want ErrNotMergeable and no rebase", name, err, f.sent())
		}
	}

	if err := c.UpdateBranch(t.Context(), dotComConn(t), mrURL, ""); err == nil || len(f.sent()) != 2 {
		t.Errorf("UpdateBranch without a head = %v after %v; want refused before sending", err, f.sent())
	}
}

func TestUpdateBranch_GitLabRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   int
		message  string
		conflict bool
	}{
		{http.StatusConflict, "Failed to enqueue the rebase operation", true},
		{http.StatusUnprocessableEntity, "Rebase failed: Rebase locally, resolve all conflicts, then push the branch.", true},
		{http.StatusForbidden, "403 Forbidden", false},
		{http.StatusMethodNotAllowed, "405 Method Not Allowed", false},
	}

	for _, tt := range tests {
		f, c := newFakeGitLab(t)
		f.rebase = func() (int, any) { return tt.status, map[string]any{keyMessage: tt.message} }

		err := c.UpdateBranch(t.Context(), dotComConn(t), mrURL, headSHA)

		var mc *ports.MergeConflictError
		if tt.conflict && (!errors.Is(err, ports.ErrMergeConflict) || !errors.As(err, &mc) || errors.Is(err, ports.ErrNotMergeable)) {
			t.Errorf("rebase answered %d: err = %v; want a *MergeConflictError", tt.status, err)
		}

		if !tt.conflict && (!errors.Is(err, ports.ErrNotMergeable) || errors.Is(err, ports.ErrMergeConflict)) {
			t.Errorf("rebase answered %d: err = %v; want ErrNotMergeable", tt.status, err)
		}

		if err != nil && !strings.Contains(err.Error(), tt.message) {
			t.Errorf("rebase answered %d: err = %v; want GitLab's reason", tt.status, err)
		}
	}
}

func TestDeleteBranch_DeletesTheBranchAndToleratesAGoneOne(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)

	if err := c.DeleteBranch(t.Context(), dotComConn(t), mrURL, branch); err != nil {
		t.Fatal(err)
	}

	if len(f.deleted) != 1 || f.deleted[0] != "goal%2F3-add-x" {
		t.Errorf("deleted %v; want goal/3-add-x, encoded as one path segment", f.deleted)
	}

	if err := c.DeleteBranch(t.Context(), dotComConn(t), "https://gitlab.com/acme/platform/web/-/merge_requests/9", branch); err != nil {
		t.Errorf("deleting on a project path the fake does not know (404) = %v; want nil", err)
	}

	if err := c.DeleteBranch(t.Context(), dotComConn(t), mrURL, "../main"); err == nil {
		t.Error("deleting ../main succeeded; want refused")
	}
}

func TestClient_TokenNeverInErrorsNorSentElsewhere(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)

	// GitLab echoing the credential back must not put it in an error.
	f.merge = func(map[string]any) (int, any) {
		return http.StatusInternalServerError, map[string]any{keyMessage: "bad token " + token}
	}

	if _, err := c.Merge(t.Context(), dotComConn(t), mrURL, ports.Merge{Method: methodSquash, HeadSHA: headSHA}); err == nil ||
		strings.Contains(err.Error(), token) {
		t.Errorf("err = %v; want an error without the token", err)
	}

	f.rebase = func() (int, any) { return http.StatusConflict, map[string]any{keyMessage: "bad token " + token} }

	if err := c.UpdateBranch(t.Context(), dotComConn(t), mrURL, headSHA); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("UpdateBranch err = %v; want an error without the token", err)
	}

	// A redirect is not followed: the token would travel to another host.
	var leaked bool

	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = leaked || r.Header.Get("PRIVATE-TOKEN") != ""
	}))
	t.Cleanup(elsewhere.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // the redirect under test
	}))
	t.Cleanup(redirect.Close)

	moved := New(Config{APIURL: redirect.URL})

	if _, err := moved.PullRequest(t.Context(), dotComConn(t), mrURL); err == nil || leaked {
		t.Errorf("PullRequest via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", err, leaked)
	}
}

// GitLab does not read branch heads or compare commits yet: both say so and send nothing.
func TestBranchHeadAndCompare_Unsupported(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	repo := "https://gitlab.com/acme/platform/app"

	if _, err := c.BranchHead(t.Context(), dotComConn(t), repo, ""); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("BranchHead = %v; want ErrUnsupported", err)
	}

	if _, err := c.Compare(t.Context(), dotComConn(t), repo, "a1", "b2"); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Compare = %v; want ErrUnsupported", err)
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}
