package gitlab

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	// history is the project's commits, oldest first, each with the diffs GitLab lists for it;
	// branches names each branch's head. seedHistory fills them.
	history  []fakeCommit
	branches map[string]string
}

// fakeCommit is one commit of fakeGitLab's history and the diffs GitLab lists for it.
type fakeCommit struct {
	sha, message string
	diffs        []map[string]any
	// diffStatus, when set, is GitLab's answer to a read of the commit's diff.
	diffStatus int
}

// seedHistory gives the project n commits c1…cn on main, its default branch, each changing
// fI.go, and a branch goal/3-add-x at headSHA.
func (f *fakeGitLab) seedHistory(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.history = nil
	for i := 1; i <= n; i++ {
		name := "f" + strconv.Itoa(i) + ".go"
		f.history = append(f.history, fakeCommit{
			sha: "c" + strconv.Itoa(i), message: "  change " + name + "  \n\nWhy it changed.",
			diffs: []map[string]any{{"old_path": name, "new_path": name, "diff": "@@ -0,0 +1 @@"}},
		})
	}

	f.branches = map[string]string{"main": "c" + strconv.Itoa(n), branch: headSHA}
}

// compare answers GitLab's compare of from...to on the linear history: the commits after from
// up to to, oldest first, with every diff between them.
func (f *fakeGitLab) compare(from, to string) (int, any) {
	at := func(sha string) int {
		return slices.IndexFunc(f.history, func(c fakeCommit) bool { return c.sha == sha })
	}

	i, j := at(from), at(to)
	if i < 0 || j < 0 {
		return http.StatusNotFound, map[string]any{keyMessage: "404 Ref Not Found"}
	}

	commits, diffs := []map[string]any{}, []map[string]any{}
	for _, c := range f.history[i+1 : max(i+1, j+1)] {
		commits = append(commits, map[string]any{"id": c.sha, "short_id": c.sha, "title": "ignored", "message": c.message})
		diffs = append(diffs, c.diffs...)
	}

	return http.StatusOK, map[string]any{"commit": nil, "commits": commits, "diffs": diffs, "compare_timeout": false, "compare_same_ref": false}
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
		case route == "GET "+project:
			f.reply(w, http.StatusOK, map[string]any{"path_with_namespace": "acme/platform/web", "default_branch": "main"})
		case strings.HasPrefix(route, "GET "+project+"/repository/branches/"):
			// GitLab reads the branch as one segment: a slash in it must arrive escaped.
			segment := strings.TrimPrefix(route, "GET "+project+"/repository/branches/")
			name, err := url.PathUnescape(segment)

			f.mu.Lock()
			sha, ok := f.branches[name]
			f.mu.Unlock()

			if err != nil || strings.Contains(segment, "/") || !ok {
				f.reply(w, http.StatusNotFound, map[string]any{keyMessage: "404 Branch Not Found"})
				return
			}

			f.reply(w, http.StatusOK, map[string]any{"name": "x", "commit": map[string]any{"id": sha, "short_id": sha[:min(len(sha), 8)]}})
		case route == "GET "+project+"/repository/compare":
			q := r.URL.Query()
			if q.Get("straight") != "false" {
				t.Errorf("compare query %s; want straight=false", r.URL.RawQuery)
			}

			f.mu.Lock()
			status, res := f.compare(q.Get("from"), q.Get("to"))
			f.mu.Unlock()

			f.reply(w, status, res)
		case strings.HasPrefix(route, "GET "+project+"/repository/commits/") && strings.HasSuffix(route, "/diff"):
			if r.URL.Query().Get("per_page") != "100" {
				t.Errorf("diff query %s; want per_page=100", r.URL.RawQuery)
			}

			sha := strings.TrimSuffix(strings.TrimPrefix(route, "GET "+project+"/repository/commits/"), "/diff")

			f.mu.Lock()
			i := slices.IndexFunc(f.history, func(c fakeCommit) bool { return c.sha == sha })
			f.mu.Unlock()

			switch {
			case i < 0:
				f.reply(w, http.StatusNotFound, map[string]any{keyMessage: "404 Commit Not Found"})
			case f.history[i].diffStatus != 0:
				f.reply(w, f.history[i].diffStatus, map[string]any{keyMessage: "500 Internal Server Error"})
			default:
				f.reply(w, http.StatusOK, f.history[i].diffs)
			}
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

	if _, err := moved.BranchHead(t.Context(), webRepo(t), token, ""); err == nil || leaked {
		t.Errorf("BranchHead via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", err, leaked)
	}

	if _, err := moved.Compare(t.Context(), webRepo(t), token, "c1", "c2"); err == nil || leaked {
		t.Errorf("Compare via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", err, leaked)
	}

	// The reads echoing the credential back keep it out of their errors too, and carry it only
	// in the PRIVATE-TOKEN header.
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != token || strings.Contains(r.URL.String(), token) {
			t.Errorf("request %s: want the token in PRIVATE-TOKEN only", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "bad token " + token})
	}))
	t.Cleanup(echo.Close)

	echoing := New(Config{APIURL: echo.URL})

	if _, err := echoing.BranchHead(t.Context(), webRepo(t), token, branch); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("BranchHead err = %v; want an error without the token", err)
	}

	if _, err := echoing.Compare(t.Context(), webRepo(t), token, "c1", "c2"); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("Compare err = %v; want an error without the token", err)
	}
}

