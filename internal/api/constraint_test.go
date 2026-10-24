package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// leaseHistory gives the line a lease history: build holds one of its two slots from Oct 22
// 00:00 to 06:00 for an item that waited 24 h before it; another item waits from Oct 23 00:00
// to now (09:00).
func (h *harness) leaseHistory(t *testing.T) {
	t.Helper()

	waited := h.seed(t, stBuild, domain.StateQueued, oct(21, 0))
	h.seed(t, stBuild, domain.StateQueued, oct(23, 0))

	claimed, lease, err := h.store.ClaimNext(inOrg(t), ports.Claim{
		FactoryID: lineID, Station: build, Slot: 0, Now: oct(22, 0), ExpiresAt: oct(22, 1),
	})
	if err != nil || claimed.ID != waited.ID {
		t.Fatalf("claim = %s, %v; want the item waiting longest", claimed.ID, err)
	}

	h.move(t, claimed, domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
		tr.Lease, tr.Outcome, tr.At = &lease, domain.LeaseAdvanced, oct(22, 6)
	})
}

// The candidates are measured over the range from the factory's first lease: build held one of
// its two slots 6 h of the 33 h since; two items waited 33 h in all in front of it over the
// range's 153 h. The line's definition names build its constraint (domain.Line.ConstraintOf
// picks the busiest candidate of a line that names none).
func TestAPI_ConstraintReportOverTheRange(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.leaseHistory(t)

	var r gen.ConstraintReport
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/constraint?from=2026-10-17", "", &r); code != http.StatusOK {
		t.Fatalf("constraint = %d", code)
	}

	if r.Source != gen.Configured || orZero(r.Constraint) != stBuild || orZero(r.Configured) != stBuild ||
		r.CoverageSince == nil || !r.CoverageSince.Equal(oct(22, 0)) || len(r.Candidates) != 1 {
		t.Fatalf("report = %+v, want build, configured, measured from the lease history of Oct 22", r)
	}

	c := r.Candidates[0]
	got := fmt.Sprintf("%s: %d slots %v, busy %.4f, waiting %.4f, wait %.0f min, service %.0f min, %d leases, estimate %v",
		c.Station, c.Slots, c.Agents, c.BusyRatio, c.AvgWaiting, orZero(c.MeanWaitMinutes), orZero(c.MeanServiceMinutes), c.Leases, c.Estimate)

	want := fmt.Sprintf("build: 2 slots [backend frontend], busy %.4f, waiting %.4f, wait 990 min, service 360 min, 1 leases, estimate <nil>",
		6.0/66, 33.0/153)
	if got != want {
		t.Errorf("candidate =\n  %s\nwant\n  %s", got, want)
	}
}

// The board names its constraint and how it was found; the overview gives its busy share.
func TestAPI_ConstraintOnTheBoardAndTheOverview(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.leaseHistory(t)

	var board gen.Board
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); board.ConstraintSource != gen.Configured ||
		orZero(board.Constraint) != stBuild {
		t.Errorf("board constraint = %v (%s), want build, configured", board.Constraint, board.ConstraintSource)
	}

	var overview gen.FactoryOverviewList
	h.call(t, http.MethodGet, "/v1/orgs/acme/overview", "", &overview)

	for _, o := range overview.Factories {
		if o.Factory.Name == "line" && (o.Constraint == nil || orZero(o.Constraint.Busy) <= 0) {
			t.Errorf("overview of line: constraint %+v, want build with its busy share", o.Constraint)
		}
	}
}

// Without lease history there are no candidates; a line without a configured constraint has the
// snapshot of its loads (acme has no agent station: none).
func TestAPI_ConstraintReportWithoutHistory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, stBuild, domain.StateQueued, oct(23, 0))

	var r gen.ConstraintReport
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/constraint", "", &r); code != http.StatusOK ||
		r.CoverageSince != nil || len(r.Candidates) != 0 {
		t.Errorf("report = %d %+v, want no candidates before any lease", code, r)
	}

	var acme gen.ConstraintReport
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/constraint", "", &acme); acme.Source != gen.Snapshot ||
		acme.Constraint != nil || acme.Configured != nil {
		t.Errorf("report of acme = %+v, want no constraint from the snapshot", acme)
	}

	var board gen.Board
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/board", "", &board); board.ConstraintSource != gen.Snapshot {
		t.Errorf("board constraint source = %s, want snapshot", board.ConstraintSource)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/constraint?to=2026-10-24", "", nil); code != http.StatusBadRequest {
		t.Errorf("a range ending tomorrow = %d, want 400", code)
	}
}

// orZero is what p points to, or its zero value.
func orZero[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}
