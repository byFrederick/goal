// Package mcp implements ports.ToolLister with the MCP Go SDK's streamable HTTP client: it
// connects to a remote MCP server from goald, lists its tools and closes the session.
package mcp

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Timeout bounds one ListTools: connecting, initializing and listing every page.
const Timeout = 10 * time.Second

var _ ports.ToolLister = (*Lister)(nil)

// Lister lists remote MCP servers' tools through its transport, which must dial public addresses
// only (goald's connection transport); it follows no redirect.
type Lister struct {
	transport http.RoundTripper
}

// New returns a lister sending through transport.
func New(transport http.RoundTripper) *Lister { return &Lister{transport: transport} }

// ListTools implements ports.ToolLister.
func (l *Lister) ListTools(ctx context.Context, e ports.MCPEndpoint) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	client := &http.Client{
		Transport:     headers{next: l.transport, set: maps.Clone(e.Headers)},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       Timeout,
	}

	cs, err := sdk.NewClient(&sdk.Implementation{Name: "goal", Version: "1"}, nil).Connect(ctx,
		&sdk.StreamableClientTransport{Endpoint: e.URL, HTTPClient: client, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", e.URL, err)
	}
	defer func() { _ = cs.Close() }()

	names := []string{}

	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("tools/list of %s: %w", e.URL, err)
		}

		if len(names) == domain.MaxEnvironmentTestTools {
			return nil, fmt.Errorf("tools/list of %s: more than %d tools", e.URL, domain.MaxEnvironmentTestTools)
		}

		names = append(names, tool.Name)
	}

	slices.Sort(names)

	return names, nil
}

// headers sets the endpoint's headers on every request it sends.
type headers struct {
	next http.RoundTripper
	set  map[string]string
}

func (h headers) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.set {
		r.Header.Set(k, v)
	}

	return h.next.RoundTrip(r)
}
