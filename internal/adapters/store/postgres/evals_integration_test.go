//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// evalsDB is a migrated database with an organization, its factory and a published version.
type evalsDB struct {
	connectionsDB
	evals   *Evals
	org     domain.OrgID
	factory domain.FactoryID
	version domain.DefinitionVersionID
}

func newEvalsDB(t *testing.T) evalsDB {
	t.Helper()

	d := newConnectionsDB(t)
	org := newOrgID(t, d.store)
	factory := saveFactory(domain.WithOrg(t.Context(), org), t, d.store)

	return evalsDB{
		connectionsDB: d, evals: NewEvals(d.store), org: org, factory: factory,
		version: newPublishedVersion(t, d.store.pool, org, factory),
	}
}

// in is t's context in the organization.
func (d evalsDB) in(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), d.org)
}

// Names and hashes the eval tests use: two cases, what checks a try, a definition's content and
// a case file's.
const (
	evalMerge   = "merge-step"
	evalReview  = "spec-review"
	evalChecker = "contract"
)

var (
	evalContent = domain.BlobHash(strings.Repeat("c", 64))
	evalCaseSHA = domain.BlobHash(strings.Repeat("e", 64))
)

func evalCase(name string, tries int) domain.EvalCase {
	return domain.EvalCase{Name: name, SHA256: evalCaseSHA, Tries: tries}
}

func (d evalsDB) newEvalRun(content domain.BlobHash, cases ...domain.EvalCase) domain.NewEvalRun {
	return domain.NewEvalRun{
		FactoryID: d.factory, VersionID: d.version, ContentSHA256: content, Trigger: domain.EvalManual, Cases: cases, EstimateUSD: 1.25,
	}
}

