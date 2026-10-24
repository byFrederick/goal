package gitworkspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/cgi" //nolint:gosec // test-only host for git-http-backend; httpoxy was fixed in Go 1.6.3
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const (
	someBranch = "goal/1"
	token      = "tok-for-tests-only-6f1f3f0e"
	existing   = "goal/7-existing-work"
)

// remote serves a bare repository over git's smart HTTP protocol (git http-backend behind a
// test server) and refuses any request without the token, as a private GitHub repository does.
type remote struct {
	url    string
	host   string // host:port, as its clone URL names it
	authed atomic.Int64
}

func newRemote(t *testing.T) *remote {
	t.Helper()

	execPath, err := exec.CommandContext(t.Context(), "git", "--exec-path").Output()
	if err != nil {
		t.Skipf("git is not installed: %v", err)
	}

	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("git-http-backend is not available: %v", err)
	}

	root := t.TempDir()
	bare := filepath.Join(root, "acme.git")
	work := t.TempDir()

	run(t, "", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	run(t, "", "init", "--quiet", "--initial-branch=main", work)
	write(t, filepath.Join(work, "README.md"), "acme\n")
	run(t, work, "add", ".")
	run(t, work, "commit", "--quiet", "-m", "init")
	run(t, work, "checkout", "--quiet", "-b", existing)
	write(t, filepath.Join(work, "earlier.txt"), "work of an earlier run\n")
	run(t, work, "add", ".")
	run(t, work, "commit", "--quiet", "-m", "earlier run")
	run(t, work, "push", "--quiet", bare, "main", existing)

	r := &remote{}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	git := &cgi.Handler{
		Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}, InheritEnv: []string{"PATH"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", `Basic realm="acme"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}

		r.authed.Add(1)
		git.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)

	r.url = srv.URL + "/acme.git"
	r.host = srv.Listener.Addr().String()

	return r
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // the test's own git commands
	cmd.Dir = dir

	// baseEnv, not os.Environ: under a git hook (the pre-push hook runs these tests) GIT_DIR names
	// the pushing repository, and inheriting it would make these commands rewrite that repository.
	cmd.Env = append(baseEnv(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newWorkspaces returns workspaces that may clone from hosts (github.com when none are given).
func newWorkspaces(t *testing.T, hosts ...string) *Workspaces {
	t.Helper()

	return New(porttest.Secrets{"ACME_TOKEN": token}, Config{Root: t.TempDir(), Hosts: hosts}, nil)
}

func TestPrepare_ClonesTheItemsBranchWithoutLeavingTheToken(t *testing.T) {
	t.Parallel()

	r := newRemote(t)
	t.Cleanup(func() { // after the parallel subtests
		if r.authed.Load() == 0 {
			t.Error("no request reached the repository with the token")
		}
	})

	tests := []struct {
		name    string
		branch  string
		existed bool
		file    string
	}{
		{name: "new branch from the default branch", branch: "goal/8-new-work", file: "README.md"},
		{name: "existing branch", branch: existing, existed: true, file: "earlier.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorkspaces(t, r.host)

			ws, err := w.Prepare(t.Context(), ports.WorkspaceSpec{
				RunID: "run-1", Repository: domain.Repository{URL: r.url, AuthRef: "env:ACME_TOKEN"}, Branch: tt.branch,
			})
			if err != nil {
				t.Fatal(err)
			}

			if ws.Branch != tt.branch || ws.Existed != tt.existed || ws.Dir != filepath.Join(w.cfg.Root, "run-1", "repo") {
				t.Errorf("workspace = %+v, want branch %s (existed %v) in <root>/run-1/repo", ws, tt.branch, tt.existed)
			}

			if _, err := os.Stat(filepath.Join(ws.Dir, tt.file)); err != nil {
				t.Errorf("checkout lacks %s: %v", tt.file, err)
			}

			head, err := w.git(t.Context(), ws.Dir, nil, "symbolic-ref", "--short", "HEAD")
			if err != nil || strings.TrimSpace(head) != tt.branch {
				t.Errorf("HEAD = %q, %v; want %s", head, err, tt.branch)
			}

			noTokenUnder(t, w.cfg.Root)

			// The workspace itself has no credentials: fetching again is refused.
			if _, err := w.git(t.Context(), ws.Dir, nil, "fetch", "--quiet", "origin"); err == nil {
				t.Error("fetch from the workspace succeeded without credentials")
			}

			if err := w.Remove(t.Context(), ws); err != nil {
				t.Fatal(err)
			}

			if _, err := os.Stat(filepath.Join(w.cfg.Root, "run-1")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("run folder still there after Remove: %v", err)
			}
		})
	}
}

// noTokenUnder fails if any file under dir holds the token, raw or base64-encoded.
func noTokenUnder(t *testing.T, dir string) {
	t.Helper()

	encoded := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))

	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		body, err := os.ReadFile(p) //nolint:gosec // the test's own temp dir
		if err != nil {
			return err
		}

		if bytes.Contains(body, []byte(token)) || bytes.Contains(body, []byte(encoded)) ||
			bytes.Contains(bytes.ToLower(body), []byte("extraheader")) {
			t.Errorf("%s holds the clone credential", p)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrepare_FailsCleanlyWithoutTheToken(t *testing.T) {
	t.Parallel()

	r := newRemote(t)
	w := New(porttest.Secrets{"ACME_TOKEN": "wrong-" + token}, Config{Root: t.TempDir(), Hosts: []string{r.host}}, nil)

	_, err := w.Prepare(t.Context(), ports.WorkspaceSpec{
		RunID: "run-2", Repository: domain.Repository{URL: r.url, AuthRef: "env:ACME_TOKEN"}, Branch: "goal/9-x",
	})
	if err == nil {
		t.Fatal("clone with a wrong token succeeded")
	}

	if strings.Contains(err.Error(), "wrong-"+token) {
		t.Errorf("error %q quotes the token", err)
	}

	if _, serr := os.Stat(filepath.Join(w.cfg.Root, "run-2")); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("failed workspace left behind: %v", serr)
	}
}

func TestPrepare_RejectsUnsafeRepositoriesAndBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		url    string
		branch string
	}{
		{name: "credentials in the url", url: "https://user:pass@git.example.com/acme.git", branch: someBranch}, //nolint:gosec // a URL the adapter must refuse
		{name: "plain http to a remote host", url: "http://git.example.com/acme.git", branch: someBranch},
		{name: "ssh", url: "ssh://git@git.example.com/acme.git", branch: someBranch},
		{name: "bad branch", url: "https://git.example.com/acme.git", branch: "goal/1..x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorkspaces(t, "git.example.com")

			_, err := w.Prepare(t.Context(), ports.WorkspaceSpec{
				RunID: "run-3", Repository: domain.Repository{URL: tt.url}, Branch: tt.branch,
			})
			if err == nil {
				t.Errorf("Prepare(%s, %s) succeeded", tt.url, tt.branch)
			}
		})
	}
}

