package api_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/jonah"
)

const (
	acmeJudges   = "/v1/orgs/acme/factories/acme/judges"
	specFollowed = "spec-followed"
)

var _ api.JudgeTries = (*jonah.Loop)(nil)

// followedJudge is acme's judge of the writer's runs.
var followedJudge = domain.Judge{
	Slug: specFollowed, Title: "Spec followed", Criterion: "The change does what the spec asks.", Agents: []string{"writer"},
	Outcomes: []domain.JudgeOutcome{{Score: 1, Label: "yes", Meaning: "All of it"}, {Score: 0, Label: "no", Meaning: "Not all"}},
	PassMark: 0.5, Sample: 0.2, Enabled: true, Rubric: "# Spec followed\n",
}

// judgeBody is a valid write of a judge of acme's writer, as JSON.
const judgeBody = `{"title":"Tests added","criterion":"The change adds tests.","agents":["writer"],` +
	`"outcomes":[{"score":1,"label":"yes","meaning":"Tests for each change"},{"score":0,"label":"no","meaning":"None"}],` +
	`"pass_mark":0.5,"sample":0.1,"rubric":"# Tests added\nLook for tests.\n"`

// acmeRecord is acme as the definition store has it.
func (h *harness) acmeRecord(t *testing.T) ports.FactoryRecord {
	t.Helper()

	rec, err := h.defs.FactoryRecord(inOrg(t), acmeID)
	if err != nil {
		t.Fatal(err)
	}

	return rec
}

// setJudges makes judges those of acme's current version.
func (h *harness) setJudges(t *testing.T, judges ...domain.Judge) {
	t.Helper()

	h.judges.Set(testOrg, acmeID, h.acmeRecord(t).Current.ID, judges...)
}

