package gitlab

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	token   = "glpat-test-token-never-shown"
	mrURL   = "https://gitlab.com/acme/platform/web/-/merge_requests/3"
	project = "/projects/acme%2Fplatform%2Fweb"
	headSHA = "4b825dc6"
	branch  = "goal/3-add-x"

	keyState    = "state"
	stateClosed = "closed"
	keyStatus   = "detailed_merge_status"
	keyMessage  = "message"
	squashSHA   = "squash-sha"
	cannotMerge = "Branch cannot be merged"
	// octo is a user the stand-in GitLab knows.
	octo = "octo"
)

// selfManagedHost is a self-managed GitLab host.
const selfManagedHost = "gitlab.example.com"

// gitLabDotCom is gitlab.com, as the organization's list has it.
var gitLabDotCom = domain.BuiltInCodeHosts()[1]

// repoOf is the project at raw, a project's or a merge request's URL, on the built-in hosts or
// selfManagedHost.
func repoOf(t *testing.T, raw string) domain.CodeRepository {
	t.Helper()

	r, err := domain.ParseCodeRepository(domain.ListedCodeHosts([]domain.CodeHost{{Host: selfManagedHost, Kind: domain.CodeHostGitLab}}), raw)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// webRepo is acme/platform/web on gitlab.com, the fake's project.
func webRepo(t *testing.T) domain.CodeRepository {
	t.Helper()

	return repoOf(t, mrURL)
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

// The API is the project host's own, gitlab.com or a self-managed GitLab.
func TestClient_APIForTheConnectionsHost(t *testing.T) {
	t.Parallel()

	c := New(Config{})

	for _, tt := range []struct{ url, api string }{
		{mrURL, "https://gitlab.com/api/v4"},
		{"https://" + selfManagedHost + "/acme/platform/web/-/merge_requests/3", "https://" + selfManagedHost + "/api/v4"},
	} {
		m, err := c.open(repoOf(t, tt.url), token, tt.url)
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

			pr, err := c.PullRequest(t.Context(), webRepo(t), token, mrURL)
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

// A merge request URL outside the project it is called with (another host, another project, not
// a merge request's), a project on a host of another kind, and an empty token are refused before
// anything is sent.
func TestPullRequest_OnlyOnTheConnectionsHost(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	selfManaged := repoOf(t, "https://"+selfManagedHost+"/acme/platform/web")
	onGitHub := domain.CodeRepository{Host: domain.BuiltInCodeHosts()[0], Owner: "acme/platform", Name: "web"}

	for _, tt := range []struct {
		repo  domain.CodeRepository
		token string
		url   string
	}{
		{webRepo(t), token, "https://" + selfManagedHost + "/acme/platform/web/-/merge_requests/3"},
		{webRepo(t), token, "https://github.com/acme/platform/web/pull/3"},
		{selfManaged, token, mrURL},
		{webRepo(t), token, "https://gitlab.com/acme/web/-/merge_requests/3"},
		{webRepo(t), token, "http://gitlab.com/acme/platform/web/-/merge_requests/3"},
		{webRepo(t), token, "https://gitlab.com/acme/platform/web/-/merge_requests/3?x=1"},
		{webRepo(t), token, "https://gitlab.com/acme/platform/web/-/issues/3"},
		{webRepo(t), token, "https://gitlab.com/web/-/merge_requests/3"},
		{onGitHub, token, mrURL},
		{domain.CodeRepository{}, token, mrURL},
		{webRepo(t), "", mrURL},
	} {
		if _, err := c.PullRequest(t.Context(), tt.repo, tt.token, tt.url); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("PullRequest(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
		}

		if err := c.DeleteBranch(t.Context(), tt.repo, tt.token, tt.url, branch); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("DeleteBranch(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
		}

		if err := c.UpdateBranch(t.Context(), tt.repo, tt.token, tt.url, headSHA); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("UpdateBranch(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
		}

		if _, err := c.Merge(t.Context(), tt.repo, tt.token, tt.url, ports.Merge{Method: methodSquash, HeadSHA: headSHA}); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("Merge(%s, %s) = %v; want ErrNotMergeable", tt.repo, tt.url, err)
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

		sha, err := c.Merge(t.Context(), webRepo(t), token, mrURL, ports.Merge{Method: method, HeadSHA: headSHA})
		if err != nil || sha != squashSHA {
			t.Fatalf("Merge(%s) = %q, %v; want squash-sha", method, sha, err)
		}

		if asked["sha"] != headSHA || asked["squash"] != (method == methodSquash) {
			t.Errorf("merge (%s) request %v; want head 4b825dc6, squash %v", method, asked, method == methodSquash)
		}
	}

	// GitLab cannot rebase in one call: refused, nothing sent.
	f, c := newFakeGitLab(t)
	if _, err := c.Merge(t.Context(), webRepo(t), token, mrURL, ports.Merge{Method: "rebase", HeadSHA: headSHA}); !errors.Is(err, ports.ErrNotMergeable) ||
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

		_, err := c.Merge(t.Context(), webRepo(t), token, mrURL, ports.Merge{Method: methodSquash, HeadSHA: headSHA})
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

	if err := c.UpdateBranch(t.Context(), webRepo(t), token, mrURL, headSHA); err != nil {
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

		if err := c.UpdateBranch(t.Context(), webRepo(t), token, mrURL, headSHA); !errors.Is(err, ports.ErrNotMergeable) ||
			slices.Contains(f.sent(), rebase) {
			t.Errorf("%s: UpdateBranch = %v after %v; want ErrNotMergeable and no rebase", name, err, f.sent())
		}
	}

	if err := c.UpdateBranch(t.Context(), webRepo(t), token, mrURL, ""); err == nil || len(f.sent()) != 2 {
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

		err := c.UpdateBranch(t.Context(), webRepo(t), token, mrURL, headSHA)

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

	if err := c.DeleteBranch(t.Context(), webRepo(t), token, mrURL, branch); err != nil {
		t.Fatal(err)
	}

	if len(f.deleted) != 1 || f.deleted[0] != "goal%2F3-add-x" {
		t.Errorf("deleted %v; want goal/3-add-x, encoded as one path segment", f.deleted)
	}

	if err := c.DeleteBranch(t.Context(), webRepo(t), token, "https://gitlab.com/acme/platform/web/-/merge_requests/9", branch); err != nil {
		t.Errorf("deleting on a project path the fake does not know (404) = %v; want nil", err)
	}

	if err := c.DeleteBranch(t.Context(), webRepo(t), token, mrURL, "../main"); err == nil {
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

	if _, err := c.Merge(t.Context(), webRepo(t), token, mrURL, ports.Merge{Method: methodSquash, HeadSHA: headSHA}); err == nil ||
		strings.Contains(err.Error(), token) {
		t.Errorf("err = %v; want an error without the token", err)
	}

	f.rebase = func() (int, any) { return http.StatusConflict, map[string]any{keyMessage: "bad token " + token} }

	if err := c.UpdateBranch(t.Context(), webRepo(t), token, mrURL, headSHA); err == nil || strings.Contains(err.Error(), token) {
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

	if _, err := moved.PullRequest(t.Context(), webRepo(t), token, mrURL); err == nil || leaked {
		t.Errorf("PullRequest via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", err, leaked)
	}
}

// GitLab does not read branch heads or compare commits yet: both say so and send nothing.
func TestBranchHeadAndCompare_Unsupported(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	repo := "https://gitlab.com/acme/platform/app"

	if _, err := c.BranchHead(t.Context(), repoOf(t, repo), token, ""); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("BranchHead = %v; want ErrUnsupported", err)
	}

	if _, err := c.Compare(t.Context(), repoOf(t, repo), token, "a1", "b2"); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Compare = %v; want ErrUnsupported", err)
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}

// GitLab does not list repositories yet: it says so and sends nothing.
func TestClient_RepositoriesUnsupported(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)

	for _, owner := range []string{"", "acme"} {
		if _, err := c.Repositories(t.Context(), gitLabDotCom, token, owner); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("Repositories(%q) = %v; want ErrUnsupported", owner, err)
		}
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}

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

// accessGitLab is a stand-in GitLab answering GET /projects/:id for the token: acme/platform/app
// with Developer access (30) through its group, acme/docs with Reporter (20), any other project
// 404 and another token 401.
func accessGitLab(t *testing.T) *httptest.Server {
	t.Helper()

	level := func(project, group any) map[string]any {
		return map[string]any{"permissions": map[string]any{"project_access": project, "group_access": group}}
	}

	projects := map[string]any{
		"/projects/acme%2Fplatform%2Fapp": level(nil, map[string]int{"access_level": 30}),
		"/projects/acme%2Fdocs":           level(map[string]int{"access_level": 20}, nil),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		p, ok := projects[r.URL.EscapedPath()]

		switch {
		case r.Method != http.MethodGet || r.Header.Get("PRIVATE-TOKEN") != token:
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "401 Unauthorized"})
		case !ok:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "404 Project Not Found"})
		default:
			_ = json.NewEncoder(w).Encode(p)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// A token GitLab shows the project to reads it, and pushes with Developer access or more; an
// unknown project and a bad token fail the read with GitLab's message.
func TestRepositoryAccess(t *testing.T) {
	t.Parallel()

	c := New(Config{APIURL: accessGitLab(t).URL})

	for _, tc := range []accessCase{
		{name: "developer", repo: "https://gitlab.com/acme/platform/app.git", token: token, read: true, push: true},
		{name: "reporter", repo: "https://gitlab.com/acme/docs", token: token, read: true, reason: "Reporter"},
		{name: "unknown project", repo: "https://gitlab.com/acme/gone", token: token, reason: "404 Project Not Found"},
		{name: "bad token", repo: "https://gitlab.com/acme/docs", token: "glpat-wrong", reason: "401 Unauthorized"},
	} {
		tc.check(t, c)
	}
}

// A URL that is no project's is refused before anything is sent, and a token GitLab echoes is in
// no reason.
func TestRepositoryAccess_Refusals(t *testing.T) {
	t.Parallel()

	c := New(Config{APIURL: accessGitLab(t).URL})

	for _, bad := range []domain.CodeRepository{
		{},
		{Host: gitLabDotCom, Name: "app"},
		{Host: gitLabDotCom, Owner: "acme/..", Name: "app"},
		{Host: domain.BuiltInCodeHosts()[0], Owner: "acme", Name: "app"},
	} {
		if _, err := c.RepositoryAccess(t.Context(), bad, token); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("RepositoryAccess(%+v) err = %v; want domain.ErrInvalid", bad, err)
		}
	}

	if _, err := c.RepositoryAccess(t.Context(), webRepo(t), ""); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("RepositoryAccess without a token = %v; want domain.ErrInvalid", err)
	}

	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "token " + token + " is revoked"})
	}))
	t.Cleanup(echo.Close)

	got, err := New(Config{APIURL: echo.URL}).RepositoryAccess(t.Context(), repoOf(t, "https://gitlab.com/acme/docs"), token)
	if err != nil || got.Read.OK || strings.Contains(got.Read.Reason+got.Push.Reason, token) {
		t.Errorf("access = %+v, err = %v; want read failed without the token", got, err)
	}
}

