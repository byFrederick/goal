package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// ErrNoRoute means a routed station has no agent for the item's lane.
var ErrNoRoute = errors.New("no route for lane")

// StationKind says who works at a station.
type StationKind string

// Station kinds, as written in factory.yaml.
const (
	KindQueue StationKind = "queue"
	KindAgent StationKind = "agent"
	KindHuman StationKind = "human"
	KindDone  StationKind = "done"
)

// StationAction is what goald does on a human's behalf when they act at a human station.
type StationAction string

// ActionMerge marks the human station where approving an item means merging its pull request.
const ActionMerge StationAction = "merge"

// Line is the part of a factory definition the engine needs: its stations in order, its
// agents and its flow policy. internal/config builds it from factory.yaml.
type Line struct {
	// OrgID is the organization that owns the factory; the engine binds every store call it
	// makes for the line to it. It comes from where the definition is stored, never from the
	// definition itself.
	OrgID     OrgID
	FactoryID FactoryID
	Runtime   Runtime
	Stations  []Station
	Agents    []AgentSpec
	// ExpediteTypes are the work types that enter with class of service expedite.
	ExpediteTypes []string
	// Flow is the line's flow policy and targets.
	Flow FlowPolicy
	// Purpose is what the factory is for; every agent task carries it.
	Purpose string
	// VersionID and Number name the published definition version the line comes from; every
	// run the engine starts records it and runs on it to the end.
	VersionID DefinitionVersionID
	Number    int
	// Paused lines take no work: the engine claims and releases nothing at the factory, and
	// runs in progress finish.
	Paused bool
}

// Runtime is what every agent task of the factory shares.
type Runtime struct {
	Image string
	Model string // default model; an agent's own Model wins
	// MaxTurnsPerTask and PerItemUSD are passed to the runner as limits; zero means unset.
	MaxTurnsPerTask int
	PerItemUSD      float64
	// WeeklyUSD caps the factory's agent spend over BudgetWindow; zero means unlimited. The
	// engine enforces it (WeeklyBudget) by OnExhausted, whose zero value is BudgetDrain.
	WeeklyUSD   float64
	OnExhausted BudgetPolicy
	// Repository is what every task's workspace is cloned from; the zero value means tasks
	// get an empty workspace.
	Repository Repository
}

// Repository is a git repository that agent tasks work on.
type Repository struct {
	// URL is an https clone URL without credentials.
	URL string
	// AuthRef is a secret reference to the token goald clones with, on the host only; it never
	// reaches a task. Empty for a public repository.
	AuthRef string
}

// Station is one step of the line.
type Station struct {
	ID    string
	Kind  StationKind
	Agent string
	// Route maps an item lane to an agent; used when Agent is empty.
	Route map[string]string
	// Agents is how many agent slots the station runs; WIP caps items in progress there.
	Agents int
	WIP    int
	// Buffer is how many items are kept ready in front of the station; it matters at the drum.
	Buffer int
	// Output names the contract the station's output must satisfy; empty means none.
	Output string
	// Checks are commands run against the run's workspace, isolated from the host, after the
	// output satisfies its contract; each must exit 0 before the item moves on.
	Checks []string
	Gate   *Gate
	// Action is set on human stations where goald acts for the human (ActionMerge); empty
	// means the human acts outside Goal and only records the decision.
	Action StationAction
}

// Gate sends rejected work back to OnReject, at most MaxRework times per item.
type Gate struct {
	OnReject  string
	MaxRework int
}

// AgentSpec is everything a runner needs to start one agent.
type AgentSpec struct {
	Name string
	// PromptPath is the agent's prompt file, resolved against the factory folder.
	PromptPath string
	Model      string
	// MaxTurns and PerItemUSD cap one task of this agent; zero falls back to the Runtime's.
	MaxTurns    int
	PerItemUSD  float64
	Skills      []SkillRef
	Connections []string
	ToolsAllow  []string
	ToolsDeny   []string
}

// SkillRef is a skill an agent loads: a local folder (resolved path) or a skills.sh source.
type SkillRef struct {
	Name   string
	Source string
}

