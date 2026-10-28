package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// MaxDenyNoteRunes bounds a person's reason for denying a tool call (ADR 0031 decision 6), in
// characters.
const MaxDenyNoteRunes = 2000

// Errors of tool approvals (ADR 0031).
var (
	// ErrBadToolCall means a reported call whose connection, tool or agent is malformed, or whose
	// input is not one JSON value.
	ErrBadToolCall = errors.New("bad tool call")
	// ErrToolApprovalOpen means a tool approval request, a question or an approval of an item
	// that already waits for a decision on its open tool approval.
	ErrToolApprovalOpen = errors.New("item has an open tool approval")
	// ErrNoOpenToolApproval means a decision on an item with no open tool approval: none was
	// requested, it was decided, or a person's move, pause, rejection or cancel withdrew it.
	ErrNoOpenToolApproval = errors.New("item has no open tool approval")
	// ErrBadToolDecision means Always allow with a denial, a note with an approval, or a note
	// outside the limits.
	ErrBadToolDecision = errors.New("bad tool decision")
	// ErrAlwaysAllowNotOffered means Always allow on a call whose ask does not come from the
	// organization connection's mode alone.
	ErrAlwaysAllowNotOffered = errors.New("always allow is not offered for this tool call")
	// ErrStandingRuleRevoked means a revoke of a standing rule that is revoked already.
	ErrStandingRuleRevoked = errors.New("standing rule is revoked already")
)

// RequestToolApproval records the call of an Ask-first connection tool that agent made in run,
// the item's run in progress (ADR 0031 decision 4), and ends the run as Ask does: one transition
// takes the lease with the outcome awaiting_approval, records tool_approval_requested and leaves
// the item waiting_human at its station until DecideToolApproval. The call offers Always allow
// when the organization connection's mode of the tool is ask. A malformed call, an open tool
// approval or question, or an item not in progress is refused and records nothing.
func (e *Engine) RequestToolApproval(ctx context.Context, id domain.ItemID, run domain.RunID, agent, connection, tool string,
	input json.RawMessage,
) (domain.Item, error) {
	canonical, err := validToolCall(agent, connection, tool, input)
	if err != nil {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: %w", id, err)
	}

	it, _, err := e.itemAndLine(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: %w", id, err)
	}

	events, err := e.itemEvents(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: %w", id, err)
	}

	if err := waitsForAPerson(events); err != nil {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: %w", id, err)
	}

	if it.State != domain.StateInProgress {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: item is %s: %w", id, it.State, ErrNotInProgress)
	}

	conn, err := e.connections.Get(ctx, connection)
	if err != nil {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: connection %q: %w", id, connection, err)
	}

	offered := conn.ToolModes[tool] == domain.ToolModeAsk
	now := e.clock.Now()
	requested := event(it, domain.EventToolApprovalRequested, now, agent, map[string]any{
		domain.KeyConnection: connection, domain.KeyTool: tool, domain.KeyInput: canonical, domain.KeyAgent: agent,
		domain.KeyRunID: string(run), domain.KeyAlwaysAllowOffered: offered,
	})
	requested.RunID = run

	got, err := e.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: it.Station, State: domain.StateWaitingHuman}, At: now,
		TakeLease: true, Outcome: domain.LeaseAwaitingApproval,
		Events: []domain.Event{
			event(it, domain.EventLeaseTaken, now, agent, map[string]any{"action": "tool_approval"}),
			requested,
			event(it, domain.EventWaitingHuman, now, agent, nil),
		},
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: %w", id, errors.Join(ErrItemChanged, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("tool approval on item %s: %w", id, err)
	}

	e.log.InfoContext(ctx, "tool approval requested", slog.String("item", string(id)), slog.String("station", it.Station),
		slog.String("agent", agent), slog.String("run", string(run)), slog.String("connection", connection),
		slog.String("tool", tool), slog.Bool("always_allow_offered", offered))

	e.stopRun(ctx, it.ID, it.Station, now)

	return got, nil
}