// resolveLog is a Secrets that records every reference it resolves.
type resolveLog struct {
	porttest.Secrets

	refs *atomic.Int64
}

func (s resolveLog) Resolve(ctx context.Context, ref string) (string, error) {
	s.refs.Add(1)

	return s.Secrets.Resolve(ctx, ref)
}

// A definition names the repository's URL and the install holds the token, so a clone URL on a
// host the install does not clone from (an attacker's server, here the same address on another
// port than the repository's) is refused before the token is resolved: git, which sends the
// token on its first request, never reaches that host. Without configured hosts only github.com
// is allowed, so the repository's own server is refused too.
func TestPrepare_RefusesHostsItDoesNotCloneFrom(t *testing.T) {
	t.Parallel()

	r := newRemote(t)

	var seen, authorized atomic.Int64

	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.Add(1)

		if req.Header.Get("Authorization") != "" {
			authorized.Add(1)
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(attacker.Close)

	t.Cleanup(func() { // after the parallel subtests, before the servers close
		if n, a := seen.Load(), authorized.Load(); n != 0 || a != 0 {
			t.Errorf("the other host got %d requests, %d with an Authorization header; want none", n, a)
		}

		if r.authed.Load() != 0 {
			t.Error("cloned from the repository's server although its host is not allowed")
		}
	})

	tests := []struct {
		name  string
		url   string
		hosts []string
	}{
		{name: "another host", url: attacker.URL + "/x.git", hosts: []string{r.host}},
		{name: "another host, default hosts", url: attacker.URL + "/x.git"},
		{name: "the repository's server, default hosts", url: r.url},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var resolved atomic.Int64

			w := New(resolveLog{Secrets: porttest.Secrets{"ACME_TOKEN": token}, refs: &resolved},
				Config{Root: t.TempDir(), Hosts: tt.hosts}, nil)

			_, err := w.Prepare(t.Context(), ports.WorkspaceSpec{
				RunID: "run-5", Repository: domain.Repository{URL: tt.url, AuthRef: "env:ACME_TOKEN"}, Branch: someBranch,
			})
			if err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("Prepare(%s) = %v, want the host refused", tt.url, err)
			}

			if n := resolved.Load(); n != 0 {
				t.Errorf("resolved the token %d times before refusing the host", n)
			}

			if _, serr := os.Stat(filepath.Join(w.cfg.Root, "run-5")); !errors.Is(serr, os.ErrNotExist) {
				t.Errorf("refused workspace left behind: %v", serr)
			}
		})
	}
}

