package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
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
				got, err = h.e.Reject(inOrg(t), it.ID, human, "split it in two")
			} else {
				got, err = h.e.Approve(inOrg(t), it.ID, human)
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.Position() != tt.want || got.ReworkCount != tt.rework {
				t.Fatalf("item at %+v rework %d, want %+v rework %d", got.Position(), got.ReworkCount, tt.want, tt.rework)
			}

			events := h.eventsOf(it.ID, tt.event)
			if len(events) != 1 || events[0].Actor != human.Name || events[0].Data["actor"] != "user:local" {
				t.Fatalf("events %v, want one %s by the local user (user:local)", h.kinds(it.ID), tt.event)
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

	if _, err := h.e.Approve(inOrg(t), it.ID, human); !errors.Is(err, engine.ErrNotWaitingHuman) {
		t.Errorf("Approve on a queued item: err = %v, want ErrNotWaitingHuman", err)
	}

	if _, err := h.e.Reject(inOrg(t), it.ID, human, "no"); !errors.Is(err, engine.ErrCannotSendBack) {
		t.Errorf("Reject on a queued item: err = %v, want ErrCannotSendBack", err)
	}

	h.step(t)

	if _, err := h.e.Reject(inOrg(t), it.ID, human, ""); err == nil {
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

	got, err := h.e.Approve(inOrg(t), it.ID, human)
	if err != nil {
		t.Fatal(err)
	}

	if got.State != domain.StateDone {
		t.Errorf("approved escalation is %s at %s, want done", got.State, got.Station)
	}
}

const stSpec = "spec"

// controlLine is spec → work → approve (human) → done; sending back from work goes to spec.
func controlLine() domain.Line {
	return lineOf(agentStation(stSpec, "architect", 1, 1), agentStation(stWork, agWorker, 1, 1),
		domain.Station{ID: stApprove, Kind: domain.KindHuman})
}

// at puts an item at station in state, as earlier transitions would have.
func (h *harness) at(t *testing.T, it domain.Item, station string, state domain.ItemState) domain.Item {
	t.Helper()

	got, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: station, State: state}, At: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// audited returns the audit entries of one action in the test's organization.
func (h *harness) audited(t *testing.T, action string) []domain.AuditEntry {
	t.Helper()

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: action, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	return entries
}

func TestMove_UpstreamIsReworkAndNeedsAReason(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	it := h.at(t, h.submit(t, domain.NewItem{}), stApprove, domain.StateWaitingHuman)

	if _, err := h.e.Move(inOrg(t), it.ID, stSpec, human, " "); !errors.Is(err, engine.ErrReasonRequired) {
		t.Fatalf("Move upstream without a reason: err = %v; want ErrReasonRequired", err)
	}

	got, err := h.e.Move(inOrg(t), it.ID, stSpec, human, "the spec misses the empty case")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stSpec, State: domain.StateQueued}) || got.ReworkCount != 1 {
		t.Errorf("moved item at %+v rework %d; want queued at spec, rework 1", got.Position(), got.ReworkCount)
	}

	moved := h.eventsOf(it.ID, domain.EventMoved)
	last := moved[len(moved)-1]

	if last.Data["manual"] != true || last.Data["reason"] != "the spec misses the empty case" || last.Data["to"] != stSpec ||
		last.Actor != human.Name || last.Data["actor"] != "user:local" {
		t.Errorf("moved event %+v; want a manual move to spec by the local user with the reason", last)
	}

	if a := h.audited(t, domain.AuditItemMoved); len(a) != 1 || a[0].TargetID != string(it.ID) || a[0].Data["to"] != stSpec {
		t.Errorf("audit %+v; want one item.moved to spec", a)
	}
}

func TestMove_DownstreamArrivesInTheStationsState(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{}) // queued at the inbox

	got, err := h.e.Move(inOrg(t), it.ID, stApprove, human, "")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stApprove, State: domain.StateWaitingHuman}) || got.ReworkCount != 0 {
		t.Errorf("moved item at %+v rework %d; want waiting at approve, no rework", got.Position(), got.ReworkCount)
	}

	if n := len(h.eventsOf(it.ID, domain.EventWaitingHuman)); n != 1 {
		t.Errorf("events %v; want one waiting_human on arrival", h.kinds(it.ID))
	}
}

