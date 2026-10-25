// Package ports declares the interfaces the engine uses to reach the outside world. Adapters in
// internal/adapters implement them; internal/ports/porttest holds in-memory fakes for tests.
//
// Ports import only internal/domain. Connections and MasterKey (connections.go, phase 4) hold an
// organization's connections and their sealed credentials; until runs use them (spec 4, 4-11)
// model credentials reach tasks as secret references in the engine's task environment.
package ports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// Errors adapters return so the engine can tell expected races from failures. Adapters wrap
// them (errors.Is must match).
var (
	// ErrNotFound means the requested item does not exist.
	ErrNotFound = errors.New("not found")
	// ErrNoWork means no item is waiting at the station.
	ErrNoWork = errors.New("no work waiting")
	// ErrSlotTaken means another claim holds the station slot.
	ErrSlotTaken = errors.New("slot taken")
	// ErrOrgFull means the organization runs as many agents as its slot cap allows.
	ErrOrgFull = errors.New("organization slots full")
	// ErrFactoryFull means the factory runs as many agents as its slot cap allows.
	ErrFactoryFull = errors.New("factory slots full")
	// ErrConflict means a compare-and-set guard failed: the item is no longer where a
	// transition expected it, or a draft is no longer at the revision a save expected.
	ErrConflict = errors.New("changed concurrently")
	// ErrLeaseLost means the lease expired or was released; the holder must stop working.
	ErrLeaseLost = errors.New("lease lost")
	// ErrContractViolation means an output does not satisfy its contract (as opposed to the
	// validator itself failing).
	ErrContractViolation = errors.New("contract violation")
	// ErrSecretNotFound means a secret reference names nothing.
	ErrSecretNotFound = errors.New("secret not found")
	// ErrNotMergeable means a pull request cannot be merged as it stands: it is closed, has
	// conflicts or failing required checks, its head moved, or its URL is not a pull request on
	// the connection's host. Nothing was changed.
	ErrNotMergeable = errors.New("pull request not mergeable")
)

