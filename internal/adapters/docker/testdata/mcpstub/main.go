// Command mcpstub is a stand-in for a stdio MCP server image: its probe tool reports what the
// sidecar container lets it see and do. The integration tests cross-compile it for the Docker
// host and build it into goal-mcp-stub:test (Dockerfile next to this file).
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// A server that logs its configuration: the sidecar must redact it.
	fmt.Fprintf(os.Stderr, "mcpstub starting with args %q and STUB_SECRET=%s\n", os.Args[1:], os.Getenv("STUB_SECRET"))

	srv := mcp.NewServer(&mcp.Implementation{Name: "mcpstub", Version: "1"}, nil)

	type none struct{}

	mcp.AddTool(srv, &mcp.Tool{Name: "probe"}, func(context.Context, *mcp.CallToolRequest, none) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: probe()}}}, nil, nil
	})

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// probe reports the process's user, filesystem, environment and network as key=value words.
func probe() string {
	words := []string{
		fmt.Sprintf("uid=%d", os.Getuid()),
		"rootfs=" + writable("/probe"),
		"tmp=" + writable("/tmp/probe"),
		"home=" + os.Getenv("HOME"),
		"secret=" + os.Getenv("STUB_SECRET"),
		"args=" + strings.Join(os.Args[1:], ","),
		"network=" + network(),
	}

	return strings.Join(words, " ")
}

func writable(path string) string {
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		return "readonly"
	}

	_ = os.Remove(path)

	return "writable"
}

// network is "none" when the container has only a loopback interface.
func network() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "unknown"
	}

	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback == 0 && i.Flags&net.FlagUp != 0 {
			return "attached"
		}
	}

	return "none"
}
