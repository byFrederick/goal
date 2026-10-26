package api_test

import (
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const leadTimePath = "/v1/orgs/acme/factories/line/metrics/lead-time?from=2026-10-17&to=2026-10-23"

// The bridge splits the gap between the actual p85 and the target among the kinds of time the
// slow items spent; the line's six-hour target comes from its definition.
func TestAPI_LeadTimeBridgeOverTarget(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// Every item waits at merge for a person: 2 h and 3 h (fast), 9, 10 and 11 h (slow).
	for i, hours := range []int{2, 3, 9, 10, 11} {
		created, done := oct(18+i, 0), oct(18+i, hours)
		h.move(t, h.seed(t, stMerge, domain.StateWaitingHuman, created), domain.Position{Station: stDone, State: domain.StateDone},
			func(tr *ports.Transition) { tr.At, tr.DoneAt = done, done })
	}

	h.finished(t, oct(10, 0), oct(10, 20), false) // before the range
	h.seed(t, stBuild, domain.StateQueued, oct(23, 8))

	var b gen.LeadTimeBridge
	if code := h.call(t, http.MethodGet, leadTimePath, "", &b); code != http.StatusOK {
		t.Fatalf("lead time = %d", code)
	}

	// p85 of 2, 3, 9, 10, 11 h is 10.4 h: 4.4 h over the target, all of it waiting on you.
	got := fmt.Sprintf("%s: target %v, actual %.1f, gap %.1f, %d done, %d over", b.Status, *b.TargetHours,
		*b.ActualP85Hours, *b.GapHours, b.ItemsDone, b.OverTargetItems)
	if want := "over_target: target 6, actual 10.4, gap 4.4, 5 done, 3 over"; got != want {
		t.Errorf("bridge = %s, want %s", got, want)
	}

	if len(b.Steps) != 1 || b.Steps[0].Component != gen.BridgeStepComponentWaitingOnYou || b.Steps[0].Station != nil ||
		math.Abs(b.Steps[0].Hours-4.4) > 1e-9 || b.Steps[0].Share != 1 || b.Steps[0].Items != 3 {
		t.Errorf("steps = %+v, want all 4.4 h waiting on you, 3 items", b.Steps)
	}

	if c := b.Constraint; c == nil || c.Station != stBuild || c.BusyRatio != nil {
		t.Errorf("constraint = %+v, want build, busy unknown before any lease", c)
	}
}

func TestAPI_LeadTimeBridgeStatuses(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for i := range 4 {
		h.finished(t, oct(18+i, 0), oct(18+i, 2), false)
	}

	var few gen.LeadTimeBridge
	if h.call(t, http.MethodGet, leadTimePath, "", &few); few.Status != gen.NotEnoughData || few.ItemsDone != 4 || len(few.Steps) != 0 {
		t.Errorf("4 items done: %+v, want not enough data", few)
	}

	h.finished(t, oct(22, 0), oct(22, 5), false)

	var on gen.LeadTimeBridge
	if h.call(t, http.MethodGet, leadTimePath, "", &on); on.Status != gen.OnTarget || on.GapHours != nil || len(on.Steps) != 0 {
		t.Errorf("5 items under the target: %+v, want on target without steps", on)
	}
}

func TestAPI_LeadTimeBridgeWithoutATargetAndBadQueries(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var none gen.LeadTimeBridge
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics/lead-time", "", &none); none.Status != gen.NoTarget ||
		none.TargetHours != nil || none.ActualP85Hours != nil || none.Constraint != nil {
		t.Errorf("a line without a target: %+v, want no target", none)
	}

	for _, query := range []string{"?to=2026-10-24", "?type=chore", "?from=2026-10-23&to=2026-10-22"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics/lead-time"+query, "", nil); code != http.StatusBadRequest {
			t.Errorf("lead time%s = %d, want 400", query, code)
		}
	}
}
