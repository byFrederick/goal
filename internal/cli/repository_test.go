package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/ports"
)

// The repository the CLI's repository tests name, and the token its secret holds.
const (
	cliRepo       = "https://github.com/acme/app"
	cliRepoToken  = "ghp_cli-repo-token-5b7a" //nolint:gosec // a made-up token for the fake code host
	cmdRepository = "repository"
)

// goal repository test prints the read and push checks with the host's reasons, exits 1 when one
// failed, and no output holds the token; an unknown secret is goald's 404.
func TestRepositoryTestCommand(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if out, err := g.goalIn(t, cliRepoToken+"\n", cmdSecret, verbCreate, "repo-token", "--all-factories"); err != nil {
		t.Fatalf("create = %q, %v", out, err)
	}

	g.codehost.SetAccess(cliRepo, cliRepoToken, ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "GitHub shows acme/app to the token"},
		Push: ports.AccessCheck{Reason: "GitHub grants the token no push to acme/app"},
	})

	out, err := g.goal(t, cmdRepository, "test", cliRepo, "--secret", "repo-token")
	if !errors.Is(err, errRepositoryTestFailed) {
		t.Fatalf("repository test = %v; want errRepositoryTestFailed\n%s", err, out)
	}

	expectOutput(t, "read only", out, []string{"✓ read: GitHub shows acme/app", "✗ push: GitHub grants the token no push"}, cliRepoToken)

	g.codehost.SetAccess(cliRepo, cliRepoToken, ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "GitHub shows acme/app to the token"},
		Push: ports.AccessCheck{OK: true, Reason: "GitHub grants the token push to acme/app"},
	})

	expectOutput(t, "read and push", g.mustGoal(t, cmdRepository, "test", cliRepo, "--secret", "repo-token"),
		[]string{"✓ read", "✓ push"}, cliRepoToken)

	if _, err := g.goal(t, cmdRepository, "test", cliRepo, "--secret", "gone"); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("an unknown secret = %v; want goald's refusal naming it", err)
	}

	for _, args := range [][]string{{cmdRepository}, {cmdRepository, "test"}, {cmdRepository, "test", cliRepo}, {cmdRepository, "check", cliRepo}} {
		if _, err := g.goal(t, args...); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v; want a usage error", args, err)
		}
	}
}
