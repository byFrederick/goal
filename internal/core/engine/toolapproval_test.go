package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const (
	approvedHeading = "## Approved tool calls"
	deniedHeading   = "## Denied tool calls"
	// askTool asks first on connTracker; offTool is off there.
	askTool = "create_comment"
	offTool = "delete_issue"
)

// callInput is the input of the tests' Ask-first call, as the agent wrote it.
var callInput = json.RawMessage(`{ "issue": 7, "body": "LGTM" }`)

// askFirst sets connTracker's tools: askTool asks first and offTool is off.
func (h *harness) askFirst(t *testing.T) {
	t.Helper()

	modes := map[string]domain.ConnectionToolMode{askTool: domain.ToolModeAsk, offTool: domain.ToolModeOff}
	if _, err := h.connections.Update(inOrg(t), connTracker, domain.ConnectionPatch{ToolModes: modes}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

// requestApproval has the item's running agent call askTool, the way the gateway passes the
// call on.
func (h *harness) requestApproval(t *testing.T, e *engine.Engine, it domain.Item, run domain.RunID) domain.Item {
	t.Helper()

	got, err := e.RequestToolApproval(inOrg(t), it.ID, run, agWorker, connTracker, askTool, callInput)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// awaiting is an item whose run called askTool and ended: it waits for a person's decision.
func (h *harness) awaiting(t *testing.T) (domain.Item, domain.ToolApproval) {
	t.Helper()

	h.askFirst(t)
	it, run := h.asking(t)
	h.requestApproval(t, h.e, it, run)
	h.wait(t, h.e)

	a, ok := domain.OpenToolApproval(h.store.Events(it.ID))
	if !ok {
		t.Fatalf("events %v; want an open tool approval", h.kinds(it.ID))
	}

	return h.item(t, it.ID), a
}

// Requesting approval ends the run under the lease outcome awaiting_approval, whichever instance
// the gateway reached, and leaves the item waiting for a person with the one approval open.
func TestRequestToolApproval_EndsTheRunAndWaitsForAPerson(t *testing.T) {
	t.Parallel()

	for _, other := range []bool{false, true} {
		name := "by the instance running it"
		if other {
			name = "told through Interrupt"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
			h.askFirst(t)
			it, run := h.asking(t)

			requester := h.e
			if other {
				requester = h.engine(t, h.store)
			}

			got := h.requestApproval(t, requester, it, run)

			if other {
				h.e.Interrupt(ports.ItemNotice{
					OrgID: testOrg, FactoryID: factoryID, ItemID: it.ID, Kind: domain.EventLeaseTaken, Station: stWork,
					At: h.clock.Now(),
				})
			}

			h.wait(t, h.e) // returns only once the run was cancelled

			want := domain.Position{Station: stWork, State: domain.StateWaitingHuman}
			if got.Position() != want || h.item(t, it.ID).Position() != want {
				t.Errorf("item at %+v; want %+v", got.Position(), want)
			}

			if _, held := h.store.Lease(it.ID); held {
				t.Error("the item still holds a lease")
			}

			h.expectOutcomes(t, it.ID, domain.LeaseAwaitingApproval)

			if runs := h.runs(t, testOrg, it.ID); len(runs) != 1 || runs[0].Result.Status != domain.RunCancelled {
				t.Errorf("runs %+v; want the one run cancelled", runs)
			}

			reqs := h.eventsOf(it.ID, domain.EventToolApprovalRequested)
			if len(reqs) != 1 {
				t.Fatalf("events %v; want one tool_approval_requested", h.kinds(it.ID))
			}

			a := domain.ToolApprovalOf(reqs[0])
			if a.Connection != connTracker || a.Tool != askTool || string(a.Input) != `{"body":"LGTM","issue":7}` ||
				a.Agent != agWorker || a.RunID != run || a.Station != stWork || !a.AlwaysAllowOffered || reqs[0].RunID != run {
				t.Errorf("approval %+v (input %s); want the agent's call with its run, offering Always allow", a, a.Input)
			}

			if open, ok := domain.OpenToolApproval(h.store.Events(it.ID)); !ok || open.EventID != reqs[0].ID {
				t.Errorf("open approval %+v, %v; want the one requested", open, ok)
			}

			for k, want := range map[domain.EventKind]int{
				domain.EventLeaseTaken: 1, domain.EventWaitingHuman: 1,
				domain.EventReleased: 0, domain.EventAgentCancelled: 0, domain.EventBlocked: 0,
			} {
				if n := len(h.eventsOf(it.ID, k)); n != want {
					t.Errorf("events %v; want %d %s", h.kinds(it.ID), want, k)
				}
			}
		})
	}
}

func TestRequestToolApproval_Refusals(t *testing.T) {
	t.Parallel()

	t.Run("bad calls, a second request and an item not in progress", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
		h.askFirst(t)
		it, run := h.asking(t)

		for _, tt := range []struct {
			name, connection, tool string
			input                  json.RawMessage
			want                   error
		}{
			{name: "input not JSON", connection: connTracker, tool: askTool, input: json.RawMessage(`{`), want: engine.ErrBadToolCall},
			{name: "malformed tool", connection: connTracker, tool: "no spaces", input: callInput, want: engine.ErrBadToolCall},
			{name: "malformed connection", connection: "No Such", tool: askTool, input: callInput, want: engine.ErrBadToolCall},
			{name: "unknown connection", connection: "missing", tool: askTool, input: callInput, want: ports.ErrNotFound},
		} {
			if _, err := h.e.RequestToolApproval(inOrg(t), it.ID, run, agWorker, tt.connection, tt.tool, tt.input); !errors.Is(err, tt.want) {
				t.Errorf("%s: RequestToolApproval = %v; want %v", tt.name, err, tt.want)
			}
		}

		if n := len(h.eventsOf(it.ID, domain.EventToolApprovalRequested)); n != 0 || h.item(t, it.ID).State != domain.StateInProgress {
			t.Fatalf("events %v after refused requests; want none recorded", h.kinds(it.ID))
		}

		h.requestApproval(t, h.e, it, run)
		h.wait(t, h.e)

		before := len(h.store.Events(it.ID))
		if _, err := h.e.RequestToolApproval(inOrg(t), it.ID, run, agWorker, connTracker, askTool, callInput); !errors.Is(err, engine.ErrToolApprovalOpen) {
			t.Errorf("a second request = %v; want ErrToolApprovalOpen", err)
		}

		if _, err := h.e.Ask(inOrg(t), it.ID, run, agWorker, "Which?", nil); !errors.Is(err, engine.ErrToolApprovalOpen) {
			t.Errorf("Ask while a tool approval is open = %v; want ErrToolApprovalOpen", err)
		}

		if after := len(h.store.Events(it.ID)); after != before {
			t.Errorf("events %v; the refused requests must record nothing", h.kinds(it.ID))
		}

		queued := h.submit(t, domain.NewItem{})
		if _, err := h.e.RequestToolApproval(inOrg(t), queued.ID, run, agWorker, connTracker, askTool, callInput); !errors.Is(err, engine.ErrNotInProgress) {
			t.Errorf("a request on a queued item = %v; want ErrNotInProgress", err)
		}
	})

	t.Run("an open question", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
		h.askFirst(t)
		it, run := h.asking(t)
		h.ask(t, h.e, it, run)
		h.wait(t, h.e)

		if _, err := h.e.RequestToolApproval(inOrg(t), it.ID, run, agWorker, connTracker, askTool, callInput); !errors.Is(err, engine.ErrQuestionOpen) {
			t.Errorf("a request while a question is open = %v; want ErrQuestionOpen", err)
		}

		if n := len(h.eventsOf(it.ID, domain.EventToolApprovalRequested)); n != 0 {
			t.Errorf("events %v; want no request recorded", h.kinds(it.ID))
		}
	})
}

