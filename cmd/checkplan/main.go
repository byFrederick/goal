// Command checkplan plans `task check:changed`: it lists the files a branch changed since its
// merge base with -base (origin/main by default), committed, staged, unstaged and untracked, and
// prints the tasks of `task check` those files need, one per line on stdout, in task check's
// order. Why each part runs goes to stderr.
//
// It never passes silently: a shared file or a path no rule knows runs everything, a missing
// base runs everything, and any other failure exits non-zero, so an empty plan always means that
// nothing needs a check.
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
	"slices"
	"strings"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "checkplan:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("checkplan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	base := fs.String("base", "origin/main", "the ref whose merge base with HEAD the branch is compared with")
	dir := fs.String("C", ".", "the repository")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	changed, err := changedFiles(ctx, *dir, *base)

	var (
		pl     plan
		header string
	)

	switch {
	case errors.Is(err, errNoBase):
		pl, header = everything(err.Error()), "cannot compare with "+*base
	case err != nil:
		return err
	case len(changed) == 0:
		_, err := fmt.Fprintf(stderr, "check:changed: nothing changed since %s\n", *base)

		return err
	default:
		pl, header = planFor(changed), fmt.Sprintf("%d changed files since %s", len(changed), *base)
	}

	var report strings.Builder

	report.WriteString("check:changed: " + header + "\n")

	for _, r := range pl.reasons {
		report.WriteString("  " + r + "\n")
	}

	if len(pl.tasks) == 0 {
		report.WriteString("  none of them needs a check\n")
	}

	for _, n := range pl.notes {
		report.WriteString("note: " + n + "\n")
	}

	if _, err := io.WriteString(stderr, report.String()); err != nil {
		return err
	}

	for _, t := range pl.tasks {
		if _, err := fmt.Fprintln(stdout, t); err != nil {
			return err
		}
	}

	return nil
}

// errNoBase reports that there is nothing to compare the branch with; the plan is then everything.
var errNoBase = errors.New("nothing to compare with")

// changedFiles lists, sorted and once each, the files changed since the merge base of base and
// HEAD: committed, staged, unstaged and untracked (not ignored). Renames count as a deletion and
// an addition, so a file moved out of a part still selects it.
func changedFiles(ctx context.Context, dir, base string) ([]string, error) {
	if _, err := git(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, err
	}

	if _, err := git(ctx, dir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		if !exitCode(err, 1) {
			return nil, err
		}

		return nil, fmt.Errorf("%w: %s not found (git fetch origin main, or set BASE)", errNoBase, base)
	}

	out, err := git(ctx, dir, "merge-base", base, "HEAD")
	if err != nil {
		if exitCode(err, 1) {
			return nil, fmt.Errorf("%w: %s and HEAD have no merge base (a shallow clone?)", errNoBase, base)
		}

		return nil, err
	}

	mergeBase := strings.TrimSpace(string(out))

	listings := [][]string{
		{"diff", "--name-only", "--no-renames", "-z", mergeBase, "HEAD"},
		{"diff", "--name-only", "--no-renames", "-z", "--cached"},
		{"diff", "--name-only", "--no-renames", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	}

	var files []string

	for _, args := range listings {
		out, err := git(ctx, dir, args...)
		if err != nil {
			return nil, err
		}

		for f := range strings.SplitSeq(string(out), "\x00") {
			if f != "" {
				files = append(files, f)
			}
		}
	}

	slices.Sort(files)

	return slices.Compact(files), nil
}

// exitCode reports whether err is a command that exited with code.
func exitCode(err error, code int) bool {
	var exit *exec.ExitError

	return errors.As(err, &exit) && exit.ExitCode() == code
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotePath=false"}, args...)...) //nolint:gosec // git with this command's own arguments

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return out, nil
}