func TestMove_Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		to   string
		done bool
		want error
	}{
		{name: "its own station", to: stInbox, want: engine.ErrSameStation},
		{name: "a done station", to: stDone, want: engine.ErrDoneStation},
		{name: "a station not on the line", to: "nowhere", want: engine.ErrUnknownStation},
		{name: "a done item", to: stSpec, done: true, want: engine.ErrItemClosed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
			it := h.submit(t, domain.NewItem{})

			if tt.done {
				it = h.at(t, it, stDone, domain.StateDone)
			}

			if _, err := h.e.Move(inOrg(t), it.ID, tt.to, human, "because"); !errors.Is(err, tt.want) {
				t.Errorf("Move to %s: err = %v; want %v", tt.to, err, tt.want)
			}

			if got := h.item(t, it.ID); got.Position() != it.Position() {
				t.Errorf("a refused move changed the item to %+v", got.Position())
			}
		})
	}
}

func TestPause_BlocksWhereItIsAndRetryResumes(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	it := h.at(t, h.submit(t, domain.NewItem{}), stApprove, domain.StateWaitingHuman)

	got, err := h.e.Pause(inOrg(t), it.ID, human, "waiting for the vendor")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stApprove, State: domain.StateBlocked}) {
		t.Errorf("paused item at %+v; want blocked at approve", got.Position())
	}

	paused := h.eventsOf(it.ID, domain.EventPaused)
	if len(paused) != 1 || paused[0].Data["reason"] != "waiting for the vendor" || paused[0].Data["actor"] != "user:local" {
		t.Errorf("paused events %+v; want one by the local user with the reason", paused)
	}

	if a := h.audited(t, domain.AuditItemPaused); len(a) != 1 {
		t.Errorf("audit %+v; want one item.paused", a)
	}

	if _, err := h.e.Pause(inOrg(t), it.ID, human, ""); !errors.Is(err, engine.ErrAlreadyBlocked) {
		t.Errorf("Pause of a paused item: err = %v; want ErrAlreadyBlocked", err)
	}

	if got, err = h.e.Retry(inOrg(t), it.ID, human); err != nil || got.State != domain.StateWaitingHuman {
		t.Fatalf("Retry = %s, %v; want waiting at approve again", got.State, err)
	}

	done := h.at(t, got, stDone, domain.StateDone)
	if _, err := h.e.Pause(inOrg(t), done.ID, human, ""); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("Pause of a done item: err = %v; want ErrItemClosed", err)
	}
}

func TestReject_SendsBackBlockedItemsButNotQueuedOnes(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

	if _, err := h.e.Reject(inOrg(t), it.ID, human, "no"); !errors.Is(err, engine.ErrCannotSendBack) {
		t.Errorf("Reject of a queued item: err = %v; want ErrCannotSendBack", err)
	}

	it = h.at(t, it, stWork, domain.StateBlocked)

	got, err := h.e.Reject(inOrg(t), it.ID, human, "start over")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stSpec, State: domain.StateQueued}) || got.ReworkCount != 1 {
		t.Errorf("sent back item at %+v rework %d; want queued at spec, rework 1", got.Position(), got.ReworkCount)
	}

	if a := h.audited(t, domain.AuditItemRejected); len(a) != 1 || a[0].Data["reason"] != "start over" {
		t.Errorf("audit %+v; want one item.rejected with the reason", a)
	}
}

type control func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error)