func TestBranchHead_NamedAndDefaultBranch(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	f.seedHistory(3)

	for _, tt := range []struct{ branch, want string }{
		{"", "c3"},
		{"main", "c3"},
		{branch, headSHA},
	} {
		got, err := c.BranchHead(t.Context(), webRepo(t), token, tt.branch)
		if err != nil || got != tt.want {
			t.Errorf("BranchHead(%q) = %q, %v; want %q", tt.branch, got, err, tt.want)
		}
	}

	if got := f.sent(); !slices.Contains(got, "GET "+project) || !slices.Contains(got, "GET "+project+"/repository/branches/goal%2F3-add-x") {
		t.Errorf("requests %v; want the project read for its default branch and the named branch read as one segment", got)
	}
}

func TestBranchHead_UnknownIsNotFound(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	f.seedHistory(1)

	for _, tt := range []struct{ repo, branch string }{
		{"https://gitlab.com/acme/platform/web", "gone"},
		{"https://gitlab.com/acme/other", ""},
		{"https://gitlab.com/acme/other", "main"},
	} {
		if _, err := c.BranchHead(t.Context(), repoOf(t, tt.repo), token, tt.branch); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("BranchHead(%s, %q) = %v; want ErrNotFound", tt.repo, tt.branch, err)
		}
	}

	before := len(f.sent())
	if _, err := c.BranchHead(t.Context(), webRepo(t), token, "../main"); err == nil || len(f.sent()) != before {
		t.Errorf("BranchHead(../main) = %v after %v; want refused before sending", err, f.sent())
	}
}

// hostRecorder answers every request itself and records the host each went to.
type hostRecorder struct {
	mu    sync.Mutex
	hosts []string
}