// Store is the engine's only state. Every method is atomic; methods that change state append
// their events in the same transaction. All of them are safe to call from many goald
// instances at once.
type Store interface {
	// CreateItem inserts an item with the next per-factory number and appends a created event.
	CreateItem(ctx context.Context, n domain.NewItem) (domain.Item, error)

	// Item returns one item, or ErrNotFound.
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)

	// Waiting returns up to limit queued or ready items at a station in pull order: expedite
	// first, then priority (highest first), then age (oldest first).
	Waiting(ctx context.Context, factory domain.FactoryID, station string, limit int) ([]domain.Item, error)

	// Leases returns the live leases held at a station; their slots are the station's WIP.
	Leases(ctx context.Context, factory domain.FactoryID, station string) ([]domain.Lease, error)

	// ClaimNext takes the next waiting item at c.Station in pull order (FOR UPDATE SKIP
	// LOCKED), sets it in_progress, inserts a lease on c.Slot and appends a claimed event.
	// It returns ErrOrgFull or ErrFactoryFull when the organization's or the factory's leases
	// already reach its slot cap (Tenancy.UpdateOrg sets them; checked first, under a lock that
	// serializes the organization's claims while a cap is set), ErrNoWork when nothing waits and
	// ErrSlotTaken when the slot already holds a lease; in each case nothing changes. The unique
	// (factory, station, slot) lease is what keeps WIP at or under capacity across instances.
	ClaimNext(ctx context.Context, c Claim) (domain.Item, domain.Lease, error)

	// HeartbeatLease extends a held lease to expiresAt. It returns ErrLeaseLost when no lease
	// with the same item and AcquiredAt exists any more.
	HeartbeatLease(ctx context.Context, l domain.Lease, now, expiresAt time.Time) (domain.Lease, error)

	// RequeueExpiredLeases deletes up to limit leases that expired before now (SKIP LOCKED),
	// records each as a lease span ended now with outcome expired, returns each item to queued at
	// its station, marks the item's running runs failed, and appends a lease_expired event per
	// item. It returns the leases it removed.
	RequeueExpiredLeases(ctx context.Context, now time.Time, limit int) ([]domain.Lease, error)

	// FinishLostRuns finishes, as cancelled with error domain.RunLostError and ended at now, up to
	// limit of the organization's running runs whose lease is gone and ended before endedBefore,
	// those whose lease ended first. Such a run lost its instance: a person took the item's lease
	// (Transition.TakeLease) and the instance that would have finished the run with its real cost
	// had died. A newer lease of the item at the run's station is not the run's. Each run gets a
	// run_finished event. It returns the runs it finished; runs another transaction holds are left
	// to the next call.
	FinishLostRuns(ctx context.Context, endedBefore, now time.Time, limit int) ([]domain.Run, error)

	// RunsOver returns those of runs, of the organization in ctx, that are over for good: no
	// longer running (finished by their instance or by the sweeps) and no longer holding their
	// lease (the item's lease at the run's station acquired no later than the run started, which
	// a run's checks use after its agent task ended): it was released, taken, or swept as expired
	// (RequeueExpiredLeases). Such a run's sandbox may be deleted (engine.ReapOrphans). Ids that
	// name no run of the organization are not returned: they may be another deployment's runs on a
	// shared sandbox backend. Order is unspecified.
	RunsOver(ctx context.Context, runs []domain.RunID) ([]domain.RunID, error)

	// StartRun records a running agent run and appends a run_started event.
	StartRun(ctx context.Context, r NewRun) (domain.RunID, error)

	// FinishRun records a run's result, adds its cost to the item and appends a run_finished
	// event with its status, error, cost and turns. Finishing a run twice is a no-op.
	FinishRun(ctx context.Context, id domain.RunID, res domain.RunResult, endedAt time.Time) error

	// Runs returns up to limit of an item's runs, oldest first (by start, then id). An unknown
	// item has none.
	Runs(ctx context.Context, item domain.ItemID, limit int) ([]domain.Run, error)

	// Run returns one run, or ErrNotFound.
	Run(ctx context.Context, id domain.RunID) (domain.Run, error)

	// Transition moves an item as described by t, compare-and-set on t.From (and on t.Lease
	// when set). It returns ErrConflict or ErrLeaseLost when the guard fails; nothing changes.
	// Releasing a lease records its lease span, so t.Lease needs t.Outcome (an error otherwise).
	Transition(ctx context.Context, t Transition) (domain.Item, error)

	// AppendEvents appends events that come with no state change.
	AppendEvents(ctx context.Context, events ...domain.Event) error

	// AppendNote appends e, a person's note (kind note), to an item that is neither done nor
	// cancelled, with the audit entry item.noted by `by` (the note's length, never its text), and
	// returns it with its id. It returns ErrConflict when the item is done or cancelled and
	// ErrNotFound when there is no such item.
	AppendNote(ctx context.Context, e domain.Event, by domain.Actor) (domain.Event, error)

	// NotesSinceLastRun returns an item's notes appended after its latest run_started event (every
	// note before its first run), in append order: what the next run's brief carries.
	NotesSinceLastRun(ctx context.Context, item domain.ItemID) ([]domain.Event, error)

	// ItemEvents returns up to limit of an item's events with an id greater than after, in
	// append order (ascending id). An unknown item has no events; an id no item could have is
	// ErrNotFound.
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)

	// ItemByNumber returns a factory's item by its per-factory number, or ErrNotFound.
	ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error)

	// ListItems returns up to q.Limit of a factory's items that match q, newest (highest
	// number) first.
	ListItems(ctx context.Context, q ItemQuery) ([]domain.Item, error)

	// StationLoads returns the load of every station of a factory that has open items: how
	// many wait (queued or ready), how many are in progress and how many wait for a person's
	// decision. Order is unspecified.
	StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error)

	// SpendSince is the summed cost_usd of the factory's agent runs started at or after since;
	// 0 when there are none. The engine reads it once per tick for each factory with a weekly
	// budget (domain.Runtime.WeeklyBudget).
	SpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error)

	// Attention returns, for every factory of the organization, the items waiting for a person
	// (with their split by work type), the blocked items and the open andon pull, ordered by
	// factory id. Factories with nothing pending are included with zeros.
	Attention(ctx context.Context) ([]domain.Attention, error)

	// PullAndon opens an andon pull on p.FactoryID: p's kind, station, reason, data and PulledAt,
	// pulled by the actor (PulledBy is its Ref()), with the audit entry andon.pulled in the same
	// transaction. It returns the stored pull with its id. It returns ErrConflict when the factory
	// already has an open pull and ErrNotFound when the organization has no such factory; nothing
	// changes in either case. The commit notifies the organization's stream (event factory, kind
	// andon_pulled).
	PullAndon(ctx context.Context, p domain.AndonPull, by domain.Actor) (domain.AndonPull, error)

	// ClearAndon closes the factory's open andon pull at `at` on the actor's behalf, with an optional note, with
	// the audit entry andon.cleared, and returns it. It returns ErrNotFound when none is open.
	// The commit notifies the stream (kind andon_cleared).
	ClearAndon(ctx context.Context, factory domain.FactoryID, by domain.Actor, note string, at time.Time) (domain.AndonPull, error)

	// OpenAndon returns the factory's open andon pull, or nil while its line runs.
	OpenAndon(ctx context.Context, factory domain.FactoryID) (*domain.AndonPull, error)

	// AndonPulls returns up to limit of the factory's andon pulls, open and cleared, the most
	// recently pulled first.
	AndonPulls(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.AndonPull, error)

	// LabelTouch sets the reason of the touch event names (its naming event, domain.TouchesOf) on
	// by's behalf, with the audit entry touch.labelled {event_id, reason} in the same transaction.
	// It is never an item event, so it is not a touch itself. It returns ErrNotFound when event
	// names no touch of the organization; reason must be valid (domain.TouchReason.Valid).
	LabelTouch(ctx context.Context, event int64, reason domain.TouchReason, by domain.Actor) error
}

