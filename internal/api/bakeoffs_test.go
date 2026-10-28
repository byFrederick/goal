package api_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/jonah"
)

const (
	lineEvals    = "/v1/orgs/acme/factories/line/evals"
	lineBakeOffs = lineEvals + "/bake-offs"
	// The line's backend runs on the runtime's model; its two cases run on that and one more.
	bakeOffAgent  = "backend"
	bakeOffNow    = "claude-fable-5-1"
	bakeOffOther  = "claude-haiku-5-5"
	bakeOffArms   = `"models":["` + bakeOffNow + `","` + bakeOffOther + `"]`
	bakeOffStart  = `{"agent":"` + bakeOffAgent + `",` + bakeOffArms + `,"trials":2}`
	caseLease     = "fixes-the-lease"
	caseFlag      = "adds-a-flag"
	bakeOffPrompt = "agents/backend.md"
)

// publishBakeOffCases publishes the line's v2 with two cases of its backend, checked by its
// contract, and the judge evalJudge as the version's, and returns v2.
func (h *harness) publishBakeOffCases(t *testing.T) domain.DefinitionVersion {
	t.Helper()

	svc, ctx := h.authors(t)

	if _, _, err := svc.OpenDraft(ctx, lineID, authoring.OpenDraftRequest{}); err != nil {
		t.Fatal(err)
	}

	var rev int64

	for _, name := range []string{caseLease, caseFlag} {
		content := `apiVersion: goal/v1
kind: EvalCase
metadata:
  description: The backend makes the change
spec:
  agent: ` + bakeOffAgent + `
  input:
    title: Make the change
    body: The tests pass.
  tries: 1
  check:
    contract: true
`

		saved, err := svc.PutFile(ctx, lineID, authoring.FileWrite{Path: config.CasesDir + "/" + name + ".yaml", Content: []byte(content), Create: true})
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}

		rev = saved.Revision
	}

	rec, err := h.defs.FactoryRecord(inOrg(t), lineID)
	if err != nil {
		t.Fatal(err)
	}

	v, err := svc.Publish(ctx, lineID, authoring.PublishRequest{Revision: rev, ExpectedCurrent: rec.Current.ID})
	if err != nil {
		t.Fatalf("publish the cases: %v", err)
	}

	h.evalJudges.Set(testOrg, lineID, v.ID, domain.Judge{Slug: evalJudge, Title: evalJudgeTitle})

	return v
}

// startBakeOff starts a bake-off of the line with body and wants it queued.
func startBakeOff(t *testing.T, h *harness, body string) gen.BakeOffStarted {
	t.Helper()

	var out gen.BakeOffStarted
	if code, p := evalCall(t, h, http.MethodPost, lineBakeOffs, body, &out); code != http.StatusAccepted {
		t.Fatalf("start bake-off %s = %d %+v, want 202", body, code, p)
	}

	return out
}