// Station returns the station with the given id.
func (l Line) Station(id string) (Station, bool) {
	i := slices.IndexFunc(l.Stations, func(s Station) bool { return s.ID == id })
	if i < 0 {
		return Station{}, false
	}

	return l.Stations[i], true
}

// First returns the station new items enter at.
func (l Line) First() (Station, error) {
	if len(l.Stations) == 0 {
		return Station{}, fmt.Errorf("factory %s: line has no stations", l.FactoryID)
	}

	return l.Stations[0], nil
}

// Next returns the station after id.
func (l Line) Next(id string) (Station, error) {
	i := slices.IndexFunc(l.Stations, func(s Station) bool { return s.ID == id })
	if i < 0 {
		return Station{}, fmt.Errorf("station %q is not on the line", id)
	}

	if i == len(l.Stations)-1 {
		return Station{}, fmt.Errorf("station %q is the last station", id)
	}

	return l.Stations[i+1], nil
}

// ReworkTarget is where rejected work at station id goes back to: the gate's onReject, or
// else the nearest agent station before it.
func (l Line) ReworkTarget(id string) (Station, error) {
	i := slices.IndexFunc(l.Stations, func(s Station) bool { return s.ID == id })
	if i < 0 {
		return Station{}, fmt.Errorf("station %q is not on the line", id)
	}

	if g := l.Stations[i].Gate; g != nil {
		if s, ok := l.Station(g.OnReject); ok {
			return s, nil
		}

		return Station{}, fmt.Errorf("station %q: gate.onReject %q is not on the line", id, g.OnReject)
	}

	for j := i - 1; j >= 0; j-- {
		if l.Stations[j].Kind == KindAgent {
			return l.Stations[j], nil
		}
	}

	return Station{}, fmt.Errorf("station %q: no agent station upstream to send work back to", id)
}

// Agent returns the agent with the given name.
func (l Line) Agent(name string) (AgentSpec, bool) {
	i := slices.IndexFunc(l.Agents, func(a AgentSpec) bool { return a.Name == name })
	if i < 0 {
		return AgentSpec{}, false
	}

	return l.Agents[i], true
}

// ClassFor returns the class of service a new item of workType enters with.
func (l Line) ClassFor(workType string) ClassOfService {
	if slices.Contains(l.ExpediteTypes, workType) {
		return ClassExpedite
	}

	return ClassStandard
}

// ItemTarget is the lead time an item of class c is measured against: the expedite target for
// expedite items, the line's lead-time target otherwise; 0 means no target.
func (l Line) ItemTarget(c ClassOfService) time.Duration {
	if c == ClassExpedite {
		return l.Flow.Targets.ExpediteLeadTime
	}

	return l.Flow.Targets.LeadTime
}

// Capacity is how many items the station may have in progress: min(agents, wip), where an
// unset value does not limit and a station always has at least one slot.
func (s Station) Capacity() int {
	n := max(s.Agents, 1)
	if s.WIP > 0 {
		n = min(n, s.WIP)
	}

	return n
}

// AgentFor returns the agent that works an item of the given lane at this station.
func (s Station) AgentFor(lane string) (string, error) {
	if s.Agent != "" {
		return s.Agent, nil
	}

	if a, ok := s.Route[lane]; ok {
		return a, nil
	}

	return "", fmt.Errorf("station %s, lane %q: %w", s.ID, lane, ErrNoRoute)
}

// Arrival is the state an item takes when it arrives at this station.
func (s Station) Arrival() ItemState {
	switch s.Kind {
	case KindHuman:
		return StateWaitingHuman
	case KindDone:
		return StateDone
	case KindQueue, KindAgent:
		return StateQueued
	}

	return StateQueued
}

// CanRework reports whether an item already reworked reworkCount times may go back again.
func (g Gate) CanRework(reworkCount int) bool { return reworkCount < g.MaxRework }

// Limits are the caps of one task of this agent: its own, else the runtime's.
func (a AgentSpec) Limits(rt Runtime) (maxTurns int, perItemUSD float64) {
	maxTurns, perItemUSD = a.MaxTurns, a.PerItemUSD
	if maxTurns == 0 {
		maxTurns = rt.MaxTurnsPerTask
	}

	if perItemUSD == 0 {
		perItemUSD = rt.PerItemUSD
	}

	return maxTurns, perItemUSD
}