// FlowStats is the read model of how work flows: spans of time items spend at stations and the
// statistics derived from them. It is separate from Store so the write side stays focused; the
// PostgreSQL adapter implements both on one pool and porttest.Store both over the same state.
// Every method reads within the organization in ctx (domain.WithOrg), as Store's do.
type FlowStats interface {
	// ItemSpans returns an item's spans in the order it entered them, the open one last. An
	// item the organization does not have has none.
	ItemSpans(ctx context.Context, id domain.ItemID) ([]domain.Span, error)

	// CurrentRun returns the item's run in progress with its lease's slot and the turns reported
	// so far, or nil when the item has none (no running run under a live lease).
	CurrentRun(ctx context.Context, id domain.ItemID) (*domain.CurrentRun, error)

	// LiveTurns counts, for every item of the factory with a running run under a live lease, the
	// turns its newest running run has reported so far (its usage events), by item.
	LiveTurns(ctx context.Context, factory domain.FactoryID) (map[domain.ItemID]int, error)

	// StationTimes measures every station of the factory over [since, until) (see
	// domain.StationTimes); a station appears when it has a lease, a waiting item or a verdict to
	// count. It also returns when the factory's lease history begins, the earliest acquisition of
	// a lease span or live lease (zero when there is none): busy shares measured before it would
	// read as idle time.
	StationTimes(ctx context.Context, factory domain.FactoryID, since, until time.Time) ([]domain.StationTimes, time.Time, error)

	// FlowMetrics measures a factory's flow over [q.Since, q.Until) for the items of q.Type:
	// items done per UTC day, done, reworked, lead-time p50 and p85 (linear interpolation), the
	// open items now, the spend of the runs started in the window, and the long waits at station
	// longWaitsAt (none when ""): the items with a span waiting there (queued or ready) that
	// overlaps the window and had lasted over domain.LongWait by q.Until.
	FlowMetrics(ctx context.Context, q FlowQuery, longWaitsAt string) (domain.FlowMetrics, error)

	// DoneByBucket counts the items of q.Type done in n consecutive buckets of length d that end
	// at q.Until, oldest first; q.Since is not used.
	DoneByBucket(ctx context.Context, q FlowQuery, d time.Duration, n int) ([]int, error)

	// ItemTimes returns the items of q.Type done in [q.Since, q.Until), by when they were done:
	// each one's lead time and its time in each component of the lead-time bridge, from its
	// spans (domain.ComponentOf). It returns at most limit items, those done last, so the read
	// stays bounded however busy the window.
	ItemTimes(ctx context.Context, q FlowQuery, limit int) ([]domain.ItemTime, error)

	// GateDecisions tallies the last n verdicts (gate_approved and gate_rejected events) at a
	// gate station of the factory, keeping only those after `after` (the last andon clear; zero
	// for all of them), newest first (domain.TallyGate).
	GateDecisions(ctx context.Context, factory domain.FactoryID, station string, after time.Time, n int) (domain.GateTally, error)
	// AgentStats measures one agent of a factory over [since, until) (see domain.AgentStats):
	// its tasks from the lease spans that ended in the window, its runs and their denied tool
	// calls from the runs that started in it. A send-back is the advanced task's item entering its
	// next rework round from another station; its label comes from the rejection (gate_rejected
	// or rejected) that station gave then, or from a person's manual move back.
	AgentStats(ctx context.Context, factory domain.FactoryID, agent string, since, until time.Time) (domain.AgentStats, error)

	// InboxItems returns up to limit of the open items in the given factories that may need a
	// person (the organization's inbox): waiting for a decision, blocked (with the reason of its
	// latest blocked or paused event), or in the expedite lane, most recently changed first
	// (updated_at descending, then id). work_items_attention_idx (migration 00033) serves it.
	InboxItems(ctx context.Context, factories []domain.FactoryID, limit int) ([]domain.AttentionItem, error)

	// Touches measures the human touches (domain.Touch) of the factory's items of q.Type finished
	// in [q.Since, q.Until), of those whose last agent run ran on q.VersionID when it is set
	// (domain.TallyTouches), and counts the andon pulls people made in the window (LineStops).
	Touches(ctx context.Context, q FlowQuery) (domain.TouchStats, error)

	// OrgTouches measures, as Touches does, each of factories (q.FactoryID is not used) and all
	// of them together, in the order given; a factory the organization does not have counts
	// nothing.
	OrgTouches(ctx context.Context, factories []domain.FactoryID, q FlowQuery) (domain.TouchStats, []domain.FactoryTouchStats, error)

	// TouchesByBucket measures, as Touches does but without line stops, the items finished in n
	// consecutive buckets of length d that end at q.Until, oldest first; q.Since is not used.
	TouchesByBucket(ctx context.Context, q FlowQuery, d time.Duration, n int) ([]domain.TouchStats, error)

	// ItemTouches returns an item's touches in time order with their reasons; none for an item
	// the organization does not have.
	ItemTouches(ctx context.Context, item domain.ItemID) ([]domain.Touch, error)

	// TouchCounts counts the touches of each of items that has any, by item: the board's cards.
	TouchCounts(ctx context.Context, items []domain.ItemID) (map[domain.ItemID]int, error)
}

