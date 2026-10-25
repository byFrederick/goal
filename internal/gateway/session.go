package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/domain"
)

// sep joins a server and a tool into the name an agent sees: "knowledge__search".
const sep = "__"

// session is one request's view of a task: its claims and the knowledge sources its tools reach.
type session struct {
	g      *Gateway
	claims domain.TaskClaims
	// sources are the knowledge sources the run's version lists, which Goal's knowledge tools
	// search and read; none serves no knowledge tool.
	sources []string
}

// server is the MCP server for this request. Tools are not registered with it: tools/list and
// tools/call are answered by intercept, from what the task may call.
func (s *session) server() *mcp.Server {
	srv := mcp.NewServer(implementation, &mcp.ServerOptions{
		Instructions: "Goal gateway: Goal's own tools for this run, named <server>" + sep + "<tool>.",
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	srv.AddReceivingMiddleware(s.intercept)

	return srv
}

func (s *session) intercept(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		switch r := req.(type) {
		case *mcp.ListToolsRequest:
			return s.listTools(), nil
		case *mcp.CallToolRequest:
			return s.callTool(ctx, r.Params)
		default:
			return next(ctx, method, req)
		}
	}
}

// listTools lists the tools the task may call: the knowledge tools when its version lists sources.
func (s *session) listTools() *mcp.ListToolsResult {
	tools := []*mcp.Tool{}
	if len(s.sources) > 0 {
		tools = append(tools, knowledgeTools(s.sources)...)
	}

	// The list depends on the task's token, so it is private. The result bypasses the SDK's own
	// tools/list handler, which would fill in the cache scope; clients reject an empty one.
	return &mcp.ListToolsResult{Tools: tools, Cacheable: mcp.Cacheable{CacheScope: "private"}}
}

// callTool answers one call and records it. A tool the task may not call is a protocol error, as
// for an unknown tool.
func (s *session) callTool(ctx context.Context, p *mcp.CallToolParamsRaw) (*mcp.CallToolResult, error) {
	if tool, ok := s.knowledgeTool(p.Name); ok {
		return s.callKnowledge(ctx, tool, p.Arguments), nil
	}

	start := s.g.clock.Now()
	server, tool, _ := strings.Cut(p.Name, sep)

	s.record(ctx, start, call{connection: server, tool: tool, outcome: outcomeDenied, requestBytes: len(p.Arguments)})

	return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("tool %q is not available to this agent", p.Name)}
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
