package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
)

// envCommand runs goal env test: the tests of a factory's environment (ADR 0037 decisions 9 and
// 17), which goald runs with the secrets opened inside it, never shown.
func envCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 || args[0] != "test" {
		return errUsage
	}

	return envTest(ctx, out, getenv, args[1:])
}

// errEnvTestFailed is a test run in which a server failed; the output says which.
var errEnvTestFailed = errors.New("an MCP server failed its test")

// envTest runs goal env test <factory> [<env>] [--server <name>] [--version V]: tools/list on
// each remote MCP server of the environment (default) of the draft, or of the current version
// while no draft is open. It exits 1 when a server failed.
func envTest(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("env test")
	orgFlag := addOrgFlag(fs)
	server := fs.String("server", "", "only this MCP server")
	version := fs.String("version", "", "draft, current or a version number")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) < 1 || len(pos) > 2 {
		return errUsage
	}

	env := "default"
	if len(pos) == 2 {
		env = pos[1]
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	body := gen.EnvironmentTestRequest{Server: optional(*server), Version: optional(*version)}

	res, err := c.TestEnvironmentWithResponse(ctx, c.org, pos[0], env, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if printEnvTest(out, pos[0], *res.JSON200) {
		return errEnvTestFailed
	}

	return nil
}

// printEnvTest prints a test's servers and reports whether one failed.
func printEnvTest(out *printer, factory string, r gen.EnvironmentTestResult) bool {
	out.printf("%s %s (%s)\n", factory, r.Environment, versionWord(r.Version))

	if len(r.Servers) == 0 {
		out.printf("  no MCP servers to test\n")
	}

	failed := false

	for _, s := range r.Servers {
		out.printf("  %s\n", serverTestLine(s))

		failed = failed || s.Tested && !s.Ok
	}

	return failed
}

// versionWord is a tested version as goal env test prints it: draft, or v<number>.
func versionWord(v string) string {
	if v == "draft" {
		return v
	}

	return "v" + v
}

// serverTestLine is one server's result: its tools, why it failed, or that it was not tested.
func serverTestLine(s gen.ServerTest) string {
	switch {
	case !s.Tested:
		return fmt.Sprintf("- %-20s not tested: goald starts no stdio server", s.Server)
	case !s.Ok:
		return fmt.Sprintf("✗ %-20s %s", s.Server, deref(s.Error))
	case len(s.Tools) == 0:
		return fmt.Sprintf("✓ %-20s no tools", s.Server)
	default:
		return fmt.Sprintf("✓ %-20s %d tools: %s", s.Server, len(s.Tools), strings.Join(s.Tools, ", "))
	}
}
