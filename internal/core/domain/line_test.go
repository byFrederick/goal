package domain

import (
	"slices"
	"strings"
	"testing"
)

// An agent's own caps replace the runtime's; an unset one falls back to the runtime's.
func TestAgentSpec_Limits(t *testing.T) {
	t.Parallel()

	rt := Runtime{MaxTurnsPerTask: 40, PerItemUSD: 3}

	tests := []struct {
		name      string
		agent     AgentSpec
		wantTurns int
		wantUSD   float64
	}{
		{name: "runtime's", wantTurns: 40, wantUSD: 3},
		{name: "own turns", agent: AgentSpec{MaxTurns: 12}, wantTurns: 12, wantUSD: 3},
		{name: "own budget", agent: AgentSpec{PerItemUSD: 1.5}, wantTurns: 40, wantUSD: 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if turns, usd := tt.agent.Limits(rt); turns != tt.wantTurns || usd != tt.wantUSD {
				t.Errorf("Limits = %d, %v; want %d, %v", turns, usd, tt.wantTurns, tt.wantUSD)
			}
		})
	}
}

// Since the cutover a line runs only on connections: a version with a form they replaced, or
// without a model connection, is refused with what to do, naming the forms.
func TestLine_Refusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line Line
		want []string // in the reason; none for a line that runs
	}{
		{name: "on connections", line: Line{Runtime: Runtime{ModelConnection: "claude"}}},
		{
			name: "a removed form", line: Line{
				Number: 3, Runtime: Runtime{ModelConnection: "claude"}, Removed: []string{"spec.runtime.repository.token (env-reference-removed)"},
			},
			want: []string{"version 3", "spec.runtime.repository.token (env-reference-removed)", "publish a version with connections (spec 4 §4.4)"},
		},
		{
			name: "no model connection", line: Line{Number: 2},
			want: []string{"version 2", "runtime.modelConnection", "publish a version with connections (spec 4 §4.4)"},
		},
	}

	for _, tt := range tests {
		got := tt.line.Refusal()
		if (got == "") != (len(tt.want) == 0) {
			t.Errorf("%s: refusal %q; want one: %v", tt.name, got, len(tt.want) > 0)
		}

		for _, w := range tt.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: refusal %q; want %q in it", tt.name, got, w)
			}
		}
	}
}

// A merge may use the line's connections and its repository's, each once.
func TestLine_MergeConnections(t *testing.T) {
	t.Parallel()

	const repos = "repos"

	l := Line{Connections: []string{repos, "datadog"}, Runtime: Runtime{Repository: Repository{Connection: "gitlab"}}}
	if got := l.MergeConnections(); !slices.Equal(got, []string{repos, "datadog", "gitlab"}) {
		t.Errorf("MergeConnections = %v", got)
	}

	l.Runtime.Repository.Connection = repos
	if got := l.MergeConnections(); !slices.Equal(got, []string{repos, "datadog"}) {
		t.Errorf("MergeConnections with the repository's listed = %v", got)
	}
}

// A station's lane agents are its agent, or each agent its route uses once, sorted; queue,
// human and done stations have none.
func TestStation_LaneAgents(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		st   Station
		want []string
	}{
		{"single agent", Station{Kind: KindAgent, Agent: "writer"}, []string{"writer"}},
		{"routed", Station{Kind: KindAgent, Route: map[string]string{"web": "web-dev", "api": "api-dev", "db": "api-dev"}}, []string{"api-dev", "web-dev"}},
		{"queue", Station{Kind: KindQueue}, []string{}},
		{"human", Station{Kind: KindHuman}, []string{}},
	} {
		if got := tt.st.LaneAgents(); !slices.Equal(got, tt.want) || got == nil {
			t.Errorf("%s: lane agents = %#v, want %#v", tt.name, got, tt.want)
		}
	}
}

// A name reads as words, the first capitalized: never the raw name of a definition.
func TestStation_Humanize(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"infra-engineer":   "Infra engineer",
		"backend":          "Backend",
		"code_reviewer":    "Code reviewer",
		"merge":            "Merge",
		" qa--lead ":       "Qa lead",
		"":                 "",
		"élan-agent":       "Élan agent",
		"Already Readable": "Already Readable",
	} {
		if got := Humanize(name); got != want {
			t.Errorf("Humanize(%q) = %q, want %q", name, got, want)
		}
	}
}

// The landing station is integration.landAt when set, else the first merge station after the
// planning station.
func TestLine_LandingStation(t *testing.T) {
	t.Parallel()

	gate := &Gate{OnReject: stationBuild}
	stations := []Station{
		{ID: "backlog", Kind: KindQueue},
		{ID: "early-merge", Kind: KindAgent, Gate: gate, Action: ActionMerge},
		{ID: "planning", Kind: KindAgent},
		{ID: stationBuild, Kind: KindAgent},
		{ID: "gatekeep", Kind: KindAgent, Gate: gate},
		{ID: "ship", Kind: KindAgent, Gate: gate, Action: ActionMerge},
		{ID: "ship-again", Kind: KindAgent, Gate: gate, Action: ActionMerge},
		{ID: "finished", Kind: KindDone},
	}
	early, planning, ship, shipAgain := stations[1].ID, stations[2].ID, stations[5].ID, stations[6].ID

	tests := []struct {
		name, landAt, plan, want string
		ok                       bool
	}{
		{name: "landAt", landAt: shipAgain, plan: planning, want: shipAgain, ok: true},
		{name: "first merge after the plan", plan: planning, want: ship, ok: true},
		{name: "merge before the plan skipped", plan: stationBuild, want: ship, ok: true},
		{name: "plan at a merge station", plan: early, want: ship, ok: true},
		{name: "no merge station after the plan", plan: shipAgain},
		{name: "plan not on the line", plan: "nowhere"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := Line{Stations: stations}
			l.Flow.Assemblies.Integration.LandAt = tt.landAt

			s, ok := l.LandingStation(tt.plan)
			if ok != tt.ok || s.ID != tt.want {
				t.Errorf("LandingStation(%q) = %q, %v; want %q, %v", tt.plan, s.ID, ok, tt.want, tt.ok)
			}
		})
	}

	if s, ok := (Line{Stations: []Station{stations[2], stations[7]}}).LandingStation(planning); ok {
		t.Errorf("LandingStation on a line with no merge station = %q, want none", s.ID)
	}
}
