package sandboxtest

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// Script answers the suite's commands the way sh in a real sandbox would, for running the suite
// on porttest.Sandboxes. Any other command exits 127. Each call returns a script with its own
// files.
func Script() porttest.ExecFunc {
	var fs files

	answers := map[string]porttest.ExecFunc{
		scriptWrite: fs.write,
		scriptRead:  fs.read,
		scriptExit: func(_ context.Context, _ porttest.ExecCall, stdout, stderr io.Writer) int {
			_, _ = fmt.Fprintln(stdout, "out")
			_, _ = fmt.Fprintln(stderr, "oops")

			return 3
		},
		scriptOK: func(context.Context, porttest.ExecCall, io.Writer, io.Writer) int { return 0 },
		scriptStderr: func(_ context.Context, _ porttest.ExecCall, _, stderr io.Writer) int {
			_, _ = io.WriteString(stderr, strings.Repeat("0123456789abcdef\n", 1024)+"end\n")

			return 0
		},
		scriptLong: func(ctx context.Context, _ porttest.ExecCall, stdout, _ io.Writer) int {
			_, _ = fmt.Fprintln(stdout, "first")

			select {
			case <-ctx.Done():
				return 137
			case <-time.After(time.Minute):
			}

			_, _ = fmt.Fprintln(stdout, "late")

			return 0
		},
		scriptEnv: func(_ context.Context, c porttest.ExecCall, stdout, _ io.Writer) int {
			_, _ = fmt.Fprintf(stdout, "%s|%s|%s\n", c.Env[envBox], c.Env[envExec], c.Env[envOver])

			return 0
		},
		scriptPwd: func(_ context.Context, c porttest.ExecCall, stdout, _ io.Writer) int {
			_, _ = fmt.Fprintln(stdout, cmp.Or(c.Dir, "/"))

			return 0
		},
	}

	return func(ctx context.Context, c porttest.ExecCall, stdout, stderr io.Writer) int {
		var answer porttest.ExecFunc
		if len(c.Argv) == 3 && c.Argv[0] == "sh" && c.Argv[1] == "-c" {
			answer = answers[c.Argv[2]]
		}

		if answer == nil {
			_, _ = fmt.Fprintf(stderr, "sandboxtest.Script: no answer for %q\n", c.Argv)

			return 127
		}

		return answer(ctx, c, stdout, stderr)
	}
}

// files is each run's /tmp/sandboxtest.
type files struct {
	mu sync.Mutex
	m  map[domain.RunID]string
}

func (f *files) write(_ context.Context, c porttest.ExecCall, _, _ io.Writer) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.m == nil {
		f.m = map[domain.RunID]string{}
	}

	f.m[c.Sandbox.RunID] = "kept\n"

	return 0
}

func (f *files) read(_ context.Context, c porttest.ExecCall, stdout, stderr io.Writer) int {
	f.mu.Lock()
	content, ok := f.m[c.Sandbox.RunID]
	f.mu.Unlock()

	if !ok {
		_, _ = fmt.Fprintln(stderr, "cat: /tmp/sandboxtest: No such file or directory")

		return 1
	}

	_, _ = io.WriteString(stdout, content)

	return 0
}
