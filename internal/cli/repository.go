package cli

import (
	"context"
	"errors"

	"github.com/byFrederick/goal/internal/api/gen"
)

// errRepositoryTestFailed is a repository test in which a check failed or did not run; the output
// says which.
var errRepositoryTestFailed = errors.New("the repository test did not pass")

// repositoryCommand runs goal repository test: whether a secret's token can read a repository and
// push to it (ADR 0037 decision 14), asked by goald of the code host with the secret opened there.
func repositoryCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 || args[0] != "test" {
		return errUsage
	}

	return repositoryTest(ctx, out, getenv, args[1:])
}

// repositoryTest runs goal repository test <url> --secret <name>. It exits 1 when a check failed
// or the host gave no answer.
func repositoryTest(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("repository test")
	orgFlag := addOrgFlag(fs)
	secret := fs.String("secret", "", "the secret that holds the token")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *secret == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.TestRepositoryWithResponse(ctx, c.org, gen.RepositoryTestRequest{Url: pos[0], Secret: *secret})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	r := *res.JSON200

	out.printf("%s with secret %s\n", pos[0], *secret)
	out.printf("  %s read: %s\n", mark(r.Read.Ok), r.Read.Reason)
	out.printf("  %s push: %s\n", mark(r.Push.Ok), r.Push.Reason)

	if !r.Tested || !r.Read.Ok || !r.Push.Ok {
		return errRepositoryTestFailed
	}

	return nil
}

// mark is ✓ for a check that passed, ✗ for one that did not.
func mark(ok bool) string {
	if ok {
		return "✓"
	}

	return "✗"
}
