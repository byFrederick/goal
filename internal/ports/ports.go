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
	// ErrConflict means the item is no longer where a transition expected it.
	ErrConflict = errors.New("item moved concurrently")
	// ErrLeaseLost means the lease expired or was released; the holder must stop working.
	ErrLeaseLost = errors.New("lease lost")
	// ErrContractViolation means an output does not satisfy its contract (as opposed to the
	// validator itself failing).
	ErrContractViolation = errors.New("contract violation")
	// ErrSecretNotFound means a secret reference names nothing.
	ErrSecretNotFound = errors.New("secret not found")
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
	// It returns ErrNoWork when nothing waits and ErrSlotTaken when the slot already holds a
	// lease; in both cases nothing changes. The unique (factory, station, slot) lease is what
	// keeps WIP at or under capacity across instances.
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
	// event. Finishing a run twice is a no-op.
	FinishRun(ctx context.Context, id domain.RunID, res domain.RunResult, endedAt time.Time) error

	// Transition moves an item as described by t, compare-and-set on t.From (and on t.Lease
	// when set). It returns ErrConflict or ErrLeaseLost when the guard fails; nothing changes.
	Transition(ctx context.Context, t Transition) (domain.Item, error)

	// AppendEvents appends events that come with no state change.
	AppendEvents(ctx context.Context, events ...domain.Event) error
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
	// Brief is the work: the item's title and body, plus feedback from a failed attempt.
	Brief string
	// Inputs are the item's outputs from earlier stations, by station id.
	Inputs map[string]json.RawMessage
	// Output names the contract the result must satisfy; empty when none.
	Output string
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
