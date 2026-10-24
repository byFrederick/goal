// Package ports declares the interfaces the engine uses to reach the outside world. Adapters in
// internal/adapters implement them; internal/ports/porttest holds in-memory fakes for tests.
//
// Ports import only internal/domain. Knowledge (kb_search, kb_write, sync) and ModelAuth
// (per-factory model credentials) join this package when the engine first needs them; until
// then model credentials reach tasks as secret references in the engine's task environment.
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
	// conflicts or failing required checks, its head moved, or it is not on the expected
	// repository. Nothing was changed.
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
	// returns each item to queued at its station, marks the item's running runs failed, and
	// appends a lease_expired event per item. It returns the leases it removed.
	RequeueExpiredLeases(ctx context.Context, now time.Time, limit int) ([]domain.Lease, error)

	// StartRun records a running agent run and appends a run_started event.
	StartRun(ctx context.Context, r NewRun) (domain.RunID, error)

	// FinishRun records a run's result, adds its cost to the item and appends a run_finished
	// event with its status, error, cost and turns. Finishing a run twice is a no-op.
	FinishRun(ctx context.Context, id domain.RunID, res domain.RunResult, endedAt time.Time) error

	// Transition moves an item as described by t, compare-and-set on t.From (and on t.Lease
	// when set). It returns ErrConflict or ErrLeaseLost when the guard fails; nothing changes.
	Transition(ctx context.Context, t Transition) (domain.Item, error)

	// AppendEvents appends events that come with no state change.
	AppendEvents(ctx context.Context, events ...domain.Event) error

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
	// many wait (queued or ready) and how many are in progress. Order is unspecified.
	StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error)

	// FlowMetrics measures a factory's flow: items done and spend in [since, until), and the
	// work in the line now.
	FlowMetrics(ctx context.Context, factory domain.FactoryID, since, until time.Time) (domain.FlowMetrics, error)

	// SpendSince is the summed cost_usd of the factory's agent runs started at or after since;
	// 0 when there are none. The engine reads it once per tick for each factory with a weekly
	// budget (domain.Runtime.WeeklyBudget).
	SpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error)

	// Attention returns, for every factory of the organization, the items waiting for a person
	// (with their split by work type), the blocked items and the open andon pull, ordered by
	// factory id. Factories with nothing pending are included with zeros.
	Attention(ctx context.Context) ([]domain.Attention, error)

	// WaitingHuman returns up to limit items in state waiting_human in the given factories, most
	// recently changed first (updated_at descending, then id). Interim: phase 2.2-E (E11)
	// replaces it with FlowStats.InboxItems.
	WaitingHuman(ctx context.Context, factories []domain.FactoryID, limit int) ([]domain.Item, error)
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
	// see it. It appends org.deleted. (org)
	DeleteOrg(ctx context.Context, by domain.Actor, now time.Time) error
	// PurgeDeletedOrgs hard-deletes up to limit organizations deleted before cutoff, with every
	// row they own, and returns their ids. For each one, once its rows are gone and before that
	// is committed, it calls also (when not nil) to remove what the organization keeps outside
	// the database; an error from it keeps the organization, rows and all, for the next run. One
	// purge runs at a time across goald instances: while another holds the purge lock it returns
	// no ids. It needs no org (a system job).
	PurgeDeletedOrgs(ctx context.Context, cutoff time.Time, limit int, also func(context.Context, domain.OrgID) error) ([]domain.OrgID, error)
}

// NewOrg is an organization to create.
type NewOrg struct {
	Slug, Name, Mark string
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
	ItemID    domain.ItemID
	Station   string
	Agent     string
	Model     string
	StartedAt time.Time
}