// Approving puts the item back in its station's arrival state without counting a rework.
func TestDecideToolApproval_ApproveResumesWithoutRework(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, a := h.awaiting(t)

	got, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, true, false, "")
	if err != nil {
		t.Fatal(err)
	}

	if want := (domain.Position{Station: stWork, State: domain.StateQueued}); got.Position() != want || got.ReworkCount != 0 {
		t.Errorf("item at %+v rework %d; want %+v without rework", got.Position(), got.ReworkCount, want)
	}

	grants := h.eventsOf(it.ID, domain.EventToolApprovalGranted)
	if len(grants) != 1 || grants[0].Data[domain.KeyApprovalID] != a.EventID || grants[0].Data[domain.KeyAlwaysAllow] != false ||
		grants[0].Actor != human.Name || grants[0].Data["actor"] != human.Ref() {
		t.Errorf("grants %+v; want one for the approval by the person", grants)
	}

	if _, ok := domain.OpenToolApproval(h.store.Events(it.ID)); ok {
		t.Error("the approval is still open after its approval")
	}

	if rules, err := h.connections.StandingRules(inOrg(t), true); err != nil || len(rules) != 0 {
		t.Errorf("standing rules %+v, %v; want none without Always allow", rules, err)
	}

	// Approving again finds nothing to decide.
	if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, true, false, ""); !errors.Is(err, engine.ErrNoOpenToolApproval) {
		t.Errorf("a second approval = %v; want ErrNoOpenToolApproval", err)
	}
}