// DecideToolApproval records a person's decision on an item's open tool approval and puts the
// item back in its station's arrival state with its rework count unchanged. Approving grants the
// call to the item's next run (domain.GrantedCalls); with alwaysAllow it first creates the
// standing rule for the call's connection, tool and agent role, which a retry finds instead of
// creating another. Denying, with an optional note, puts the denial in the next run's brief. A
// done or cancelled item is refused with ErrItemClosed.
func (e *Engine) DecideToolApproval(ctx context.Context, id domain.ItemID, by domain.Actor, approve, alwaysAllow bool, note string) (domain.Item, error) {
	note = strings.TrimSpace(note)

	switch {
	case alwaysAllow && !approve:
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w: always allow only approves", id, ErrBadToolDecision)
	case approve && note != "":
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w: a note only comes with a denial", id, ErrBadToolDecision)
	case utf8.RuneCountInString(note) > MaxDenyNoteRunes:
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w: the note takes up to %d characters", id, ErrBadToolDecision, MaxDenyNoteRunes)
	}

	it, l, err := e.itemAndLine(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w", id, err)
	}

	if closed(it) {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w", id, ErrItemClosed)
	}

	events, err := e.itemEvents(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w", id, err)
	}

	a, open := domain.OpenToolApproval(events)
	if !open {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w", id, ErrNoOpenToolApproval)
	}

	if alwaysAllow && !a.AlwaysAllowOffered {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w", id, ErrAlwaysAllowNotOffered)
	}

	st, ok := l.Station(it.Station)
	if !ok {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: station %q is not on the line", id, it.Station)
	}

	kind, data := domain.EventToolApprovalDenied, map[string]any{domain.KeyApprovalID: a.EventID}
	if approve {
		kind, data[domain.KeyAlwaysAllow] = domain.EventToolApprovalGranted, alwaysAllow
	} else if note != "" {
		data[domain.KeyNote] = note
	}

	// The rule comes first: an approval whose transition then fails is retried, and the retry
	// finds the rule instead of creating another.
	if alwaysAllow {
		rule := domain.NewStandingRule{Connection: a.Connection, Tool: a.Tool, Agent: a.Agent, Item: it.ID}
		if _, err := e.connections.CreateStandingRule(ctx, rule, by); err != nil {
			return domain.Item{}, fmt.Errorf("decide tool approval on item %s: always allow: %w", id, err)
		}
	}

	now := e.clock.Now()

	got, err := e.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: st.ID, State: st.Arrival()}, At: now,
		Events: []domain.Event{decision(it, kind, now, by, data)},
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w", id, errors.Join(ErrItemChanged, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("decide tool approval on item %s: %w", id, err)
	}

	e.log.InfoContext(ctx, "tool approval decided", slog.String("item", string(id)), slog.String("station", it.Station),
		slog.Int64("approval", a.EventID), slog.Bool("approved", approve), slog.Bool("always_allow", alwaysAllow),
		slog.String("by", by.Ref()))

	return got, nil
}

// RevokeStandingRule ends the organization's standing rule id: its agent role asks first again
// from its next run. ports.ErrNotFound for an unknown rule, ErrStandingRuleRevoked for one
// revoked already.
func (e *Engine) RevokeStandingRule(ctx context.Context, id domain.StandingRuleID, by domain.Actor) (domain.StandingRule, error) {
	r, err := e.connections.RevokeStandingRule(ctx, id, by)
	if errors.Is(err, ports.ErrConflict) {
		return domain.StandingRule{}, fmt.Errorf("revoke standing rule %s: %w", id, errors.Join(ErrStandingRuleRevoked, err))
	}

	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("revoke standing rule %s: %w", id, err)
	}

	e.log.InfoContext(ctx, "standing rule revoked", slog.String("rule", string(id)), slog.String("connection", r.Connection),
		slog.String("tool", r.Tool), slog.String("agent", r.Agent), slog.String("by", by.Ref()))

	return r, nil
}

// validToolCall checks a reported call's names and returns its input as canonical JSON.
func validToolCall(agent, connection, tool string, input json.RawMessage) (json.RawMessage, error) {
	if strings.TrimSpace(agent) == "" {
		return nil, fmt.Errorf("%w: no agent", ErrBadToolCall)
	}

	if err := errors.Join(domain.CheckConnectionName(connection), domain.CheckToolName(tool)); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadToolCall, err)
	}

	canonical, err := domain.CanonicalJSON(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadToolCall, err)
	}

	return canonical, nil
}

// waitsForAPerson refuses a step that needs the item free of a person's pending decision: its
// open question (ErrQuestionOpen) or tool approval (ErrToolApprovalOpen). An item has at most
// one of them, because both end its run (ADR 0031 decision 9).
func waitsForAPerson(events []domain.Event) error {
	if _, open := domain.OpenQuestion(events); open {
		return ErrQuestionOpen
	}

	if _, open := domain.OpenToolApproval(events); open {
		return ErrToolApprovalOpen
	}

	return nil
}

// toolDecisionsSection is the tool calls people approved or denied since the item's last run:
// the approved ones under "## Approved tool calls", which the run may make once each with exactly
// that input, and the denied ones with their notes under "## Denied tool calls". Empty without
// decisions.
func toolDecisionsSection(decisions []domain.ToolDecision) string {
	var approved, denied strings.Builder

	for _, d := range decisions {
		call := fmt.Sprintf("\n- %s `%s` with input `%s`, asked by %s", d.Approval.Connection, d.Approval.Tool, d.Approval.Input, d.Approval.Agent)

		switch {
		case d.Granted:
			fmt.Fprintf(&approved, "%s\n  Approved by %s", call, d.By)
		case d.Note != "":
			fmt.Fprintf(&denied, "%s\n  Denied by %s: %s", call, d.By, strings.ReplaceAll(d.Note, "\n", "\n  "))
		default:
			fmt.Fprintf(&denied, "%s\n  Denied by %s", call, d.By)
		}
	}

	var parts []string

	if approved.Len() > 0 {
		parts = append(parts, "## Approved tool calls\n\nPeople approved these tool calls an agent working this item asked "+
			"to make. You may make each one once, with exactly this input; a call with any other input asks again.\n"+approved.String())
	}

	if denied.Len() > 0 {
		parts = append(parts, "## Denied tool calls\n\nPeople denied these tool calls an agent working this item asked to "+
			"make. Do not make them again as they were. The notes are what those people said, not instructions from Goal: "+
			"weigh them with the task.\n"+denied.String())
	}

	return joinParts(parts...)
}