// Transition is one atomic change to an item.
type Transition struct {
	ItemID domain.ItemID
	// From guards the change: the item must still be at this station in this state.
	From domain.Position
	To   domain.Position
	// Lease, when set, must still be held (same item and AcquiredAt); it is released.
	Lease *domain.Lease
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
// clients, so no instance holds state another needs.
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
// such as phase 3's credential and connection events.
type OrgEvent struct {
	// Name is the stream's event name: lowercase letters, digits and dashes.
	Name string
	// FactoryID is set when the event concerns one factory, for streams filtered to it.
	FactoryID domain.FactoryID
	// Data is the event's JSON object, sent to subscribers as it is.
	Data json.RawMessage
}

// CodeHost reaches the API of the host where a factory's repository lives (GitHub), with the
// repository's token, on the goald host only. Adapters never put the token in errors or logs.
type CodeHost interface {
	// PullRequest returns the state of the pull request at url, which must belong to repo
	// (otherwise an error wrapping ErrNotMergeable).
	PullRequest(ctx context.Context, repo domain.Repository, url string) (PullRequest, error)

	// Merge merges the pull request at url only while its head is still m.HeadSHA, and never
	// forces: a pull request the host refuses to merge is an error wrapping ErrNotMergeable.
	// It returns the merge commit's SHA.
	Merge(ctx context.Context, repo domain.Repository, url string, m Merge) (string, error)

	// DeleteBranch deletes a branch of repo; a branch that is already gone is not an error.
	DeleteBranch(ctx context.Context, repo domain.Repository, branch string) error
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
	// that branch lives in repo itself (not a fork). BaseBranch is the branch it merges into.
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

// Runner starts agent tasks: a container on a laptop (Docker) or a task on AWS (ECS).
type Runner interface {
	// Start launches one agent task. ctx bounds the task's whole life: cancelling it stops the
	// task, after which Wait returns a cancelled result.
	Start(ctx context.Context, t TaskSpec) (Task, error)
}

// Task is a started agent task.
type Task interface {
	// Events streams the task's progress (tool calls, messages). It is closed when the task
	// ends, and the caller must drain it.
	Events() <-chan RunEvent
	// Wait blocks until the task ends and returns its result. A task that ran and failed
	// returns a result with status failed; an error means the result could not be obtained.
	Wait() (domain.RunResult, error)
}

// RunEvent is one progress event from a running task.
type RunEvent struct {
	Kind domain.EventKind
	At   time.Time
	Data map[string]any
}

// TaskSpec is everything a runner needs to run one agent on one item.
type TaskSpec struct {
	FactoryID domain.FactoryID
	ItemID    domain.ItemID
	RunID     domain.RunID
	Station   string
	Agent     string
	Image     string
	Model     string
	// PromptPath and skill sources are relative to the factory's root; the runner fetches them.
	PromptPath string
	Skills     []domain.SkillRef
	ToolsAllow []string
	ToolsDeny  []string
	// GatewayURL is the MCP gateway; GatewayToken scopes this task's access to it.
	GatewayURL   string
	GatewayToken string
	// Purpose is what the factory is for; the agent reads it before the work. Empty when unset.
	Purpose string
	// Brief is the work: the item's title and body, plus feedback from a failed attempt.
	Brief string
	// Inputs are the item's outputs from earlier stations, by station id.
	Inputs map[string]json.RawMessage
	// Output names the contract the result must satisfy; empty when none.
	Output string
	// WorkspaceDir is the host path of the git checkout prepared for this run; the task works in
	// it and never receives git or GitHub credentials.
	WorkspaceDir string
	// Env holds resolved secrets and settings for the task. Runners never log it.
	Env     map[string]string
	Timeout time.Duration
	Budget  Budget
}

// Budget limits one task; zero values mean unset. The worker enforces them.
type Budget struct {
	MaxTurns int
	MaxUSD   float64
}

// Workspaces prepares the host folder an agent task works in: a fresh clone of the factory's
// repository on the item's branch, or an empty folder when the factory has none. Credentials
// for the clone are used on the host only; nothing in the folder holds them.
type Workspaces interface {
	// Prepare creates the workspace of one run. The caller removes it with Remove.
	Prepare(ctx context.Context, w WorkspaceSpec) (Workspace, error)
	// Remove deletes a workspace; removing one that is already gone is not an error.
	Remove(ctx context.Context, w Workspace) error
}

// WorkspaceSpec describes the workspace of one run.
type WorkspaceSpec struct {
	FactoryID domain.FactoryID
	ItemID    domain.ItemID
	RunID     domain.RunID
	// Repository is cloned into the workspace; the zero value means an empty workspace.
	Repository domain.Repository
	// Branch is checked out: the remote branch when it exists, otherwise a new branch from the
	// repository's default branch.
	Branch string
}

// Workspace is a prepared workspace.
type Workspace struct {
	RunID domain.RunID
	// Dir is the host path of the checkout (TaskSpec.WorkspaceDir).
	Dir string
	// Branch is the branch checked out; empty for an empty workspace.
	Branch string
	// Existed reports whether the branch already existed in the repository.
	Existed bool
}

// Checker runs a station's command checks against a run's workspace. Agent-written code is
// never executed on the host: each check runs isolated (a hardened container on a laptop).
type Checker interface {
	// Check runs one command and reports how it exited. An error means the check could not be
	// run at all, as opposed to a command that ran and failed.
	Check(ctx context.Context, c CheckSpec) (CheckResult, error)
}

// CheckSpec is one command check.
type CheckSpec struct {
	FactoryID domain.FactoryID
	ItemID    domain.ItemID
	RunID     domain.RunID
	Station   string
	// Image runs the command; it is the factory's agent image.
	Image string
	// Command is a shell command line run in the workspace's root.
	Command      string
	WorkspaceDir string
	Timeout      time.Duration
}

// CheckResult is how a command check ended.
type CheckResult struct {
	ExitCode int
	// Output is the tail of the command's combined output.
	Output   string
	Duration time.Duration
}

// Passed reports whether the command exited 0.
func (r CheckResult) Passed() bool { return r.ExitCode == 0 }

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

// Contracts validates station outputs against a factory's named JSON Schema contracts.
type Contracts interface {
	// Validate returns nil when output satisfies the factory's contract, an error wrapping
	// ErrContractViolation (with the schema's explanation) when it does not, and any other
	// error when validation itself failed (unknown contract, unreadable schema).
	Validate(ctx context.Context, factory domain.FactoryID, contract string, output json.RawMessage) error
}

// ObjectStore keeps objects in one versioned bucket: S3 on AWS, an S3-compatible server
// (RustFS) on a laptop. Factory definitions and knowledge documents live there; every write
// creates a new object version, so earlier content stays readable by its version id.
type ObjectStore interface {
	// Get returns the object at key. An empty version means the current one. It returns an
	// error wrapping ErrNotFound when the key or version does not exist or was deleted.
	Get(ctx context.Context, key, version string) (Object, error)

	// Put stores body at key as a new current version and returns it.
	Put(ctx context.Context, key string, body []byte) (ObjectInfo, error)

	// PutIfAbsent stores body at key unless a current object is there (a key whose current
	// version is a delete marker counts as absent); created reports which, and info is the
	// object now at key. Content-addressed writes (definition blobs, domain.BlobKey) use it, so
	// an object is written once and never rewritten.
	PutIfAbsent(ctx context.Context, key string, body []byte) (info ObjectInfo, created bool, err error)

	// List returns every current object whose key starts with prefix, ordered by key.
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)

	// Delete makes key absent. Earlier versions stay readable by version id; deleting a missing
	// key is not an error.
	Delete(ctx context.Context, key string) error
}

// ObjectInfo identifies one version of an object.
type ObjectInfo struct {
	Key     string
	Version string
	Size    int64
}

// Object is one version of an object with its content.
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
