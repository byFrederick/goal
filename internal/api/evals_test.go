package api_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

const (
	acmeEvals = "/v1/orgs/acme/factories/acme/evals"
	// The cases of TestEvals_ tests: one checked by acme's writer's contract, one by a judge.
	caseByContract = "writes-the-spec"
	caseByJudge    = "follows-the-spec"
	evalJudge      = "spec-followed"
	evalJudgeTitle = "Spec followed"
	// evalAgent is acme's one agent, which the cases run.
	evalAgent = "writer"
)

var (
	_ api.EvalLoop     = (*jonah.Evals)(nil)
	_ api.EvalRunReads = (*porttest.Evals)(nil)
)

// evalCaseYAML is a case of acme's writer, with check.
func evalCaseYAML(check string) string {
	return `apiVersion: goal/v1
kind: EvalCase
metadata:
  description: The writer turns a request into a spec
spec:
  agent: writer
  input:
    title: Add a version flag
    body: The CLI prints its version.
  tries: 1
  check:
    ` + check + "\n"
}

// evalJudgeFile is acme's judge of the writer's runs.
const evalJudgeFile = `---
title: Spec followed
criterion: The change does what the spec asks.
agents: [writer]
outcomes:
  - {score: 1, label: "yes", meaning: All of it}
  - {score: 0, label: "no", meaning: Not all}
passMark: 0.5
sample: 0.2
---
Read the change.
`

// evalDeps are h's eval runs and judges, with a jonah eval loop over them and the store's finished
// runs.
func (h *harness) evalDeps(tenancy *porttest.Tenancy) *api.EvalDeps {
	h.evals, h.evalJudges = porttest.NewEvals(h.clock.Now), porttest.NewJudges()
	h.evalLoop = h.evalLoopWith(tenancy, nil)

	return &api.EvalDeps{Runs: h.evals, Loop: h.evalLoop, Judges: h.evalJudges, Versions: h.versions}
}

// evalLoopWith is a jonah eval loop over h's eval runs and the store's finished runs, applying
// bake-offs to drafts through an authoring service over h's definitions, with its deps changed by
// change.
func (h *harness) evalLoopWith(tenancy *porttest.Tenancy, change func(*jonah.EvalDeps)) *jonah.Evals {
	d := jonah.EvalDeps{
		Orgs: tenancy, Evals: h.evals, Suites: porttest.NewEvalSuites(), Runner: porttest.NewEvalRunner(nil),
		Finished: porttest.NewFinishedRuns(h.store), Clock: h.clock,
		Drafts: authoring.New(authoring.Deps{Store: h.defs, Loads: h.store, Templates: h.objects, Clock: h.clock}),
	}
	if change != nil {
		change(&d)
	}

	return jonah.NewEvals(d, jonah.Config{})
}

// withoutEvals is a goald that runs no evals.
func withoutEvals(_ *harness, d *api.Deps) { d.Evals = nil }

// withEvalGate gates the API's publishes on their eval cases, as goald will once it runs evals.
func withEvalGate(h *harness, d *api.Deps) {
	d.Definitions = authoring.New(authoring.Deps{
		Store: h.defs, Loads: h.store, Templates: h.objects, Clock: h.clock,
		EvalGate: &authoring.EvalGate{Runs: h.evals, Starter: h.evalLoop},
	})
}

// authors is an authoring service over h's definitions, acting as the local user.
func (h *harness) authors(t *testing.T) (*authoring.Service, context.Context) {
	t.Helper()

	return authoring.New(authoring.Deps{Store: h.defs, Loads: h.store, Templates: h.objects, Clock: h.clock}),
		domain.WithActor(inOrg(t), domain.LocalUser)
}