func (h *hostRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.hosts = append(h.hosts, r.URL.Host)
	h.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"default_branch":"main","commit":{"id":"c9"},"commits":[]}`)), Request: r,
	}, nil
}

// A project on a self-managed host is read from that host's API only. A project on a host of
// another kind, one that names no namespace or name, and an empty token are refused before
// anything is sent.
func TestBranchHead_OnlyOnTheConnectionsHost(t *testing.T) {
	t.Parallel()

	rec := &hostRecorder{}
	c := New(Config{Transport: rec})
	selfManaged := repoOf(t, "https://"+selfManagedHost+"/acme/platform/web")

	if sha, err := c.BranchHead(t.Context(), selfManaged, token, ""); err != nil || sha != "c9" {
		t.Fatalf("BranchHead on %s = %q, %v; want c9", selfManagedHost, sha, err)
	}

	if _, err := c.Compare(t.Context(), selfManaged, token, "c1", "c9"); err != nil {
		t.Fatalf("Compare on %s = %v", selfManagedHost, err)
	}

	rec.mu.Lock()
	hosts := slices.Clone(rec.hosts)
	rec.mu.Unlock()

	if len(hosts) != 3 || slices.ContainsFunc(hosts, func(h string) bool { return h != selfManagedHost }) {
		t.Errorf("requests went to %v; want three, all to %s", hosts, selfManagedHost)
	}

	f, faked := newFakeGitLab(t)
	onGitHub := domain.CodeRepository{Host: domain.BuiltInCodeHosts()[0], Owner: "acme/platform", Name: "web"}

	for _, tt := range []struct {
		repo  domain.CodeRepository
		token string
	}{
		{onGitHub, token},
		{domain.CodeRepository{Host: gitLabDotCom, Owner: "acme"}, token},
		{domain.CodeRepository{Host: gitLabDotCom, Owner: "..", Name: "web"}, token},
		{domain.CodeRepository{}, token},
		{webRepo(t), ""},
	} {
		if _, err := faked.BranchHead(t.Context(), tt.repo, tt.token, ""); err == nil {
			t.Errorf("BranchHead(%+v) succeeded; want refused", tt.repo)
		}

		if _, err := faked.Compare(t.Context(), tt.repo, tt.token, "c1", "c2"); err == nil {
			t.Errorf("Compare(%+v) succeeded; want refused", tt.repo)
		}
	}

	if got := f.sent(); len(got) != 0 {
		t.Errorf("requests %v; want none", got)
	}
}

func TestCompare_CommitsOldestFirstWithFiles(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	f.seedHistory(4)
	// c3 renames a file and deletes another: both of each's paths count, once.
	f.history[2].diffs = []map[string]any{
		{"old_path": "old.go", "new_path": "new.go", "renamed_file": true},
		{"old_path": "gone.go", "new_path": "gone.go", "deleted_file": true},
	}

	got, err := c.Compare(t.Context(), webRepo(t), token, "c1", "c4")
	if err != nil {
		t.Fatal(err)
	}

	want := ports.Comparison{Commits: []ports.Commit{
		{SHA: "c2", Subject: "change f2.go", Files: []string{"f2.go"}},
		{SHA: "c3", Subject: "change f3.go", Files: []string{"new.go", "old.go", "gone.go"}},
		{SHA: "c4", Subject: "change f4.go", Files: []string{"f4.go"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Compare = %+v; want %+v", got, want)
	}

	if sent := f.sent(); !slices.Equal(sent, []string{
		"GET " + project + "/repository/compare",
		"GET " + project + "/repository/commits/c2/diff",
		"GET " + project + "/repository/commits/c3/diff",
		"GET " + project + "/repository/commits/c4/diff",
	}) {
		t.Errorf("requests %v; want the compare, then each commit's diff", sent)
	}

	// Nothing since: no commits, not truncated.
	if got, err := c.Compare(t.Context(), webRepo(t), token, "c4", "c4"); err != nil || len(got.Commits) != 0 || got.Truncated {
		t.Errorf("Compare(c4, c4) = %+v, %v; want no commits", got, err)
	}
}

func TestCompare_UnknownCommitIsNotFound(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	f.seedHistory(2)

	for _, tt := range []struct{ repo, base, head string }{
		{"https://gitlab.com/acme/platform/web", "gone", "c2"},
		{"https://gitlab.com/acme/platform/web", "c1", "gone"},
		{"https://gitlab.com/acme/other", "c1", "c2"},
	} {
		if _, err := c.Compare(t.Context(), repoOf(t, tt.repo), token, tt.base, tt.head); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("Compare(%s, %s, %s) = %v; want ErrNotFound", tt.repo, tt.base, tt.head, err)
		}
	}

	before := len(f.sent())

	for _, tt := range []struct{ base, head string }{{"", "c2"}, {"c1", ""}, {"c1..c0", "c2"}, {"c1", "c2..c3"}} {
		if _, err := c.Compare(t.Context(), webRepo(t), token, tt.base, tt.head); err == nil {
			t.Errorf("Compare(%q, %q) succeeded; want refused", tt.base, tt.head)
		}
	}

	if got := f.sent(); len(got) != before {
		t.Errorf("requests %v; want none for refused refs", got[before:])
	}

	// A commit whose diff cannot be read fails the whole call.
	f.mu.Lock()
	f.history[1].diffStatus = http.StatusInternalServerError
	f.mu.Unlock()

	if got, err := c.Compare(t.Context(), webRepo(t), token, "c1", "c2"); err == nil || !strings.Contains(err.Error(), "c2") {
		t.Errorf("Compare with an unreadable diff = %+v, %v; want an error naming c2", got, err)
	}
}

// Past 100 commits, Compare returns the oldest 100 and says it truncated. The compare answer
// carries every commit's diff, far over the 1 MiB other answers are held to.
func TestCompare_TruncatesPast100(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	f.seedHistory(ports.MaxCompareCommits + 30)

	patch := strings.Repeat("+x\n", 8<<10)
	for i := range f.history {
		f.history[i].diffs[0]["diff"] = patch
	}

	got, err := c.Compare(t.Context(), webRepo(t), token, "c1", "c"+strconv.Itoa(ports.MaxCompareCommits+30))
	if err != nil {
		t.Fatal(err)
	}

	if !got.Truncated || len(got.Commits) != ports.MaxCompareCommits {
		t.Fatalf("Compare = %d commits, truncated %v; want %d and truncated", len(got.Commits), got.Truncated, ports.MaxCompareCommits)
	}

	if first, last := got.Commits[0].SHA, got.Commits[len(got.Commits)-1].SHA; first != "c2" || last != "c101" {
		t.Errorf("commits %s to %s; want c2 to c101, the oldest", first, last)
	}

	if sent := f.sent(); len(sent) != 1+ports.MaxCompareCommits {
		t.Errorf("%d requests; want the compare, then each of the 100 commits' diffs", len(sent))
	}

	// Exactly 100 is not truncated.
	if got, err := c.Compare(t.Context(), webRepo(t), token, "c1", "c101"); err != nil || got.Truncated || len(got.Commits) != 100 {
		t.Errorf("Compare(c1, c101) = %d commits, truncated %v, %v; want 100, not truncated", len(got.Commits), got.Truncated, err)
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
