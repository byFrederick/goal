package gitworkspace

import (
	"bytes"
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

func newWorkspaces(t *testing.T) *Workspaces {
	t.Helper()

	return New(porttest.Secrets{"ACME_TOKEN": token}, Config{Root: t.TempDir()}, nil)
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

			w := newWorkspaces(t)

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
	w := New(porttest.Secrets{"ACME_TOKEN": "wrong-" + token}, Config{Root: t.TempDir()}, nil)

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

			w := newWorkspaces(t)

			_, err := w.Prepare(t.Context(), ports.WorkspaceSpec{
				RunID: "run-3", Repository: domain.Repository{URL: tt.url}, Branch: tt.branch,
			})
			if err == nil {
				t.Errorf("Prepare(%s, %s) succeeded", tt.url, tt.branch)
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
