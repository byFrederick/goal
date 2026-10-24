package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Reasons a granted connection is not served. They reach the agent and the logs, so they
// never carry a value.
const (
	reasonStdio       = "stdio (container) MCP servers are not served by this goald"
	reasonHost        = "a stdio server without an image would run on the host, which the gateway never does"
	reasonUnpinned    = "its image is not pinned to a version tag or a digest"
	reasonKind        = " connections are not served yet"
	reasonURL         = "its url is not https (plain http is allowed to loopback only)"
	reasonCredentials = "its credentials are not configured"
	reasonUpstream    = "the upstream server did not answer"
)

// metaConnections is the tools/list _meta key that reports each granted connection's status.
const metaConnections = "goal/connections"

// session is one request's view of a task: its claims, its granted connections with their
// resolved credentials, and its tool policy.
type session struct {
	g      *Gateway
	claims domain.TaskClaims
	policy policy
	conns  []*connection
	// secrets are the resolved credential values, scrubbed from everything the gateway
	// returns, logs or records.
	secrets []string
}

// connection is a granted connection: a remote server (url and headers) or a sidecar.
// unavailable, when set, says why it is not served and err holds the cause for the log.
type connection struct {
	name        string
	url         string
	headers     map[string]string
	sidecar     *ports.SidecarSpec
	unavailable string
	err         error
}

// grant prepares the connections named both by the agent and by the token, in agent order.
func (s *session) grant(ctx context.Context, all []config.Connection, agentConns []string) {
	for _, name := range agentConns {
		i := slices.IndexFunc(all, func(c config.Connection) bool { return c.Name == name })
		if i < 0 || !slices.Contains(s.claims.Connections, name) {
			continue
		}

		s.conns = append(s.conns, s.prepare(ctx, all[i]))
	}
}

func (s *session) prepare(ctx context.Context, c config.Connection) *connection {
	conn := &connection{name: c.Name, url: c.URL}

	switch {
	case c.Type == "mcp" && c.Transport == "http":
	case c.Type == "mcp":
		s.prepareSidecar(ctx, c, conn)

		return conn
	default:
		conn.unavailable = c.Type + reasonKind

		return conn
	}

	if err := checkURL(c.URL); err != nil {
		conn.unavailable, conn.err = reasonURL, err

		return conn
	}

	conn.headers = make(map[string]string, len(c.Headers))

	for name, value := range c.Headers {
		v, err := s.resolve(ctx, value)
		if err != nil {
			conn.unavailable, conn.err = reasonCredentials, fmt.Errorf("header %s: %w", name, err)

			return conn
		}

		conn.headers[name] = v
	}

	return conn
}

// prepareSidecar describes the sidecar of a stdio connection, with its env values and command
// words resolved on the host.
func (s *session) prepareSidecar(ctx context.Context, c config.Connection, conn *connection) {
	switch {
	case s.g.sidecars == nil:
		conn.unavailable = reasonStdio

		return
	case c.Image == "":
		conn.unavailable = reasonHost

		return
	case !config.Pinned(c.Image):
		conn.unavailable = reasonUnpinned

		return
	}

	spec := &ports.SidecarSpec{
		OrgID: s.claims.OrgID, FactoryID: s.claims.FactoryID, ItemID: s.claims.ItemID, RunID: s.claims.RunID, Connection: c.Name,
		Image: c.Image, Network: c.Network, Env: make(map[string]string, len(c.Env)),
	}

	for name, value := range c.Env {
		v, err := s.resolve(ctx, value)
		if err != nil {
			conn.unavailable, conn.err = reasonCredentials, fmt.Errorf("env %s: %w", name, err)

			return
		}

		spec.Env[name] = v
	}

	for i, arg := range c.Command {
		v, err := s.resolve(ctx, arg)
		if err != nil {
			conn.unavailable, conn.err = reasonCredentials, fmt.Errorf("command argument %d: %w", i+1, err)

			return
		}

		spec.Command = append(spec.Command, v)
	}

	conn.sidecar = spec
}