// publishCases publishes acme's v2 with its judge and two cases, caseByContract and caseByJudge,
// and makes the judge the version's for the judges port.
func (h *harness) publishCases(t *testing.T) {
	t.Helper()

	svc, ctx := h.authors(t)

	if _, _, err := svc.OpenDraft(ctx, acmeID, authoring.OpenDraftRequest{}); err != nil {
		t.Fatal(err)
	}

	var rev int64

	for path, content := range map[string]string{
		config.JudgesDir + "/" + evalJudge + ".md":       evalJudgeFile,
		config.CasesDir + "/" + caseByContract + ".yaml": evalCaseYAML("contract: true"),
		config.CasesDir + "/" + caseByJudge + ".yaml":    evalCaseYAML("judge: " + evalJudge),
	} {
		saved, err := svc.PutFile(ctx, acmeID, authoring.FileWrite{Path: path, Content: []byte(content), Create: true})
		if err != nil {
			t.Fatalf("put %s: %v", path, err)
		}

		rev = saved.Revision
	}

	rec, err := h.defs.FactoryRecord(inOrg(t), acmeID)
	if err != nil {
		t.Fatal(err)
	}

	v, err := svc.Publish(ctx, acmeID, authoring.PublishRequest{Revision: rev, ExpectedCurrent: rec.Current.ID})
	if err != nil {
		t.Fatalf("publish the cases: %v", err)
	}

	h.evalJudges.Set(testOrg, acmeID, v.ID, domain.Judge{Slug: evalJudge, Title: evalJudgeTitle})
}

