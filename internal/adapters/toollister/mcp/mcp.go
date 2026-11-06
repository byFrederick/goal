// Package mcp implements ports.ToolLister with the MCP Go SDK's clients: it connects to a remote
// MCP server from goald over streamable HTTP, or starts a stdio one as a child process over the
// SDK's command transport, lists its tools with their readOnlyHint and closes the session.
package mcp

import (
	"context"
	"errors"
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

// Timeout bounds one ListTools: connecting (or starting the process), initializing and listing
// every page.
const Timeout = 10 * time.Second

var _ ports.ToolLister = (*Lister)(nil)

// Lister lists MCP servers' tools: a remote one's through its transport, which must dial public
// addresses only (goald's connection transport), following no redirect; a stdio one's from a
// child process.
type Lister struct {
	transport http.RoundTripper
	timeout   time.Duration
}

// New returns a lister sending through transport.
func New(transport http.RoundTripper) *Lister { return &Lister{transport: transport, timeout: Timeout} }

// ListTools implements ports.ToolLister.
func (l *Lister) ListTools(ctx context.Context, e ports.MCPEndpoint) ([]ports.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	if len(e.Command) > 0 {
		return l.stdio(ctx, e)
	}

	client := &http.Client{
		Transport:     headers{next: l.transport, set: maps.Clone(e.Headers)},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       l.timeout,
	}

	return list(ctx, e.URL, &sdk.StreamableClientTransport{Endpoint: e.URL, HTTPClient: client, DisableStandaloneSSE: true, MaxRetries: -1})
}

// list connects over t to the server named name, lists its tools and closes the session.
func list(ctx context.Context, name string, t sdk.Transport) ([]ports.Tool, error) {
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "goal", Version: "1"}, nil).Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", name, timedOut(ctx, err))
	}
	defer func() { _ = cs.Close() }()

	out := []ports.Tool{}

	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("tools/list of %s: %w", name, timedOut(ctx, err))
		}

		if len(out) == domain.MaxEnvironmentTestTools {
			return nil, fmt.Errorf("tools/list of %s: more than %d tools", name, domain.MaxEnvironmentTestTools)
		}

		out = append(out, ports.Tool{Name: tool.Name, ReadOnly: tool.Annotations != nil && tool.Annotations.ReadOnlyHint})
	}

	slices.SortFunc(out, func(a, b ports.Tool) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

// timedOut is err, said as the timeout when ctx's time ended.
func timedOut(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("no answer in time: %w", err)
	}

	return err
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
