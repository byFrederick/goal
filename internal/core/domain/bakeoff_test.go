package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Model ids the bake-off tests use: shaped like a model's, named like none.
const (
	testModelNow   = "claude-test-now-1"
	testModelOther = "claude-test-other-1"
	testModelThird = "claude-test-third-1"
	testBakeAgent  = "web-engineer"
)

func TestNewBakeOff_Check(t *testing.T) {
	t.Parallel()

	hash := BlobHash(strings.Repeat("a", 64))
	valid := func() NewBakeOff {
		return NewBakeOff{
			FactoryID: "00000000-0000-7000-8000-0000000000f1", VersionID: "00000000-0000-7000-8000-0000000000a1", ContentSHA256: hash,
			Station: stationBuild, Agent: testBakeAgent, TriesPerCase: 3, EstimateUSD: 2,
			Cases: []EvalCase{{Name: testEvalCase, SHA256: hash}, {Name: "spec-review", SHA256: hash}},
			Arms:  []NewBakeOffArm{{Model: testModelNow, Current: true}, {Model: testModelOther}, {Model: testModelThird}},
		}
	}

	tests := []struct {
		name string
		edit func(n *NewBakeOff)
		ok   bool
	}{
		{"a valid bake-off", func(*NewBakeOff) {}, true},
		{"two arms", func(n *NewBakeOff) { n.Arms = n.Arms[:2] }, true},
		{"four arms", func(n *NewBakeOff) { n.Arms = append(n.Arms, NewBakeOffArm{Model: "claude-test-fourth-1"}) }, true},
		{"one try per case", func(n *NewBakeOff) { n.TriesPerCase = 1 }, true},
		{"ten tries per case", func(n *NewBakeOff) { n.TriesPerCase = 10 }, true},
		{"one arm", func(n *NewBakeOff) { n.Arms = n.Arms[:1] }, false},
		{"five arms", func(n *NewBakeOff) {
			n.Arms = append(n.Arms, NewBakeOffArm{Model: "claude-test-fourth-1"}, NewBakeOffArm{Model: "claude-test-fifth-1"})
		}, false},
		{"a model twice", func(n *NewBakeOff) { n.Arms[2].Model = testModelOther }, false},
		{"a model that is not a model id", func(n *NewBakeOff) { n.Arms[1].Model = "gpt-test" }, false},
		{"an upper-case model id", func(n *NewBakeOff) { n.Arms[1].Model = "claude-Test" }, false},
		{"no current arm", func(n *NewBakeOff) { n.Arms[0].Current = false }, false},
		{"two current arms", func(n *NewBakeOff) { n.Arms[1].Current = true }, false},
		{"no station", func(n *NewBakeOff) { n.Station = "" }, false},
		{"no agent", func(n *NewBakeOff) { n.Agent = "" }, false},
		{"a bad agent slug", func(n *NewBakeOff) { n.Agent = "Frontend Engineer" }, false},
		{"no version", func(n *NewBakeOff) { n.VersionID = "" }, false},
		{"no factory", func(n *NewBakeOff) { n.FactoryID = "" }, false},
		{"bad content hash", func(n *NewBakeOff) { n.ContentSHA256 = "not-hex" }, false},
		{"zero tries per case", func(n *NewBakeOff) { n.TriesPerCase = 0 }, false},
		{"eleven tries per case", func(n *NewBakeOff) { n.TriesPerCase = 11 }, false},
		{"no cases", func(n *NewBakeOff) { n.Cases = nil }, false},
		{"a case twice", func(n *NewBakeOff) { n.Cases[1].Name = testEvalCase }, false},
		{"negative estimate", func(n *NewBakeOff) { n.EstimateUSD = -1 }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n := valid()
			tt.edit(&n)

			err := n.Check()
			if tt.ok && err != nil {
				t.Errorf("Check = %v, want nil", err)
			}

			if !tt.ok && !errors.Is(err, ErrInvalid) {
				t.Errorf("Check = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestArmSummary_Check(t *testing.T) {
	t.Parallel()

	share := 0.5
	over := 1.5

	tests := []struct {
		name string
		s    ArmSummary
		ok   bool
	}{
		{"a summary", ArmSummary{Tries: 6, Passed: 4, Errors: 1, CostPerItemUSD: 0.2, TimePerItem: time.Minute, MeanTurns: 12, PerCase: map[string]int{testEvalCase: 2}}, true},
		{"judged", ArmSummary{Tries: 2, Passed: 1, Judge: "plain-language", JudgedShare: &share}, true},
		{"more passed than tries", ArmSummary{Tries: 2, Passed: 3}, false},
		{"passed and errors over tries", ArmSummary{Tries: 2, Passed: 1, Errors: 2}, false},
		{"negative cost", ArmSummary{Tries: 1, CostPerItemUSD: -1}, false},
		{"negative time", ArmSummary{Tries: 1, TimePerItem: -time.Second}, false},
		{"negative turns", ArmSummary{Tries: 1, MeanTurns: -1}, false},
		{"a share over one", ArmSummary{Tries: 1, Judge: "plain-language", JudgedShare: &over}, false},
		{"a share without its judge", ArmSummary{Tries: 1, JudgedShare: &share}, false},
		{"a judge without its share", ArmSummary{Tries: 1, Judge: "plain-language"}, false},
		{"a case passed more than tried", ArmSummary{Tries: 1, PerCase: map[string]int{testEvalCase: 2}}, false},
		{"a bad case name", ArmSummary{Tries: 1, PerCase: map[string]int{"Bad Case": 0}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.s.Check()
			if tt.ok && err != nil {
				t.Errorf("Check = %v, want nil", err)
			}

			if !tt.ok && !errors.Is(err, ErrInvalid) {
				t.Errorf("Check = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestRecommendBakeOffArm(t *testing.T) {
	t.Parallel()

	// arm is a summarized arm with passed and errors out of 6 tries at cost per item.
	arm := func(n int, current bool, passed, errs int, cost float64) BakeOffArm {
		return BakeOffArm{
			Arm: n, Model: fmt.Sprintf("claude-test-%d", n), Current: current,
			Summary: &ArmSummary{Tries: 6, Passed: passed, Errors: errs, CostPerItemUSD: cost},
		}
	}
	done := func(arms ...BakeOffArm) BakeOff {
		return BakeOff{EvalRun: EvalRun{Kind: EvalKindBakeOff, State: EvalRunDone}, Arms: arms}
	}

	tests := []struct {
		name   string
		b      BakeOff
		want   int
		wantOK bool
	}{
		{"the highest pass rate", done(arm(1, true, 4, 0, 0.1), arm(2, false, 5, 0, 0.9), arm(3, false, 3, 0, 0.01)), 2, true},
		{"the same pass rate at a lower cost", done(arm(1, true, 5, 0, 0.9), arm(2, false, 5, 0, 0.09), arm(3, false, 5, 0, 0.3)), 2, true},
		{"the current arm wins", done(arm(1, true, 6, 0, 0.5), arm(2, false, 5, 0, 0.05)), 1, true},
		{"a tie goes to the lower arm", done(arm(1, true, 5, 0, 0.2), arm(2, false, 5, 0, 0.2)), 1, true},
		{"errors count as not passed", done(arm(1, true, 3, 3, 0.2), arm(2, false, 4, 0, 0.4)), 2, true},
		{"pass rate over different tries", done(
			arm(1, true, 4, 0, 0.1),
			BakeOffArm{Arm: 2, Model: "claude-test-c", Summary: &ArmSummary{Tries: 2, Passed: 1, CostPerItemUSD: 0.01}},
		), 1, true},
		{"an arm errored on every try", done(arm(1, true, 5, 0, 0.2), arm(2, false, 0, 6, 0)), 0, false},
		{"an arm not summarized", done(arm(1, true, 5, 0, 0.2), BakeOffArm{Arm: 2, Model: "claude-test-c"}), 0, false},
		{"an arm with no tries", done(arm(1, true, 5, 0, 0.2), BakeOffArm{Arm: 2, Model: "claude-test-c", Summary: &ArmSummary{}}), 0, false},
		{"no arms", done(), 0, false},
		{"still running", BakeOff{EvalRun: EvalRun{Kind: EvalKindBakeOff, State: EvalRunRunning}, Arms: []BakeOffArm{arm(1, true, 5, 0, 0.2), arm(2, false, 6, 0, 0.1)}}, 0, false},
		{"a failed run", BakeOff{EvalRun: EvalRun{Kind: EvalKindBakeOff, State: EvalRunFailed}, Arms: []BakeOffArm{arm(1, true, 5, 0, 0.2), arm(2, false, 6, 0, 0.1)}}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := RecommendBakeOffArm(tt.b)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("RecommendBakeOffArm = %d, %t; want %d, %t", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestArmSummary_PassRate(t *testing.T) {
	t.Parallel()

	if got := (ArmSummary{Tries: 4, Passed: 3}).PassRate(); got != 0.75 {
		t.Errorf("PassRate = %v, want 0.75", got)
	}

	if got := (ArmSummary{}).PassRate(); got != 0 {
		t.Errorf("PassRate of no tries = %v, want 0", got)
	}
}
