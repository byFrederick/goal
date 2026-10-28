package porttest_test

import (
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const goMod = "go.mod"

// githubConn is a connection whose git login is on github.com.
func githubConn() domain.ResolvedConnection {
	return domain.ResolvedConnection{
		Connection: domain.Connection{Name: "github", Kind: "github"},
		Kind: domain.ConnectionKind{Name: "github", Spec: domain.KindSpec{
			Fields: []domain.KindField{
				{Name: "token", Secret: true, Required: true},
				{Name: "host", Default: "github.com"},
			},
			Env: map[string]string{"GH_TOKEN": "{{token}}"},
			Git: &domain.KindGit{Host: "{{host}}", Username: "x-access-token", PasswordEnv: "GH_TOKEN"},
		}},
		Values: map[string]string{"token": "test-token"},
	}
}

// The fake must update branches by the adapters' rules, or tests built on it prove nothing.
func TestCodeHost_UpdateBranch(t *testing.T) {
	t.Parallel()

	const (
		pr       = "https://github.com/acme/app/pull/7"
		conflict = "https://github.com/acme/app/pull/8"
		head     = "4b825dc6"
	)

	conn := githubConn()
	h := porttest.NewCodeHost()
	h.Add(ports.PullRequest{URL: pr, Open: true, Mergeable: true, HeadSHA: head})
	h.Add(ports.PullRequest{URL: conflict, Open: true, HeadSHA: head})
	h.Add(ports.PullRequest{URL: "https://github.com/acme/app/pull/9", HeadSHA: head})
	h.Conflict(conflict, goMod, "internal/x.go")

	// A moved head, a closed, unknown or off-host pull request: refused, nothing changes.
	for _, tt := range []struct{ url, head string }{
		{pr, "moved"},
		{"https://github.com/acme/app/pull/9", head},
		{"https://github.com/acme/app/pull/10", head},
		{"https://gitlab.example.com/acme/app/-/merge_requests/7", head},
	} {
		if err := h.UpdateBranch(t.Context(), conn, tt.url, tt.head); !errors.Is(err, ports.ErrNotMergeable) {
			t.Errorf("UpdateBranch(%s, %s) = %v; want ErrNotMergeable", tt.url, tt.head, err)
		}
	}

	err := h.UpdateBranch(t.Context(), conn, conflict, head)

	var mc *ports.MergeConflictError
	if !errors.Is(err, ports.ErrMergeConflict) || !errors.As(err, &mc) || !slices.Equal(mc.Files, []string{goMod, "internal/x.go"}) {
		t.Fatalf("UpdateBranch(conflicting) = %v; want a *MergeConflictError naming go.mod and internal/x.go", err)
	}

	if errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("a conflict %v also matches ErrNotMergeable; callers must tell them apart", err)
	}

	if err := h.UpdateBranch(t.Context(), conn, pr, head); err != nil {
		t.Fatalf("UpdateBranch = %v; want nil", err)
	}

	got, err := h.PullRequest(t.Context(), conn, pr)
	if err != nil || got.HeadSHA != "updated-"+head {
		t.Errorf("head after the update = %q, %v; want updated-%s", got.HeadSHA, err, head)
	}

	if calls := h.UpdatedBranches(); !slices.Equal(calls, []string{pr}) {
		t.Errorf("UpdatedBranches = %v; want only %s", calls, pr)
	}
}

// An up-to-date branch updates like GitHub's: nothing to merge in, the head stays, no conflict.
func TestCodeHost_UpdateBranchUpToDate(t *testing.T) {
	t.Parallel()

	const (
		pr   = "https://github.com/acme/app/pull/7"
		head = "4b825dc6"
	)

	conn := githubConn()
	h := porttest.NewCodeHost()
	h.Add(ports.PullRequest{URL: pr, Open: true, Mergeable: true, HeadSHA: head})
	h.UpToDate(pr)

	if err := h.UpdateBranch(t.Context(), conn, pr, head); err != nil {
		t.Fatalf("UpdateBranch(up to date) = %v; want nil", err)
	}

	got, err := h.PullRequest(t.Context(), conn, pr)
	if err != nil || got.HeadSHA != head {
		t.Errorf("head after updating an up-to-date branch = %q, %v; want %s unchanged", got.HeadSHA, err, head)
	}

	if calls := h.UpdatedBranches(); len(calls) != 0 {
		t.Errorf("UpdatedBranches = %v; want none: nothing was merged in", calls)
	}

	if err := h.UpdateBranch(t.Context(), conn, pr, "moved"); !errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("UpdateBranch(up to date, moved head) = %v; want ErrNotMergeable", err)
	}
}

