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
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	token      = "glpat-test-token-never-shown"
	mrURL      = "https://gitlab.com/acme/platform/web/-/merge_requests/3"
	project    = "/projects/acme%2Fplatform%2Fweb"
	headSHA    = "4b825dc6"
	branch     = "goal/3-add-x"
	mainBranch = "main"

	keyState    = "state"
	stateClosed = "closed"
	keyStatus   = "detailed_merge_status"
	keyMessage  = "message"
	squashSHA   = "squash-sha"
	cannotMerge = "Branch cannot be merged"
	// octo is a user the stand-in GitLab knows, and acme/platform a group of the group acme.
	octo          = "octo"
	acmeGroup     = "acme"
	platformGroup = "acme/platform"
	unauthorized  = "401 Unauthorized"
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

	f.branches = map[string]string{mainBranch: "c" + strconv.Itoa(n), branch: headSHA}
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
		"iid": 3, keyState: "opened", "sha": headSHA, "source_branch": branch, "target_branch": mainBranch,
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

		if f.serveHistory(t, w, r, route) {
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

// serveHistory answers the reads of the project's history (the project, its branches, compare
// and a commit's diff), reporting whether route was one.
func (f *fakeGitLab) serveHistory(t *testing.T, w http.ResponseWriter, r *http.Request, route string) bool {
	t.Helper()

	switch {
	case route == "GET "+project:
		f.reply(w, http.StatusOK, map[string]any{"path_with_namespace": "acme/platform/web", "default_branch": mainBranch})
	case strings.HasPrefix(route, "GET "+project+"/repository/branches/"):
		// GitLab reads the branch as one segment: a slash in it must arrive escaped.
		segment := strings.TrimPrefix(route, "GET "+project+"/repository/branches/")
		name, err := url.PathUnescape(segment)

		f.mu.Lock()
		sha, ok := f.branches[name]
		f.mu.Unlock()

		if err != nil || strings.Contains(segment, "/") || !ok {
			f.reply(w, http.StatusNotFound, map[string]any{keyMessage: "404 Branch Not Found"})
			return true
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
	default:
		return false
	}

	return true
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
			want.URL, want.HeadSHA, want.HeadBranch, want.BaseBranch = mrURL, headSHA, branch, mainBranch
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

	for name, call := range map[string]func(*Client) error{
		"PullRequest": func(c *Client) error { _, err := c.PullRequest(t.Context(), webRepo(t), token, mrURL); return err },
		"BranchHead":  func(c *Client) error { _, err := c.BranchHead(t.Context(), webRepo(t), token, ""); return err },
		"Compare":     func(c *Client) error { _, err := c.Compare(t.Context(), webRepo(t), token, "c1", "c2"); return err },
	} {
		if err := call(moved); err == nil || leaked {
			t.Errorf("%s via a redirect = %v, token sent elsewhere: %v; want an error and no token sent", name, err, leaked)
		}
	}

	// The reads echoing the credential back keep it out of their errors too.
	echoing := echoGitLab(t)

	for name, err := range map[string]error{
		"BranchHead": func() error { _, err := echoing.BranchHead(t.Context(), webRepo(t), token, branch); return err }(),
		"Compare":    func() error { _, err := echoing.Compare(t.Context(), webRepo(t), token, "c1", "c2"); return err }(),
	} {
		if err == nil || strings.Contains(err.Error(), token) {
			t.Errorf("%s err = %v; want an error without the token", name, err)
		}
	}
}

// echoGitLab is a client of a GitLab that answers every request 500 with the token in its
// message, and checks that the token arrives in the PRIVATE-TOKEN header only.
func echoGitLab(t *testing.T) *Client {
	t.Helper()

	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != token || strings.Contains(r.URL.String(), token) {
			t.Errorf("request %s: want the token in PRIVATE-TOKEN only", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "bad token " + token})
	}))
	t.Cleanup(echo.Close)

	return New(Config{APIURL: echo.URL})
}

