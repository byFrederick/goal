package engine_test

import (
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// fieldClose is the output field the close tests' lines map.
const fieldClose = "close"

// closeReason is a close the tests' agents write: between 10 and 500 characters.
const closeReason = "Nothing to do: the index already exists."

// withClose is l with fieldClose mapped to close.
func withClose(l domain.Line) domain.Line {
	l.Flow.Outputs.Close = fieldClose

	return l
}

// fileFrom files an item no person filed, with origin o.
func (h *harness) fileFrom(t *testing.T, o domain.Origin) domain.Item {
	t.Helper()

	r := domain.FileRequest{FactoryID: factoryID, Origin: o, Type: typeFeature, Title: anItem}
	if o == domain.OriginFollowUp {
		r.Generation = 1
	}

	it, err := h.e.Submit(inOrg(t), r)
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// An accepted output with a close cancels a trigger's or a follow-up's item in the transition that
// accepts it, with lease outcome advanced and the agent as the actor: the item does not move on.
func TestEngine_FinishClosesTriggerAndFollowUpItems(t *testing.T) {
	t.Parallel()

	for _, o := range []domain.Origin{domain.OriginTrigger, domain.OriginFollowUp} {
		t.Run(string(o), func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, withClose(lineOf(agentStation(stBuild, agWorker, 1, 1), agentStation(stReview, agReviewer, 1, 1))),
				byAgent(map[string]string{agWorker: `{"close":"  ` + closeReason + ` "}`, agReviewer: `{}`}))
			it := h.fileFrom(t, o)

			h.step(t)

			got := h.item(t, it.ID)
			if got.Station != stBuild || got.State != domain.StateCancelled {
				t.Fatalf("item at %s/%s; want cancelled at %s", got.Station, got.State, stBuild)
			}

			if outs := h.outcomes(it.ID); len(outs) != 1 || outs[0] != domain.LeaseAdvanced {
				t.Errorf("lease outcomes %v; want one advanced", outs)
			}

			evs := h.eventsOf(it.ID, domain.EventCancelled)
			if len(evs) != 1 {
				t.Fatalf("events %v; want one cancelled", h.kinds(it.ID))
			}

			ev := evs[0]
			if ev.Actor != agWorker || ev.Data["reason"] != closeReason || ev.Data["station"] != stBuild ||
				ev.Data["agent"] != agWorker || ev.Data["closed"] != true {
				t.Errorf("cancelled event by %q with %v; want the agent closing it with the reason", ev.Actor, ev.Data)
			}

			if n := len(h.eventsOf(it.ID, domain.EventLeaseTaken)); n != 0 {
				t.Errorf("%d lease_taken events; want none, as Cancel is not called", n)
			}
		})
	}
}

// A close is refused, and the item stays open, on a person's item, on a part, with a plan and
// when its reason is too short or too long; an empty close or a line that maps none changes
// nothing.
func TestEngine_CloseRefused(t *testing.T) {
	t.Parallel()

	plan := `"plan":[{"key":"a","title":"A","brief":"a"}]`

	tests := []struct {
		name   string
		line   domain.Line
		origin domain.Origin
		out    []string
		part   bool
	}{
		{name: "person", line: withClose(planLine(false)), origin: domain.OriginPerson, out: []string{`{"close":"` + closeReason + `"}`}},
		{name: "part", line: withClose(planLine(true)), origin: domain.OriginTrigger, part: true, out: []string{
			`{` + plan + `}`, `{"close":"` + closeReason + `"}`,
		}},
		{name: "with a plan", line: withClose(planLine(true)), origin: domain.OriginTrigger, out: []string{`{` + plan + `,"close":"` + closeReason + `"}`}},
		{name: "too short", line: withClose(planLine(false)), origin: domain.OriginTrigger, out: []string{`{"close":"no need"}`}},
		{name: "too long", line: withClose(planLine(false)), origin: domain.OriginTrigger, out: []string{`{"close":"` + strings.Repeat("a", domain.MaxCloseReason+1) + `"}`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := &byCall{runs: map[string]int{}, out: map[string][]string{agArchitect: tt.out, agWorker: {`{}`}}}
			h := newHarness(t, tt.line, b.agent)
			it := h.fileFrom(t, tt.origin)

			h.step(t)

			if tt.part {
				ps := h.parts(t, it.ID)
				if len(ps.Items) != 1 {
					t.Fatalf("parts %+v; want one", ps)
				}

				it = ps.Items[0]

				h.step(t)
			}

			if got := h.item(t, it.ID); got.State == domain.StateCancelled || got.State == domain.StateAssembly {
				t.Fatalf("item is %s; want the close refused and the item open", got.State)
			}

			failed := h.eventsOf(it.ID, domain.EventContractFailed)
			if len(failed) == 0 {
				t.Fatalf("events %v; want the output to fail its check", h.kinds(it.ID))
			}

			if msg, _ := failed[0].Data["error"].(string); !strings.Contains(msg, "ask a person instead") {
				t.Errorf("contract_failed error %q; want it to tell the agent to ask a person", msg)
			}
		})
	}

	for _, tt := range []struct {
		name string
		line domain.Line
		out  string
	}{
		{name: "empty", line: withClose(lineOf(agentStation(stBuild, agWorker, 1, 1))), out: `{"close":" \n "}`},
		{name: "not mapped", line: lineOf(agentStation(stBuild, agWorker, 1, 1)), out: `{"close":"` + closeReason + `"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, tt.line, byAgent(map[string]string{agWorker: tt.out}))
			it := h.fileFrom(t, domain.OriginTrigger)

			h.step(t)

			if got := h.item(t, it.ID); got.State != domain.StateDone {
				t.Errorf("item is %s with events %v; want it done", got.State, h.kinds(it.ID))
			}
		})
	}
}

// A triage item closed by its agent is cancelled, so its follow-ups keep File.
func TestEngine_ClosedTriageKeepsFile(t *testing.T) {
	t.Parallel()

	b := &byCall{runs: map[string]int{}, out: map[string][]string{
		agArchitect: {`{"followups":["Look at the logs"]}`, `{"close":"` + closeReason + `"}`}, agWorker: {`{}`},
	}}
	h := newHarness(t, withClose(withFollowUps(planLine(true))), b.agent)
	src := h.submit(t, domain.NewItem{})

	h.untilDone(t, src.ID, 4)
	h.step(t)

	fs := h.followUps(t, src.ID)
	if len(fs) != 1 || fs[0].FiledItem == "" {
		t.Fatalf("follow-ups = %+v; want one in a triage item", fs)
	}

	h.clock.Advance(25 * time.Hour)

	for range 3 {
		h.step(t)
	}

	tri := h.item(t, fs[0].FiledItem)
	if !tri.Triage || tri.State != domain.StateCancelled {
		t.Fatalf("triage item is %s (triage %v); want it closed", tri.State, tri.Triage)
	}

	fs = h.followUps(t, src.ID)
	if !domain.Fileable(fs[0], tri.State, tri.Triage) {
		t.Fatalf("follow-up %+v of a closed triage item; want File", fs[0])
	}

	ada := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}
	if _, err := h.e.FileFollowUp(inOrg(t), src.ID, fs[0].ID, domain.FileRequest{By: ada, Title: "Read the logs"}); err != nil {
		t.Errorf("FileFollowUp after the close: %v", err)
	}
}
