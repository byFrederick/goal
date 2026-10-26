package engine_test

import (
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// Rule names the violations tests' line declares.
const (
	ruleTestsFirst = "tests-first"
	ruleWrapErrors = "wrap-errors"
)

// rulesLine is reviewLine whose version declares two rules.
func rulesLine() domain.Line {
	l := reviewLine(2)
	l.Rules = []domain.Rule{{Name: ruleTestsFirst}, {Name: ruleWrapErrors}}

	return l
}

func TestFinish_RecordsRuleViolations(t *testing.T) {
	t.Parallel()

	two := `[{"rule":"tests-first","reason":"no failing test before the fix"},` +
		`{"rule":"wrap-errors","reason":"the store error is returned bare"}]`

	bothViolated := []string{ruleTestsFirst + "=no failing test before the fix", ruleWrapErrors + "=the store error is returned bare"}

	tests := []struct {
		name   string
		review string
		want   []string // rule=reason of the rule_violated events, in order
		state  domain.ItemState
		logged string // a rule the log must name as unknown
	}{
		{
			name:   "two violations at an approving review",
			review: `{"decision":"approve","violations":` + two + `}`,
			want:   bothViolated, state: domain.StateDone,
		},
		{
			name:   "a rejecting review records its violations",
			review: `{"decision":"reject","reason":"see the violations","violations":` + two + `}`,
			want:   bothViolated, state: domain.StateQueued,
		},
		{name: "no violations", review: `{"decision":"approve"}`, state: domain.StateDone},
		{name: "an empty list", review: `{"decision":"approve","violations":[]}`, state: domain.StateDone},
		{name: "not a list", review: `{"decision":"approve","violations":"tests-first"}`, state: domain.StateDone},
		{
			name: "unknown and empty rules are not counted",
			review: `{"decision":"approve","violations":[{"rule":"no-such-rule","reason":"made up"},` +
				`{"rule":"","reason":"nameless"},{"reason":"no rule"},` +
				`{"rule":"wrap-errors","reason":"the store error is returned bare"}]}`,
			want: bothViolated[1:], state: domain.StateDone, logged: "no-such-rule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, rulesLine(), byAgent(map[string]string{agWorker: `{}`, agReviewer: tt.review}))
			it := h.submit(t, domain.NewItem{})

			h.step(t) // inbox → build, built → review
			h.step(t) // reviewed

			if got := h.item(t, it.ID); got.State != tt.state {
				t.Fatalf("item is %s, want %s (events %v)", got.State, tt.state, h.kinds(it.ID))
			}

			if got := violatedRules(t, h.eventsOf(it.ID, domain.EventRuleViolated)); got != strings.Join(tt.want, ",") {
				t.Fatalf("rule_violated rules = %s, want %v (events %v)", got, tt.want, h.kinds(it.ID))
			}

			if tt.logged != "" {
				logs := h.logs.String()
				if !strings.Contains(logs, `"msg":"unknown rule violated"`) || !strings.Contains(logs, tt.logged) {
					t.Errorf("logs do not name the unknown rule %s:\n%s", tt.logged, logs)
				}
			}
		})
	}

	t.Run("an output that fails its contract records none", func(t *testing.T) {
		t.Parallel()

		work := agentStation(stWork, agWorker, 1, 1)
		work.Output = contractResult
		l := lineOf(work)
		l.Rules = []domain.Rule{{Name: ruleTestsFirst}}

		h := newHarness(t, l, porttest.Succeed(`{"violations":[{"rule":"tests-first","reason":"no test"}]}`))
		h.contracts.Require(v1, contractResult, "answer")
		it := h.submit(t, domain.NewItem{})

		h.step(t)

		if got := h.item(t, it.ID); got.State != domain.StateBlocked {
			t.Fatalf("item is %s, want blocked (events %v)", got.State, h.kinds(it.ID))
		}

		if got := h.eventsOf(it.ID, domain.EventRuleViolated); len(got) != 0 {
			t.Errorf("%d rule_violated events for a rejected output, want none", len(got))
		}
	})
}

// violatedRules are the rule=reason of rule_violated events, comma-separated in order, after checking
// each carries its reason and the review station's agent.
func violatedRules(t *testing.T, events []domain.Event) string {
	t.Helper()

	rules := make([]string, 0, len(events))

	for _, ev := range events {
		rule, _ := ev.Data["rule"].(string)
		reason, _ := ev.Data["reason"].(string)
		rules = append(rules, rule+"="+reason)

		if ev.Data["station"] != "review" || ev.Data["agent"] != agReviewer || ev.Data["reason"] == "" {
			t.Errorf("rule_violated data = %v, want the reason, station review and agent %s", ev.Data, agReviewer)
		}
	}

	return strings.Join(rules, ",")
}
