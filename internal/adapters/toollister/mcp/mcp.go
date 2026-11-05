// Package mcp implements ports.ToolLister with the MCP Go SDK's streamable HTTP client: it
// connects to a remote MCP server from goald, lists its tools with their readOnlyHint and closes
// the session.
package mcp

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
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
func (l *Lister) ListTools(ctx context.Context, e ports.MCPEndpoint) ([]ports.Tool, error) {
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

	out := []ports.Tool{}

	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("tools/list of %s: %w", e.URL, err)
		}

		if len(out) == domain.MaxEnvironmentTestTools {
			return nil, fmt.Errorf("tools/list of %s: more than %d tools", e.URL, domain.MaxEnvironmentTestTools)
		}

		out = append(out, ports.Tool{Name: tool.Name, ReadOnly: tool.Annotations != nil && tool.Annotations.ReadOnlyHint})
	}

	slices.SortFunc(out, func(a, b ports.Tool) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
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