// FlowQuery is what a FlowStats read measures: one factory's items over [Since, Until).
type FlowQuery struct {
	FactoryID    domain.FactoryID
	Since, Until time.Time
	// Type keeps the items of one work type; "" keeps every type.
	Type string
	// VersionID keeps, where a read says so (Touches), the items whose last agent run ran on this
	// definition version; "" keeps every item.
	VersionID domain.DefinitionVersionID
}

// Tenancy keeps the install's organizations. Methods marked (org) act on the organization in
// ctx (domain.WithOrg) and fail with an error wrapping domain.ErrNoOrg without one. Each
// mutating method appends its audit entry in the same transaction, so a change is never
// unaudited. Rule violations (a malformed, reserved or taken slug, a bad name or mark) wrap
// domain.ErrInvalid.
type Tenancy interface {
	// Orgs returns every live organization of the install, by name. It needs no org.
	Orgs(ctx context.Context) ([]domain.OrgSummary, error)
	// Org finds a live organization by slug or id; ErrNotFound when unknown or deleted. It
	// needs no org.
	Org(ctx context.Context, ref string) (domain.Org, error)
	// ActiveOrgs returns the ids of every live organization, for jobs that serve them all.
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)

	// CreateOrg creates an organization (the mark defaults to domain.DefaultMark of the name)
	// and appends org.created to its log. It needs no org.
	CreateOrg(ctx context.Context, by domain.Actor, n NewOrg) (domain.Org, error)
	// UpdateOrg changes the fields u sets and appends one org.updated entry with each changed
	// field's from and to; nothing changed appends nothing. (org)
	UpdateOrg(ctx context.Context, by domain.Actor, u OrgUpdate) (domain.Org, error)
	// FactorySlots returns every factory of the organization with its slot cap (0: none). (org)
	FactorySlots(ctx context.Context) (map[domain.FactoryID]int, error)
	// DeleteOrg soft-deletes the organization: from now on Org, Orgs and ActiveOrgs no longer
	// see it. It appends org.deleted and, at commit, a DefinitionNotices notice of kind NoticeOrg, so
	// every goald drops its lines at once (the postgres adapter; the fake leaves that to tests). (org)
	DeleteOrg(ctx context.Context, by domain.Actor, now time.Time) error
	// DeletedOrgs returns up to limit organizations deleted before cutoff, oldest deletion
	// first. It needs no org (a system job).
	DeletedOrgs(ctx context.Context, cutoff time.Time, limit int) ([]domain.OrgID, error)
	// PurgeOrg hard-deletes org with every row it owns, in a transaction of its own, and reports
	// whether it did: false, with nothing done, when org is not deleted, was deleted on or after
	// cutoff, or is being purged by another goald instance (which it skips instead of waiting
	// for). Once the rows are gone and before that is committed, it calls also (when not nil) to
	// remove what the organization keeps outside the database; an error from it keeps the
	// organization, rows and all, for the next run, and is returned. It needs no org (a system
	// job).
	PurgeOrg(ctx context.Context, org domain.OrgID, cutoff time.Time, also func(context.Context, domain.OrgID) error) (bool, error)
}