// pastRun is a finished run of acme's writer on a new item titled title.
func (h *harness) pastRun(t *testing.T, title string) domain.Run {
	t.Helper()

	ctx := inOrg(t)

	rec, err := h.defs.FactoryRecord(ctx, acmeID)
	if err != nil {
		t.Fatal(err)
	}

	it, err := h.store.CreateItem(ctx, domain.NewItem{
		FactoryID: acmeID, Type: typeFeature, Lane: "backend", Title: title, Body: "The CLI prints its version.", Station: stInbox, CreatedAt: t0.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	id, err := h.store.StartRun(ctx, ports.NewRun{
		FactoryID: acmeID, DefinitionVersionID: rec.Current.ID, ItemID: it.ID, Station: stInbox, Agent: evalAgent, StartedAt: t0.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.4}, t0); err != nil {
		t.Fatal(err)
	}

	r, err := h.store.Run(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// workTries records every claimable try: pass for caseByContract, fail for any other case. Then
// it finishes run.
func (h *harness) workTries(t *testing.T, run string) {
	t.Helper()

	ctx := inOrg(t)

	for {
		c, err := h.evals.ClaimResult(ctx, h.clock.Now(), time.Hour)
		if err != nil {
			break
		}

		state := domain.EvalFail
		if c.Case == caseByContract {
			state = domain.EvalPass
		}

		if err := h.evals.RecordResult(ctx, c, domain.EvalOutcome{State: state, CheckedBy: domain.EvalCheckedByContract, CostUSD: 0.25}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := h.evals.FinishRun(ctx, domain.EvalRunID(run), domain.EvalRunDone, h.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// evalsAs makes every authorization the real permission table's answer for a member of testOrg
// with role on acme, signed in a minute ago.
func (h *harness) evalsAs(role domain.FactoryRole) { h.evalsAsOn(acmeID, role) }

// evalsAsOn is evalsAs with role on factory.
func (h *harness) evalsAsOn(factory domain.FactoryID, role domain.FactoryRole) {
	perms := api.NewPermissions(h.clock)
	p := domain.Principal{
		Kind: domain.PrincipalUser, User: "019a0000-0000-7000-8000-0000000000ab", Name: "Ada", Org: testOrg, OrgRole: domain.OrgMember,
		FactoryRoles: map[domain.FactoryID]domain.FactoryRole{factory: role},
		Session:      &domain.Session{ID: "s", CreatedAt: t0.Add(-time.Minute), ExpiresAt: t0.Add(time.Hour), Browser: true},
	}

	h.seam.deny(func(r api.AccessRequest) error {
		r.Principal = p

		return perms.Authorize(context.Background(), r)
	})
}

// evalCall makes a call and returns its status and its problem, when it answered one.
func evalCall(t *testing.T, h *harness, method, path, body string, out any) (int, gen.Problem) {
	t.Helper()

	var p gen.Problem

	if out == nil {
		out = &p
	}

	code := h.call(t, method, path, body, out)

	return code, p
}

func wantEvalProblem(t *testing.T, what string, code int, p gen.Problem, status int, pcode string) {
	t.Helper()

	if code != status || deref(p.Code) != pcode {
		t.Errorf("%s = %d %q, want %d %q", what, code, deref(p.Code), status, pcode)
	}
}

func TestEvals_Unavailable(t *testing.T) {
	t.Parallel()

	h := newHarnessLogged(t, nil, nil, withoutEvals)

	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, acmeEvals, ""},
		{http.MethodPost, acmeEvals + "/runs", `{}`},
		{http.MethodPost, acmeEvals + "/estimate", `{}`},
		{http.MethodGet, acmeEvals + "/runs/r1", ""},
		{http.MethodPost, acmeEvals + "/runs/r1/cancel", ""},
		{http.MethodPost, acmeEvals + "/cases", `{"run":"r1","name":"a-case"}`},
		{http.MethodGet, acmeEvals + "/bake-offs", ""},
		{http.MethodPost, acmeEvals + "/bake-offs", `{"agent":"writer"}`},
		{http.MethodGet, acmeEvals + "/bake-offs/b1", ""},
		{http.MethodPost, acmeEvals + "/bake-offs/b1/rerun", ""},
		{http.MethodPost, acmeEvals + "/bake-offs/b1/apply", applyArmOne},
	} {
		code, p := evalCall(t, h, c.method, c.path, c.body, nil)
		wantEvalProblem(t, c.method+" "+c.path, code, p, http.StatusServiceUnavailable, "evals-unavailable")
	}

	// The factory is resolved first: an unknown one is 404, not 503.
	code, p := evalCall(t, h, http.MethodGet, "/v1/orgs/acme/factories/nope/evals", "", nil)
	wantEvalProblem(t, "list of an unknown factory", code, p, http.StatusNotFound, "factory-not-found")
}

func TestEvals_ListCasesAndSummary(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var empty gen.EvalList
	if code, _ := evalCall(t, h, http.MethodGet, acmeEvals, "", &empty); code != http.StatusOK || len(empty.Cases) != 0 || empty.Summary.Cases != 0 {
		t.Fatalf("list without cases = %d %+v, want 200 and none", code, empty)
	}

	h.publishCases(t)

	var started gen.EvalRunStarted
	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/runs", `{}`, &started); code != http.StatusAccepted {
		t.Fatalf("start = %d, want 202", code)
	}

	h.workTries(t, started.Run.Id)

	var got gen.EvalList
	if code, _ := evalCall(t, h, http.MethodGet, acmeEvals, "", &got); code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}

	wantEvalSummary(t, got.Summary, started.Run.Id)
	wantEvalCases(t, got.Cases)
}

// wantEvalSummary wants the summary of TestEvals_ListCasesAndSummary: v2, 1 of 2 cases and 1 of 2
// tries passing, and run its latest with 1 try per case.
func wantEvalSummary(t *testing.T, sum gen.EvalSummary, run string) {
	t.Helper()

	ok := valueOf(sum.Version) == 2 && sum.Cases == 2 && sum.CasesPassing == 1 && sum.Tries == 2 && sum.TriesPassed == 1 &&
		valueOf(sum.FirstTryPassRate) == 0.5 && deref(sum.LatestRunId) == run && valueOf(sum.LatestTriesPerCase) == 1 && sum.LatestRunAt != nil
	if !ok {
		t.Errorf("summary = %+v, want v2, 1 of 2 cases passing, 1 of 2 tries, the latest run %s with 1 try per case", sum, run)
	}
}

// wantEvalCases wants the cases of TestEvals_ListCasesAndSummary: the judged case failed, the
// contract's passed.
func wantEvalCases(t *testing.T, cases []gen.EvalCaseEntry) {
	t.Helper()

	if len(cases) != 2 {
		t.Fatalf("cases = %+v, want 2", cases)
	}

	judged, contract := cases[0], cases[1]
	wantEvalAll(t, "first case", judged,
		evalCheck{judged.Name == caseByJudge, caseByJudge},
		evalCheck{judged.CheckedBy.Kind == gen.EvalCheckedByKindJudge && deref(judged.CheckedBy.Judge) == evalJudge, "checked by " + evalJudge},
		evalCheck{deref(judged.CheckedBy.Title) == evalJudgeTitle, "the judge's title " + evalJudgeTitle},
		evalCheck{len(judged.LastTries) == 1 && judged.LastTries[0].State == gen.EvalResultStateFail, "its one try failed"})
	wantEvalAll(t, "second case", contract,
		evalCheck{contract.Name == caseByContract && contract.Agent == evalAgent, caseByContract + " of the writer"},
		evalCheck{contract.Description == "The writer turns a request into a spec", "its description"},
		evalCheck{contract.CheckedBy.Kind == gen.EvalCheckedByKindContract, "checked by its contract"},
		evalCheck{valueOf(contract.CostUsd) == 0.25, "costing 0.25"},
		evalCheck{len(contract.LastTries) == 1 && contract.LastTries[0].State == gen.EvalResultStatePass, "its one try passed"})
}

// evalCheck is one condition of an answer and what it says.
type evalCheck struct {
	ok   bool
	what string
}

// wantEvalAll reports each check of v that fails.
func wantEvalAll(t *testing.T, name string, v any, checks ...evalCheck) {
	t.Helper()

	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s = %+v, want %s", name, v, c.what)
		}
	}
}

func TestEvals_StartAndEstimate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// The line factory has no cases.
	code, p := evalCall(t, h, http.MethodPost, "/v1/orgs/acme/factories/line/evals/estimate", `{}`, nil)
	wantEvalProblem(t, "estimate without cases", code, p, http.StatusBadRequest, "eval-case-unknown")

	h.publishCases(t)
	h.pastRun(t, "A finished run") // prices the writer's tries at 0.4

	var est gen.EvalEstimate
	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/estimate", `{}`, &est); code != http.StatusOK || est.Usd < 0.799 || est.Usd > 0.801 || len(est.Unpriced) != 0 {
		t.Errorf("estimate = %d %+v, want 200 and 0.8 (two tries at the writer's 0.4)", code, est)
	}

	if _, err := h.evals.LatestRunForContent(inOrg(t), acmeID, domain.BlobHash("x")); err == nil {
		t.Error("the estimate started a run")
	}

	var one gen.EvalRunStarted
	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/runs", `{"case":"`+caseByJudge+`"}`, &one); code != http.StatusAccepted ||
		deref(one.Run.Case) != caseByJudge || len(one.Run.Results) != 1 || valueOf(one.Run.Version) != 2 || one.Run.State != gen.EvalRunState(domain.EvalRunQueued) {
		t.Errorf("start of one case = %d %+v, want 202, a queued run of v2 with its one try", code, one.Run)
	}

	for _, c := range []struct{ what, body, code string }{
		{"a bad case name", `{"case":"Not A Name"}`, ""},
		{"an unknown case", `{"case":"no-such-case"}`, "eval-case-unknown"},
		{"a bad content", `{"on":"elsewhere"}`, ""},
	} {
		code, p := evalCall(t, h, http.MethodPost, acmeEvals+"/runs", c.body, nil)
		wantEvalProblem(t, "start with "+c.what, code, p, http.StatusBadRequest, c.code)
	}

	code, p = evalCall(t, h, http.MethodPost, acmeEvals+"/runs", `{"on":"draft"}`, nil)
	wantEvalProblem(t, "start on a draft that is not open", code, p, http.StatusNotFound, "no-draft")

	code, p = evalCall(t, h, http.MethodPost, "/v1/orgs/acme/factories/nope/evals/runs", `{}`, nil)
	wantEvalProblem(t, "start of an unknown factory", code, p, http.StatusNotFound, "factory-not-found")
}