// ownerGitLab is a stand-in GitLab listing projects, 100 a page with X-Next-Page while there are
// more: the group acme/platform (subgroups included, which it requires) has projects with the
// given access levels, the user octo the levels of user, any other group or user 404 and another
// token 401. min_access_level filters by level, as GitLab does. It records each request.
func ownerGitLab(t *testing.T, group, user []int) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu    sync.Mutex
		paths []string
	)

	listings := map[string][]int{"/groups/acme%2Fplatform/projects": group, "/users/octo/projects": user}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		paths = append(paths, r.Method+" "+r.URL.EscapedPath())

		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		levels, ok := listings[r.URL.EscapedPath()]
		q := r.URL.Query()

		switch {
		case r.Method != http.MethodGet || r.Header.Get("PRIVATE-TOKEN") != token:
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "401 Unauthorized"})

			return
		case !ok:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "404 Group Not Found"})

			return
		case strings.HasPrefix(r.URL.Path, "/groups/") && q.Get("include_subgroups") != "true":
			t.Errorf("%s without include_subgroups=true", r.URL.Path)
		}

		least, _ := strconv.Atoi(q.Get("min_access_level"))

		var matching []int
		for _, l := range levels {
			if l >= least {
				matching = append(matching, l)
			}
		}

		page, err := strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 {
			page = 1
		}

		from, to := min((page-1)*100, len(matching)), min(page*100, len(matching))

		out := []map[string]any{}
		for i := from; i < to; i++ {
			out = append(out, map[string]any{"id": i, "path_with_namespace": "acme/platform/p" + strconv.Itoa(i)})
		}

		if to < len(matching) {
			w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
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

// levels are n access levels of level.
func levels(n, level int) []int { return slices.Repeat([]int{level}, n) }

// OwnerAccess counts a nested group's projects, subgroups included, or a user's when no group
// has the path, and those the token can push to (Developer or more), across pages; it stops at
// ports.MaxOwnerRepositories and says so. An unknown owner and a bad token are refusals in the
// result. It only lists projects.
func TestOwnerAccess(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, owner, token string
		group, user        []int
		want               ports.OwnerAccess
		reason             string
	}{
		{
			name: "nested group", owner: "acme/platform", token: token, group: slices.Concat(levels(2, 20), levels(3, 30), levels(1, 50)),
			want: ports.OwnerAccess{Readable: 6, Pushable: 4}, reason: "6",
		},
		{name: "user namespace", owner: octo, token: token, user: levels(150, 10), want: ports.OwnerAccess{Readable: 150}},
		{
			name: "truncated", owner: "acme/platform", token: token, group: levels(ports.MaxOwnerRepositories+1, 40),
			want: ports.OwnerAccess{Readable: ports.MaxOwnerRepositories, Pushable: ports.MaxOwnerRepositories, Truncated: true},
		},
		{name: "unknown owner", owner: "acme/gone", token: token, reason: "acme/gone"},
		{name: "unknown user", owner: "nobody", token: token, reason: "nobody"},
		{name: "bad token", owner: "acme/platform", token: "glpat-wrong", group: levels(1, 30), reason: "401 Unauthorized"},
	} {
		srv, paths := ownerGitLab(t, tc.group, tc.user)

		got, err := New(Config{APIURL: srv.URL}).OwnerAccess(t.Context(), gitLabDotCom, tc.token, tc.owner)
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
			if !strings.HasPrefix(p, "GET /groups/") && !strings.HasPrefix(p, "GET /users/") {
				t.Errorf("%s: sent %s; want only project listings", tc.name, p)
			}
		}

		if n := len(paths()); n > 1+2*ports.MaxOwnerRepositories/100 {
			t.Errorf("%s: sent %d requests; want at most %d", tc.name, n, 1+2*ports.MaxOwnerRepositories/100)
		}
	}
}