// A person moving, pausing or sending back an item mid-run takes its lease: the run is stopped
// and finished as cancelled, and the instance running it does not move the item again. That
// instance learns it at once when it took the lease itself or is told through Interrupt, and
// otherwise at its next heartbeat.
func TestHumanControl_MidRunCancelsTheRun(t *testing.T) {
	t.Parallel()

	actions := []struct {
		name   string
		act    control
		want   domain.Position
		rework int
	}{
		{
			name: "move",
			act: func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
				return e.Move(ctx, id, stApprove, human, "")
			},
			want: domain.Position{Station: stApprove, State: domain.StateWaitingHuman},
		},
		{
			name: "pause",
			act: func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
				return e.Pause(ctx, id, human, "hold on")
			},
			want: domain.Position{Station: stWork, State: domain.StateBlocked},
		},
		{
			name: "send back",
			act: func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
				return e.Reject(ctx, id, human, "wrong approach")
			},
			want: domain.Position{Station: stSpec, State: domain.StateQueued}, rework: 1,
		},
	}

	ways := []struct {
		name string
		// other: another instance acts, not the one running the item.
		other bool
		learn func(h *harness, it domain.Item)
	}{
		{name: "by the instance running it", learn: func(*harness, domain.Item) {}},
		{name: "told through Interrupt", other: true, learn: func(h *harness, it domain.Item) {
			h.e.Interrupt(ports.ItemNotice{
				OrgID: testOrg, FactoryID: factoryID, ItemID: it.ID, Kind: domain.EventLeaseTaken, Station: stWork,
				At: h.clock.Now(),
			})
		}},
		{name: "at its next heartbeat", other: true, learn: func(h *harness, _ domain.Item) { h.clock.Advance(heartbeat) }},
	}

	for _, a := range actions {
		for _, w := range ways {
			t.Run(a.name+" "+w.name, func(t *testing.T) {
				t.Parallel()

				h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
				it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

				if err := h.e.Tick(inOrg(t)); err != nil {
					t.Fatal(err)
				}

				if err := h.clock.WaitTickers(bounded(t), 1); err != nil { // the run heartbeats
					t.Fatal(err)
				}

				actor := h.e
				if w.other {
					actor = h.engine(t, h.store)
				}

				got, err := a.act(actor, inOrg(t), it.ID)
				if err != nil {
					t.Fatal(err)
				}

				w.learn(h, it)
				h.wait(t, h.e) // returns only once the blocking run was cancelled

				if got.Position() != a.want || got.ReworkCount != a.rework {
					t.Errorf("item at %+v rework %d; want %+v rework %d", got.Position(), got.ReworkCount, a.want, a.rework)
				}

				h.expectRunTaken(t, it.ID, a.want)
			})
		}
	}
}

// An old lease_taken notice, from before the item's current claim or of another station, never
// stops the run working it now.
func TestInterrupt_IgnoresNoticesOfAnEarlierClaim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		station string
		at      time.Time
	}{
		{name: "taken before this claim", station: stWork, at: t0.Add(-time.Minute)},
		{name: "taken at another station", station: stSpec, at: t0.Add(time.Hour)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := make(chan struct{})
			h := newHarness(t, controlLine(), porttest.Block(release, `{}`))
			it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

			if err := h.e.Tick(inOrg(t)); err != nil {
				t.Fatal(err)
			}

			if err := h.runner.WaitStarted(bounded(t), 1); err != nil {
				t.Fatal(err)
			}

			h.e.Interrupt(ports.ItemNotice{OrgID: testOrg, ItemID: it.ID, Kind: domain.EventLeaseTaken, Station: tt.station, At: tt.at})
			close(release)
			h.wait(t, h.e)

			if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stApprove, State: domain.StateWaitingHuman}) {
				t.Errorf("item at %+v; want the run to finish and move it on to approve", got.Position())
			}
		})
	}
}

// expectRunTaken checks that a person took the item's lease while it ran: the item stays at want,
// the lease span is taken, the one run was cancelled and reported ended, and the instance that ran
// it moved nothing.
func (h *harness) expectRunTaken(t *testing.T, id domain.ItemID, want domain.Position) {
	t.Helper()

	if now := h.item(t, id); now.Position() != want {
		t.Errorf("after the run ended the item is at %+v; want it left at %+v", now.Position(), want)
	}

	if _, held := h.store.Lease(id); held {
		t.Error("the item still holds a lease")
	}

	h.expectOutcomes(t, id, domain.LeaseTaken)

	if runs := h.runs(t, testOrg, id); len(runs) != 1 || runs[0].Result.Status != domain.RunCancelled {
		t.Errorf("runs %+v; want the one run cancelled", runs)
	}

	if n := len(h.eventsOf(id, domain.EventLeaseTaken)); n != 1 {
		t.Errorf("events %v; want one lease_taken", h.kinds(id))
	}

	for _, k := range []domain.EventKind{domain.EventReleased, domain.EventAgentCancelled, domain.EventBlocked} {
		if n := len(h.eventsOf(id, k)); n != 0 {
			t.Errorf("events %v; the instance that lost the run must not move the item (%s)", h.kinds(id), k)
		}
	}

	if ended := h.endedRuns(); len(ended) != 1 {
		t.Errorf("RunEnded told %v; want the cancelled run", ended)
	}
}
