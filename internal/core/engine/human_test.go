package engine_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
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
		// planned: the line requires the approval at a human station; a send-back it does not.
		planned bool
	}{
		{name: "approve", want: domain.Position{Station: stDone, State: domain.StateDone}, event: domain.EventApproved, planned: true},
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

			if events[0].Data[domain.KeyPlanned] != tt.planned {
				t.Errorf("%s event data %v, want planned %v", tt.event, events[0].Data, tt.planned)
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

	// An escalation is no decision the line requires: an unplanned touch.
	if approved := h.eventsOf(it.ID, domain.EventApproved); len(approved) != 1 || approved[0].Data[domain.KeyPlanned] != false {
		t.Errorf("approved events %+v, want one with planned false", approved)
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

// A run still winding down after a person sent its item back (agent stop, sandbox)
// must not stop the item's next run: when the old run's work ends it forgets and cancels only
// its own claim, so the next one keeps running and finishes.
func TestHumanControl_WindingDownRunLeavesTheNextClaimRunning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		windDown, release := make(chan struct{}), make(chan struct{})
		h := newHarness(t, controlLine(), func(ctx context.Context, run porttest.ExecCall, _ io.Writer) domain.RunResult {
			if run.Sandbox.Station == stWork { // the first run: once cancelled, slow to wind down
				<-ctx.Done()
				<-windDown

				return domain.RunResult{Status: domain.RunCancelled, Error: ctx.Err().Error()}
			}

			select {
			case <-release:
				return ok(`{}`)
			case <-ctx.Done():
				return domain.RunResult{Status: domain.RunCancelled, Error: ctx.Err().Error()}
			}
		})
		it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

		if err := h.e.Tick(inOrg(t)); err != nil {
			t.Fatal(err)
		}

		synctest.Wait() // the first run works at work

		if _, err := h.e.Reject(inOrg(t), it.ID, human, "wrong approach"); err != nil {
			t.Fatal(err)
		}

		if err := h.e.Tick(inOrg(t)); err != nil { // the item's next claim, at spec
			t.Fatal(err)
		}

		synctest.Wait()

		if n := len(h.worker.Runs()); n != 2 {
			t.Fatalf("%d runs started; want the first still winding down and the next at spec", n)
		}

		close(windDown)
		synctest.Wait() // the first run's work has ended

		if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stSpec, State: domain.StateInProgress}) {
			t.Fatalf("after the first run ended the item is at %+v (events %v); want its next run still working at spec",
				got.Position(), h.kinds(it.ID))
		}

		close(release)
		h.wait(t, h.e)

		if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stWork, State: domain.StateQueued}) {
			t.Errorf("item at %+v (events %v); want the next run to finish and move it on to work", got.Position(), h.kinds(it.ID))
		}

		h.expectOutcomes(t, it.ID, domain.LeaseTaken, domain.LeaseAdvanced)
	})
}

// An old lease_taken notice, from before the item's current claim or of another station, never
// stops the run working it now, and neither does any other notice of the current claim, such as
// the run's own relayed progress.
func TestInterrupt_IgnoresNoticesOfAnEarlierClaim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    domain.EventKind
		station string
		at      time.Time
	}{
		{name: "taken before this claim", kind: domain.EventLeaseTaken, station: stWork, at: t0.Add(-time.Minute)},
		{name: "taken at another station", kind: domain.EventLeaseTaken, station: stSpec, at: t0.Add(time.Hour)},
		{name: "another notice of this claim", kind: domain.EventRunStarted, station: stWork, at: t0.Add(time.Hour)},
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

			if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
				t.Fatal(err)
			}

			h.e.Interrupt(ports.ItemNotice{OrgID: testOrg, ItemID: it.ID, Kind: tt.kind, Station: tt.station, At: tt.at})
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
}