// NewOrg is an organization to create.
type NewOrg struct {
	Slug, Name, Mark string
	// Owner becomes the organization's first owner: the user who creates it (GOAL_AUTH=session).
	// Empty, as under GOAL_AUTH=none, makes the install's earliest platform admin its owner, or
	// nobody before anyone has signed in (the bootstrap owner then owns every live organization).
	Owner domain.UserID
}

// OrgUpdate names the fields to change; nil leaves a field as it is.
type OrgUpdate struct {
	Name, Slug, Mark *string
	// MaxSlots sets the organization's slot cap (domain.Org.MaxSlots); 0 clears it.
	MaxSlots *int
	// FactorySlots sets the slot caps of factories of the organization; 0 clears a cap.
	// Per-factory caps may add up to more than the organization's.
	FactorySlots map[domain.FactoryID]int
}

// OrgChange is an OrgUpdate checked against the organization as it is.
type OrgChange struct {
	// Org is the organization after the update.
	Org domain.Org
	// FactorySlots holds the factory caps that change: the new cap, or 0 when it is cleared.
	FactorySlots map[domain.FactoryID]int
	// Audit is the org.updated data: each changed field's {from, to}, a cleared cap as nil, and
	// the changed factory caps under "factory_slots" by factory id. Empty when nothing changes.
	Audit map[string]any
}

