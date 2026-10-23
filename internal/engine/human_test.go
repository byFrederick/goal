package engine_test

import (
	"errors"
	"testing"

	"github.com/byFrederick/factory/internal/domain"
	"github.com/byFrederick/factory/internal/engine"
	"github.com/byFrederick/factory/internal/ports/porttest"
)

// humanLine is spec → approve (human) → done.
func humanLine() domain.Line {
	return lineOf(agentStation("spec", "architect", 1, 1), domain.Station{ID: stApprove, Kind: domain.KindHuman})
}

func TestHuman_ApproveAdvancesRejectSendsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reject bool
		want   domain.Position
		rework int
		event  domain.EventKind
	}{
		{name: "approve", want: domain.Position{Station: stDone, State: domain.StateDone}, event: domain.EventApproved},
		{
			name: "reject", reject: true, want: domain.Position{Station: "spec", State: domain.StateQueued},
			rework: 1, event: domain.EventRejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, humanLine(), porttest.Succeed(`{}`))
			it := h.submit(t, domain.NewItem{})
			h.step(t)

			var (
				got domain.Item
				err error
			)

			if tt.reject {
				got, err = h.e.Reject(t.Context(), it.ID, "fcruz", "split it in two")
			} else {
				got, err = h.e.Approve(t.Context(), it.ID, "fcruz")
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.Position() != tt.want || got.ReworkCount != tt.rework {
				t.Fatalf("item at %+v rework %d, want %+v rework %d", got.Position(), got.ReworkCount, tt.want, tt.rework)
			}

			events := h.eventsOf(it.ID, tt.event)
			if len(events) != 1 || events[0].Actor != "fcruz" {
				t.Fatalf("events %v, want one %s by fcruz", h.kinds(it.ID), tt.event)
			}

			if tt.reject && events[0].Data["reason"] != "split it in two" {
				t.Errorf("rejected event data %v, want the reason", events[0].Data)
			}
		})
	}
}

func TestHuman_DecisionsNeedAnItemWaitingForAHuman(t *testing.T) {
	t.Parallel()

	h := newHarness(t, humanLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{}) // still queued in the inbox

	if _, err := h.e.Approve(t.Context(), it.ID, "fcruz"); !errors.Is(err, engine.ErrNotWaitingHuman) {
		t.Errorf("Approve on a queued item: err = %v, want ErrNotWaitingHuman", err)
	}

	if _, err := h.e.Reject(t.Context(), it.ID, "fcruz", "no"); !errors.Is(err, engine.ErrNotWaitingHuman) {
		t.Errorf("Reject on a queued item: err = %v, want ErrNotWaitingHuman", err)
	}

	h.step(t)

	if _, err := h.e.Reject(t.Context(), it.ID, "fcruz", ""); err == nil {
		t.Error("Reject without a reason succeeded")
	}
}

func TestHuman_ApprovingAGateEscalationAdvancesIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t, reviewLine(0),
		byAgent(map[string]string{agWorker: `{}`, agReviewer: `{"decision":"reject","reason":"unsure"}`}))
	it := h.submit(t, domain.NewItem{})
	h.step(t)
	h.step(t)

	got, err := h.e.Approve(t.Context(), it.ID, "fcruz")
	if err != nil {
		t.Fatal(err)
	}

	if got.State != domain.StateDone {
		t.Errorf("approved escalation is %s at %s, want done", got.State, got.Station)
	}
}