func (d evalsDB) newRun(t *testing.T, content domain.BlobHash, cases ...domain.EvalCase) domain.EvalRun {
	t.Helper()

	r, err := d.evals.CreateRun(d.in(t), d.newEvalRun(content, cases...), domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// agentRun starts an agent run in the factory: what a try's run_id names.
func (d evalsDB) agentRun(t *testing.T) domain.RunID {
	t.Helper()

	it, err := d.store.CreateItem(d.in(t), domain.NewItem{
		FactoryID: d.factory, Type: typeTask, Title: "eval", Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	id, err := d.store.StartRun(d.in(t), ports.NewRun{
		FactoryID: d.factory, DefinitionVersionID: d.version, ItemID: it.ID, Station: stWork, Agent: agWorker, StartedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return id
}

func (d evalsDB) claim(t *testing.T, now time.Time) domain.EvalResult {
	t.Helper()

	r, err := d.evals.ClaimResult(d.in(t), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// claimAndRecord claims the next try and records it with state and cost.
func (d evalsDB) claimAndRecord(t *testing.T, now time.Time, state domain.EvalResultState, cost float64) domain.EvalResult {
	t.Helper()

	r := d.claim(t, now)
	if err := d.evals.RecordResult(d.in(t), r, domain.EvalOutcome{State: state, CheckedBy: evalChecker, CostUSD: cost}); err != nil {
		t.Fatal(err)
	}

	return r
}

func (d evalsDB) getRun(t *testing.T, id domain.EvalRunID) domain.EvalRun {
	t.Helper()

	r, err := d.evals.GetRun(d.in(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Errorf("%s = %v, want %v", what, err, want)
	}
}

// resultLine is a try as the tests compare it: case/try state lease run checked_by cost turns.
func resultLine(r domain.EvalResult) string {
	return fmt.Sprintf("%s/%d %s lease=%t run=%t %s %v %d", r.Case, r.Try, r.State, !r.LeaseUntil.IsZero(), r.RunID != "",
		r.CheckedBy, r.CostUSD, r.Turns)
}

func resultLines(rs []domain.EvalResult) string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, resultLine(r))
	}

	return strings.Join(out, "\n")
}

func TestEvals_CreateQueuesPendingTries(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	run := d.newRun(t, evalContent, evalCase(evalReview, 1), evalCase(evalMerge, 3))

	want := domain.EvalRun{
		ID: run.ID, FactoryID: d.factory, VersionID: d.version, ContentSHA256: evalContent, Trigger: domain.EvalManual,
		State: domain.EvalRunQueued, EstimateUSD: 1.25, CreatedBy: domain.LocalUser, CreatedAt: run.CreatedAt,
	}

	got := d.getRun(t, run.ID)
	head := got
	head.Results = nil

	if fmt.Sprint(head) != fmt.Sprint(want) || run.CreatedAt.IsZero() {
		t.Errorf("run = %+v, want %+v", head, want)
	}

	pending := resultLines([]domain.EvalResult{
		{Case: evalMerge, Try: 1, State: domain.EvalPending},
		{Case: evalMerge, Try: 2, State: domain.EvalPending},
		{Case: evalMerge, Try: 3, State: domain.EvalPending},
		{Case: evalReview, Try: 1, State: domain.EvalPending},
	})
	if lines := resultLines(got.Results); lines != pending {
		t.Errorf("results:\n%s\nwant by case and try:\n%s", lines, pending)
	}

	if lines := resultLines(run.Results); lines != pending {
		t.Errorf("CreateRun's results:\n%s\nwant:\n%s", lines, pending)
	}

	if got.Results[0].CaseSHA256 != evalCaseSHA {
		t.Errorf("case hash = %s, want %s", got.Results[0].CaseSHA256, evalCaseSHA)
	}
}

func TestEvals_CreateDraftRunOfOneCase(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	n := d.newEvalRun(evalContent, evalCase(evalMerge, 1))
	n.VersionID, n.Trigger, n.Case = "", domain.EvalOnPublish, evalMerge

	draft, err := d.evals.CreateRun(d.in(t), n, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if got := d.getRun(t, draft.ID); got.VersionID != "" || got.Case != evalMerge || got.Trigger != domain.EvalOnPublish {
		t.Errorf("draft run = %+v, want no version, the one case, on publish", got)
	}
}

func TestEvals_CreateRefusesUnknownFactoryOrVersion(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)

	unknown := d.newEvalRun(evalContent, evalCase(evalMerge, 1))
	unknown.FactoryID, unknown.VersionID = domain.FactoryID(newUUID(t)), ""
	_, err := d.evals.CreateRun(d.in(t), unknown, domain.LocalUser)
	wantErr(t, "CreateRun of an unknown factory", err, ports.ErrNotFound)

	otherVersion := d.newEvalRun(evalContent, evalCase(evalMerge, 1))
	otherVersion.FactoryID = saveFactory(d.in(t), t, d.store)
	_, err = d.evals.CreateRun(d.in(t), otherVersion, domain.LocalUser)
	wantErr(t, "CreateRun on another factory's version", err, ports.ErrNotFound)

	_, err = d.evals.CreateRun(d.in(t), d.newEvalRun(evalContent), domain.LocalUser)
	wantErr(t, "CreateRun with no cases", err, domain.ErrInvalid)

	_, err = d.evals.GetRun(d.in(t), domain.EvalRunID(newUUID(t)))
	wantErr(t, "GetRun of an unknown run", err, ports.ErrNotFound)
}

func TestEvals_ClaimAndRecord(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	run := d.newRun(t, evalContent, evalCase(evalMerge, 2))
	agent := d.agentRun(t)

	claimed := d.claim(t, now)
	if claimed.EvalRunID != run.ID || resultLine(claimed) != resultLine(domain.EvalResult{Case: evalMerge, Try: 1, State: domain.EvalRunning, LeaseUntil: now}) ||
		!claimed.LeaseUntil.Equal(now.Add(time.Minute)) {
		t.Errorf("claimed %+v, want %s try 1 running until %s", claimed, evalMerge, now.Add(time.Minute))
	}

	if got := d.getRun(t, run.ID); got.State != domain.EvalRunRunning {
		t.Errorf("run state after a claim = %s, want running", got.State)
	}

	o := domain.EvalOutcome{State: domain.EvalPass, RunID: agent, CheckedBy: evalChecker, Detail: map[string]any{"why": "valid output"}, CostUSD: 0.4, Turns: 7}
	if err := d.evals.RecordResult(d.in(t), claimed, o); err != nil {
		t.Fatal(err)
	}

	wantErr(t, "recording the try twice", d.evals.RecordResult(d.in(t), claimed, o), ports.ErrLeaseLost)

	if second := d.claimAndRecord(t, now, domain.EvalError, 0.1); second.Try != 2 {
		t.Errorf("second claim took try %d, want 2", second.Try)
	}

	_, err := d.evals.ClaimResult(d.in(t), now, time.Minute)
	wantErr(t, "claim with every try recorded", err, ports.ErrNotFound)

	got := d.getRun(t, run.ID)
	if got.CostUSD != 0.5 {
		t.Errorf("run cost = %v, want 0.5, the sum of its tries", got.CostUSD)
	}

	want := "merge-step/1 pass lease=false run=true contract 0.4 7\nmerge-step/2 error lease=false run=false contract 0.1 0"
	if lines := resultLines(got.Results); lines != want || got.Results[0].RunID != agent || got.Results[0].Detail["why"] != "valid output" {
		t.Errorf("recorded tries:\n%s\nwant:\n%s, with the agent run and detail", lines, want)
	}
}

func TestEvals_FinishRun(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	run := d.newRun(t, evalContent, evalCase(evalMerge, 1))

	_, err := d.evals.FinishRun(d.in(t), run.ID, domain.EvalRunRunning, now)
	wantErr(t, "FinishRun running", err, domain.ErrInvalid)

	done, err := d.evals.FinishRun(d.in(t), run.ID, domain.EvalRunDone, now)
	if err != nil || done.State != domain.EvalRunDone || !done.FinishedAt.Equal(now) {
		t.Errorf("FinishRun = %+v, %v; want done at %s", done, err, now)
	}

	_, err = d.evals.FinishRun(d.in(t), run.ID, domain.EvalRunCancelled, now)
	wantErr(t, "finishing a finished run", err, ports.ErrConflict)

	_, err = d.evals.FinishRun(d.in(t), domain.EvalRunID(newUUID(t)), domain.EvalRunDone, now)
	wantErr(t, "finishing an unknown run", err, ports.ErrNotFound)

	// A finished run's pending tries are not claimed.
	_, err = d.evals.ClaimResult(d.in(t), now, time.Minute)
	wantErr(t, "claim with only a finished run's tries", err, ports.ErrNotFound)
}

func TestEvals_ClaimSkipsLockedAndReclaimsExpired(t *testing.T) {
	t.Parallel()

	const tries = 8

	d := newEvalsDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	d.newRun(t, evalContent, evalCase(evalMerge, tries))

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = map[int]int{}
	)

	for range tries {
		wg.Go(func() {
			r, err := d.evals.ClaimResult(d.in(t), now, time.Minute)
			if err != nil {
				t.Errorf("concurrent claim: %v", err)

				return
			}

			mu.Lock()
			claimed[r.Try]++
			mu.Unlock()
		})
	}

	wg.Wait()

	for try := 1; try <= tries; try++ {
		if claimed[try] != 1 {
			t.Errorf("concurrent claims took try %d %d times, want once (%v)", try, claimed[try], claimed)
		}
	}

	_, err := d.evals.ClaimResult(d.in(t), now.Add(59*time.Second), time.Minute)
	wantErr(t, "claim while every lease holds", err, ports.ErrNotFound)

	later := now.Add(time.Minute)

	again, err := d.evals.ClaimResult(d.in(t), later, time.Minute)
	if err != nil || again.Try != 1 || !again.LeaseUntil.Equal(later.Add(time.Minute)) {
		t.Errorf("claim after the leases ended = %+v, %v; want try 1 under a new lease", again, err)
	}
}

func TestEvals_RecordAfterLeaseLost(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	d.newRun(t, evalContent, evalCase(evalMerge, 1))

	first := d.claim(t, now)

	second := d.claim(t, now.Add(2*time.Minute))
	if second.Try != first.Try {
		t.Fatalf("reclaim = %+v, want the expired try", second)
	}

	pass := domain.EvalOutcome{State: domain.EvalPass, CheckedBy: evalChecker, CostUSD: 0.2}
	wantErr(t, "record under the first, lost lease", d.evals.RecordResult(d.in(t), first, pass), ports.ErrLeaseLost)

	if err := d.evals.RecordResult(d.in(t), second, pass); err != nil {
		t.Errorf("record under the current lease = %v", err)
	}

	if got := d.getRun(t, second.EvalRunID); got.CostUSD != 0.2 {
		t.Errorf("run cost = %v, want only the recorded try's 0.2", got.CostUSD)
	}
}

// historyLine is a case's history as the tests compare it: each try as <run>/<try> <state>,
// then the latest run and its cost.
func historyLine(h domain.EvalCaseHistory, runs map[domain.EvalRunID]string) string {
	out := []string{h.Case}
	for _, r := range h.Tries {
		out = append(out, fmt.Sprintf("%s/%d %s", runs[r.EvalRunID], r.Try, r.State))
	}

	return strings.Join(append(out, fmt.Sprintf("latest %s %v", runs[h.LatestRunID], h.LatestCostUSD)), ", ")
}

func TestEvals_CaseHistory(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	old := d.newRun(t, evalContent, evalCase(evalMerge, 2), evalCase(evalReview, 1))
	d.claimAndRecord(t, now, domain.EvalPass, 0.1)  // merge-step 1
	d.claimAndRecord(t, now, domain.EvalFail, 0.2)  // merge-step 2
	d.claimAndRecord(t, now, domain.EvalError, 0.3) // spec-review 1

	latest := d.newRun(t, evalContent, evalCase(evalMerge, 2))
	d.claimAndRecord(t, now, domain.EvalPass, 0.5)   // merge-step 1
	d.claimAndRecord(t, now, domain.EvalPass, 0.7)   // merge-step 2
	d.newRun(t, evalContent, evalCase(evalMerge, 1)) // pending: not in the history

	h, err := d.evals.CaseHistory(d.in(t), d.factory)
	if err != nil {
		t.Fatal(err)
	}

	runs := map[domain.EvalRunID]string{old.ID: "old", latest.ID: "new"}
	lines := make([]string, 0, len(h))

	for _, c := range h {
		lines = append(lines, historyLine(c, runs))
	}

	want := "merge-step, new/2 pass, new/1 pass, old/2 fail, latest new 1.2\nspec-review, old/1 error, latest old 0.3"
	if got := strings.Join(lines, "\n"); got != want {
		t.Errorf("history:\n%s\nwant the last 3 tries newest first and the latest run's cost:\n%s", got, want)
	}

	if h, err := d.evals.CaseHistory(d.in(t), saveFactory(d.in(t), t, d.store)); err != nil || len(h) != 0 {
		t.Errorf("history of a factory without runs = %+v, %v; want none", h, err)
	}
}

func TestEvals_LatestRunForContent(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)

	d.newRun(t, evalContent, evalCase(evalMerge, 1))
	newest := d.newRun(t, evalContent, evalCase(evalMerge, 1))
	d.newRun(t, domain.BlobHash(strings.Repeat("d", 64)), evalCase(evalMerge, 1))

	got, err := d.evals.LatestRunForContent(d.in(t), d.factory, evalContent)
	if err != nil || got.ID != newest.ID || got.Results != nil {
		t.Errorf("LatestRunForContent = %+v, %v; want %s without results", got, err, newest.ID)
	}

	_, err = d.evals.LatestRunForContent(d.in(t), d.factory, domain.BlobHash(strings.Repeat("f", 64)))
	wantErr(t, "LatestRunForContent of content never run", err, ports.ErrNotFound)
}

func TestEvals_OtherOrgInvisible(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	run := d.newRun(t, evalContent, evalCase(evalMerge, 1))
	labs := domain.WithOrg(t.Context(), newOrgID(t, d.store))

	_, err := d.evals.GetRun(labs, run.ID)
	wantErr(t, "labs GetRun", err, ports.ErrNotFound)

	_, err = d.evals.ClaimResult(labs, time.Now(), time.Minute)
	wantErr(t, "labs ClaimResult", err, ports.ErrNotFound)

	_, err = d.evals.FinishRun(labs, run.ID, domain.EvalRunCancelled, time.Now())
	wantErr(t, "labs FinishRun", err, ports.ErrNotFound)

	_, err = d.evals.LatestRunForContent(labs, d.factory, evalContent)
	wantErr(t, "labs LatestRunForContent", err, ports.ErrNotFound)

	_, err = d.evals.CreateRun(labs, d.newEvalRun(evalContent, evalCase(evalMerge, 1)), domain.LocalUser)
	wantErr(t, "labs CreateRun on acme's factory and version", err, ports.ErrNotFound)

	if h, err := d.evals.CaseHistory(labs, d.factory); err != nil || len(h) != 0 {
		t.Errorf("labs CaseHistory = %+v, %v; want none", h, err)
	}
}

func TestEvals_RLSAndGrants(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	d.newRun(t, evalContent, evalCase(evalMerge, 1))
	labs := newOrgID(t, d.store)

	for _, table := range []string{"eval_runs", "eval_results"} {
		err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("labs sees %d rows of %s, %v; want none", n, table, err)
			}

			tag, err := tx.Exec(t.Context(), `UPDATE `+table+` SET state = state WHERE true`)
			if err != nil || tag.RowsAffected() != 0 {
				t.Errorf("labs updates %d rows of %s, %v; want none", tag.RowsAffected(), table, err)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// goal_app updates only the columns the store changes, and deletes nothing.
	for _, stmt := range []string{
		`UPDATE eval_runs SET content_sha256 = repeat('0', 64)`,
		`UPDATE eval_runs SET org_id = '` + string(labs) + `'`,
		`UPDATE eval_results SET case_name = 'other-case'`,
		`DELETE FROM eval_results`,
		`DELETE FROM eval_runs`,
	} {
		err := inOrgTx(t, d.store.pool, d.org, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err })
		if err == nil {
			t.Errorf("goal_app ran %s", stmt)
		}
	}
}

// goal_purge_org deletes agent_runs before factories: a try's run_id is cleared, then the
// factory's eval runs and tries go with it.
func TestPurgeOrg_RemovesEvals(t *testing.T) {
	t.Parallel()

	d := newEvalsDB(t)
	d.newRun(t, evalContent, evalCase(evalMerge, 1))

	r := d.claim(t, time.Now())
	if err := d.evals.RecordResult(d.in(t), r, domain.EvalOutcome{State: domain.EvalPass, RunID: d.agentRun(t)}); err != nil {
		t.Fatal(err)
	}

	deleted := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if err := d.store.DeleteOrg(d.in(t), domain.LocalUser, deleted); err != nil {
		t.Fatal(err)
	}

	if ok, err := d.store.PurgeOrg(t.Context(), d.org, deleted.Add(time.Second), nil); !ok || err != nil {
		t.Fatalf("PurgeOrg = %v, %v", ok, err)
	}

	var n int
	if err := d.admin.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM eval_runs) + (SELECT count(*) FROM eval_results)`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d eval rows left, %v; want none", n, err)
	}
}