// Apply checks u by the domain rules against o and its factories' caps (every factory of the
// organization, 0 for no cap) and returns the change. Errors wrap domain.ErrInvalid, also for a
// factory that is not the organization's. Whether a new slug is free is the store's to decide.
func (u OrgUpdate) Apply(o domain.Org, factorySlots map[domain.FactoryID]int) (OrgChange, error) {
	c := OrgChange{Org: o, FactorySlots: map[domain.FactoryID]int{}, Audit: map[string]any{}}

	for _, f := range []struct {
		name  string
		to    *string
		field *string
		check func(string) error
	}{
		{"name", u.Name, &c.Org.Name, domain.CheckOrgName},
		{"slug", u.Slug, &c.Org.Slug, domain.CheckSlug},
		{"mark", u.Mark, &c.Org.Mark, domain.CheckMark},
	} {
		if f.to == nil || *f.to == *f.field {
			continue
		}

		if err := f.check(*f.to); err != nil {
			return OrgChange{}, err
		}

		c.Audit[f.name] = map[string]any{"from": *f.field, "to": *f.to}
		*f.field = *f.to
	}

	if u.MaxSlots != nil && *u.MaxSlots != o.MaxSlots {
		if err := checkCap(*u.MaxSlots); err != nil {
			return OrgChange{}, err
		}

		c.Audit["max_slots"] = map[string]any{"from": capValue(o.MaxSlots), "to": capValue(*u.MaxSlots)}
		c.Org.MaxSlots = *u.MaxSlots
	}

	factories := map[string]any{}

	for id, to := range u.FactorySlots {
		from, ok := factorySlots[id]
		if !ok {
			return OrgChange{}, fmt.Errorf("factory %s is not in this organization: %w", id, domain.ErrInvalid)
		}

		if err := checkCap(to); err != nil {
			return OrgChange{}, fmt.Errorf("factory %s: %w", id, err)
		}

		if to != from {
			c.FactorySlots[id] = to
			factories[string(id)] = map[string]any{"from": capValue(from), "to": capValue(to)}
		}
	}

	if len(factories) > 0 {
		c.Audit["factory_slots"] = factories
	}

	return c, nil
}

// checkCap accepts 0 (no cap) and the caps domain.CheckSlots allows.
func checkCap(n int) error {
	if n == 0 {
		return nil
	}

	return domain.CheckSlots(n)
}

// capValue is a cap as audit data records it: nil when there is none.
func capValue(n int) any {
	if n == 0 {
		return nil
	}

	return n
}

// Audit reads an organization's audit log and appends entries that come with no other change;
// other ports append their own entries in their transactions. Both methods act on the
// organization in ctx (org).
type Audit interface {
	// Append appends e; a zero e.At is the time of the append.
	Append(ctx context.Context, e domain.AuditEntry) error
	// AuditLog returns up to q.Limit entries that match q, newest first.
	AuditLog(ctx context.Context, q AuditQuery) ([]domain.AuditEntry, error)
}

// AuditQuery selects audit entries. Zero fields match everything.
type AuditQuery struct {
	// Before, when positive, keeps entries with a smaller id: the cursor of the next page.
	Before     int64
	Limit      int
	Action     string
	TargetKind string
	TargetID   string
	FactoryID  domain.FactoryID
}

// ItemQuery selects a factory's items for a listing.
type ItemQuery struct {
	FactoryID domain.FactoryID
	// States, when not empty, keeps items in one of these states.
	States []domain.ItemState
	// Station, when not empty, keeps items at this station.
	Station string
	// Before, when positive, keeps items numbered below it: the cursor of the next page.
	Before int64
	Limit  int
}

// Claim asks for the next item at a station for one slot.
type Claim struct {
	FactoryID domain.FactoryID
	// Station is the full station spec; the lease's agent is Station.AgentFor(item.Lane), or ""
	// when the item's lane has no route (the engine then blocks the item).
	Station   domain.Station
	Slot      int
	Now       time.Time
	ExpiresAt time.Time
}

// NewRun describes an agent run that is starting.
type NewRun struct {
	FactoryID domain.FactoryID
	// DefinitionVersionID is the published version of the factory the run starts on.
	DefinitionVersionID domain.DefinitionVersionID
	ItemID              domain.ItemID
	Station             string
	Agent               string
	Model               string
	StartedAt           time.Time
}

// Transition is one atomic change to an item.
type Transition struct {
	ItemID domain.ItemID
	// From guards the change: the item must still be at this station in this state.
	From domain.Position
	To   domain.Position
	// Lease, when set, must still be held (same item and AcquiredAt); it is released and recorded
	// as a lease span with Outcome, which it requires.
	Lease *domain.Lease
	// TakeLease releases the item's lease whatever its AcquiredAt, if it has one: a person acting
	// on an item in progress takes it from the instance running it, which stops at its next
	// heartbeat or when Interrupt tells it. It is recorded as a lease span with Outcome, which it
	// requires. Lease and TakeLease are exclusive.
	TakeLease bool
	Outcome   domain.LeaseOutcome
	// Audit, when set, is appended to the organization's audit log with the change.
	Audit *domain.AuditEntry
	// Lane, when non-empty, replaces the item's lane.
	Lane string
	// Output, when set, is stored as the item's output for OutputStation.
	OutputStation string
	Output        json.RawMessage
	// Rework increments the item's rework count.
	Rework bool
	At     time.Time
	// DoneAt is stored when To.State is done.
	DoneAt time.Time
	Events []domain.Event
}

