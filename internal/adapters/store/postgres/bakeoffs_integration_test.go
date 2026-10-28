//go:build integration

package postgres

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The agent and models the bake-off tests use: ids shaped like a definition's, named like none.
const (
	bakeStation = "build"
	bakeAgent   = "frontend-engineer"
	bakeNow     = "claude-test-now-1"
	bakeOther   = "claude-test-other-1"
	bakeThird   = "claude-test-third-1"
	// bakeOffAgentCheck is the CHECK that a bake-off, and only a bake-off, names its agent.
	bakeOffAgentCheck = "eval_runs_bake_off_agent"
)

func (d evalsDB) newBakeOffOf(models ...string) domain.NewBakeOff {
	n := domain.NewBakeOff{
		FactoryID: d.factory, VersionID: d.version, ContentSHA256: evalContent, Station: bakeStation, Agent: bakeAgent,
		Cases: []domain.EvalCase{evalCase(evalReview, 0), evalCase(evalMerge, 0)}, TriesPerCase: 2, EstimateUSD: 3.5,
	}

	for i, m := range models {
		n.Arms = append(n.Arms, domain.NewBakeOffArm{Model: m, Current: i == 0})
	}

	return n
}

// newBakeOff queues a bake-off of the current model and two others: 2 cases × 2 tries × 3 arms.
func (d evalsDB) newBakeOff(t *testing.T) domain.BakeOff {
	t.Helper()

	b, err := d.evals.CreateBakeOff(d.in(t), d.newBakeOffOf(bakeNow, bakeOther, bakeThird), domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func (d evalsDB) getBakeOff(t *testing.T, id domain.EvalRunID) domain.BakeOff {
	t.Helper()

	b, err := d.evals.GetBakeOff(d.in(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func armLines(arms []domain.BakeOffArm) string {
	out := make([]string, 0, len(arms))
	for _, a := range arms {
		out = append(out, fmt.Sprintf("%d %s current=%t summary=%t", a.Arm, a.Model, a.Current, a.Summary != nil))
	}

	return strings.Join(out, "\n")
}

// armResultLine is a try with its arm and duration.
func armResultLine(r domain.EvalResult) string {
	return fmt.Sprintf("arm %d %s %v", r.Arm, resultLine(r), r.Duration)
}

// pendingTries are a new bake-off's tries as armResultLine shows them: by arm, case and try.
func pendingTries(arms int, cases []string, tries int) string {
	var out []string

	for arm := 1; arm <= arms; arm++ {
		for _, c := range cases {
			for try := 1; try <= tries; try++ {
				out = append(out, armResultLine(domain.EvalResult{Arm: arm, Case: c, Try: try, State: domain.EvalPending}))
			}
		}
	}

	return strings.Join(out, "\n")
}

func armResultLines(rs []domain.EvalResult) string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, armResultLine(r))
	}

	return strings.Join(out, "\n")
}

func TestEvals_CreateBakeOff(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	b := d.newBakeOff(t)

	if b.Kind != domain.EvalKindBakeOff || b.Station != bakeStation || b.Agent != bakeAgent || b.TriesPerCase != 2 ||
		b.Trigger != domain.EvalManual || b.VersionID != d.version || b.State != domain.EvalRunQueued || b.EstimateUSD != 3.5 {
		t.Errorf("bake-off = %+v, want kind bake-off of %s/%s, 2 tries per case, manual, queued", b.EvalRun, b.Station, b.Agent)
	}

	wantArms := strings.Join([]string{
		"1 " + bakeNow + " current=true summary=false",
		"2 " + bakeOther + " current=false summary=false",
		"3 " + bakeThird + " current=false summary=false",
	}, "\n")
	if got := armLines(b.Arms); got != wantArms {
		t.Errorf("arms:\n%s\nwant:\n%s", got, wantArms)
	}

	if got, want := armResultLines(b.Results), pendingTries(3, []string{evalMerge, evalReview}, 2); got != want {
		t.Errorf("results:\n%s\nwant one pending try per arm, case and try:\n%s", got, want)
	}
}

// The arms take turns on each case and try, and a claimed try records its arm and duration.
func TestEvals_BakeOffClaimsArmsInTurn(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	b := d.newBakeOff(t)
	now := time.Now()
	claimed := make([]string, 0, 4)

	for range 4 {
		r := d.claim(t, now)
		claimed = append(claimed, fmt.Sprintf("arm %d %s/%d", r.Arm, r.Case, r.Try))

		if err := d.evals.RecordResult(d.in(t), r, domain.EvalOutcome{
			State: domain.EvalPass, CheckedBy: evalChecker, CostUSD: 0.5, Turns: 3, Duration: 1500 * time.Millisecond,
		}); err != nil {
			t.Fatal(err)
		}
	}

	wantClaims := "arm 1 merge-step/1, arm 2 merge-step/1, arm 3 merge-step/1, arm 1 merge-step/2"
	if got := strings.Join(claimed, ", "); got != wantClaims {
		t.Errorf("claims = %s, want %s", got, wantClaims)
	}

	after := d.getBakeOff(t, b.ID)
	if r := after.Results[0]; r.Arm != 1 || r.Case != evalMerge || r.Try != 1 || r.State != domain.EvalPass || r.Duration != 1500*time.Millisecond {
		t.Errorf("first try = %s, want arm 1 merge-step/1 passed in 1.5s", armResultLine(r))
	}

	if after.CostUSD != 2 {
		t.Errorf("cost = %v, want 2", after.CostUSD)
	}
}

func TestEvals_CreateBakeOffRefuses(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)

	_, err := d.evals.CreateBakeOff(d.in(t), d.newBakeOffOf(bakeNow), domain.LocalUser)
	wantErr(t, "CreateBakeOff of one arm", err, domain.ErrInvalid)

	unknown := d.newBakeOffOf(bakeNow, bakeOther)
	unknown.FactoryID = domain.FactoryID(newUUID(t))
	_, err = d.evals.CreateBakeOff(d.in(t), unknown, domain.LocalUser)
	wantErr(t, "CreateBakeOff of an unknown factory", err, ports.ErrNotFound)

	labs := domain.WithOrg(t.Context(), newOrgID(t, d.store))
	_, err = d.evals.CreateBakeOff(labs, d.newBakeOffOf(bakeNow, bakeOther), domain.LocalUser)
	wantErr(t, "labs CreateBakeOff on acme's factory and version", err, ports.ErrNotFound)
}

func TestEvals_ListBakeOffsNewestFirst(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	older := d.newBakeOff(t)
	d.newRun(t, evalContent, evalCase(evalMerge, 1))

	newer, err := d.evals.CreateBakeOff(d.in(t), d.newBakeOffOf(bakeOther, bakeNow), domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	got, err := d.evals.ListBakeOffs(d.in(t), d.factory, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].ID != newer.ID || got[1].ID != older.ID {
		t.Fatalf("ListBakeOffs = %+v, want %s then %s", got, newer.ID, older.ID)
	}

	wantArms := "1 " + bakeOther + " current=true summary=false\n2 " + bakeNow + " current=false summary=false"
	if got[0].Results != nil || armLines(got[0].Arms) != wantArms || len(got[1].Arms) != 3 {
		t.Errorf("ListBakeOffs = %+v, want arms without results", got)
	}

	if first, err := d.evals.ListBakeOffs(d.in(t), d.factory, 1); err != nil || len(first) != 1 || first[0].ID != newer.ID {
		t.Errorf("ListBakeOffs of 1 = %+v, %v; want the newest", first, err)
	}
}

func TestEvals_ListBakeOffsOfOtherOrgOrNoLimit(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	d.newBakeOff(t)

	labs := domain.WithOrg(t.Context(), newOrgID(t, d.store))
	if none, err := d.evals.ListBakeOffs(labs, d.factory, 10); err != nil || len(none) != 0 {
		t.Errorf("labs ListBakeOffs = %+v, %v; want none", none, err)
	}

	_, err := d.evals.ListBakeOffs(d.in(t), d.factory, 0)
	wantErr(t, "ListBakeOffs of 0", err, domain.ErrInvalid)
}

func TestEvals_GetBakeOffWithArmsAndResults(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	b := d.newBakeOff(t)

	got := d.getBakeOff(t, b.ID)
	if got.ID != b.ID || armLines(got.Arms) != armLines(b.Arms) || len(got.Results) != 12 || got.Station != bakeStation {
		t.Errorf("GetBakeOff = %+v, want the run with 3 arms and 12 tries", got)
	}

	run := d.newRun(t, evalContent, evalCase(evalMerge, 1))
	_, err := d.evals.GetBakeOff(d.in(t), run.ID)
	wantErr(t, "GetBakeOff of an eval run", err, ports.ErrNotFound)

	_, err = d.evals.GetBakeOff(d.in(t), domain.EvalRunID(newUUID(t)))
	wantErr(t, "GetBakeOff of an unknown run", err, ports.ErrNotFound)

	labs := domain.WithOrg(t.Context(), newOrgID(t, d.store))
	_, err = d.evals.GetBakeOff(labs, b.ID)
	wantErr(t, "labs GetBakeOff", err, ports.ErrNotFound)

	// GetRun still reads a bake-off as an eval run, with its arms' tries.
	if r := d.getRun(t, b.ID); r.Kind != domain.EvalKindBakeOff || len(r.Results) != 12 {
		t.Errorf("GetRun of a bake-off = kind %s with %d tries, want bake-off with 12", r.Kind, len(r.Results))
	}
}

func TestEvals_RecordArmSummary(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	b := d.newBakeOff(t)
	share := 0.75
	s := domain.ArmSummary{
		Tries: 4, Passed: 3, Errors: 1, CostPerItemUSD: 0.125, TimePerItem: 90 * time.Second, MeanTurns: 11.5,
		Judge: "plain-language", JudgedShare: &share, PerCase: map[string]int{evalMerge: 2, evalReview: 1},
	}

	if err := d.evals.RecordArmSummary(d.in(t), b.ID, 2, s); err != nil {
		t.Fatal(err)
	}

	got := d.getBakeOff(t, b.ID)
	if got.Arms[0].Summary != nil || got.Arms[2].Summary != nil {
		t.Errorf("arms 1 and 3 have summaries: %+v", got.Arms)
	}

	wantSummary(t, got.Arms[1].Summary, s)

	// A later summary replaces it; one without a judge has no share.
	s2 := domain.ArmSummary{Tries: 4, Passed: 4, CostPerItemUSD: 0.25, TimePerItem: time.Minute, MeanTurns: 9}
	if err := d.evals.RecordArmSummary(d.in(t), b.ID, 2, s2); err != nil {
		t.Fatal(err)
	}

	s2.PerCase = map[string]int{}
	wantSummary(t, d.getBakeOff(t, b.ID).Arms[1].Summary, s2)

	if listed, err := d.evals.ListBakeOffs(d.in(t), d.factory, 1); err != nil || listed[0].Arms[1].Summary == nil {
		t.Errorf("ListBakeOffs = %+v, %v; want arm 2's summary", listed, err)
	}
}

func TestEvals_RecordArmSummaryRefuses(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	b := d.newBakeOff(t)
	s := domain.ArmSummary{Tries: 4, Passed: 4}

	err := d.evals.RecordArmSummary(d.in(t), b.ID, 4, s)
	wantErr(t, "RecordArmSummary of arm 4 of 3", err, ports.ErrNotFound)

	err = d.evals.RecordArmSummary(d.in(t), domain.EvalRunID(newUUID(t)), 1, s)
	wantErr(t, "RecordArmSummary of an unknown run", err, ports.ErrNotFound)

	err = d.evals.RecordArmSummary(d.in(t), b.ID, 1, domain.ArmSummary{Tries: 1, Passed: 2})
	wantErr(t, "RecordArmSummary of more passed than tries", err, domain.ErrInvalid)

	labs := domain.WithOrg(t.Context(), newOrgID(t, d.store))
	err = d.evals.RecordArmSummary(labs, b.ID, 1, s)
	wantErr(t, "labs RecordArmSummary", err, ports.ErrNotFound)
}

// wantSummary fails t unless got is want, its judged share compared by value.
func wantSummary(t *testing.T, got *domain.ArmSummary, want domain.ArmSummary) {
	t.Helper()

	line := func(s domain.ArmSummary) string {
		share := "none"
		if s.JudgedShare != nil {
			share = fmt.Sprint(*s.JudgedShare)
		}

		s.JudgedShare = nil

		return fmt.Sprintf("%+v share=%s", s, share)
	}

	if got == nil || line(*got) != line(want) {
		t.Errorf("summary = %+v, want %s", got, line(want))
	}
}

// A bake-off runs other models than the definition's, so it is neither the publish gate's run nor
// part of the history that prices the next eval run.
func TestEvals_BakeOffIsNotAnEvalRun(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	d.newBakeOff(t)

	r := d.claim(t, time.Now())
	if err := d.evals.RecordResult(d.in(t), r, domain.EvalOutcome{State: domain.EvalPass, CheckedBy: evalChecker, CostUSD: 9}); err != nil {
		t.Fatal(err)
	}

	_, err := d.evals.LatestRunForContent(d.in(t), d.factory, evalContent)
	wantErr(t, "LatestRunForContent with only a bake-off", err, ports.ErrNotFound)

	if h, err := d.evals.CaseHistory(d.in(t), d.factory); err != nil || len(h) != 0 {
		t.Errorf("CaseHistory = %+v, %v; want none from a bake-off", h, err)
	}
}

// wantConstraint fails t unless err is PostgreSQL refusing stmt by the constraint or index named.
func wantConstraint(t *testing.T, stmt string, err error, constraint string) {
	t.Helper()

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.ConstraintName != constraint {
		t.Errorf("%s = %v, want refused by %s", stmt, err, constraint)
	}
}

func TestEvals_BakeOffRefusesNoAgent(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	insert := `INSERT INTO eval_runs (id, kind, factory_id, content_sha256, trigger, created_by, station, agent, tries_per_case)
VALUES ($1, $2, '` + string(d.factory) + `', repeat('c', 64), 'manual', 'user:local', $3, $4, $5)`

	for _, tt := range []struct {
		name                string
		kind                string
		station, agent      *string
		tries               *int16
		refused             bool
		constraintOrNothing string
	}{
		{"a bake-off without an agent", string(domain.EvalKindBakeOff), new(bakeStation), nil, new(int16(3)), true, bakeOffAgentCheck},
		{"a bake-off without a station", string(domain.EvalKindBakeOff), nil, new(bakeAgent), new(int16(3)), true, bakeOffAgentCheck},
		{"a bake-off without tries per case", string(domain.EvalKindBakeOff), new(bakeStation), new(bakeAgent), nil, true, bakeOffAgentCheck},
		{"an eval run naming an agent", string(domain.EvalKindEval), new(bakeStation), new(bakeAgent), nil, true, bakeOffAgentCheck},
		{"an unknown kind", "compare", nil, nil, nil, true, "eval_runs_kind_check"},
		{"a bake-off of an agent", string(domain.EvalKindBakeOff), new(bakeStation), new(bakeAgent), new(int16(3)), false, ""},
		{"an eval run", string(domain.EvalKindEval), nil, nil, nil, false, ""},
	} {
		err := inOrgTx(t, d.store.pool, d.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), insert, newUUID(t), tt.kind, tt.station, tt.agent, tt.tries)

			return err
		})

		if tt.refused {
			wantConstraint(t, tt.name, err, tt.constraintOrNothing)
		} else if err != nil {
			t.Errorf("%s = %v, want stored", tt.name, err)
		}
	}
}

