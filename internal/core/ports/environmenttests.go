package ports

import (
	"context"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
)

// EnvironmentTests keep the latest test of each MCP server, remote or stdio, of the
// organization's factories' environments (ADR 0037 decision 9), for the organization in ctx: one
// per factory, environment and server. A test holds tool names, the names of those the server marked read-only, and a
// scrubbed error, never a secret value.
type EnvironmentTests interface {
	// Record keeps t as the latest test of its factory, environment and server, replacing the one
	// before. t must pass Check (domain.ErrInvalid); ErrNotFound for a factory the organization
	// does not have.
	Record(ctx context.Context, t domain.EnvironmentTest) error
	// List is the organization's latest tests, by factory, environment and server; only factory's
	// when it is set.
	List(ctx context.Context, factory domain.FactoryID) ([]domain.EnvironmentTest, error)
}

// MCPEndpoint is an MCP server to call, resolved: remote, at URL with the headers its requests
// carry, or stdio, started from Command (one the tool catalog ships) with Env as its whole
// environment. Headers and Env may hold secret values, so it is never logged.
type MCPEndpoint struct {
	URL     string
	Headers map[string]string
	Command []string
	Env     map[string]string
}

// String shows e without its headers' or env's values.
func (e MCPEndpoint) String() string {
	if len(e.Command) > 0 {
		return "MCPEndpoint{" + strings.Join(e.Command, " ") + "}"
	}

	return "MCPEndpoint{" + e.URL + "}"
}

// GoString is String, so %#v hides the values too.
func (e MCPEndpoint) GoString() string { return e.String() }

// Tool is one tool a server listed: its name, and whether its annotations.readOnlyHint is true.
// A tool without annotations or without the hint is not read-only, as MCP says.
type Tool struct {
	Name     string
	ReadOnly bool
}

// ToolLister asks an MCP server for its tools (MCP tools/list), from goald, within a bounded
// time: a remote one through public addresses only, following no redirects; a stdio one as a
// child process with only its endpoint's env, killed when the time ends.
type ToolLister interface {
	// ListTools connects to e (or starts it), lists its tools and returns them, sorted by name.
	// Its error may echo what the server answered or wrote to stderr, so the caller scrubs it of
	// secret values.
	ListTools(ctx context.Context, e MCPEndpoint) ([]Tool, error)
}