// finishedRun is a finished run of acme's writer on a new item titled title, ended at endedAt.
func (h *harness) finishedRun(t *testing.T, title string, endedAt time.Time) domain.Run {
	t.Helper()

	ctx := inOrg(t)

	it, err := h.store.CreateItem(ctx, domain.NewItem{FactoryID: acmeID, Title: title, Station: "inbox", CreatedAt: endedAt.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	id, err := h.store.StartRun(ctx, ports.NewRun{
		FactoryID: acmeID, DefinitionVersionID: h.acmeRecord(t).Current.ID, ItemID: it.ID, Station: "inbox", Agent: "writer",
		StartedAt: endedAt.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded}, endedAt); err != nil {
		t.Fatal(err)
	}

	r, err := h.store.Run(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// score records judge's production score of run: outcome label at score, costing cost.
func (h *harness) score(t *testing.T, judge string, run domain.Run, label string, score, cost float64) {
	t.Helper()

	ctx := inOrg(t)

	if _, err := h.judgeScores.Enqueue(ctx, domain.NewJudgeScore{
		FactoryID: acmeID, VersionID: run.DefinitionVersionID, Judge: judge, RunID: run.ID, ItemID: run.ItemID, At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	c, err := h.judgeScores.Claim(ctx, h.clock.Now(), h.clock.Now().Add(time.Hour))
	if err != nil || c == nil {
		t.Fatalf("claim = %v, %v", c, err)
	}

	if _, err := h.judgeScores.Record(ctx, c.Lease, domain.JudgeResult{
		State: domain.JudgeScoreScored, Outcome: label, Score: score, Passed: score >= 0.5, Reason: "Because.", CostUSD: cost, At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// as makes every authorization the real permission table's answer for p, in place of the
// harness's local user.
func (h *harness) as(p domain.Principal) {
	perms := api.NewPermissions(h.clock)

	h.seam.deny(func(r api.AccessRequest) error {
		r.Principal = p

		return perms.Authorize(context.Background(), r)
	})
}

// acmeMember is a member of testOrg with role on acme, signed in a minute ago.
func acmeMember(role domain.FactoryRole) domain.Principal {
	return domain.Principal{
		Kind: domain.PrincipalUser, User: "019a0000-0000-7000-8000-0000000000aa", Name: "Ada", Org: testOrg, OrgRole: domain.OrgMember,
		FactoryRoles: map[domain.FactoryID]domain.FactoryRole{acmeID: role},
		Session:      &domain.Session{ID: "s", CreatedAt: t0.Add(-time.Minute), ExpiresAt: t0.Add(time.Hour), Browser: true},
	}
}

// orgReader is an API key of testOrg that may read the organization but no factory.
func orgReader() domain.Principal {
	return domain.Principal{
		Kind: domain.PrincipalService, Key: "019a0000-0000-7000-8000-0000000000kk", Name: "ci", Org: testOrg,
		Scopes: []domain.Permission{"org:read"}, KeyCeiling: domain.FactoryViewer,
	}
}

func problemOfCall(t *testing.T, h *harness, method, path, body string) (int, gen.Problem) {
	t.Helper()

	var p gen.Problem

	code := h.call(t, method, path, body, &p)

	return code, p
}

func wantProblem(t *testing.T, what string, code int, p gen.Problem, status int, pcode string) {
	t.Helper()

	if code != status || deref(p.Code) != pcode {
		t.Errorf("%s = %d %q, want %d %q", what, code, deref(p.Code), status, pcode)
	}
}

func TestJudges_List(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setJudges(t) // v1 has none

	var empty gen.JudgeList
	if code := h.call(t, http.MethodGet, acmeJudges, "", &empty); code != http.StatusOK || len(empty.Judges) != 0 || empty.JudgeCostUsd != 0 {
		t.Fatalf("list without judges = %d %+v, want 200 and none", code, empty)
	}

	paused := domain.Judge{Slug: "tests-added", Title: "Tests added", Criterion: "Tests.", PassMark: 0.5, Sample: 0, Rubric: "x"}
	h.setJudges(t, followedJudge, paused)

	h.clock.Advance(2 * time.Hour)
	h.score(t, specFollowed, h.finishedRun(t, "Add a flag", t0.Add(time.Hour)), "yes", 1, 0.02)
	h.score(t, specFollowed, h.finishedRun(t, "Fix a typo", t0.Add(time.Hour)), "no", 0, 0.03)

	var got gen.JudgeList
	if code := h.call(t, http.MethodGet, acmeJudges, "", &got); code != http.StatusOK {
		t.Fatalf("list = %d, want 200", code)
	}

	if got.Version == nil || *got.Version != 1 || len(got.Judges) != 2 {
		t.Fatalf("list = %+v, want version 1 and two judges", got)
	}

	f, p := got.Judges[0], got.Judges[1]
	if f.Slug != specFollowed || f.Title != followedJudge.Title || f.PassMark != 0.5 || f.Sample != 0.2 || !f.Enabled ||
		!slices.Equal(f.Agents, []string{"writer"}) {
		t.Errorf("first judge = %+v, want spec-followed as its file says", f)
	}

	if s := f.Summary; s.Scored != 2 || s.Passed != 1 || s.PassRate != 0.5 || len(s.Days) != 1 || s.Days[0].Day.Format(time.DateOnly) != "2026-10-23" {
		t.Errorf("spec-followed's summary = %+v, want 2 scored, 1 passed on 2026-10-23", s)
	}

	if p.Slug != "tests-added" || p.Enabled || p.Agents == nil || p.Summary.Scored != 0 || p.Summary.Days == nil {
		t.Errorf("second judge = %+v, want tests-added, disabled, no agents and nothing scored", p)
	}

	if got.JudgeCostUsd < 0.0499 || got.JudgeCostUsd > 0.0501 {
		t.Errorf("judge cost = %v, want 0.05", got.JudgeCostUsd)
	}

	// A viewer reads; a key without factory:read is refused.
	h.as(acmeMember(domain.FactoryViewer))

	if code := h.call(t, http.MethodGet, acmeJudges, "", nil); code != http.StatusOK {
		t.Errorf("a viewer's list = %d, want 200", code)
	}

	h.as(orgReader())

	code, pr := problemOfCall(t, h, http.MethodGet, acmeJudges, "")
	wantProblem(t, "list without factory:read", code, pr, http.StatusForbidden, "forbidden")
}

func TestJudges_Get(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setJudges(t, followedJudge)
	h.clock.Advance(2 * time.Hour)
	h.score(t, specFollowed, h.finishedRun(t, "Add a flag", t0.Add(time.Hour)), "yes", 1, 0.02)

	var j gen.Judge
	if code := h.call(t, http.MethodGet, acmeJudges+"/"+specFollowed, "", &j); code != http.StatusOK {
		t.Fatalf("get = %d, want 200", code)
	}

	if j.Slug != specFollowed || j.Rubric != followedJudge.Rubric || len(j.Outcomes) != 2 || j.Outcomes[0].Label != "yes" ||
		j.Version != 1 || j.Model != nil || j.Summary.Scored != 1 || j.Summary.CostUsd != 0.02 {
		t.Errorf("get = %+v, want spec-followed of v1 with its file and one score", j)
	}

	if len(j.Recent) != 1 || j.Recent[0].ItemTitle != "Add a flag" || !strings.HasPrefix(j.Recent[0].ItemRef, "acme#") ||
		deref(j.Recent[0].Outcome) != "yes" || j.Recent[0].Score == nil || *j.Recent[0].Score != 1 || deref(j.Recent[0].Reason) != "Because." {
		t.Errorf("recent = %+v, want the one score of an acme item", j.Recent)
	}

	// acme names no size, and its connection is its model connection.
	if r := j.RunsOn; r.Size != "medium" || !slices.Equal(r.Connections, []string{"claude"}) || !r.Inherited ||
		!slices.Equal(r.Sees, []gen.JudgeRunsOnSees{gen.Events, gen.Spec, gen.PullRequest}) {
		t.Errorf("runs on = %+v, want medium, claude, inherited", r)
	}

	code, p := problemOfCall(t, h, http.MethodGet, acmeJudges+"/nobody-here", "")
	wantProblem(t, "get of an unknown judge", code, p, http.StatusNotFound, "judge-not-found")

	h.as(acmeMember(domain.FactoryViewer))

	if code := h.call(t, http.MethodGet, acmeJudges+"/"+specFollowed, "", nil); code != http.StatusOK {
		t.Errorf("a viewer's get = %d, want 200", code)
	}

	h.as(orgReader())

	code, p = problemOfCall(t, h, http.MethodGet, acmeJudges+"/"+specFollowed, "")
	wantProblem(t, "get without factory:read", code, p, http.StatusForbidden, "forbidden")
}

// Without Deps.Judges every judges route answers 503 judges-unavailable, after the factory is
// resolved: an unknown factory is still 404.
func TestJudges_Unavailable(t *testing.T) {
	t.Parallel()

	h := newHarnessLogged(t, nil, nil, func(d *api.Deps) { d.Judges = nil })

	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, acmeJudges, ""},
		{http.MethodPost, acmeJudges, `{"slug":"tests-added",` + judgeBody[1:] + "}"},
		{http.MethodGet, acmeJudges + "/" + specFollowed, ""},
		{http.MethodPut, acmeJudges + "/" + specFollowed, judgeBody + "}"},
		{http.MethodPost, acmeJudges + "/" + specFollowed + "/try", ""},
	} {
		code, p := problemOfCall(t, h, r.method, r.path, r.body)
		wantProblem(t, r.method+" "+r.path, code, p, http.StatusServiceUnavailable, "judges-unavailable")
	}

	code, p := problemOfCall(t, h, http.MethodGet, "/v1/orgs/acme/factories/nope/judges", "")
	wantProblem(t, "list of an unknown factory", code, p, http.StatusNotFound, "factory-not-found")
}

// publishedFile is path in acme's version number.
func (h *harness) publishedFile(t *testing.T, number int, path string) string {
	t.Helper()

	ctx := inOrg(t)

	v, err := h.defs.VersionByNumber(ctx, acmeID, number)
	if err != nil {
		t.Fatal(err)
	}

	hash, ok := v.Manifest[path]
	if !ok {
		t.Fatalf("version %d has no %s", number, path)
	}

	b, err := h.defs.Blob(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// wantNoDraft fails unless acme has no open draft.
func (h *harness) wantNoDraft(t *testing.T, after string) {
	t.Helper()

	if d := h.acmeRecord(t).Draft; d != nil {
		t.Errorf("after %s acme has draft %+v, want none", after, d)
	}
}

func TestJudges_Create(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setJudges(t, followedJudge)

	var j gen.Judge
	if code := h.call(t, http.MethodPost, acmeJudges, `{"slug":"tests-added",`+judgeBody[1:]+`}`, &j); code != http.StatusCreated {
		t.Fatalf("create = %d %+v, want 201", code, j)
	}

	if j.Slug != "tests-added" || j.Version != 2 || !j.Enabled || j.Title != "Tests added" || len(j.Outcomes) != 2 || j.Recent == nil {
		t.Errorf("create = %+v, want tests-added of v2", j)
	}

	want := "---\ntitle: Tests added\ncriterion: The change adds tests.\nagents:\n    - writer\noutcomes:\n" +
		"    - score: 1\n      label: \"yes\"\n      meaning: Tests for each change\n" +
		"    - score: 0\n      label: \"no\"\n      meaning: None\n" +
		"passMark: 0.5\nsample: 0.1\nenabled: true\n---\n\n# Tests added\nLook for tests.\n"
	if got := h.publishedFile(t, 2, "evals/judges/tests-added.md"); got != want {
		t.Errorf("v2's judge file =\n%s\nwant\n%s", got, want)
	}

	h.wantNoDraft(t, "a create")
	h.setJudges(t, followedJudge) // as the judges adapter reads v2

	// A slug the current version has, and one that is no slug.
	code, p := problemOfCall(t, h, http.MethodPost, acmeJudges, `{"slug":"spec-followed",`+judgeBody[1:]+`}`)
	wantProblem(t, "create of an existing judge", code, p, http.StatusConflict, "judge-exists")

	if code := h.call(t, http.MethodPost, acmeJudges, `{"slug":"../factory",`+judgeBody[1:]+`}`, nil); code != http.StatusBadRequest {
		t.Errorf("create of a bad slug = %d, want 400", code)
	}

	// A judge of an agent acme does not have fails validation: its problems, and no draft left.
	code, p = problemOfCall(t, h, http.MethodPost, acmeJudges, `{"slug":"other-judge",`+strings.Replace(judgeBody[1:], `"writer"`, `"reviewer"`, 1)+`}`)
	wantProblem(t, "create of an invalid judge", code, p, http.StatusConflict, "definition-invalid")

	if p.Problems == nil || !slices.ContainsFunc(*p.Problems, func(d gen.DefinitionProblem) bool {
		return d.File != nil && *d.File == "evals/judges/other-judge.md"
	}) {
		t.Errorf("invalid judge's problems = %+v, want one in its file", p.Problems)
	}

	h.wantNoDraft(t, "an invalid create")

	// A stale expected version: the publish is refused and the draft discarded.
	code, p = problemOfCall(t, h, http.MethodPost, acmeJudges, `{"slug":"other-judge","expected_current_version_id":"stale",`+judgeBody[1:]+`}`)
	wantProblem(t, "create on a stale version", code, p, http.StatusConflict, "draft-base-moved")

	h.wantNoDraft(t, "a stale create")

	// A viewer may not write.
	h.as(acmeMember(domain.FactoryViewer))

	code, p = problemOfCall(t, h, http.MethodPost, acmeJudges, `{"slug":"third-judge",`+judgeBody[1:]+`}`)
	wantProblem(t, "a viewer's create", code, p, http.StatusForbidden, "forbidden")
}

func TestJudges_Update(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setJudges(t, followedJudge)

	var j gen.Judge
	if code := h.call(t, http.MethodPut, acmeJudges+"/"+specFollowed, judgeBody+`,"enabled":false,"model":"claude-haiku-5-5"}`, &j); code != http.StatusOK {
		t.Fatalf("update = %d, want 200", code)
	}

	if j.Slug != specFollowed || j.Version != 2 || j.Enabled || deref(j.Model) != "claude-haiku-5-5" || j.Title != "Tests added" {
		t.Errorf("update = %+v, want spec-followed of v2, disabled, on haiku", j)
	}

	if got := h.publishedFile(t, 2, "evals/judges/spec-followed.md"); !strings.Contains(got, "model: claude-haiku-5-5\nenabled: false\n---\n") {
		t.Errorf("v2's judge file =\n%s\nwant the model and enabled: false", got)
	}

	h.setJudges(t, followedJudge) // as the judges adapter reads v2

	code, p := problemOfCall(t, h, http.MethodPut, acmeJudges+"/nobody-here", judgeBody+"}")
	wantProblem(t, "update of an unknown judge", code, p, http.StatusNotFound, "judge-not-found")

	// An open draft is left as it is.
	if code := h.call(t, http.MethodPost, acmeDraft, `{}`, nil); code != http.StatusCreated {
		t.Fatalf("open draft = %d, want 201", code)
	}

	before := h.acmeRecord(t).Draft

	code, p = problemOfCall(t, h, http.MethodPut, acmeJudges+"/"+specFollowed, judgeBody+"}")
	wantProblem(t, "update with an open draft", code, p, http.StatusConflict, "draft-open")

	if after := h.acmeRecord(t).Draft; after == nil || after.Revision != before.Revision {
		t.Errorf("the open draft became %+v, want it untouched at revision %d", after, before.Revision)
	}

	h.as(acmeMember(domain.FactoryViewer))

	code, p = problemOfCall(t, h, http.MethodPut, acmeJudges+"/"+specFollowed, judgeBody+"}")
	wantProblem(t, "a viewer's update", code, p, http.StatusForbidden, "forbidden")
}

func TestJudges_Try(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setJudges(t, followedJudge)

	for i := range 12 {
		h.finishedRun(t, "Item "+string(rune('a'+i)), t0.Add(-time.Duration(12-i)*time.Minute))
	}

	var got gen.JudgeTries
	if code := h.call(t, http.MethodPost, acmeJudges+"/"+specFollowed+"/try", "", &got); code != http.StatusAccepted {
		t.Fatalf("try = %d, want 202", code)
	}

	if len(got.Tries) != 10 {
		t.Fatalf("try queued %d, want the 10 newest runs", len(got.Tries))
	}

	if first := got.Tries[0]; first.State != gen.JudgeScoreStateQueued || first.ItemTitle != "Item l" || !strings.HasPrefix(first.ItemRef, "acme#") {
		t.Errorf("first try = %+v, want the newest run's item, queued", first)
	}

	code, p := problemOfCall(t, h, http.MethodPost, acmeJudges+"/nobody-here/try", "")
	wantProblem(t, "try of an unknown judge", code, p, http.StatusNotFound, "judge-not-found")

	h.as(acmeMember(domain.FactoryViewer))

	code, p = problemOfCall(t, h, http.MethodPost, acmeJudges+"/"+specFollowed+"/try", "")
	wantProblem(t, "a viewer's try", code, p, http.StatusForbidden, "forbidden")
}