// workBakeOff records every claimable try of bake-off id: arm 2 passes cheaply, arm 1 fails at a
// higher cost. Then it records each arm's summary, as Jonah's loop does, with evalJudge's share,
// and finishes the bake-off.
func (h *harness) workBakeOff(t *testing.T, id string) {
	t.Helper()

	ctx := inOrg(t)

	for {
		c, err := h.evals.ClaimResult(ctx, h.clock.Now(), time.Hour)
		if err != nil {
			break
		}

		o := domain.EvalOutcome{State: domain.EvalFail, CheckedBy: domain.EvalCheckedByContract, CostUSD: 0.5, Turns: 10}
		if c.Arm == 2 {
			o.State, o.CostUSD, o.Turns = domain.EvalPass, 0.1, 4
		}

		if err := h.evals.RecordResult(ctx, c, o); err != nil {
			t.Fatal(err)
		}
	}

	b, err := h.evals.GetBakeOff(ctx, domain.EvalRunID(id))
	if err != nil {
		t.Fatal(err)
	}

	for _, a := range b.Arms {
		s := domain.ArmSummary{Tries: 2 * b.TriesPerCase, CostPerItemUSD: 0.5, TimePerItem: time.Minute, MeanTurns: 10, PerCase: map[string]int{caseLease: 0, caseFlag: 0}}
		share := 0.0

		if a.Arm == 2 {
			s.Passed, s.CostPerItemUSD, s.MeanTurns = s.Tries, 0.1, 4
			s.PerCase = map[string]int{caseLease: b.TriesPerCase, caseFlag: b.TriesPerCase}
			share = 0.5
		}

		s.Judge, s.JudgedShare = evalJudge, &share

		if err := h.evals.RecordArmSummary(ctx, b.ID, a.Arm, s); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := h.evals.FinishRun(ctx, b.ID, domain.EvalRunDone, h.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// finishedBackendRun records a finished run of the line's backend that cost usd, which prices
// its tries.
func (h *harness) finishedBackendRun(t *testing.T, usd float64) {
	t.Helper()

	ctx := inOrg(t)

	rec, err := h.defs.FactoryRecord(ctx, lineID)
	if err != nil {
		t.Fatal(err)
	}

	it, err := h.store.CreateItem(ctx, domain.NewItem{FactoryID: lineID, Type: typeFeature, Lane: "api", Title: "A change", Station: stInbox, CreatedAt: t0.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	id, err := h.store.StartRun(ctx, ports.NewRun{
		FactoryID: lineID, DefinitionVersionID: rec.Current.ID, ItemID: it.ID, Station: "build", Agent: bakeOffAgent, StartedAt: t0.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, t0); err != nil {
		t.Fatal(err)
	}
}

// withBudget keeps a learning budget with usd left.
func withBudget(usd float64) func(*harness, *api.Deps) {
	return func(h *harness, d *api.Deps) {
		h.evalLoop = h.evalLoopWith(h.tenancy, func(e *jonah.EvalDeps) { e.Budget = budgetLeft(usd) })
		d.Evals.Loop = h.evalLoop
	}
}

// budgetLeft is a learning budget with a fixed amount left.
type budgetLeft float64

func (b budgetLeft) RemainingUSD(context.Context) (float64, error) { return float64(b), nil }

func TestBakeOffs_StartAndGet(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	started := startBakeOff(t, h, bakeOffStart)

	b := started.BakeOff
	if b.State != gen.BakeOffState(domain.EvalRunQueued) || b.Agent != bakeOffAgent || b.Station != "build" || b.Version != 2 ||
		b.Trials != 2 || b.Cases != 2 || b.RecommendedArm != nil || b.FinishedAt != nil {
		t.Errorf("started = %+v, want a queued bake-off of the backend at build on v2, 2 trials of 2 cases, no recommendation", b)
	}

	if len(b.Arms) != 2 || b.Arms[0] != (gen.BakeOffArm{Arm: 1, Model: bakeOffNow, IsCurrent: true}) ||
		b.Arms[1] != (gen.BakeOffArm{Arm: 2, Model: bakeOffOther}) {
		t.Errorf("arms = %+v, want %s (now) and %s without summaries", b.Arms, bakeOffNow, bakeOffOther)
	}

	if started.Estimate.Unpriced == nil {
		t.Error("the estimate has no unpriced list")
	}

	h.workBakeOff(t, b.Id)

	var got gen.BakeOff
	if code, _ := evalCall(t, h, http.MethodGet, lineBakeOffs+"/"+b.Id, "", &got); code != http.StatusOK {
		t.Fatalf("get = %d, want 200", code)
	}

	if got.State != gen.BakeOffState(domain.EvalRunDone) || got.FinishedAt == nil || got.RecommendedArm == nil || *got.RecommendedArm != 2 {
		t.Errorf("done = %+v, want done and arm 2 recommended", got)
	}

	s := got.Arms[1].Summary
	if s == nil || s.PassRate != 1 || s.CostPerItemUsd != 0.1 || s.Turns != 4 || s.TimePerItemSeconds != 60 || s.Tries != 4 {
		t.Fatalf("arm 2's summary = %+v, want all 4 tries passed at 0.1, 4 turns, a minute", s)
	}

	if s.Judged == nil || s.Judged.Judge != evalJudge || deref(s.Judged.Title) != evalJudgeTitle || s.Judged.Share != 0.5 {
		t.Errorf("arm 2 judged = %+v, want %s titled %q at 0.5", s.Judged, evalJudge, evalJudgeTitle)
	}

	want := []gen.BakeOffCase{
		{Case: caseFlag, Arms: []gen.BakeOffCaseArm{{Arm: 1, Passed: 0}, {Arm: 2, Passed: 2}}},
		{Case: caseLease, Arms: []gen.BakeOffCaseArm{{Arm: 1, Passed: 0}, {Arm: 2, Passed: 2}}},
	}
	if !reflect.DeepEqual(got.PerCase, want) {
		t.Errorf("per case = %+v, want %+v", got.PerCase, want)
	}
}

func TestBakeOffs_ListNewestFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var empty gen.BakeOffList
	if code, _ := evalCall(t, h, http.MethodGet, lineBakeOffs, "", &empty); code != http.StatusOK || empty.BakeOffs == nil || len(empty.BakeOffs) != 0 {
		t.Fatalf("list without bake-offs = %d %+v, want 200 and []", code, empty)
	}

	h.publishBakeOffCases(t)

	first := startBakeOff(t, h, bakeOffStart)
	h.workBakeOff(t, first.BakeOff.Id)
	h.clock.Advance(time.Minute)

	second := startBakeOff(t, h, `{"agent":"`+bakeOffAgent+`",`+bakeOffArms+`,"trials":1}`)

	var list gen.BakeOffList
	if code, _ := evalCall(t, h, http.MethodGet, lineBakeOffs, "", &list); code != http.StatusOK || len(list.BakeOffs) != 2 {
		t.Fatalf("list = %d %+v, want 200 and two", code, list)
	}

	if list.BakeOffs[0].Id != second.BakeOff.Id || list.BakeOffs[1].Id != first.BakeOff.Id {
		t.Errorf("list = %s, %s, want the newest %s first", list.BakeOffs[0].Id, list.BakeOffs[1].Id, second.BakeOff.Id)
	}

	done := list.BakeOffs[1]
	if done.RecommendedArm == nil || *done.RecommendedArm != 2 || done.Cases != 2 || done.Arms[1].Summary == nil {
		t.Errorf("the done bake-off in the list = %+v, want arm 2 recommended and its 2 cases from the summaries", done)
	}
}

func TestBakeOffs_Rerun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	first := startBakeOff(t, h, bakeOffStart)
	h.workBakeOff(t, first.BakeOff.Id)

	var again gen.BakeOffStarted
	if code, p := evalCall(t, h, http.MethodPost, lineBakeOffs+"/"+first.BakeOff.Id+"/rerun", "", &again); code != http.StatusAccepted {
		t.Fatalf("rerun = %d %+v, want 202", code, p)
	}

	b := again.BakeOff
	if b.Id == first.BakeOff.Id || b.State != gen.BakeOffState(domain.EvalRunQueued) || b.Agent != bakeOffAgent || b.Trials != 2 ||
		len(b.Arms) != 2 || b.Arms[0].Model != bakeOffNow || b.Arms[1].Model != bakeOffOther || b.Version != 2 {
		t.Errorf("rerun = %+v, want a new queued bake-off of the same agent, arms and trials on v2", b)
	}
}

// draftModelOf is the model of agent in the line's draft.
func (h *harness) draftModelOf(t *testing.T, agent string) string {
	t.Helper()

	svc, ctx := h.authors(t)

	m, err := svc.DraftModel(ctx, lineID)
	if err != nil {
		t.Fatal(err)
	}

	list, _ := m.Model.Agents["agents"].([]any)
	for _, e := range list {
		if a, _ := e.(map[string]any); a["name"] == agent {
			model, _ := a["model"].(string)

			return model
		}
	}

	t.Fatalf("the draft has no agent %s", agent)

	return ""
}

func TestBakeOffs_ApplyReturnsTheDraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	b := startBakeOff(t, h, bakeOffStart).BakeOff
	h.workBakeOff(t, b.Id)

	var d gen.Draft
	if code, p := evalCall(t, h, http.MethodPost, lineBakeOffs+"/"+b.Id+"/apply", `{"arm":2}`, &d); code != http.StatusOK {
		t.Fatalf("apply = %d %+v, want 200", code, p)
	}

	if d.Id == "" || d.Base == nil || d.Base.Number != 2 {
		t.Errorf("draft = %+v, want the line's draft based on v2", d)
	}

	if got := h.draftModelOf(t, bakeOffAgent); got != bakeOffOther {
		t.Errorf("the draft's %s model = %q, want %s", bakeOffAgent, got, bakeOffOther)
	}

	rec, err := h.defs.FactoryRecord(inOrg(t), lineID)
	if err != nil || rec.Current.Number != 2 {
		t.Errorf("current after apply = %+v %v, want v2: apply never publishes", rec.Current, err)
	}
}

func TestBakeOffs_ApplyDraftConflict(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	b := startBakeOff(t, h, bakeOffStart).BakeOff

	// The backend's prompt changes in a draft after the bake-off's version.
	svc, ctx := h.authors(t)
	if _, _, err := svc.OpenDraft(ctx, lineID, authoring.OpenDraftRequest{}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.PutFile(ctx, lineID, authoring.FileWrite{Path: bakeOffPrompt, Content: []byte("# backend, again\n")}); err != nil {
		t.Fatal(err)
	}

	code, p := evalCall(t, h, http.MethodPost, lineBakeOffs+"/"+b.Id+"/apply", `{"arm":2}`, nil)
	wantEvalProblem(t, "apply after the agent changed", code, p, http.StatusPreconditionFailed, "draft-conflict")
}

func TestBakeOffs_ApplyUnknownArm(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	b := startBakeOff(t, h, bakeOffStart).BakeOff

	code, p := evalCall(t, h, http.MethodPost, lineBakeOffs+"/"+b.Id+"/apply", `{"arm":7}`, nil)
	wantEvalProblem(t, "apply of an arm it does not have", code, p, http.StatusUnprocessableEntity, "invalid-bake-off")
}

func TestBakeOffs_StartInvalid(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	for _, c := range []struct{ what, body string }{
		{"an unknown agent", `{"agent":"nobody",` + bakeOffArms + `}`},
		{"an agent without cases", `{"agent":"frontend","models":["claude-haiku-5-5","claude-opus-5-5"]}`},
		{"one arm", `{"agent":"backend","models":["` + bakeOffNow + `"]}`},
		{"no arm and no default models", `{"agent":"backend"}`},
		{"arms without the agent's model", `{"agent":"backend","models":["claude-haiku-5-5","claude-opus-5-5"]}`},
		{"an arm that is not a model id", `{"agent":"backend","models":["` + bakeOffNow + `","gpt"]}`},
		{"an arm twice", `{"agent":"backend","models":["` + bakeOffNow + `","` + bakeOffNow + `"]}`},
		{"five arms", `{"agent":"backend","models":["` + bakeOffNow + `","claude-a","claude-b","claude-c","claude-d"]}`},
		{"11 trials", `{"agent":"backend",` + bakeOffArms + `,"trials":11}`},
		{"0 trials", `{"agent":"backend",` + bakeOffArms + `,"trials":0}`},
	} {
		code, p := evalCall(t, h, http.MethodPost, lineBakeOffs, c.body, nil)
		wantEvalProblem(t, "start with "+c.what, code, p, http.StatusUnprocessableEntity, "invalid-bake-off")
	}

	var list gen.BakeOffList
	if code, _ := evalCall(t, h, http.MethodGet, lineBakeOffs, "", &list); code != http.StatusOK || len(list.BakeOffs) != 0 {
		t.Errorf("list after the refusals = %d %d bake-offs, want none", code, len(list.BakeOffs))
	}
}

func TestBakeOffs_StartOverBudget(t *testing.T) {
	t.Parallel()

	h := newHarnessLogged(t, nil, nil, withBudget(2))
	h.publishBakeOffCases(t)
	h.finishedBackendRun(t, 0.4)

	// 2 cases × 3 tries × 2 arms at 0.4 a try.
	code, p := evalCall(t, h, http.MethodPost, lineBakeOffs, `{"agent":"backend",`+bakeOffArms+`}`, nil)
	wantEvalProblem(t, "start over the budget", code, p, http.StatusConflict, "learning-budget-exhausted")

	if p.Estimate == nil || p.Estimate.Usd < 4.799 || p.Estimate.Usd > 4.801 {
		t.Errorf("estimate = %+v, want 4.8", p.Estimate)
	}

	var list gen.BakeOffList
	if code, _ := evalCall(t, h, http.MethodGet, lineBakeOffs, "", &list); code != http.StatusOK || len(list.BakeOffs) != 0 {
		t.Errorf("list after the refusal = %d %d bake-offs, want none", code, len(list.BakeOffs))
	}

	// Within the budget, it starts.
	startBakeOff(t, h, `{"agent":"backend",`+bakeOffArms+`,"trials":1}`)
}

func TestBakeOffs_NotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	b := startBakeOff(t, h, bakeOffStart).BakeOff

	var run gen.EvalRunStarted
	if code, _ := evalCall(t, h, http.MethodPost, lineEvals+"/runs", `{}`, &run); code != http.StatusAccepted {
		t.Fatalf("start an eval run = %d", code)
	}

	for _, c := range []struct{ what, path string }{
		{"another factory's bake-off", acmeEvals + "/bake-offs/" + b.Id},
		{"an eval run", lineBakeOffs + "/" + run.Run.Id},
		{"an unknown id", lineBakeOffs + "/no-such-bake-off"},
	} {
		for _, call := range []struct{ method, suffix, body string }{
			{http.MethodGet, "", ""},
			{http.MethodPost, "/rerun", ""},
			{http.MethodPost, "/apply", `{"arm":1}`},
		} {
			code, p := evalCall(t, h, call.method, c.path+call.suffix, call.body, nil)
			wantEvalProblem(t, call.method+" "+c.what+call.suffix, code, p, http.StatusNotFound, "eval-run-not-found")
		}
	}
}

func TestBakeOffs_ViewerMayReadButNotStartRerunOrApply(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishBakeOffCases(t)

	b := startBakeOff(t, h, bakeOffStart).BakeOff

	h.evalsAsOn(lineID, domain.FactoryViewer)

	for _, path := range []string{lineBakeOffs, lineBakeOffs + "/" + b.Id} {
		if code, _ := evalCall(t, h, http.MethodGet, path, "", nil); code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, code)
		}
	}

	for _, c := range []struct{ path, body string }{
		{lineBakeOffs, bakeOffStart},
		{lineBakeOffs + "/" + b.Id + "/rerun", ""},
		{lineBakeOffs + "/" + b.Id + "/apply", `{"arm":1}`},
	} {
		code, p := evalCall(t, h, http.MethodPost, c.path, c.body, nil)
		wantEvalProblem(t, "viewer POST "+c.path, code, p, http.StatusForbidden, "forbidden")
	}
}