func TestBranchHead_NamedAndDefaultBranch(t *testing.T) {
	t.Parallel()

	f, c := newFakeGitLab(t)
	f.seedHistory(3)

	for _, tt := range []struct{ branch, want string }{
		{"", "c3"},
		{mainBranch, "c3"},
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
		{"https://gitlab.com/acme/other", mainBranch},
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

// ciProject is one project of ciGitLab: what GraphQL answers as its jobsEnabled, and its REST
// builds_access_level, nil when the answer has no such field.
type ciProject struct {
	jobsEnabled bool
	builds      *string
}

// ciGitLab is a stand-in GitLab with recorded answers: GET /api/v4/projects/:id shows each of
// projects with Developer access and its builds_access_level, POST /api/graphql answers the
// project's jobsEnabled for a Bearer token, and any other project is 404. It records each
// request with its GraphQL body.
func ciGitLab(t *testing.T, projects map[string]ciProject) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu       sync.Mutex
		requests []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		mu.Lock()

		requests = append(requests, strings.TrimSpace(r.Method+" "+r.URL.EscapedPath()+" "+string(body)))

		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/graphql":
			var q struct {
				Query     string            `json:"query"`
				Variables map[string]string `json:"variables"`
			}

			if r.Header.Get("Authorization") != "Bearer "+token || json.Unmarshal(body, &q) != nil || !strings.Contains(q.Query, "jobsEnabled") {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: unauthorized})

				return
			}

			p, ok := projects[q.Variables["fullPath"]]
			if !ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"project": nil}})

				return
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"project": map[string]any{"jobsEnabled": p.jobsEnabled}}})
		case r.Method == http.MethodGet && r.Header.Get("PRIVATE-TOKEN") == token:
			full, _ := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4/projects/"))

			p, ok := projects[full]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: "404 Project Not Found"})

				return
			}

			answer := map[string]any{"permissions": map[string]any{"project_access": map[string]int{"access_level": 30}}}
			if p.builds != nil {
				answer["builds_access_level"] = *p.builds
			}

			_ = json.NewEncoder(w).Encode(answer)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: unauthorized})
		}
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return slices.Clone(requests)
	}
}

// ciCase is one RepositoryAccess call of TestRepositoryAccess_CI and what it must answer: read and
// push alike, the CI check and a word of its reason.
type ciCase struct {
	name, repo string
	read, ci   bool
	reason     string
}

// check calls RepositoryAccess on c and checks the answer against tc, and that the token is in no
// CI reason.
func (tc ciCase) check(t *testing.T, c *Client) {
	t.Helper()

	got, err := c.RepositoryAccess(t.Context(), repoOf(t, "https://gitlab.com/"+tc.repo), token)
	if err != nil {
		t.Fatalf("%s: err = %v", tc.name, err)
	}

	if got.Read.OK != tc.read || got.Push.OK != tc.read || got.CI.OK != tc.ci || got.CI.Reason == "" {
		t.Errorf("%s: access = %+v; want read and push %v, CI %v with a reason", tc.name, got, tc.read, tc.ci)
	}

	if tc.reason != "" && !strings.Contains(got.CI.Reason, tc.reason) {
		t.Errorf("%s: CI reason %q; want it to say %q", tc.name, got.CI.Reason, tc.reason)
	}

	if strings.Contains(got.CI.Reason, token) {
		t.Errorf("%s: CI reason %q has the token", tc.name, got.CI.Reason)
	}
}

