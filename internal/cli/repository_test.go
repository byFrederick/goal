package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
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

	g.grantApp(false)

	out, err := g.goal(t, cmdRepository, "test", cliRepo, "--secret", "repo-token")
	if !errors.Is(err, errRepositoryTestFailed) {
		t.Fatalf("repository test = %v; want errRepositoryTestFailed\n%s", err, out)
	}

	expectOutput(t, "read only", out, []string{"✓ read: GitHub shows acme/app", "✗ push: GitHub grants the token no push"}, cliRepoToken)

	g.grantApp(true)

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

// The secret the repository tests keep cliRepoToken in, and what the fake host says of it.
const (
	repoSecret  = "repo-token"
	flagSecret  = "--secret"
	flagFactory = "--factory"
	appReadable = "GitHub shows acme/app to the token"
)

// createRepoSecret keeps cliRepoToken as repoSecret, open to all factories.
func (g *defGoald) createRepoSecret(t *testing.T) {
	t.Helper()

	if out, err := g.goalIn(t, cliRepoToken+"\n", cmdSecret, verbCreate, repoSecret, "--all-factories"); err != nil {
		t.Fatalf("create = %q, %v", out, err)
	}
}

// grantApp makes the fake host answer that cliRepoToken reads acme/app, and pushes when push.
func (g *defGoald) grantApp(push bool) {
	a := ports.RepositoryAccess{Read: ports.AccessCheck{OK: true, Reason: appReadable}, Push: ports.AccessCheck{Reason: "GitHub grants the token no push to acme/app"}}
	if push {
		a.Push = ports.AccessCheck{OK: true, Reason: "GitHub grants the token push to acme/app"}
	}

	g.codehost.SetAccess(cliRepo, cliRepoToken, a)
}

// testRepo runs goal repository test url --secret repoSecret with args.
func (g *defGoald) testRepo(t *testing.T, url string, args ...string) (string, error) {
	t.Helper()

	return g.goal(t, append([]string{cmdRepository, cmdTest, url, flagSecret, repoSecret}, args...)...)
}

// testForAcme runs goal repository test url --secret repoSecret --factory acme with args.
func (g *defGoald) testForAcme(t *testing.T, url string, args ...string) (string, error) {
	t.Helper()

	return g.testRepo(t, url, append([]string{flagFactory, factoryID}, args...)...)
}

// keptTests are the repository tests goald keeps for acme.
func (g *defGoald) keptTests(t *testing.T) []gen.FactoryRepositoryTest {
	t.Helper()

	c, err := connect(t.Context(), testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.ListFactoryRepositoryTestsWithResponse(t.Context(), c.org, factoryID)
	if err != nil || res.JSON200 == nil {
		t.Fatalf("list tests = %v, %v", res, err)
	}

	return res.JSON200.Tests
}

// goal repository test --factory tests on the factory's route, for the access --access names
// (write by default): it prints the summary, the warning or note and both checks, exits 1 when the
// token can't do what the access asks, and goald keeps each result under the factory.
func TestRepositoryTestCommand_Factory(t *testing.T) {
	t.Parallel()

	g := publishedAcme(t)
	g.createRepoSecret(t)
	g.grantApp(true)

	out, err := g.testForAcme(t, cliRepo)
	if err != nil {
		t.Fatalf("a write test = %v\n%s", err, out)
	}

	expectOutput(t, "write", out, []string{cliRepo + " with secret " + repoSecret, "Can read and push", "✓ read: " + appReadable, "✓ push: GitHub grants"}, cliRepoToken, "won't push")

	out, err = g.testForAcme(t, cliRepo, "--access", "read")
	if err != nil {
		t.Fatalf("a read test = %v\n%s", err, out)
	}

	expectOutput(t, "read", out, []string{"Can read", "warning: Goal won't push, but agents could"}, cliRepoToken)

	g.grantApp(false)

	out, err = g.testForAcme(t, cliRepo)
	if !errors.Is(err, errRepositoryTestFailed) {
		t.Fatalf("a write test without push = %v; want errRepositoryTestFailed\n%s", err, out)
	}

	expectOutput(t, "write without push", out, []string{"✗ push: GitHub grants the token no push"}, cliRepoToken)

	// goald kept the last test of the url, for the access it was asked: the request carried it.
	if tests := g.keptTests(t); len(tests) != 1 || tests[0].Url != "github.com/acme/app" || tests[0].Access != accessWrite || tests[0].Ok {
		t.Errorf("kept tests = %+v; want one failed write test of github.com/acme/app", tests)
	}

	if _, err := g.testRepo(t, cliRepo, flagFactory, "nope"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("an unknown factory = %v; want goald's not found", err)
	}

	for _, args := range [][]string{{flagFactory, "x", "--access", "admin"}, {"--access", "read"}} {
		if _, err := g.testRepo(t, cliRepo, args...); !errors.Is(err, errUsage) {
			t.Errorf("goal repository test %v = %v; want a usage error", args, err)
		}
	}
}