func (s *session) connection(name string) *connection {
	i := slices.IndexFunc(s.conns, func(c *connection) bool { return c.name == name })
	if i < 0 {
		return nil
	}

	return s.conns[i]
}

// server is the MCP server for this request. Tools are not registered with it: tools/list
// and tools/call are answered by intercept from the upstream servers.
func (s *session) server() *mcp.Server {
	srv := mcp.NewServer(implementation, &mcp.ServerOptions{
		Instructions: s.instructions(),
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	srv.AddReceivingMiddleware(s.intercept)

	return srv
}

func (s *session) instructions() string {
	var b strings.Builder

	b.WriteString("Goal gateway. Tools are named <connection>" + sep + "<tool>; credentials are added by the gateway.")

	for _, c := range s.conns {
		if c.unavailable != "" {
			fmt.Fprintf(&b, "\nConnection %s is unavailable: %s.", c.name, c.unavailable)
		}
	}

	return b.String()
}

func (s *session) intercept(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		switch r := req.(type) {
		case *mcp.ListToolsRequest:
			return s.listTools(ctx), nil
		case *mcp.CallToolRequest:
			return s.callTool(ctx, r.Params)
		default:
			return next(ctx, method, req)
		}
	}
}

// listTools lists the permitted tools of every served connection, asking the upstream servers
// in parallel. A connection that is unavailable or fails is logged and reported in _meta; it
// never fails the list.
func (s *session) listTools(ctx context.Context) *mcp.ListToolsResult {
	ctx, cancel := upstreamContext(ctx, s.g.cfg.ListTimeout)
	defer cancel()

	lists := make([][]*mcp.Tool, len(s.conns))
	errs := make([]error, len(s.conns))

	var wg sync.WaitGroup

	for i, c := range s.conns {
		if c.unavailable == "" {
			wg.Go(func() { lists[i], errs[i] = s.upstreamTools(ctx, c) })
		}
	}

	wg.Wait()

	status := make(map[string]string, len(s.conns))
	tools := []*mcp.Tool{}

	for i, c := range s.conns {
		if c.unavailable == "" && errs[i] != nil {
			c.unavailable, c.err = reasonUpstream, errs[i]
		}

		if c.unavailable != "" {
			status[c.name] = "unavailable: " + c.unavailable
			s.g.log.WarnContext(ctx, "gateway connection unavailable", claimAttrs(s.claims,
				"connection", c.name, "reason", c.unavailable, "error", s.redactErr(c.err))...)

			continue
		}

		status[c.name] = "available"

		tools = append(tools, lists[i]...)
	}

	// The list depends on the task's token, so it is private. The result bypasses the SDK's own
	// tools/list handler, which would fill in the cache scope; clients reject an empty one.
	return &mcp.ListToolsResult{
		Tools: tools, Meta: mcp.Meta{metaConnections: status}, Cacheable: mcp.Cacheable{CacheScope: "private"},
	}
}

func (s *session) upstreamTools(ctx context.Context, c *connection) ([]*mcp.Tool, error) {
	cs, done, err := s.open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer done()

	var out []*mcp.Tool

	for t, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list tools of %s: %w", c.name, err)
		}

		if s.policy.permits(c.name, t.Name) {
			named := *t
			named.Name = c.name + sep + t.Name
			out = append(out, &named)
		}
	}

	return out, nil
}

// callTool proxies one call to its upstream server and records it. A tool the agent may not
// call is a protocol error, as for an unknown tool; an upstream failure is a tool error the
// agent can read and act on.
func (s *session) callTool(ctx context.Context, p *mcp.CallToolParamsRaw) (*mcp.CallToolResult, error) {
	start := s.g.clock.Now()
	connName, tool, _ := strings.Cut(p.Name, sep)
	rec := call{connection: connName, tool: tool, requestBytes: len(p.Arguments)}

	c := s.connection(connName)
	if c == nil || c.unavailable != "" || tool == "" || !s.policy.permits(connName, tool) {
		rec.outcome = outcomeDenied
		s.record(ctx, start, rec)

		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: fmt.Sprintf("tool %q is not available to this agent", p.Name),
		}
	}

	res, size, err := s.forward(ctx, c, tool, p.Arguments)
	rec.responseBytes = size

	switch {
	case err != nil:
		rec.outcome, rec.err = outcomeFailed, s.redactErr(err)
		s.g.log.WarnContext(ctx, "gateway tool call failed", claimAttrs(s.claims,
			"connection", connName, "tool", tool, "error", rec.err)...)
		res = &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("connection %s: %s", connName, rec.err)}},
		}
	case res.IsError:
		rec.outcome = outcomeToolError
	default:
		rec.outcome = outcomeOK
	}

	s.record(ctx, start, rec)

	return res, nil
}