func TestEvals_GetAndCancel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishCases(t)

	var started gen.EvalRunStarted
	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/runs", `{}`, &started); code != http.StatusAccepted {
		t.Fatalf("start = %d", code)
	}

	run := acmeEvals + "/runs/" + started.Run.Id

	var got gen.EvalRun
	if code, _ := evalCall(t, h, http.MethodGet, run, "", &got); code != http.StatusOK || got.Id != started.Run.Id || len(got.Results) != 2 {
		t.Errorf("get = %d %+v, want 200 with 2 results", code, got)
	}

	code, p := evalCall(t, h, http.MethodGet, acmeEvals+"/runs/no-such-run", "", nil)
	wantEvalProblem(t, "get of an unknown run", code, p, http.StatusNotFound, "eval-run-not-found")

	code, p = evalCall(t, h, http.MethodGet, "/v1/orgs/acme/factories/line/evals/runs/"+started.Run.Id, "", nil)
	wantEvalProblem(t, "get of another factory's run", code, p, http.StatusNotFound, "eval-run-not-found")

	var cancelled gen.EvalRun
	if code, _ := evalCall(t, h, http.MethodPost, run+"/cancel", "", &cancelled); code != http.StatusOK ||
		cancelled.State != gen.EvalRunState(domain.EvalRunCancelled) || cancelled.FinishedAt == nil {
		t.Errorf("cancel = %d %+v, want 200 and cancelled", code, cancelled)
	}

	code, p = evalCall(t, h, http.MethodPost, run+"/cancel", "", nil)
	wantEvalProblem(t, "cancel of a finished run", code, p, http.StatusConflict, "eval-run-finished")

	code, p = evalCall(t, h, http.MethodPost, acmeEvals+"/runs/no-such-run/cancel", "", nil)
	wantEvalProblem(t, "cancel of an unknown run", code, p, http.StatusNotFound, "eval-run-not-found")
}