// An empty token or owner, an owner that is not a namespace path and a host of another kind are
// refused before anything is sent; a token GitLab echoes is in no reason; a redirect is not
// followed and is a refusal; a host that does not answer, or answers 5xx, is an error.
func TestOwnerAccess_Refusals(t *testing.T) {
	t.Parallel()

	srv, paths := ownerGitLab(t, levels(1, 30), nil)
	c := New(Config{APIURL: srv.URL})

	for _, bad := range []struct {
		host         domain.CodeHost
		token, owner string
	}{
		{gitLabDotCom, "", "acme"}, {gitLabDotCom, token, ""}, {gitLabDotCom, token, "acme/.."}, {gitLabDotCom, token, "acme//x"},
		{domain.BuiltInCodeHosts()[0], token, "acme"}, {domain.CodeHost{Kind: domain.CodeHostGitLab}, token, "acme"},
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
		_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "token " + token + " is revoked"})
	}))
	t.Cleanup(echo.Close)

	got, err := New(Config{APIURL: echo.URL}).OwnerAccess(t.Context(), gitLabDotCom, token, "acme")
	if err != nil || got.Readable != 0 || got.Reason == "" || strings.Contains(got.Reason, token) {
		t.Errorf("OwnerAccess with an echoed token = %+v, %v; want a refusal without the token", got, err)
	}

	var leaked atomic.Bool

	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked.Store(leaked.Load() || r.Header.Get("PRIVATE-TOKEN") != "")
	}))
	t.Cleanup(elsewhere.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // the redirect under test
	}))
	t.Cleanup(redirect.Close)

	got, err = New(Config{APIURL: redirect.URL}).OwnerAccess(t.Context(), gitLabDotCom, token, "acme")
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
		if _, err := New(Config{APIURL: api}).OwnerAccess(t.Context(), gitLabDotCom, token, "acme"); err == nil {
			t.Errorf("a host with %s: err = nil; want an error", name)
		}
	}
}