// ItemNotices streams a notice of every item event as it is committed, from any goald
// instance: PostgreSQL LISTEN/NOTIFY. Each instance listens once and fans out to its own
// clients, so no instance holds state another needs. A notification only names what changed:
// the adapter reads every notice back from the rows of the organization it names, so nothing a
// notification claims reaches a listener unread (review 2.2 M4).
type ItemNotices interface {
	// Listen calls deliver for every item event, and every other organization event (Org),
	// committed while it listens, in commit order, until ctx ends; it returns nil then. Each time it starts listening (the first time, and
	// again after reconnecting when the connection was lost, which misses the events committed
	// meanwhile) it first delivers a notice with Resync set. deliver must not block for long.
	Listen(ctx context.Context, deliver func(ItemNotice)) error
}

// ItemNotice is one committed item event as listeners see it, with the item's state right
// after it. A Resync notice carries nothing else: listeners must re-read what they show. A
// notice with Org set is another of the organization's stream events (OrgID, FactoryID and Org
// are set, the item fields are not).
type ItemNotice struct {
	Resync  bool
	EventID int64
	// OrgID is the organization of the item or event; a stream serves one organization's notices.
	OrgID domain.OrgID
	// Org, when set, makes this an organization event rather than an item event.
	Org       *OrgEvent
	FactoryID domain.FactoryID
	ItemID    domain.ItemID
	// Number is the item's per-factory number.
	Number  int64
	Kind    domain.EventKind
	Station string
	State   domain.ItemState
	At      time.Time
}

// OrgEvent is an organization's stream event that is not an item event, NOTIFYed on the
// goal_org_events channel ({org_id, event, factory_id?, data}) in the transaction that causes it,
// such as a factory's andon pulled or cleared (event factory) and phase 4's credential and
// connection events.
type OrgEvent struct {
	// Name is the stream's event name: lowercase letters, digits and dashes.
	Name string
	// FactoryID is set when the event concerns one factory, for streams filtered to it.
	FactoryID domain.FactoryID
	// Data is the event's JSON object, sent to subscribers as it is.
	Data json.RawMessage
}

// CodeHost reaches the API of a code host (GitHub, GitLab) from goald, with the credential of a
// connection whose kind names the adapter (domain.KindSpec.CodeHost): the password of its
// rendered git login (domain.ResolvedConnection.GitLogin), sent only to that login's host. A
// pull request's repository comes from its URL, which must be on that host (otherwise an error
// wrapping ErrNotMergeable, before anything is sent). Adapters follow no redirects and never put
// the credential in errors or logs.
type CodeHost interface {
	// PullRequest returns the state of the pull request (a GitLab merge request) at url.
	PullRequest(ctx context.Context, c domain.ResolvedConnection, url string) (PullRequest, error)

	// Merge merges the pull request at url only while its head is still m.HeadSHA, and never
	// forces: a pull request the host refuses to merge is an error wrapping ErrNotMergeable.
	// It returns the merge commit's SHA.
	Merge(ctx context.Context, c domain.ResolvedConnection, url string, m Merge) (string, error)

	// DeleteBranch deletes a branch of the repository the pull request at url merges into; a
	// branch that is already gone is not an error.
	DeleteBranch(ctx context.Context, c domain.ResolvedConnection, url, branch string) error
}

// PullRequest is what goald needs to know before and after merging a pull request.
type PullRequest struct {
	URL    string
	Open   bool
	Merged bool
	// Mergeable is false while the host is still computing it, as well as when the pull
	// request has conflicts or fails required checks.
	Mergeable bool
	HeadSHA   string
	// HeadBranch is the branch the pull request merges from; SameRepository reports whether
	// that branch lives in the repository it merges into (not a fork). BaseBranch is the branch
	// it merges into.
	HeadBranch     string
	SameRepository bool
	BaseBranch     string
	// MergeSHA is the merge commit once merged.
	MergeSHA string
}