// PendingReads makes the next reads report the mergeability as pending, then the pull request
// as it is; Reads counts every read; RefuseMerges refuses the next merges of a mergeable one.
func TestCodeHost_PendingReads(t *testing.T) {
	t.Parallel()

	const pr = "https://github.com/acme/app/pull/7"

	conn := githubConn()
	h := porttest.NewCodeHost()
	h.Add(ports.PullRequest{URL: pr, Open: true, Mergeable: true, HeadSHA: "4b825dc6"})
	h.PendingReads(pr, 2)

	var pending []bool

	for range 3 {
		got, err := h.PullRequest(t.Context(), conn, pr)
		if err != nil {
			t.Fatal(err)
		}

		pending = append(pending, got.MergeabilityPending)
	}

	if !slices.Equal(pending, []bool{true, true, false}) {
		t.Errorf("MergeabilityPending of three reads = %v; want pending twice, then known", pending)
	}

	if n := h.Reads(pr); n != 3 {
		t.Errorf("Reads = %d; want 3", n)
	}

	merge := ports.Merge{Method: "squash", HeadSHA: "4b825dc6"}

	h.RefuseMerges(pr, 1)

	if _, err := h.Merge(t.Context(), conn, pr, merge); !errors.Is(err, ports.ErrNotMergeable) {
		t.Errorf("Merge while refused = %v; want ErrNotMergeable", err)
	}

	if _, err := h.Merge(t.Context(), conn, pr, merge); err != nil {
		t.Errorf("Merge after the refusal = %v; want nil", err)
	}
}

// The fake must read heads and compare commits by the port's rules, or engine tests built on it
// prove nothing.
func TestCodeHost_BranchHeadAndCompare(t *testing.T) {
	t.Parallel()

	const repo = "https://github.com/acme/app"

	conn := githubConn()
	h := porttest.NewCodeHost()

	h.SetBranchHead(repo, "", "c3")
	h.SetBranchHead(repo, "goal/7-add-x", "f1")
	h.AddCommits(repo,
		ports.Commit{SHA: "c1", Subject: "first"},
		ports.Commit{SHA: "c2", Subject: "second", Files: []string{goMod}},
	)
	h.AddCommits(repo, ports.Commit{SHA: "c3", Subject: "third", Files: []string{"docs/a.md", goMod}})

	checkBranchHeads(t, h, conn, repo)
	checkCompare(t, h, conn, repo)
	checkCompareCap(t, h, conn)
}

func checkBranchHeads(t *testing.T, h *porttest.CodeHost, conn domain.ResolvedConnection, repo string) {
	t.Helper()

	for _, tt := range []struct{ branch, want string }{{"", "c3"}, {"goal/7-add-x", "f1"}} {
		if got, err := h.BranchHead(t.Context(), conn, repo, tt.branch); err != nil || got != tt.want {
			t.Errorf("BranchHead(%q) = %q, %v; want %q", tt.branch, got, err, tt.want)
		}
	}

	if _, err := h.BranchHead(t.Context(), conn, repo, "gone"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("BranchHead(gone) = %v; want ErrNotFound", err)
	}

	if _, err := h.BranchHead(t.Context(), conn, "https://gitlab.example.com/acme/app", ""); err == nil {
		t.Error("BranchHead off the connection's host succeeded; want refused")
	}
}

func checkCompare(t *testing.T, h *porttest.CodeHost, conn domain.ResolvedConnection, repo string) {
	t.Helper()

	got, err := h.Compare(t.Context(), conn, repo, "c1", "c3")
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Commits) != 2 || got.Commits[0].SHA != "c2" || got.Commits[1].SHA != "c3" || got.Truncated ||
		!slices.Equal(got.Commits[1].Files, []string{"docs/a.md", goMod}) {
		t.Errorf("Compare(c1, c3) = %+v; want c2 then c3 with their files", got)
	}

	if got, err := h.Compare(t.Context(), conn, repo, "c3", "c1"); err != nil || len(got.Commits) != 0 {
		t.Errorf("Compare(c3, c1) = %+v, %v; want no commits", got, err)
	}

	for _, tt := range []struct{ repo, base, head string }{
		{repo, "gone", "c3"},
		{repo, "c1", "gone"},
		{"https://github.com/acme/other", "c1", "c3"},
	} {
		if _, err := h.Compare(t.Context(), conn, tt.repo, tt.base, tt.head); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("Compare(%s, %s, %s) = %v; want ErrNotFound", tt.repo, tt.base, tt.head, err)
		}
	}
}

// checkCompareCap checks that past the cap Compare returns the oldest MaxCompareCommits, truncated.
func checkCompareCap(t *testing.T, h *porttest.CodeHost, conn domain.ResolvedConnection) {
	t.Helper()

	const long = "https://github.com/acme/long"

	for i := range ports.MaxCompareCommits + 5 {
		h.AddCommits(long, ports.Commit{SHA: "l" + strconv.Itoa(i)})
	}

	got, err := h.Compare(t.Context(), conn, long, "l0", "l"+strconv.Itoa(ports.MaxCompareCommits+4))
	if err != nil || !got.Truncated || len(got.Commits) != ports.MaxCompareCommits || got.Commits[0].SHA != "l1" {
		t.Errorf("Compare past the cap = %d commits, truncated %v, %v; want %d from l1, truncated",
			len(got.Commits), got.Truncated, err, ports.MaxCompareCommits)
	}
}
