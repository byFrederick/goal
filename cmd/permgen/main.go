// Command permgen generates Goal's permission table from its one source, contracts/api/permissions.yaml,
// and the x-goal-scope of every operation of contracts/api/openapi.yaml (spec 3, §8.2). `task api:gen`
// runs it; `task check` runs it with -check, which fails when a generated file is stale.
//
// It refuses to generate from a table that does not hold together: a permission with an
// unknown level or role, a fresh permission keys may carry, an operation without a scope or
// with an unknown one. Each output is a target (render function and path): the Go table for
// goald and apps/auth/'s TypeScript roles for Better Auth.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "permgen:", err)
		os.Exit(1)
	}
}

// target is one generated file: where it goes and how it is made from the table.
type target struct {
	path   string
	render func(table) ([]byte, error)
}

func run(args []string) error {
	fs := flag.NewFlagSet("permgen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	permissions := fs.String("permissions", "contracts/api/permissions.yaml", "the permission table")
	openapi := fs.String("openapi", "contracts/api/openapi.yaml", "the API contract whose operations carry x-goal-scope")
	goOut := fs.String("go", "internal/api/permissions_gen.go", "the Go table for goald")
	tsOut := fs.String("ts", "apps/auth/src/access.gen.ts", "the TypeScript roles for the auth service")
	check := fs.Bool("check", false, "fail when a generated file differs from what the sources give, writing nothing")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	targets := []target{{path: *goOut, render: renderGo}, {path: *tsOut, render: renderTS}}

	return generate(*permissions, *openapi, targets, *check)
}

// errStale reports a generated file that does not match its sources.
var errStale = errors.New("stale: run task api:gen")

func generate(permissionsPath, openapiPath string, targets []target, check bool) error {
	permissions, err := os.ReadFile(permissionsPath) //nolint:gosec // the repository file task api:gen names
	if err != nil {
		return err
	}

	openapi, err := os.ReadFile(openapiPath) //nolint:gosec // the repository file task api:gen names
	if err != nil {
		return err
	}

	t, err := load(permissions, openapi)
	if err != nil {
		return err
	}

	for _, tg := range targets {
		out, err := tg.render(t)
		if err != nil {
			return fmt.Errorf("%s: %w", tg.path, err)
		}

		if !check {
			if err := os.WriteFile(tg.path, out, 0o644); err != nil { //nolint:gosec // generated source, committed and world-readable like the rest of the tree
				return err
			}

			continue
		}

		have, err := os.ReadFile(tg.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if !bytes.Equal(have, out) {
			return fmt.Errorf("%s is %w", tg.path, errStale)
		}
	}

	return nil
}