// Merge says how to merge a pull request.
type Merge struct {
	// Method is squash, merge or rebase.
	Method  string
	HeadSHA string
}

// Sidecars runs the stdio MCP servers of a run's connections, each in its own hardened
// container (a sidecar), so the gateway can serve their tools without running them on the host
// or in the agent's container.
type Sidecars interface {
	// Start pulls the image when it is missing and starts one sidecar. ctx bounds the start
	// only; the sidecar runs until it is closed, its server exits, or its run is reaped.
	Start(ctx context.Context, s SidecarSpec) (Sidecar, error)
}

// SidecarSpec is one stdio MCP server for one run.
type SidecarSpec struct {
	// OrgID is the organization of the run, which the sidecar is labelled with like its sandbox.
	OrgID      domain.OrgID
	FactoryID  domain.FactoryID
	ItemID     domain.ItemID
	RunID      domain.RunID
	Connection string
	// Image should be pinned (a version tag or a digest); the gateway never starts one that
	// is not.
	Image string
	// Command replaces the image's command (the arguments to its entrypoint) when set.
	Command []string
	// Env holds resolved values, credentials included; adapters never log it.
	Env map[string]string
	// Network gives the sidecar outbound network access; without it the sidecar has none.
	Network bool
}

// Sidecar is a started sidecar: reads return the server's stdout and writes go to its stdin
// (newline-delimited JSON-RPC). Close stops and removes the container; it is safe to call more
// than once.
type Sidecar interface {
	io.ReadWriteCloser
}

// Secrets resolves secret references at the edge. A reference is either a literal value or
// "env:NAME"; AWS Secrets Manager ARNs and op:// references come with their adapters.
type Secrets interface {
	// Resolve returns the value ref points to, or an error wrapping ErrSecretNotFound.
	Resolve(ctx context.Context, ref string) (string, error)
}

// Contracts validates station outputs against the named JSON Schema contracts of a definition
// version: the one the run started on, whatever was published meanwhile.
type Contracts interface {
	// Validate returns nil when output satisfies the version's contract, an error wrapping
	// ErrContractViolation (with the schema's explanation) when it does not, and any other
	// error when validation itself failed (unknown version or contract, unreadable schema).
	Validate(ctx context.Context, version domain.DefinitionVersionID, contract string, output json.RawMessage) error
}

// ObjectStore keeps objects in one bucket: an S3-compatible server (RustFS) locally, R2 on
// Cloudflare. Definition files and uploaded knowledge documents live there as immutable,
// content-addressed objects. Nothing relies on object versions (R2 has none): a write replaces
// the object at its key and a delete removes it for good. A bucket that still has versioning on
// works the same.
type ObjectStore interface {
	// Get returns the object at key, or an error wrapping ErrNotFound when there is none.
	Get(ctx context.Context, key string) (Object, error)

	// Put stores body at key, replacing what was there, and returns it.
	Put(ctx context.Context, key string, body []byte) (ObjectInfo, error)

	// PutIfAbsent stores body at key unless an object is there; created reports which, and info
	// is the object now at key. Content-addressed writes (definition blobs, domain.BlobKey) use
	// it, so an object is written once and never rewritten.
	PutIfAbsent(ctx context.Context, key string, body []byte) (info ObjectInfo, created bool, err error)

	// List returns every object whose key starts with prefix, ordered by key.
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)

	// Delete removes key for good; deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error

	// DeletePrefix removes every object whose key starts with prefix and returns how many it
	// removed. It refuses an empty prefix. The purge of a deleted organization uses it.
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// ObjectInfo identifies an object's content: its ETag changes whenever the content does, so a
// reader can cache what it parsed from an object until the ETag moves.
type ObjectInfo struct {
	Key  string
	ETag string
	Size int64
}

// Object is an object with its content.
type Object struct {
	ObjectInfo

	Body []byte
}

// Clock tells time, so tests can drive it by hand.
type Clock interface {
	Now() time.Time
	// NewTicker ticks every d until stopped; slow receivers miss ticks, as with time.Ticker.
	NewTicker(d time.Duration) Ticker
}

// Ticker is a stoppable periodic tick.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}
