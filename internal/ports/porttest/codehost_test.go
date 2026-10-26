package porttest_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

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
	h.Conflict(conflict, "go.mod", "internal/x.go")

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
	if !errors.Is(err, ports.ErrMergeConflict) || !errors.As(err, &mc) || !slices.Equal(mc.Files, []string{"go.mod", "internal/x.go"}) {
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
