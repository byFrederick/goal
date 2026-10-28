package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ToolApprovals records a run's call of an Ask-first connection tool and ends the run until a
// person decides (ADR 0031 decision 4); cmd/goald adapts the engine's RequestToolApproval to it.
// It reports the engine's refusals as ErrToolApprovalOpen, ErrRunEnded and ErrBadToolCall; any
// other error is a failure the run may retry.
type ToolApprovals interface {
	RequestToolApproval(ctx context.Context, c ToolCall) error
}

// ToolCall is one POST /run/tool-approval: the task's item, run and agent role, the
// connection's name, the tool's name without the connection prefix and its input as canonical
// JSON (domain.CanonicalJSON).
type ToolCall struct {
	Item       domain.ItemID
	Run        domain.RunID
	Agent      string
	Connection string
	Tool       string
	Input      json.RawMessage
}

// The refusals of ToolApprovals.
var (
	// ErrToolApprovalOpen means the item already waits for a person: an open tool approval or
	// question.
	ErrToolApprovalOpen = errors.New("item has an open tool approval or question")
	// ErrRunEnded means the item is no longer in progress: the run's lease has ended.
	ErrRunEnded = errors.New("run has ended")
	// ErrBadToolCall means a call the engine finds malformed.
	ErrBadToolCall = errors.New("bad tool call")
)

// maxToolInputBytes bounds a reported call's input as canonical JSON (ADR 0031 decision 4).
const maxToolInputBytes = 16 << 10

// toolCallBody is the body of POST /run/tool-approval.
type toolCallBody struct {
	Connection string          `json:"connection"`
	Tool       string          `json:"tool"`
	Input      json.RawMessage `json:"input"`
}

// The plain answers of POST /run/tool-approval; the worker branches on the status alone.
const (
	answerBadToolCall  = "the body is {connection, tool, input}: a connection of the run, a tool's name and its input as one JSON value"
	answerInputTooBig  = "the tool's input is over 16 KiB as canonical JSON"
	answerApprovalOpen = "the item already waits for a person: an open tool approval or question"
	answerRunEnded     = "the run has ended: its item is no longer in progress"
)

// ServeToolApproval serves POST /run/tool-approval (ADR 0031 decision 4): the run of the task
// token reports a call of an Ask-first connection tool, which ToolApprovals records with the
// token's item, run and agent before ending the run. It answers 202 when the call waits for a
// person; 401 for a token that does not verify and 403 for one whose factory, version, agent,
// station or item the organization does not have, as GET /run; 400 for a malformed body, a
// connection the token does not grant or a bad tool name; 413 for an input over 16 KiB as
// canonical JSON; 409 when the item already waits for a person or its run has ended, each with
// its own body; 503 for any other failure. The input is never logged: it may hold a secret.
func (g *Gateway) ServeToolApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	claims, err := g.authenticate(r)
	if err != nil {
		g.unauthorized(w, r, err)
		return
	}

	ctx := domain.WithOrg(r.Context(), claims.OrgID)

	call, status, reason := g.toolCall(w, r, claims)
	if status != 0 {
		g.log.WarnContext(ctx, "tool approval refused", claimAttrs(claims, "status", status, "reason", reason)...)
		http.Error(w, answerOf(status), status)

		return
	}

	attrs := claimAttrs(claims, "connection", call.Connection, "tool", call.Tool, "input_bytes", len(call.Input))

	if _, err := g.run(ctx, claims); err != nil {
		if errors.Is(err, errForbidden) || errors.Is(err, ports.ErrNotFound) {
			g.log.WarnContext(ctx, "tool approval forbidden", append(attrs, "reason", err)...)
			http.Error(w, "forbidden", http.StatusForbidden)

			return
		}

		g.log.ErrorContext(ctx, "tool approval failed", append(attrs, "error", err)...)
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)

		return
	}

	err = g.approvals.RequestToolApproval(ctx, call)

	switch {
	case errors.Is(err, ErrToolApprovalOpen):
		g.log.InfoContext(ctx, "tool approval refused", append(attrs, "status", http.StatusConflict, "reason", "open")...)
		http.Error(w, answerApprovalOpen, http.StatusConflict)
	case errors.Is(err, ErrRunEnded):
		g.log.InfoContext(ctx, "tool approval refused", append(attrs, "status", http.StatusConflict, "reason", "ended")...)
		http.Error(w, answerRunEnded, http.StatusConflict)
	case errors.Is(err, ErrBadToolCall):
		g.log.WarnContext(ctx, "tool approval refused", append(attrs, "status", http.StatusBadRequest, "reason", "bad call")...)
		http.Error(w, answerBadToolCall, http.StatusBadRequest)
	case err != nil:
		g.log.ErrorContext(ctx, "tool approval failed", append(attrs, "error", err)...)
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)
	default:
		g.log.InfoContext(ctx, "tool approval reported", attrs...)
		w.WriteHeader(http.StatusAccepted)
	}
}

// answerOf is the body of a refusal of toolCall's.
func answerOf(status int) string {
	if status == http.StatusRequestEntityTooLarge {
		return answerInputTooBig
	}

	return answerBadToolCall
}

// toolCall reads the body of r as the run of claims reports it, bounded by MaxRequestBytes, and
// checks it: one {connection, tool, input} object, a connection the token grants, a tool's name
// and an input of one JSON value of at most maxToolInputBytes as canonical JSON. A refusal is its
// status and a reason to log, which never quotes the body.
func (g *Gateway) toolCall(w http.ResponseWriter, r *http.Request, claims domain.TaskClaims) (ToolCall, int, string) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, g.cfg.MaxRequestBytes))
	dec.DisallowUnknownFields()

	var in toolCallBody

	if err := dec.Decode(&in); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return ToolCall{}, http.StatusRequestEntityTooLarge, "body over the request limit"
		}

		// The decoder's errors may quote the body, so only their kind is logged.
		return ToolCall{}, http.StatusBadRequest, fmt.Sprintf("body is not a tool call (%T)", err)
	}

	if dec.More() {
		return ToolCall{}, http.StatusBadRequest, "body holds more than one JSON value"
	}

	if domain.CheckConnectionName(in.Connection) != nil || !slices.Contains(claims.Connections, in.Connection) {
		return ToolCall{}, http.StatusBadRequest, "connection is not one the token grants"
	}

	if domain.CheckToolName(in.Tool) != nil {
		return ToolCall{}, http.StatusBadRequest, "bad tool name"
	}

	if len(bytes.TrimSpace(in.Input)) == 0 {
		return ToolCall{}, http.StatusBadRequest, "no input"
	}

	input, err := domain.CanonicalJSON(in.Input)
	if err != nil {
		return ToolCall{}, http.StatusBadRequest, "input is not one JSON value"
	}

	if len(input) > maxToolInputBytes {
		return ToolCall{}, http.StatusRequestEntityTooLarge, fmt.Sprintf("input is %d bytes as canonical JSON", len(input))
	}

	return ToolCall{
		Item: claims.ItemID, Run: claims.RunID, Agent: claims.Agent, Connection: in.Connection, Tool: in.Tool, Input: input,
	}, 0, ""
}