// A project whose jobs GraphQL shows as enabled has CI the token can read. When they are not,
// builds_access_level decides: disabled is CI that is off, and passes; any other value, or none,
// fails with the scope and role the token needs. A failed read leaves CI not checked. Read and
// Push never change with CI, and the GraphQL query goes to /api/graphql beside the REST API.
func TestRepositoryAccess_CI(t *testing.T) {
	t.Parallel()

	disabled, private := "disabled", "private"

	srv, requests := ciGitLab(t, map[string]ciProject{
		"acme/app":      {jobsEnabled: true, builds: &private},
		"acme/ci-off":   {builds: &disabled},
		"acme/hidden":   {builds: &private},
		"acme/no-field": {},
	})
	c := New(Config{APIURL: srv.URL + "/api/v4"})

	for _, tc := range []ciCase{
		{name: "jobs enabled", repo: "acme/app", read: true, ci: true},
		{name: "CI off", repo: "acme/ci-off", read: true, ci: true, reason: "CI is off for this project"},
		{name: "pipelines hidden", repo: "acme/hidden", read: true, reason: "read_api and a role that can see pipelines"},
		{name: "no builds_access_level", repo: "acme/no-field", read: true, reason: "read_api and a role that can see pipelines"},
		{name: "failed read", repo: "acme/gone", reason: "not checked: the token cannot read acme/gone"},
	} {
		tc.check(t, c)
	}

	sent := requests()
	if !slices.ContainsFunc(sent, func(r string) bool {
		return strings.HasPrefix(r, "POST /api/graphql ") && strings.Contains(r, `"fullPath":"acme/app"`)
	}) {
		t.Errorf("requests %v; want a GraphQL query of acme/app", sent)
	}

	for _, r := range sent {
		if !strings.HasPrefix(r, "GET /api/v4/projects/") && !strings.HasPrefix(r, "POST /api/graphql ") {
			t.Errorf("sent %s; want only project reads and the GraphQL query", r)
		}
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
			_ = json.NewEncoder(w).Encode(map[string]any{keyMessage: unauthorized})

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
			name: "nested group", owner: platformGroup, token: token, group: slices.Concat(levels(2, 20), levels(3, 30), levels(1, 50)),
			want: ports.OwnerAccess{Readable: 6, Pushable: 4}, reason: "6",
		},
		{name: "user namespace", owner: octo, token: token, user: levels(150, 10), want: ports.OwnerAccess{Readable: 150}},
		{
			name: "truncated", owner: platformGroup, token: token, group: levels(ports.MaxOwnerRepositories+1, 40),
			want: ports.OwnerAccess{Readable: ports.MaxOwnerRepositories, Pushable: ports.MaxOwnerRepositories, Truncated: true},
		},
		{name: "unknown owner", owner: "acme/gone", token: token, reason: "acme/gone"},
		{name: "unknown user", owner: "nobody", token: token, reason: "nobody"},
		{name: "bad token", owner: platformGroup, token: "glpat-wrong", group: levels(1, 30), reason: unauthorized},
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
// refused before anything is sent, and a token GitLab echoes is in no reason.
func TestOwnerAccess_Refusals(t *testing.T) {
	t.Parallel()

	srv, paths := ownerGitLab(t, levels(1, 30), nil)
	c := New(Config{APIURL: srv.URL})

	for _, bad := range []struct {
		host         domain.CodeHost
		token, owner string
	}{
		{gitLabDotCom, "", acmeGroup},
		{gitLabDotCom, token, ""},
		{gitLabDotCom, token, "acme/.."},
		{gitLabDotCom, token, "acme//x"},
		{domain.BuiltInCodeHosts()[0], token, acmeGroup},
		{domain.CodeHost{Kind: domain.CodeHostGitLab}, token, acmeGroup},
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

	got, err := New(Config{APIURL: echo.URL}).OwnerAccess(t.Context(), gitLabDotCom, token, acmeGroup)
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
		leaked.Store(leaked.Load() || r.Header.Get("PRIVATE-TOKEN") != "")
	}))
	t.Cleanup(elsewhere.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // the redirect under test
	}))
	t.Cleanup(redirect.Close)

	got, err := New(Config{APIURL: redirect.URL}).OwnerAccess(t.Context(), gitLabDotCom, token, acmeGroup)
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
		if _, err := New(Config{APIURL: api}).OwnerAccess(t.Context(), gitLabDotCom, token, acmeGroup); err == nil {
			t.Errorf("a host with %s: err = nil; want an error", name)
		}
	}
}

// Until the gitlab part of ADR 0044 lands, Checks reads nothing and says it cannot.
func TestChecks_Unsupported(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("Checks sent %s %s; want no request", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	_, err := New(Config{APIURL: srv.URL}).Checks(t.Context(), webRepo(t), token, headSHA, mrURL)
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("err = %v; want errors.ErrUnsupported", err)
	}
}
