package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/adapters/toollister/mcp"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The test binary is its own stdio MCP server: run with one of these as its first argument, it
// serves that server instead of running the tests.
const (
	// stdioAcme lists get_issue (read-only) and create_issue.
	stdioAcme = "acme-stdio"
	// stdioEnv lists one tool per variable of its environment, env_<NAME>.
	stdioEnv = "env-stdio"
	// stdioFail writes ACME_KEY's value to stderr, at length, and exits 1.
	stdioFail = "fail-stdio"
	// stdioHang writes its pid to the file its next argument names and never answers.
	stdioHang = "hang-stdio"
)

// The tools the acme servers list.
const (
	toolGet    = "get_issue"
	toolCreate = "create_issue"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		if serve, ok := map[string]func([]string) error{
			stdioAcme: serveAcme, stdioEnv: serveEnv, stdioFail: serveFail, stdioHang: serveHang,
		}[os.Args[1]]; ok {
			if err := serve(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			os.Exit(0)
		}
	}

	os.Exit(m.Run())
}

func serveTools(tools ...*sdk.Tool) error {
	s := sdk.NewServer(&sdk.Implementation{Name: "acme", Version: "1"}, nil)
	for _, tool := range tools {
		sdk.AddTool(s, tool, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{}, nil, nil
		})
	}

	return s.Run(context.Background(), &sdk.StdioTransport{})
}

func serveAcme([]string) error {
	return serveTools(&sdk.Tool{Name: toolGet, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, &sdk.Tool{Name: toolCreate})
}

func serveEnv([]string) error {
	var tools []*sdk.Tool

	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		tools = append(tools, &sdk.Tool{Name: "env_" + name})
	}

	return serveTools(tools...)
}

func serveFail([]string) error {
	return errors.New(strings.Repeat("refused key "+os.Getenv("ACME_KEY")+"; ", 1000))
}

func serveHang(args []string) error {
	if err := os.WriteFile(filepath.Clean(args[0]), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil { //nolint:gosec // the test's own temp file
		return err
	}

	select {}
}

// command is the test binary started as stdio server mode, with args.
func command(t *testing.T, mode string, args ...string) []string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	return append([]string{exe, mode}, args...)
}

// A stdio server is started from its command, and its tools come back sorted with their
// readOnlyHint; one that fails says what it wrote to stderr, bounded; one that does not answer
// within the time is killed.
func TestListTools_Stdio(t *testing.T) {
	t.Parallel()

	t.Run("tools", func(t *testing.T) {
		t.Parallel()

		got, err := mcp.New(nil).ListTools(t.Context(), ports.MCPEndpoint{Command: command(t, stdioAcme)})
		if err != nil {
			t.Fatal(err)
		}

		if want := []ports.Tool{{Name: toolCreate}, {Name: toolGet, ReadOnly: true}}; !slices.Equal(got, want) {
			t.Errorf("tools = %+v; want %+v", got, want)
		}
	})

	t.Run("stderr", func(t *testing.T) {
		t.Parallel()

		_, err := mcp.New(nil).ListTools(t.Context(), ports.MCPEndpoint{Command: command(t, stdioFail), Env: map[string]string{"ACME_KEY": "test-token"}})
		if err == nil || !strings.Contains(err.Error(), "refused key test-token") {
			t.Fatalf("ListTools of a failing server = %v; want its stderr", err)
		}

		if n := len(err.Error()); n > 4<<10 {
			t.Errorf("error is %d bytes; want stderr bounded", n)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()

		pidFile := filepath.Join(t.TempDir(), "pid")

		_, err := mcp.NewWithTimeout(nil, time.Second).ListTools(t.Context(), ports.MCPEndpoint{Command: command(t, stdioHang, pidFile)})
		if err == nil {
			t.Fatal("ListTools of a server that never answers succeeded")
		}

		b, err := os.ReadFile(pidFile) //nolint:gosec // the test's own temp file
		if err != nil {
			t.Fatal(err)
		}

		pid, _ := strconv.Atoi(string(b))
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("signal 0 to the server = %v; want it gone (killed and reaped)", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		_, err := mcp.New(nil).ListTools(t.Context(), ports.MCPEndpoint{Command: []string{"goal-no-such-mcp-server", "stdio"}})
		if err == nil || !strings.Contains(err.Error(), "goal-no-such-mcp-server") {
			t.Errorf("ListTools of a missing command = %v; want an error naming it", err)
		}
	})
}

// The child gets the endpoint's env, a fixed PATH and its own HOME, and nothing of goald's own
// environment, where platform secrets live. Not parallel: it sets a variable of the test process.
func TestListTools_StdioCleanEnv(t *testing.T) {
	t.Setenv("GOAL_TEST_PLATFORM_SECRET", "test-secret")

	got, err := mcp.New(nil).ListTools(t.Context(), ports.MCPEndpoint{
		Command: command(t, stdioEnv),
		Env:     map[string]string{"ACME_KEY": "test-token", "PATH": "/tmp/acme"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := []ports.Tool{{Name: "env_ACME_KEY"}, {Name: "env_HOME"}, {Name: "env_PATH"}}; !slices.Equal(got, want) {
		t.Errorf("the server's environment = %+v; want %+v only", got, want)
	}
}