func TestEvals_BakeOffRefusesSecondCurrentArm(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	b := d.newBakeOff(t)
	insert := `INSERT INTO eval_run_arms (eval_run_id, arm, model, is_current) VALUES ('` + string(b.ID) + `', $1, $2, $3)`

	for _, tt := range []struct {
		name       string
		arm        int16
		model      string
		current    bool
		constraint string
	}{
		{"a second current arm", 4, "claude-test-fourth-1", true, "eval_run_arms_current_idx"},
		{"a model that is not a model id", 4, "gpt-test", false, "eval_run_arms_model_check"},
		{"a model twice", 4, bakeOther, false, "eval_run_arms_eval_run_id_model_key"},
		{"an arm number taken", 1, "claude-test-fourth-1", false, "eval_run_arms_pkey"},
		{"a fifth arm", 5, "claude-test-fifth-1", false, "eval_run_arms_arm_check"},
	} {
		err := inOrgTx(t, d.store.pool, d.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), insert, tt.arm, tt.model, tt.current)

			return err
		})
		wantConstraint(t, tt.name, err, tt.constraint)
	}

	err := inOrgTx(t, d.store.pool, d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), insert, 4, "claude-test-fourth-1", false)

		return err
	})
	if err != nil {
		t.Errorf("a fourth arm that is not current = %v, want stored", err)
	}
}