// The run after an approval reads the granted call in its brief and is granted it; the run
// after a denial reads the denial and its note. The run after that reads neither.
func TestDecideToolApproval_DenyPutsTheDenialInTheNextBrief(t *testing.T) {
	t.Parallel()

	var h *harness

	// The item's first two runs call askTool through the engine, as the gateway passes the call
	// on, and end when that takes their lease; the runs after them succeed.
	h = newHarness(t, controlLine(), func(ctx context.Context, run porttest.ExecCall, _ io.Writer) domain.RunResult {
		ctx = domain.WithOrg(ctx, testOrg)

		runs, err := h.store.Runs(ctx, run.Sandbox.ItemID, 10)
		if err != nil || len(runs) > 2 {
			return ok(`{}`)
		}

		if _, err := h.e.RequestToolApproval(ctx, run.Sandbox.ItemID, run.Sandbox.RunID, agWorker, connTracker, askTool, callInput); err != nil {
			t.Error(err)
		}

		<-ctx.Done()

		return domain.RunResult{Status: domain.RunCancelled, Error: ctx.Err().Error()}
	})
	h.askFirst(t)
	it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

	h.step(t) // the run that asks first

	if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, true, false, ""); err != nil {
		t.Fatal(err)
	}

	h.step(t) // the run after the approval, which asks again

	approved := h.feedback(1)
	if !strings.Contains(approved, approvedHeading) || !strings.Contains(approved, connTracker+" `"+askTool+"`") ||
		!strings.Contains(approved, `{"body":"LGTM","issue":7}`) || strings.Contains(approved, deniedHeading) {
		t.Errorf("feedback %q; want the approved call", approved)
	}

	second := h.worker.Runs()[1].Sandbox.RunID
	want := []domain.GrantedCall{{Connection: connTracker, Tool: askTool, Input: json.RawMessage(`{"body":"LGTM","issue":7}`)}}
	if got := domain.GrantedCalls(h.store.Events(it.ID), second); len(got) != 1 || got[0].Tool != want[0].Tool ||
		string(got[0].Input) != string(want[0].Input) {
		t.Errorf("granted calls of the second run %+v; want %+v", got, want)
	}

	if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, false, false, "  Not on a public issue.  "); err != nil {
		t.Fatal(err)
	}

	got := h.item(t, it.ID)
	if got.Position() != (domain.Position{Station: stWork, State: domain.StateQueued}) || got.ReworkCount != 0 {
		t.Errorf("item at %+v rework %d; want it queued without rework", got.Position(), got.ReworkCount)
	}

	denials := h.eventsOf(it.ID, domain.EventToolApprovalDenied)
	if len(denials) != 1 || denials[0].Data[domain.KeyNote] != "Not on a public issue." || denials[0].Actor != human.Name {
		t.Errorf("denials %+v; want one with the trimmed note by the person", denials)
	}

	h.step(t) // the run after the denial

	denied := h.feedback(2)
	if !strings.Contains(denied, deniedHeading) || !strings.Contains(denied, "Not on a public issue.") ||
		strings.Contains(denied, approvedHeading) {
		t.Errorf("feedback %q; want the denied call with its note only", denied)
	}

	if _, err := h.e.Reject(inOrg(t), it.ID, human, "again"); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if n := len(h.worker.Runs()); n != 4 {
		t.Fatalf("%d runs; want the one after the rejection too (events %v)", n, h.kinds(it.ID))
	}

	if last := h.feedback(3); strings.Contains(last, approvedHeading) || strings.Contains(last, deniedHeading) {
		t.Errorf("feedback %q; a later run reads no decision again", last)
	}
}

