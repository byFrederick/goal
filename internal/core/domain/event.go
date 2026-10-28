package domain

import (
	"regexp"
	"time"
)

// eventName is what an organization event may be called on the realtime stream.
var eventName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// IsEventName reports whether name may name an organization event on the realtime stream:
// lowercase letters and digits, words joined by single dashes, a letter first. The adapter that
// receives such events and the stream that writes them both refuse any other.
func IsEventName(name string) bool { return eventName.MatchString(name) }

// EventKind names what happened to an item. Kinds are open: the worker adds its own (tool_call, ...).
type EventKind string

// Event kinds the engine writes.
const (
	EventCreated        EventKind = "created"
	EventClaimed        EventKind = "claimed"
	EventRunStarted     EventKind = "run_started"
	EventRunFinished    EventKind = "run_finished"
	EventContractFailed EventKind = "contract_failed"
	EventCheckPassed    EventKind = "check_passed"
	EventCheckFailed    EventKind = "check_failed"
	EventMoved          EventKind = "moved"
	EventGateApproved   EventKind = "gate_approved"
	EventGateRejected   EventKind = "gate_rejected"
	// EventRuleViolated is one factory rule an accepted output says was broken (its violations,
	// ADR 0014 §6), written with the transition that accepts it: data rule, reason, station and
	// agent. Counting them by rule shows which rules break most.
	EventRuleViolated EventKind = "rule_violated"
	EventWaitingHuman EventKind = "waiting_human"
	EventApproved     EventKind = "approved"
	EventRejected     EventKind = "rejected"
	EventBlocked      EventKind = "blocked"
	EventLeaseExpired EventKind = "lease_expired"
	EventReleased     EventKind = "released"
	// EventAgentCancelled is an agent run the engine stopped; its item went back to its queue.
	// Data reason says why ("budget": the factory's weekly budget is used up, policy stop).
	EventAgentCancelled EventKind = "agent_cancelled"
	EventDone           EventKind = "done"
	EventRetried        EventKind = "retried"
	EventMerged         EventKind = "merged"
	// EventMergeConflict is a merge the engine stopped because a pull request's branch conflicts
	// with its base, sending the item back to be built again (flow.assemblies.onConflict rework).
	// It is not a rejection: the item's rework count is unchanged. Data reason (the pull request,
	// its base, the files and what to do) and pull_request.
	EventMergeConflict EventKind = "merge_conflict"
	// EventEdgeAdded and EventEdgeRemoved are a dependency of the item added or removed: data
	// needs (the item it needs), kind and actor (ref).
	EventEdgeAdded   EventKind = "edge_added"
	EventEdgeRemoved EventKind = "edge_removed"
	// EventAssembled is an item whose plan became parts (data parts: how many); EventPartCreated
	// a part created from it (data parent and key), its first event; EventAssemblyDone the
	// assembly closed by its last part (data state, parts_done and parts); EventPartAdded a part
	// added to an open assembly after it was assembled (data part: its id, key and actor: ref);
	// EventAssemblyLanding an assembly with an integration branch whose last part closed with a
	// part done, moved queued to its landing station instead of closing (data parts_done, parts
	// and branch).
	EventAssembled       EventKind = "assembled"
	EventPartCreated     EventKind = "part_created"
	EventPartAdded       EventKind = "part_added"
	EventAssemblyDone    EventKind = "assembly_done"
	EventAssemblyLanding EventKind = "assembly_landing"
	// EventUsage is a running agent's report of one turn's token usage; a run's usage events
	// count its turns so far.
	EventUsage EventKind = "usage"
	// EventNote is a person's note for the agents working the item; data text and actor (ref).
	// The next run's brief carries it, and a run in progress reads it from the gateway
	// (GET /run?notes_after) between its agent's turns.
	EventNote EventKind = "note"
	// EventNoteDelivered is a running agent's worker reporting that it gave the agent a note
	// left while the run works, as a user message between turns. Data note_id (the note's event
	// id); the engine stamps the run.
	EventNoteDelivered EventKind = "note_delivered"
	// EventPaused is a person stopping an open item where it is: it is blocked until a retry.
	// Data reason (optional) and actor (ref).
	EventPaused EventKind = "paused"
	// EventCancelled is a person closing an open item where it is, for good: it is cancelled.
	// Data reason (optional) and actor (ref); a part cancelled with its assembly names it
	// (assembly), and an assembly's counts the parts it cancelled (parts).
	EventCancelled EventKind = "cancelled"
	// EventLaneSet is a person setting an open item's lane, which picks the agent at stations
	// that route by lane. Data from (the old lane, "" when it had none), to and actor (ref).
	EventLaneSet EventKind = "lane_set"
	// EventLeaseTaken is a person's action on an item in progress (move, pause, cancel, send back) that
	// took its lease: the instance running it stops the run. Data actor (ref) and action.
	EventLeaseTaken EventKind = "lease_taken"
	// EventToolDenied is a tool call the agent's policy (tools.deny, permissions) refused
	// before it ran, as the worker reports it at the end of a run. Data tool and id.
	EventToolDenied EventKind = "tool_denied"
	// EventSandboxStarted is a run's sandbox up, before it is prepared. Data size and seconds.
	EventSandboxStarted EventKind = "sandbox_started"
	// EventPrepared is a run's sandbox prepared for its agent: the run bundle fetched, the
	// repository cloned on the item's branch, the tools installed. Data seconds, tools (how many
	// of the definition's) and, with a repository, branch_existed.
	EventPrepared EventKind = "prepared"
	// EventRunRetried is a run whose sandbox was lost (ports.ErrSandboxLost: a redeploy, a box
	// restart, a network blip) and that the engine runs again in a new one at the same station.
	// Data reason, attempt (the lost one) and max.
	EventRunRetried EventKind = "run_retried"
	// EventRulesApplied is the factory rules a run ran with (ADR 0014), written by the gateway
	// once per run when it first serves the run's bundle, and only when a rule applies. Data
	// rules: [{name, sha256}], the rule's name and the hash of its rule file in the run's
	// definition version.
	EventRulesApplied EventKind = "rules_applied"
	// EventQuestionAsked is an agent asking a person a question while its run works (ADR 0018):
	// the run ends and the item waits for the answer at its station. Data question, choices (0–4
	// quick answers), agent and run_id.
	EventQuestionAsked EventKind = "question_asked"
	// EventQuestionAnswered is a person's answer to the item's open question: the item goes back
	// to its station's queue and the next run's brief carries the answer. Data answer (a picked
	// choice's text, or the person's words), choice_index only when a choice was picked, and
	// actor (ref).
	EventQuestionAnswered EventKind = "question_answered"
)