// Cancelling a queued item closes it at its station with one cancelled event by the person,
// with the reason, and one item.cancelled audit entry.
func TestEngine_CancelQueuedItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

	got, err := h.e.Cancel(inOrg(t), it.ID, human, "  not needed any more ")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stWork, State: domain.StateCancelled}) || !got.DoneAt.IsZero() {
		t.Errorf("cancelled item at %+v, done %v; want cancelled at work, never done", got.Position(), got.DoneAt)
	}

	cancelled := h.eventsOf(it.ID, domain.EventCancelled)
	if len(cancelled) != 1 || cancelled[0].Data["reason"] != "not needed any more" || cancelled[0].Data["actor"] != human.Ref() {
		t.Errorf("cancelled events %+v; want one by the person with the reason", cancelled)
	}

	if a := h.audited(t, domain.AuditItemCancelled); len(a) != 1 || a[0].Data["station"] != stWork || a[0].Data["reason"] != "not needed any more" {
		t.Errorf("audit %+v; want one item.cancelled with the station and the reason", a)
	}
}

// Cancelling an item mid-run takes its lease and stops the run; the item stays cancelled.
func TestEngine_CancelInProgressTakesTheLease(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

	if err := h.e.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if err := h.clock.WaitTickers(bounded(t), 1); err != nil { // the run heartbeats
		t.Fatal(err)
	}

	got, err := h.e.Cancel(inOrg(t), it.ID, human, "")
	if err != nil {
		t.Fatal(err)
	}

	h.wait(t, h.e) // returns only once the blocking run was cancelled

	want := domain.Position{Station: stWork, State: domain.StateCancelled}
	if got.Position() != want {
		t.Errorf("cancelled item at %+v; want %+v", got.Position(), want)
	}

	h.expectRunTaken(t, it.ID, want)

	if cancelled := h.eventsOf(it.ID, domain.EventCancelled); len(cancelled) != 1 || cancelled[0].Data["reason"] != nil {
		t.Errorf("cancelled events %+v; want one, without a reason", cancelled)
	}
}

// A done or cancelled item cannot be cancelled, and the refusal writes nothing.
func TestEngine_CancelClosedItemIsRefused(t *testing.T) {
	t.Parallel()

	for _, state := range []domain.ItemState{domain.StateDone, domain.StateCancelled} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
			it := h.at(t, h.submit(t, domain.NewItem{}), stDone, state)
			before := h.kinds(it.ID)

			if _, err := h.e.Cancel(inOrg(t), it.ID, human, "again"); !errors.Is(err, engine.ErrItemClosed) {
				t.Errorf("Cancel of a %s item: err = %v; want ErrItemClosed", state, err)
			}

			if got := h.item(t, it.ID); got.Position() != it.Position() || !slices.Equal(h.kinds(it.ID), before) {
				t.Errorf("a refused cancel changed the item to %+v, events %v", got.Position(), h.kinds(it.ID))
			}

			if a := h.audited(t, domain.AuditItemCancelled); len(a) != 0 {
				t.Errorf("audit %+v; want none", a)
			}
		})
	}
}

// routedLine is controlLine with a build station that routes by lane (backend, frontend).
func routedLine() domain.Line {
	return lineOf(agentStation(stSpec, "architect", 1, 1), domain.Station{
		ID: "build", Kind: domain.KindAgent, Agents: 1, WIP: 1,
		Route: map[string]string{"backend": agBackend, "frontend": agFrontend},
	}, domain.Station{ID: stApprove, Kind: domain.KindHuman})
}

func TestSetLane_RecordsTheEventAndChangesTheLane(t *testing.T) {
	t.Parallel()

	h := newHarness(t, routedLine(), porttest.Succeed(`{}`))
	it := h.at(t, h.submit(t, domain.NewItem{Lane: "backend"}), stApprove, domain.StateWaitingHuman)

	got, err := h.e.SetLane(inOrg(t), it.ID, " frontend ", human)
	if err != nil {
		t.Fatal(err)
	}

	if got.Lane != "frontend" || got.Position() != it.Position() {
		t.Errorf("item lane %q at %+v; want frontend, still at %+v", got.Lane, got.Position(), it.Position())
	}

	if stored := h.item(t, it.ID); stored.Lane != "frontend" {
		t.Errorf("stored lane %q; want frontend", stored.Lane)
	}

	set := h.eventsOf(it.ID, domain.EventLaneSet)
	if len(set) != 1 || set[0].Data["from"] != "backend" || set[0].Data["to"] != "frontend" ||
		set[0].Actor != human.Name || set[0].Data["actor"] != "user:local" {
		t.Errorf("lane_set events %+v; want one from backend to frontend by the local user", set)
	}

	if a := h.audited(t, domain.AuditItemLaneSet); len(a) != 1 || a[0].TargetID != string(it.ID) ||
		a[0].Data["from"] != "backend" || a[0].Data["to"] != "frontend" {
		t.Errorf("audit %+v; want one item.lane_set from backend to frontend", a)
	}
}

