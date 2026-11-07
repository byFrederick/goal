package domain

import (
	"math"
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
		code string   // RefusalCode
	}{
		{
			name: "on connections", line: Line{Number: 5, Runtime: Runtime{ModelConnection: testModelConn}},
			want: []string{"version 5", "on connections", "publish a version on environment files and secrets (ADR 0037)"},
		},
		{
			name: "a removed form", line: Line{
				Number: 3, Runtime: Runtime{ModelConnection: testModelConn}, Removed: []string{"spec.runtime.repository.token (env-reference-removed)"},
			},
			want: []string{"version 3", "spec.runtime.repository.token (env-reference-removed)", "publish a version on environment files and secrets (ADR 0037)"},
		},
		{
			name: "neither a model key nor environments", line: Line{Number: 2},
			want: []string{"version 2", "on connections", "runtime.modelKey"},
		},
		{name: "on secrets", line: Line{Runtime: Runtime{ModelKey: testModelKey}, Environments: []Environment{{Name: DefaultEnvironment}}}},
		{name: "a model key without environments", line: Line{Runtime: Runtime{ModelKey: testModelKey}}},
		{
			name: "environments without a model key", line: Line{
				Number: 4, Runtime: Runtime{ModelConnection: testModelConn}, Environments: []Environment{{Name: DefaultEnvironment}},
			},
			want: []string{"version 4", "runtime.modelKey"}, code: BlockModelKeyMissing,
		},
	}

	for _, tt := range tests {
		if got := tt.line.RefusalCode(); got != tt.code {
			t.Errorf("%s: refusal code %q; want %q", tt.name, got, tt.code)
		}

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

// A repository's host names its code host on github.com and gitlab.com, whatever the case;
// forge names it on any host and wins over the host's.
func TestRepository_CodeHost(t *testing.T) {
	t.Parallel()

	const forgeGitHub, forgeGitLab = "github", "gitlab"

	tests := []struct {
		repo                Repository
		wantHost, wantForge string
	}{
		{repo: Repository{URL: "https://github.com/acme/app.git"}, wantHost: hostGitHub, wantForge: forgeGitHub},
		{repo: Repository{URL: "https://GitLab.com/acme/web.git"}, wantHost: "gitlab.com", wantForge: forgeGitLab},
		{repo: Repository{URL: "https://git.example.com/acme/app.git"}, wantHost: "git.example.com"},
		{repo: Repository{URL: "https://git.example.com:8443/acme/app.git", Forge: forgeGitLab}, wantHost: "git.example.com:8443", wantForge: forgeGitLab},
		{repo: Repository{URL: "https://github.com/acme/app.git", Forge: forgeGitLab}, wantHost: hostGitHub, wantForge: forgeGitLab},
		{repo: Repository{}},
	}

	for _, tt := range tests {
		if host, forge := tt.repo.CodeHost(); host != tt.wantHost || forge != tt.wantForge {
			t.Errorf("CodeHost of %+v = %q, %q; want %q, %q", tt.repo, host, forge, tt.wantHost, tt.wantForge)
		}
	}

	if KnownForge("GITHUB.COM") != forgeGitHub || KnownForge("git.example.com") != "" {
		t.Error("KnownForge does not name github.com's adapter alone")
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

// A merge station takes turns per merge target unless its merges is allow; a station that does
// not merge never takes turns, whatever its merges says.
func TestStation_TakesTurns(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		st   Station
		want bool
	}{
		{"merge station by default", Station{Kind: KindAgent, Action: ActionMerge}, true},
		{"merge station that serializes", Station{Kind: KindHuman, Action: ActionMerge, Merges: MergesSerialize}, true},
		{"merge station that allows", Station{Kind: KindAgent, Action: ActionMerge, Merges: MergesAllow}, false},
		{"agent station", Station{Kind: KindAgent}, false},
		{"agent station with serialize", Station{Kind: KindAgent, Merges: MergesSerialize}, false},
		{"human station", Station{Kind: KindHuman}, false},
	} {
		if got := tt.st.TakesTurns(); got != tt.want {
			t.Errorf("%s: takes turns = %v, want %v", tt.name, got, tt.want)
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

// A factory that leaves learning.jonah out gets reviews on, five touches and $10 a week (ADR 0034).
func TestDefaultJonahPolicy(t *testing.T) {
	t.Parallel()

	want := JonahPolicy{Enabled: true, ProposeAfter: 3, Share: 0.05}
	if got := DefaultJonahPolicy(); got != want {
		t.Errorf("DefaultJonahPolicy() = %+v, want %+v", got, want)
	}
}

// Jonah's budget is its share of the line's spend, each review is capped at a fifth of it, and a
// review's cap is what is left under that, at least a cent (ADR 0039 decision 16).
func TestJonahPolicy_Budget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		share         float64
		spend         JonahSpend
		wantBudget    float64
		wantPerReview float64
		wantCap       float64
		wantExhausted bool
	}{
		{name: "nothing spent", share: 0.05, spend: JonahSpend{LineUSD: 200}, wantBudget: 10, wantPerReview: 2, wantCap: 2},
		{name: "little left", share: 0.05, spend: JonahSpend{LineUSD: 200, JonahUSD: 9.5}, wantBudget: 10, wantPerReview: 2, wantCap: 0.5},
		{name: "used up", share: 0.05, spend: JonahSpend{LineUSD: 200, JonahUSD: 10}, wantBudget: 10, wantPerReview: 2, wantCap: 0.01, wantExhausted: true},
		{name: "no line spend", share: 0.05, spend: JonahSpend{}, wantCap: 0.01, wantExhausted: true},
		{name: "share off", share: 0, spend: JonahSpend{LineUSD: 200}, wantCap: 0.01, wantExhausted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := JonahPolicy{Share: tt.share}.Budget(tt.spend)
			if !near(b.BudgetUSD, tt.wantBudget) || !near(b.PerReviewUSD, tt.wantPerReview) || !near(b.SpentUSD, tt.spend.JonahUSD) {
				t.Errorf("budget = %+v, want $%v, $%v per review", b, tt.wantBudget, tt.wantPerReview)
			}

			if got := b.ReviewCap(); !near(got, tt.wantCap) {
				t.Errorf("ReviewCap() = %v, want %v", got, tt.wantCap)
			}

			if got := b.Exhausted(); got != tt.wantExhausted {
				t.Errorf("Exhausted() = %v, want %v", got, tt.wantExhausted)
			}
		})
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// A station waits for CI when its WaitFor is set; the zero value is a station without waitFor.
func TestStation_WaitsForCI(t *testing.T) {
	t.Parallel()

	if (Station{Kind: KindAgent}).WaitsForCI() {
		t.Error("a station without waitFor waits for CI")
	}

	st := Station{Kind: KindAgent, WaitFor: CIWait{Grace: DefaultCIGrace, Max: DefaultCIMax, Rounds: DefaultCIRounds}}
	if !st.WaitsForCI() {
		t.Error("a station with waitFor does not wait for CI")
	}
}
