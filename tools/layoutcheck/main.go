// Command layoutcheck checks that the descriptions of the repository's layout still match its
// tracked files (git ls-files). It reports, one line per problem, naming the file, the kind and
// the path:
//
//   - a top-level directory that the AGENTS.md repository map, or ADR 0013's layout and History,
//     does not name;
//   - a paths: glob in the frontmatter of a .claude/rules/*.md file that matches no tracked file;
//   - a path that AGENTS.md names, in its map or a link, that does not exist.
//
// It exits non-zero when there is a problem. `task layout:check` runs it.
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
		fmt.Fprintln(os.Stderr, "layoutcheck:", err)
		os.Exit(1)
	}
}

var errProblems = errors.New("the layout's descriptions do not match the tracked files")

func run(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("layoutcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dir := fs.String("C", ".", "the repository")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	r, err := load(ctx, *dir)
	if err != nil {
		return err
	}

	problems := check(r)

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

// load reads the tracked files and the descriptions from the checkout in dir.
func load(ctx context.Context, dir string) (repo, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "-c", "core.quotePath=false", "ls-files", "-z") //nolint:gosec // git with this command's own arguments

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return repo{}, fmt.Errorf("git ls-files: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	r := repo{rules: map[string]string{}}

	for f := range strings.SplitSeq(string(out), "\x00") {
		if f != "" {
			r.files = append(r.files, f)
		}
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return repo{}, fmt.Errorf("open the repository: %w", err)
	}
	defer func() { _ = root.Close() }() // read only: a close error loses nothing

	read := func(name string) (string, error) {
		b, err := root.ReadFile(name)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}

		return string(b), nil
	}

	if r.agents, err = read(agentsFile); err != nil {
		return repo{}, err
	}

	if r.adr, err = read(adrFile); err != nil {
		return repo{}, err
	}

	for _, f := range r.files {
		rest, ok := strings.CutPrefix(f, rulesDir)
		if !ok || strings.Contains(rest, "/") || path.Ext(rest) != ".md" {
			continue
		}

		if r.rules[f], err = read(f); err != nil {
			return repo{}, err
		}
	}

	return r, nil
}