func TestSetLane_AnyLaneOnALineWithoutRoutes(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{})

	if got, err := h.e.SetLane(inOrg(t), it.ID, "docs", human); err != nil || got.Lane != "docs" {
		t.Fatalf("SetLane = %q, %v; want docs", got.Lane, err)
	}
}

func TestSetLane_Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lane  string
		state domain.ItemState // the item's state; queued at the inbox when empty
		want  error
	}{
		{name: "an empty lane", lane: "  ", want: engine.ErrLaneRequired},
		{name: "a lane no station routes", lane: "mobile", want: engine.ErrUnknownLane},
		{name: "a done item", lane: "frontend", state: domain.StateDone, want: engine.ErrItemClosed},
		{name: "a cancelled item", lane: "frontend", state: domain.StateCancelled, want: engine.ErrItemClosed},
		{name: "an item in progress", lane: "frontend", state: domain.StateInProgress, want: engine.ErrItemInProgress},
		{name: "an assembly", lane: "frontend", state: domain.StateAssembly, want: engine.ErrItemIsAssembly},
		{name: "the same lane", lane: "backend", want: engine.ErrSameLane},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, routedLine(), porttest.Succeed(`{}`))
			it := h.submit(t, domain.NewItem{Lane: "backend"})

			switch tt.state {
			case "":
			case domain.StateAssembly:
				it = h.assembled(t, it)
			default:
				it = h.at(t, it, "build", tt.state)
			}

			before := h.kinds(it.ID)

			if _, err := h.e.SetLane(inOrg(t), it.ID, tt.lane, human); !errors.Is(err, tt.want) {
				t.Errorf("SetLane %q: err = %v; want %v", tt.lane, err, tt.want)
			}

			if got := h.item(t, it.ID); got.Lane != "backend" || got.Position() != it.Position() || !slices.Equal(h.kinds(it.ID), before) {
				t.Errorf("a refused SetLane changed the item: lane %q at %+v, events %v", got.Lane, got.Position(), h.kinds(it.ID))
			}

			if a := h.audited(t, domain.AuditItemLaneSet); len(a) != 0 {
				t.Errorf("audit %+v; want none", a)
			}
		})
	}
}

// assembled makes it an assembly of one part at the build station.
func (h *harness) assembled(t *testing.T, it domain.Item) domain.Item {
	t.Helper()

	it = h.at(t, it, "build", domain.StateReady)

	parent, _, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: "build", State: domain.StateAssembly}, At: h.clock.Now(),
		},
		Parts: []domain.NewPart{{Key: "part", Title: "part"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	return parent
}

// staleStore hands out an item as it was, after changing it underneath: another person acted
// between the read and the write.
type staleStore struct {
	ports.Store

	meanwhile func(ctx context.Context, it domain.Item)
}

func (s staleStore) Item(ctx context.Context, id domain.ItemID) (domain.Item, error) {
	it, err := s.Store.Item(ctx, id)
	if err == nil {
		s.meanwhile(ctx, it)
	}

	return it, err
}

func TestSetLane_ItemChangedMeanwhile(t *testing.T) {
	t.Parallel()

	h := newHarness(t, routedLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{Lane: "backend"})

	e := h.engine(t, staleStore{Store: h.store, meanwhile: func(ctx context.Context, it domain.Item) {
		if _, err := h.store.Transition(ctx, ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stSpec, State: domain.StateQueued}, At: h.clock.Now(),
		}); err != nil {
			t.Error(err)
		}
	}})

	if _, err := e.SetLane(inOrg(t), it.ID, "frontend", human); !errors.Is(err, engine.ErrItemChanged) {
		t.Errorf("SetLane of an item changed meanwhile: err = %v; want ErrItemChanged", err)
	}

	if got := h.item(t, it.ID); got.Lane != "backend" || len(h.eventsOf(it.ID, domain.EventLaneSet)) != 0 {
		t.Errorf("lane %q, lane_set events %v; want backend and none", got.Lane, h.eventsOf(it.ID, domain.EventLaneSet))
	}
}