// A version published before a variable was reserved may still name it as the repository's
// token: the install's SecretPolicy refuses it before it is resolved, so not even the trusted
// repository host receives goald's own secrets.
func TestPrepare_RefusesAReservedToken(t *testing.T) {
	t.Parallel()

	r := newRemote(t)

	var resolved atomic.Int64

	w := New(resolveLog{Secrets: porttest.Secrets{"GOAL_GATEWAY_KEY": token}, refs: &resolved}, Config{
		Root: t.TempDir(), Hosts: []string{r.host}, Policy: domain.SecretPolicy{Reserved: []string{"GOAL_GATEWAY_KEY"}},
	}, nil)

	_, err := w.Prepare(t.Context(), ports.WorkspaceSpec{
		RunID: "run-6", Repository: domain.Repository{URL: r.url, AuthRef: "env:GOAL_GATEWAY_KEY"}, Branch: someBranch,
	})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("Prepare = %v; want the reserved token refused", err)
	}

	if n, a := resolved.Load(), r.authed.Load(); n != 0 || a != 0 {
		t.Errorf("resolved the token %d times and sent it %d times; want neither", n, a)
	}

	if _, serr := os.Stat(filepath.Join(w.cfg.Root, "run-6")); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("refused workspace left behind: %v", serr)
	}
}

// A clone URL's host and port must equal an allowed host's, without case.
func TestCloneOrigin_MatchesAllowedHostsExactly(t *testing.T) {
	t.Parallel()

	hosts := []string{"github.com", "ghe.example.com:8443"}

	tests := []struct {
		url   string
		allow bool
	}{
		{url: "https://github.com/acme/app.git", allow: true},
		{url: "https://GitHub.com/acme/app.git", allow: true},
		{url: "https://ghe.example.com:8443/acme/app.git", allow: true},
		{url: "https://ghe.example.com/acme/app.git"},
		{url: "https://github.com.attacker.example/acme/app.git"},
		{url: "https://attacker.example/github.com/acme/app.git"},
		{url: "https://github.com:444/acme/app.git"},
		{url: "https://gist.github.com/acme/app.git"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()

			if _, err := cloneOrigin(tt.url, hosts); (err == nil) != tt.allow {
				t.Errorf("cloneOrigin(%s) = %v, want allowed %v", tt.url, err, tt.allow)
			}
		})
	}
}

func TestPrepare_EmptyWorkspaceWithoutARepository(t *testing.T) {
	t.Parallel()

	w := newWorkspaces(t)

	ws, err := w.Prepare(t.Context(), ports.WorkspaceSpec{RunID: "run-4", Branch: someBranch})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(ws.Dir)
	if err != nil || len(entries) != 0 || ws.Branch != "" {
		t.Errorf("workspace %+v holds %v, %v; want an empty folder and no branch", ws, entries, err)
	}
}

// The workspaces are leftovers of their runs, with no organization; Reap removes the given
// runs' workspaces only.
func TestReap_RemovesTheWorkspacesOfTheGivenRuns(t *testing.T) {
	t.Parallel()

	w := newWorkspaces(t)

	for _, run := range []domain.RunID{"live", "orphan"} {
		if _, err := w.Prepare(t.Context(), ports.WorkspaceSpec{RunID: run}); err != nil {
			t.Fatal(err)
		}
	}

	left, err := w.Leftovers(t.Context())
	if want := []ports.Leftover{{Run: "live"}, {Run: "orphan"}}; err != nil || !slices.Equal(left, want) {
		t.Fatalf("Leftovers = %v, %v; want %v", left, err, want)
	}

	if err := w.Reap(t.Context(), []domain.RunID{"orphan", "never-prepared", "../escape"}); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	if _, err := os.Stat(filepath.Join(w.cfg.Root, "live")); err != nil {
		t.Errorf("live workspace removed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(w.cfg.Root, "orphan")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan workspace kept: %v", err)
	}
}