func TestEvals_ViewerMayReadButNotStartCancelOrAdd(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishCases(t)

	var started gen.EvalRunStarted
	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/runs", `{}`, &started); code != http.StatusAccepted {
		t.Fatalf("start = %d", code)
	}

	past := h.pastRun(t, "A finished run")

	h.evalsAs(domain.FactoryViewer)

	for _, path := range []string{acmeEvals, acmeEvals + "/runs/" + started.Run.Id} {
		if code, _ := evalCall(t, h, http.MethodGet, path, "", nil); code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, code)
		}
	}

	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/estimate", `{}`, nil); code != http.StatusOK {
		t.Errorf("viewer estimate = %d, want 200", code)
	}

	for _, c := range []struct{ path, body string }{
		{acmeEvals + "/runs", `{}`},
		{acmeEvals + "/runs/" + started.Run.Id + "/cancel", ""},
		{acmeEvals + "/cases", `{"run":"` + string(past.ID) + `","name":"a-new-case"}`},
	} {
		code, p := evalCall(t, h, http.MethodPost, c.path, c.body, nil)
		wantEvalProblem(t, "viewer POST "+c.path, code, p, http.StatusForbidden, "forbidden")
	}
}

// wantWrittenCase wants version number's case name to replay pastRun's writer run, checked by its
// contract, with 2 tries.
func (h *harness) wantWrittenCase(t *testing.T, number int, name string) {
	t.Helper()

	svc, ctx := h.authors(t)
	path := config.CasesDir + "/" + name + ".yaml"

	f, err := svc.VersionFile(ctx, acmeID, authoring.VersionSelector{Kind: authoring.SelectNumber, Number: number}, path)
	if err != nil {
		t.Fatal(err)
	}

	cases, err := config.EvalCases(fstest.MapFS{path: {Data: f.Content}})
	if err != nil || len(cases) != 1 {
		t.Fatalf("the written case = %v, %v\n%s", cases, err, f.Content)
	}

	c := cases[0]
	if c.Agent != evalAgent || c.Tries != 2 || !c.Contract || c.Description != "Add a version flag to the CLI" ||
		!reflect.DeepEqual(c.Input, domain.EvalInput{Title: "Add a version flag to the CLI", Body: "The CLI prints its version.", Type: typeFeature, Lane: "backend"}) {
		t.Errorf("the written case = %+v, want the writer's run replayed, checked by its contract, 2 tries", c)
	}
}