var errResultTooLarge = errors.New("tool result exceeds the gateway's size limit")

// learnTool lists the upstream's tools until it has seen tool. Servers on the 2026 protocol
// require Mcp-Param-* headers that mirror arguments the tool's schema annotates
// (x-mcp-header), and the SDK adds them only for tools this client session has listed. A failed
// listing is left to the call itself to report.
func learnTool(ctx context.Context, cs *mcp.ClientSession, tool string) {
	for t, err := range cs.Tools(ctx, nil) {
		if err != nil || t.Name == tool {
			return
		}
	}
}

// forward calls tool on c and returns its result with credentials scrubbed, and its size.
func (s *session) forward(ctx context.Context, c *connection, tool string, args json.RawMessage) (*mcp.CallToolResult, int, error) {
	ctx, cancel := upstreamContext(ctx, s.g.cfg.CallTimeout)
	defer cancel()

	cs, done, err := s.open(ctx, c)
	if err != nil {
		return nil, 0, err
	}
	defer done()

	params := &mcp.CallToolParams{Name: tool}
	if len(args) > 0 {
		params.Arguments = args
	}

	if c.sidecar == nil {
		learnTool(ctx, cs, tool)
	}

	res, err := cs.CallTool(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("call %s: %w", tool, err)
	}

	raw, err := json.Marshal(res)
	if err != nil {
		return nil, 0, fmt.Errorf("encode result: %w", err)
	}

	if int64(len(raw)) > s.g.cfg.MaxResponseBytes {
		return nil, len(raw), errResultTooLarge
	}

	scrubbed := new(mcp.CallToolResult)
	if err := json.Unmarshal(s.scrubJSON(raw), scrubbed); err != nil {
		return nil, len(raw), fmt.Errorf("decode result: %w", err)
	}

	return scrubbed, len(raw), nil
}

// Outcomes of a tool call in its audit event.
const (
	outcomeOK        = "ok"
	outcomeToolError = "tool_error"
	outcomeFailed    = "failed"
	outcomeDenied    = "denied"
)

// maxAuditError caps the error text kept in an audit event.
const maxAuditError = 500

// call is what an audit event keeps about one tool call: no arguments, no results.
type call struct {
	connection    string
	tool          string
	outcome       string
	err           string
	requestBytes  int
	responseBytes int
}

// record appends the call's audit event. The call has already happened, so a store failure
// is logged, not returned, and the agent's cancellation does not drop the event.
func (s *session) record(ctx context.Context, start time.Time, c call) {
	data := map[string]any{
		"connection":     c.connection,
		"tool":           c.tool,
		"outcome":        c.outcome,
		"ok":             c.outcome == outcomeOK,
		"duration_ms":    s.g.clock.Now().Sub(start).Milliseconds(),
		"request_bytes":  c.requestBytes,
		"response_bytes": c.responseBytes,
	}
	if c.err != "" {
		data["error"] = strings.ToValidUTF8(c.err[:min(len(c.err), maxAuditError)], "")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditTimeout)
	defer cancel()

	err := s.g.events.AppendEvents(ctx, domain.Event{
		FactoryID: s.claims.FactoryID, ItemID: s.claims.ItemID, RunID: s.claims.RunID, At: start,
		Kind: EventToolCall, Station: s.claims.Station, Actor: s.claims.Agent, Data: data,
	})
	if err != nil {
		s.g.log.ErrorContext(ctx, "gateway audit event lost", claimAttrs(s.claims,
			"connection", c.connection, "tool", c.tool, "error", err)...)
	}
}
