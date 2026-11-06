package cli

import (
	"bytes"
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

// goal repository test without --factory says --factory is required and exits non-zero before it
// calls goald: the goald it would call is not listening. A command line without a url or secret
// is a usage error.
func TestRepositoryTestCommand(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	env := testEnv(t, "http://127.0.0.1:1", map[string]string{envOrg: orgAcme})

	err := run(t.Context(), []string{cmdRepository, cmdTest, cliRepo, flagSecret, repoSecret}, &printer{w: &buf}, env, nil)
	if !errors.Is(err, errFactoryRequired) || !strings.Contains(err.Error(), flagFactory) {
		t.Fatalf("repository test without --factory = %v; want errFactoryRequired naming --factory\n%s", err, buf.String())
	}

	for _, args := range [][]string{{cmdRepository}, {cmdRepository, cmdTest}, {cmdRepository, cmdTest, cliRepo}, {cmdRepository, "check", cliRepo}} {
		if err := run(t.Context(), args, &printer{w: &buf}, env, nil); !errors.Is(err, errUsage) {
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
// ci, when set, is the host's CI check.
func (g *defGoald) grantApp(push bool, ci ...ports.AccessCheck) {
	a := ports.RepositoryAccess{Read: ports.AccessCheck{OK: true, Reason: appReadable}, Push: ports.AccessCheck{Reason: "GitHub grants the token no push to acme/app"}}
	if push {
		a.Push = ports.AccessCheck{OK: true, Reason: "GitHub grants the token push to acme/app"}
	}

	if len(ci) > 0 {
		a.CI = ci[0]
	}

	g.codehost.SetAccess(cliRepo, cliRepoToken, a)
}

// The fake host's CI check reasons.
const (
	ciShown   = "GitHub shows the checks and statuses of acme/app"
	ciRefused = "the token has no read access to Checks and Commit statuses in acme/app"
)

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
// (write by default): it prints the summary, the warning or note, read, push and the CI check on
// its own line, exits 1 when the token can't do what the access asks, never for CI alone, and
// goald keeps each result under the factory.
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

	g.grantApp(true, ports.AccessCheck{OK: true, Reason: ciShown})

	out, err = g.testForAcme(t, cliRepo)
	if err != nil {
		t.Fatalf("a write test that reads CI = %v\n%s", err, out)
	}

	expectOutput(t, "CI readable", out, []string{"✓ push: GitHub grants", "  ✓ ci: " + ciShown}, cliRepoToken)

	if push, ci := strings.Index(out, "push:"), strings.Index(out, "ci:"); push < 0 || ci < push {
		t.Errorf("output = %q; want the ci line after push", out)
	}

	g.grantApp(true, ports.AccessCheck{Reason: ciRefused})

	out, err = g.testForAcme(t, cliRepo)
	if err != nil {
		t.Fatalf("a write test that can't read CI = %v; want success: CI never fails the test\n%s", err, out)
	}

	expectOutput(t, "CI refused", out, []string{"Can read and push", "  ✗ ci: " + ciRefused}, cliRepoToken)

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

	if _, err := g.testRepo(t, cliRepo, flagFactory, "x", "--access", "admin"); !errors.Is(err, errUsage) {
		t.Errorf("an unknown access = %v; want a usage error", err)
	}

	if _, err := g.testRepo(t, cliRepo, "--access", "read"); !errors.Is(err, errFactoryRequired) {
		t.Errorf("--access without --factory = %v; want errFactoryRequired", err)
	}

	// An owner pattern's answer has no ci, so no ci line.
	g.codehost.SetOwnerAccess("github.com", "acme", cliRepoToken, ports.OwnerAccess{Readable: 2, Reason: "GitHub lists 2 repositories of acme"})

	out, err = g.testForAcme(t, "https://github.com/acme/*", "--access", "read")
	if err != nil {
		t.Fatalf("an owner pattern test = %v\n%s", err, out)
	}

	expectOutput(t, "owner pattern", out, []string{"Can read 2 repositories in acme", "push: GitHub lists"}, "ci:")
}
