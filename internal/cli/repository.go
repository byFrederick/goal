package cli

import (
	"cmp"
	"context"
	"errors"

	"github.com/byFrederick/goal/internal/api/gen"
)

// errRepositoryTestFailed is a repository test in which a check failed or did not run; the output
// says which.
var errRepositoryTestFailed = errors.New("the repository test did not pass")

// errFactoryRequired is goal repository test without --factory: repository tests are per factory
// (ADR 0041 decision 8).
var errFactoryRequired = errors.New("goal repository test needs --factory <factory>: a repository is tested for the factory that uses it")

// repositoryCommand runs goal repository test: whether a secret's token can read a repository and
// push to it, asked by goald of the code host with the secret opened for a factory.
func repositoryCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 || args[0] != "test" {
		return errUsage
	}

	return repositoryTest(ctx, out, getenv, args[1:])
}

// repositoryTest runs goal repository test <url> --secret <name> --factory <factory> [--access
// read|write] on the factory's route (ADR 0041 decision 8), for the access its entry asks (write,
// the runtime.repository shorthand's, by default), and goald keeps the result. Without --factory
// it fails before it calls goald. It exits 1 when a check failed or the host gave no answer.
func repositoryTest(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("repository test")
	orgFlag := addOrgFlag(fs)
	secret := fs.String("secret", "", "the secret that holds the token")
	factoryRef := fs.String("factory", "", "test for this factory, which keeps the result")
	access := fs.String("access", "", "the access the factory's entry asks: read or write (default write)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *secret == "" {
		return errUsage
	}

	if *access != "" && !gen.FactoryRepositoryTestRequestAccess(*access).Valid() {
		return errUsage
	}

	if *factoryRef == "" {
		return errFactoryRequired
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	return factoryRepositoryTest(ctx, out, c, *factoryRef, gen.FactoryRepositoryTestRequest{
		Url: pos[0], Secret: *secret, Access: gen.FactoryRepositoryTestRequestAccess(cmp.Or(*access, accessWrite)),
	})
}

// accessWrite is the access a factory repository test asks for without --access.
const accessWrite = "write"

// factoryRepositoryTest tests body on the factory's route and prints the result: the summary,
// its warning or note, then both checks. It exits 1 when the token can't do what the access asks.
func factoryRepositoryTest(ctx context.Context, out *printer, c orgClient, ref string, body gen.FactoryRepositoryTestRequest) error {
	res, err := c.TestFactoryRepositoryWithResponse(ctx, c.org, ref, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	r := *res.JSON200

	out.printf("%s with secret %s for %s (%s)\n", body.Url, body.Secret, ref, body.Access)
	out.printf("  %s %s\n", mark(r.Tested && r.Ok), r.Summary)
	printTestNotes(out, "    ", r)
	out.printf("  %s read: %s\n", mark(r.Read.Ok), r.Read.Reason)
	out.printf("  %s push: %s\n", mark(r.Push.Ok), r.Push.Reason)

	if !r.Tested || !r.Ok {
		return errRepositoryTestFailed
	}

	return nil
}

// printTestNotes prints a kept repository test's warning and note, when it has them.
func printTestNotes(out *printer, indent string, r gen.FactoryRepositoryTest) {
	if r.Warning != nil {
		out.printf("%swarning: %s\n", indent, *r.Warning)
	}

	if r.Note != nil {
		out.printf("%snote: %s\n", indent, *r.Note)
	}
}

// mark is ✓ for a check that passed, ✗ for one that did not.
func mark(ok bool) string {
	if ok {
		return "✓"
	}

	return "✗"
}
