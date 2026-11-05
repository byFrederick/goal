package mcp

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/core/ports"
)

// StdioPath is the PATH a stdio server gets, and where its command is looked up: never goald's
// own. goald's image installs the tool catalog's stdio servers in /usr/local/bin.
const StdioPath = "/usr/local/bin:/usr/bin:/bin"

// maxStderr bounds the stderr a failed stdio server adds to its error: its last bytes.
const maxStderr = 2 << 10

// terminate is how long closing waits for a stdio server to exit after its stdin closes, and
// again after SIGTERM, before it is killed.
const terminate = time.Second

// stdio starts e's command as a child process with only e's env, a fixed PATH and a new empty
// HOME, lists its tools over its stdin and stdout, and stops it. The process is killed when ctx
// ends, and reaped before stdio returns.
func (l *Lister) stdio(ctx context.Context, e ports.MCPEndpoint) ([]ports.Tool, error) {
	name := e.Command[0]

	path, err := lookPath(name)
	if err != nil {
		return nil, err
	}

	home, err := os.MkdirTemp("", "goal-mcp-")
	if err != nil {
		return nil, fmt.Errorf("stdio server %s: home: %w", name, err)
	}
	defer func() { _ = os.RemoveAll(home) }()

	stderr := &lastBytes{max: maxStderr}

	cmd := exec.CommandContext(ctx, path, e.Command[1:]...)
	cmd.Env = stdioEnv(e.Env, home)
	cmd.Dir = home
	cmd.Stderr = stderr
	cmd.WaitDelay = terminate

	tools, err := list(ctx, name, &sdk.CommandTransport{Command: cmd, TerminateDuration: terminate})
	if err != nil {
		if out := strings.TrimSpace(stderr.String()); out != "" {
			return nil, fmt.Errorf("%w; stderr: %s", err, out)
		}

		return nil, err
	}

	return tools, nil
}

// lookPath is name's path: name itself when it has a slash, or the first executable file of that
// name in StdioPath's directories.
func lookPath(name string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}

	for _, dir := range filepath.SplitList(StdioPath) {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}

	return "", fmt.Errorf("stdio server %s: not installed in %s", name, StdioPath)
}

// stdioEnv is env with PATH and HOME set to StdioPath and home, as KEY=value lines by key.
func stdioEnv(env map[string]string, home string) []string {
	all := maps.Clone(env)
	if all == nil {
		all = map[string]string{}
	}

	all["PATH"], all["HOME"] = StdioPath, home

	out := make([]string, 0, len(all))
	for _, k := range slices.Sorted(maps.Keys(all)) {
		out = append(out, k+"="+all[k])
	}

	return out
}

// lastBytes keeps the last max bytes written to it.
type lastBytes struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (w *lastBytes) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.b = append(w.b, p...)
	if over := len(w.b) - w.max; over > 0 {
		w.b = slices.Delete(w.b, 0, over)
	}

	return len(p), nil
}

func (w *lastBytes) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return strings.ToValidUTF8(string(w.b), "")
}
