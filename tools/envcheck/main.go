// Command envcheck checks the environment registry, contracts/env.yaml, against the code that reads
// the environment. It reports, one line per problem:
//
//   - a read of a variable that the registry does not declare, or that is read by a process the
//     entry does not list among its readers;
//   - a declared variable that nothing reads;
//   - a variable in .env.example that the registry does not declare, or a secret there with a value
//     that looks real (not empty and not a dev-only placeholder);
//   - a Secrets Store binding in wrangler.jsonc whose entry is not classified secret;
//   - a docs/reference/environment.md that differs from the one the registry generates.
//
// It finds reads by syntax, not by running anything. Go files are parsed: a call to os.Getenv,
// os.LookupEnv, getenv, lookup or the or closure whose first argument is a string literal or a string constant
// of the same package is a read, and so is a string constant of the form "env:NAME" (a Secrets
// reference). TypeScript is matched line by line: process.env.X, env.X, env['X'], get('X') and
// required(env, 'X'). The vars and Secrets Store bindings of wrangler.jsonc are reads of the
// control Worker; ${X} in compose.yaml and Taskfile.yml are reads of compose and the Taskfile.
//
// The reader names are a fixed list: a read through a helper with another name, or by a name
// computed at run time, is not seen. Such a variable is declared with `indirect: <path>`, and counts
// as read while its name appears quoted in that file. A declared variable that is no longer read is
// always reported, so a rename shows up; a new helper does not.
//
// envcheck -write rewrites docs/reference/environment.md from the registry. `task env:check` runs it.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "envcheck:", err)
		os.Exit(1)
	}
}

var errProblems = errors.New("the environment registry does not match the code")

func run(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("envcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dir := fs.String("C", ".", "the repository")
	write := fs.Bool("write", false, "rewrite "+referenceFile+" from the registry first")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	root, err := os.OpenRoot(*dir)
	if err != nil {
		return fmt.Errorf("open the repository: %w", err)
	}
	defer func() { _ = root.Close() }() // the only write is checked where it happens

	files, err := trackedFiles(ctx, *dir)
	if err != nil {
		return err
	}

	r := repo{fsys: root.FS(), files: files}

	if *write {
		if err := writeReference(r, root); err != nil {
			return err
		}
	}

	problems, err := check(r)
	if err != nil {
		return err
	}

	for _, p := range problems {
		if _, err := fmt.Fprintln(stdout, p); err != nil {
			return err
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %d problems", errProblems, len(problems))
	}

	return nil
}

func writeReference(r repo, root *os.Root) error {
	reg, err := loadRegistry(r)
	if err != nil {
		return err
	}

	if err := root.MkdirAll(path.Dir(referenceFile), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", path.Dir(referenceFile), err)
	}

	if err := root.WriteFile(referenceFile, []byte(reference(reg)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", referenceFile, err)
	}

	return nil
}

// trackedFiles lists the checkout's tracked files (git ls-files), so build output and
// dependencies are never scanned.
func trackedFiles(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "-c", "core.quotePath=false", "ls-files", "-z") //nolint:gosec // git with this command's own arguments

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var files []string

	for f := range strings.SplitSeq(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}

	return files, nil
}