func TestDecideToolApproval_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, _ := h.awaiting(t)

	if _, err := h.e.DecideToolApproval(inOrg(t), h.submit(t, domain.NewItem{}).ID, human, true, false, ""); !errors.Is(err, engine.ErrNoOpenToolApproval) {
		t.Errorf("a decision with no approval = %v; want ErrNoOpenToolApproval", err)
	}

	for _, tt := range []struct {
		name        string
		approve     bool
		alwaysAllow bool
		note        string
	}{
		{name: "always allow with deny", alwaysAllow: true},
		{name: "a note with approve", approve: true, note: "fine"},
		{name: "note too long", note: strings.Repeat("n", engine.MaxDenyNoteRunes+1)},
	} {
		if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, tt.approve, tt.alwaysAllow, tt.note); !errors.Is(err, engine.ErrBadToolDecision) {
			t.Errorf("%s: DecideToolApproval = %v; want ErrBadToolDecision", tt.name, err)
		}
	}

	if got := h.item(t, it.ID); got.State != domain.StateWaitingHuman ||
		len(h.eventsOf(it.ID, domain.EventToolApprovalGranted))+len(h.eventsOf(it.ID, domain.EventToolApprovalDenied)) != 0 {
		t.Errorf("item %+v events %v; refused decisions change nothing", got.Position(), h.kinds(it.ID))
	}

	done := h.at(t, h.submit(t, domain.NewItem{}), stDone, domain.StateDone)
	if _, err := h.e.DecideToolApproval(inOrg(t), done.ID, human, true, false, ""); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("a decision on a done item = %v; want ErrItemClosed", err)
	}

	if _, err := h.e.DecideToolApproval(context.Background(), it.ID, human, true, true, ""); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("a decision outside an organization = %v; want ErrNoOrg", err)
	}

	if _, err := h.e.Cancel(inOrg(t), it.ID, human, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, false, false, ""); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("a decision on a cancelled item = %v; want ErrItemClosed", err)
	}
}

// Always allow creates one standing rule, however often the approval is retried, and only where
// the approval offers it.
func TestDecideToolApproval_AlwaysAllowCreatesOneStandingRule(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, _ := h.awaiting(t)

	// A retry after a transition that failed: the item changed under the first attempt.
	conflicted := h.engine(t, conflicting{Store: h.store})
	if _, err := conflicted.DecideToolApproval(inOrg(t), it.ID, human, true, true, ""); !errors.Is(err, engine.ErrItemChanged) {
		t.Fatalf("the conflicted approval = %v; want ErrItemChanged", err)
	}

	if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, true, true, ""); err != nil {
		t.Fatal(err)
	}

	rules, err := h.connections.StandingRules(inOrg(t), true)
	if err != nil {
		t.Fatal(err)
	}

	if len(rules) != 1 || rules[0].Connection != connTracker || rules[0].Tool != askTool || rules[0].Agent != agWorker ||
		rules[0].ItemID != it.ID || !rules[0].Active() || rules[0].CreatedBy.Ref() != human.Ref() {
		t.Fatalf("standing rules %+v; want one active rule for the call's connection, tool and agent role", rules)
	}

	grants := h.eventsOf(it.ID, domain.EventToolApprovalGranted)
	if len(grants) != 1 || grants[0].Data[domain.KeyAlwaysAllow] != true {
		t.Errorf("grants %+v; want one with always_allow", grants)
	}

	// The rule lifts the ask for that agent role; the off tool stays off.
	conn, err := h.connections.Get(inOrg(t), connTracker)
	if err != nil {
		t.Fatal(err)
	}

	modes := domain.EffectiveToolModes(conn.ToolModes, rules, conn.ID, agWorker)
	if _, asks := modes.Modes[askTool]; asks || modes.Modes[offTool] != domain.ToolModeOff {
		t.Errorf("effective modes %+v; want %s allowed and %s off", modes, askTool, offTool)
	}
}