// Audit of items.
const (
	// AuditItemNoted is a note left on an item; its data is the note's length, never its text.
	AuditItemNoted = "item.noted"
	// AuditItemMoved is a person's move of an item to another station (from, to, reason).
	AuditItemMoved = "item.moved"
	// AuditItemPaused is a person pausing an item (station, reason).
	AuditItemPaused = "item.paused"
	// AuditItemLaneSet is a person setting an item's lane (from, to).
	AuditItemLaneSet = "item.lane_set"
	// AuditItemCancelled is a person cancelling an item (station, reason; assembly for a part
	// cancelled with its assembly).
	AuditItemCancelled = "item.cancelled"
	// AuditItemRejected is a person sending an item back for rework (from, to, reason).
	AuditItemRejected = "item.rejected"
	// AuditItemAnswered is a person's answer to an item's open question: the answer's length, and
	// choice_index when a choice was picked, never its text.
	AuditItemAnswered = "item.answered"
	// AuditItemPartAdded is a part added to an open assembly (part, key).
	AuditItemPartAdded = "item.part_added"
	// AuditTargetItem is the target kind of entries about an item.
	AuditTargetItem = "item"
)

// ActorEngine is the actor of events the engine writes on its own.
const ActorEngine = "engine"

// Event is one append-only entry in an item's history (item_events).
type Event struct {
	ID        int64 // assigned by the store
	FactoryID FactoryID
	ItemID    ItemID
	RunID     RunID // empty when no run is involved
	At        time.Time
	Kind      EventKind
	Station   string
	// Actor is an agent name, ActorEngine or the human who acted.
	Actor string
	Data  map[string]any
}

// ActivityKinds are the milestone kinds a factory's activity lists (GET .../activity): what
// moved an item along, never a run's per-turn events (tool_call, usage, ...).
var ActivityKinds = []EventKind{
	EventCreated, EventMoved, EventGateApproved, EventGateRejected, EventApproved, EventRejected,
	EventBlocked, EventCheckFailed, EventMerged, EventDone, EventAssembled, EventCancelled,
}

// ActivityEvent is one entry of a factory's activity: a milestone event with its item's number
// and title.
type ActivityEvent struct {
	Event

	Number int64
	Title  string
}