func TestEvals_AddCaseFromRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishCases(t)

	past := h.pastRun(t, "Add a version flag to the CLI")
	body := `{"run":"` + string(past.ID) + `","name":"version-flag","tries":2}`

	var v gen.Version
	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/cases", body, &v); code != http.StatusCreated || valueOf(v.Number) != 3 {
		t.Fatalf("add case = %d %+v, want 201 and v3", code, v)
	}

	h.wantWrittenCase(t, 3, "version-flag")

	// v3 published through config.Check, so the file passes it; a judge check passes it too.
	judged := `{"run":"` + string(past.ID) + `","name":"version-judged","judge":"` + evalJudge + `","description":"The spec is followed"}`
	h.evalJudges.Set(testOrg, acmeID, domain.DefinitionVersionID(v.Id), domain.Judge{Slug: evalJudge, Title: evalJudgeTitle})

	var v4 gen.Version
	if code, _ := evalCall(t, h, http.MethodPost, acmeEvals+"/cases", judged, &v4); code != http.StatusCreated {
		t.Errorf("add a judged case = %d, want 201", code)
	}

	h.evalJudges.Set(testOrg, acmeID, domain.DefinitionVersionID(v4.Id), domain.Judge{Slug: evalJudge, Title: evalJudgeTitle})

	for _, c := range []struct {
		what, body string
		status     int
		code       string
	}{
		{"a case the current version has", body, http.StatusConflict, "eval-case-exists"},
		{"a bad name", `{"run":"` + string(past.ID) + `","name":"Bad"}`, http.StatusBadRequest, ""},
		{"an unknown judge", `{"run":"` + string(past.ID) + `","name":"by-a-ghost","judge":"no-such-judge"}`, http.StatusBadRequest, ""},
		{"an unknown run", `{"run":"no-such-run","name":"from-nowhere"}`, http.StatusNotFound, "run-not-found"},
	} {
		code, p := evalCall(t, h, http.MethodPost, acmeEvals+"/cases", c.body, nil)
		wantEvalProblem(t, "add "+c.what, code, p, c.status, c.code)
	}

	svc, ctx := h.authors(t)
	if _, _, err := svc.OpenDraft(ctx, acmeID, authoring.OpenDraftRequest{}); err != nil {
		t.Fatal(err)
	}

	code, p := evalCall(t, h, http.MethodPost, acmeEvals+"/cases", `{"run":"`+string(past.ID)+`","name":"while-drafting"}`, nil)
	wantEvalProblem(t, "add while a draft is open", code, p, http.StatusConflict, "draft-open")
}

func TestEvals_AddCaseGateRefusalKeepsTheDraft(t *testing.T) {
	t.Parallel()

	h := newHarnessLogged(t, nil, nil, withEvalGate)
	past := h.pastRun(t, "Add a version flag to the CLI")

	code, p := evalCall(t, h, http.MethodPost, acmeEvals+"/cases", `{"run":"`+string(past.ID)+`","name":"version-flag"}`, nil)
	wantEvalProblem(t, "add case under the gate", code, p, http.StatusConflict, "evals-pending")

	run, err := h.evals.GetRun(inOrg(t), domain.EvalRunID(deref(p.EvalRunId)))
	if err != nil || run.Trigger != domain.EvalOnPublish || len(run.Results) != 1 {
		t.Errorf("the gate's run = %+v, %v; want a publish run of the new case", run, err)
	}

	svc, ctx := h.authors(t)
	if _, err := svc.Draft(ctx, acmeID); err != nil {
		t.Errorf("draft after the gate refused: %v, want it open", err)
	}
}

// withEvals is d with h's eval runs and judges, changed by each of opts.
func (h *harness) withEvals(d api.Deps, tenancy *porttest.Tenancy, opts []func(*harness, *api.Deps)) api.Deps {
	d.Evals = h.evalDeps(tenancy)

	for _, opt := range opts {
		opt(h, &d)
	}

	return d
}