// A call whose ask does not come from the organization connection's mode offers no Always allow.
func TestDecideToolApproval_AlwaysAllowRefusedWhenNotOffered(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, run := h.asking(t) // connTracker's tools are all allowed: the call's ask is not the organization's

	if _, err := h.e.RequestToolApproval(inOrg(t), it.ID, run, agWorker, connTracker, askTool, callInput); err != nil {
		t.Fatal(err)
	}

	h.wait(t, h.e)

	if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, true, true, ""); !errors.Is(err, engine.ErrAlwaysAllowNotOffered) {
		t.Errorf("Always allow = %v; want ErrAlwaysAllowNotOffered", err)
	}

	if rules, _ := h.connections.StandingRules(inOrg(t), true); len(rules) != 0 {
		t.Errorf("standing rules %+v; want none", rules)
	}
}

func TestRevokeStandingRule_EndsTheRule(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, _ := h.awaiting(t)

	if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, true, true, ""); err != nil {
		t.Fatal(err)
	}

	rules, err := h.connections.StandingRules(inOrg(t), false)
	if err != nil || len(rules) != 1 {
		t.Fatalf("standing rules %+v, %v; want one", rules, err)
	}

	got, err := h.e.RevokeStandingRule(inOrg(t), rules[0].ID, human)
	if err != nil {
		t.Fatal(err)
	}

	if got.Active() || got.RevokedBy == nil || got.RevokedBy.Ref() != human.Ref() {
		t.Errorf("revoked rule %+v; want it ended by the person", got)
	}

	if active, _ := h.connections.StandingRules(inOrg(t), false); len(active) != 0 {
		t.Errorf("active rules %+v; want none", active)
	}

	if _, err := h.e.RevokeStandingRule(inOrg(t), rules[0].ID, human); !errors.Is(err, engine.ErrStandingRuleRevoked) {
		t.Errorf("revoking again = %v; want ErrStandingRuleRevoked", err)
	}

	if _, err := h.e.RevokeStandingRule(inOrg(t), "00000000-0000-4000-8000-000000000000", human); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("revoking an unknown rule = %v; want ErrNotFound", err)
	}
}

// conflicting is a store whose every transition fails as if the item changed meanwhile.
type conflicting struct {
	ports.Store
}

func (conflicting) Transition(context.Context, ports.Transition) (domain.Item, error) {
	return domain.Item{}, fmt.Errorf("transition: %w", ports.ErrConflict)
}

// The item waits for a decision on the call, not on the item.
func TestApprove_RefusedWhileAToolApprovalIsOpen(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, _ := h.awaiting(t)

	if _, err := h.e.Approve(inOrg(t), it.ID, human); !errors.Is(err, engine.ErrToolApprovalOpen) {
		t.Errorf("Approve = %v; want ErrToolApprovalOpen", err)
	}

	if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stWork, State: domain.StateWaitingHuman}) {
		t.Errorf("item at %+v; want it still waiting for the decision", got.Position())
	}
}

// Move, Pause, Reject and Cancel withdraw the open tool approval: nothing is left to decide.
func TestToolApproval_WithdrawnByAPersonsControl(t *testing.T) {
	t.Parallel()

	for name, act := range map[string]control{
		"move": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Move(ctx, id, stApprove, human, "")
		},
		"pause": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Pause(ctx, id, human, "")
		},
		"send back": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Reject(ctx, id, human, "wrong call")
		},
		"cancel": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Cancel(ctx, id, human, "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
			it, _ := h.awaiting(t)

			if _, err := act(h.e, inOrg(t), it.ID); err != nil {
				t.Fatal(err)
			}

			if _, ok := domain.OpenToolApproval(h.store.Events(it.ID)); ok {
				t.Errorf("the tool approval is still open after %s", name)
			}

			want := engine.ErrNoOpenToolApproval
			if name == "cancel" {
				want = engine.ErrItemClosed
			}

			if _, err := h.e.DecideToolApproval(inOrg(t), it.ID, human, true, false, ""); !errors.Is(err, want) {
				t.Errorf("a decision after %s = %v; want %v", name, err, want)
			}

			if n := len(h.eventsOf(it.ID, domain.EventToolApprovalGranted)) + len(h.eventsOf(it.ID, domain.EventToolApprovalDenied)); n != 0 {
				t.Errorf("events %v; a withdrawn approval records no decision", h.kinds(it.ID))
			}
		})
	}
}
