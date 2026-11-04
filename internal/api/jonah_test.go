package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/jonah"
)

// roleOwner is the owner role's name, as the policy and the views name it.
const roleOwner = "owner"

const (
	permFactoryRead    = domain.Permission("factory:read")
	permDefinitionEdit = domain.Permission("definition:edit")
	acmeJonah          = "/v1/orgs/acme/factories/acme/jonah"
	acmeProposals      = acmeJonah + "/proposals"
	rulePath           = "rules/evidence.md"
	writerNew          = "# writer\nAttach a screenshot.\n"
	ruleNew            = "Show evidence.\n"
	// reviewStation is where the evidence of TestJonah_Reads was sent back.
	reviewStation = "review"
	// rejectBody is a valid rejection.
	rejectBody = `{"reason":"Not this time."}`
)

var (
	_ api.JonahDecisions = (*jonah.Decider)(nil)
	_ jonah.Authoring    = (*api.JonahAuthoring)(nil)
	_ api.JonahReads     = ports.JonahStore(nil)
	_ api.JonahModels    = (*jonah.ModelProposals)(nil)
)

// val is *p, or T's zero value for nil.
func val[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}

// proposalFiles edit acme's writer prompt as v1 has it, by one replacement, and add a rule.
func proposalFiles() []domain.ProposalEdit {
	return []domain.ProposalEdit{
		{Path: writerFile, Replacements: []domain.Replacement{{Old: writerPrompt, New: writerNew}}},
		{Path: rulePath, Content: ruleNew},
	}
}

// review stores one finished review of acme's current version with proposals and observations,
// made at the harness's now, and returns the stored proposals.
func (h *harness) review(t *testing.T, proposals []domain.NewProposal, observations ...domain.NewObservation) []domain.Proposal {
	t.Helper()

	ctx := inOrg(t)
	now := h.clock.Now()

	if _, err := h.store.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: acmeID, VersionID: h.acmeRecord(t).Current.ID, Trigger: domain.JonahThreshold,
		WindowStart: now.Add(-time.Hour), WindowEnd: now, By: domain.LocalUser, At: now,
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := h.store.ClaimJonahReview(ctx, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, stored, err := h.store.FinishJonahReview(ctx, claimed, domain.JonahReviewResult{
		State: domain.JonahDone, At: now, Proposals: proposals, Observations: observations,
	})
	if err != nil {
		t.Fatal(err)
	}

	return stored
}

// proposeOne stores one proposal of files and returns its path.
func (h *harness) proposeOne(t *testing.T, files []domain.ProposalEdit) string {
	t.Helper()

	p := h.review(t, []domain.NewProposal{{Title: "Ask for a screenshot", Edits: files}})

	return acmeProposals + "/" + strconv.FormatInt(p[0].Number, 10)
}

// audited is the number of entries of action in the organization's audit log.
func (h *harness) audited(t *testing.T, action string) int {
	t.Helper()

	es, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: action, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	return len(es)
}

// getProposal reads the proposal at path.
func (h *harness) getProposal(t *testing.T, path string) gen.Proposal {
	t.Helper()

	var p gen.Proposal
	if code := h.call(t, http.MethodGet, path, "", &p); code != http.StatusOK {
		t.Fatalf("get %s = %d, want 200", path, code)
	}

	return p
}

func TestJonah_ApproveProposal(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := h.proposeOne(t, proposalFiles())

	var got gen.ProposalDecided
	if code := h.call(t, http.MethodPost, path+"/approve", "", &got); code != http.StatusOK {
		t.Fatalf("approve = %d, want 200", code)
	}

	if got.State != gen.ProposalStatePublished || val(got.Version) != 2 {
		t.Errorf("approve = %+v, want published as v2", got)
	}

	if f := h.publishedFile(t, 2, writerFile); f != writerNew {
		t.Errorf("v2's %s = %q, want %q", writerFile, f, writerNew)
	}

	if f := h.publishedFile(t, 2, rulePath); f != ruleNew {
		t.Errorf("v2's %s = %q, want %q", rulePath, f, ruleNew)
	}

	h.wantNoDraft(t, "an approve")

	if p := h.getProposal(t, path); p.State != gen.ProposalStatePublished || val(p.PublishedVersion) != 2 || p.DecidedBy == nil {
		t.Errorf("proposal = %+v, want published as v2 with who decided", p)
	}

	if n := h.audited(t, domain.AuditProposalPublished); n != 1 {
		t.Errorf("%d %s entries, want 1", n, domain.AuditProposalPublished)
	}
}

func TestJonah_ApproveRefusals(t *testing.T) {
	t.Parallel()

	t.Run("draft open", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		path := h.proposeOne(t, proposalFiles())

		if code := h.call(t, http.MethodPost, acmeDraft, `{}`, nil); code != http.StatusCreated {
			t.Fatalf("open draft = %d, want 201", code)
		}

		before := h.acmeRecord(t).Draft

		code, p := problemOfCall(t, h, http.MethodPost, path+"/approve", "")
		wantProblem(t, "approve with an open draft", code, p, http.StatusConflict, "draft-open")

		if after := h.acmeRecord(t).Draft; after == nil || after.Revision != before.Revision {
			t.Errorf("the open draft became %+v, want it untouched at revision %d", after, before.Revision)
		}
	})

	t.Run("old text gone", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		files := proposalFiles()
		files[0].Replacements = []domain.Replacement{{Old: "# writer, as it was once\n", New: writerNew}}
		path := h.proposeOne(t, files)

		code, p := problemOfCall(t, h, http.MethodPost, path+"/approve", "")
		wantProblem(t, "approve of a stale proposal", code, p, http.StatusConflict, "proposal-stale")

		h.wantNoDraft(t, "a stale approve")

		if got := h.getProposal(t, path); got.State != gen.ProposalStateProposed {
			t.Errorf("after a stale approve the proposal is %s, want proposed", got.State)
		}
	})

	t.Run("new file that exists now", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		files := proposalFiles()
		files[1].Path = yamlFile // a new file, but factory.yaml is there
		path := h.proposeOne(t, files)

		code, p := problemOfCall(t, h, http.MethodPost, path+"/approve", "")
		wantProblem(t, "approve of a new file that exists", code, p, http.StatusConflict, "proposal-stale")
	})
}

func TestJonah_EditProposal(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := h.proposeOne(t, proposalFiles())

	var got gen.ProposalDecided
	if code := h.call(t, http.MethodPost, path+"/edit", "", &got); code != http.StatusOK {
		t.Fatalf("edit = %d, want 200", code)
	}

	if got.State != gen.ProposalStateApplied || got.DraftRevision == nil {
		t.Errorf("edit = %+v, want applied with the draft's revision", got)
	}

	for p, want := range map[string]string{writerFile: writerNew, rulePath: ruleNew} {
		if f := h.request(t, http.MethodGet, acmeDraft+"/files/"+url.PathEscape(p), "", nil); f.status != http.StatusOK || string(f.body) != want {
			t.Errorf("draft's %s = %d %q, want %q", p, f.status, f.body, want)
		}
	}

	if rec := h.acmeRecord(t); rec.Current.Number != 1 {
		t.Errorf("edit published v%d, want acme still on v1", rec.Current.Number)
	}

	if n := h.audited(t, domain.AuditProposalApplied); n != 1 {
		t.Errorf("%d %s entries, want 1", n, domain.AuditProposalApplied)
	}

	// The draft has the rule now: a second proposal adding it as a new file conflicts.
	second := h.proposeOne(t, proposalFiles()[1:])

	code, p := problemOfCall(t, h, http.MethodPost, second+"/edit", "")
	wantProblem(t, "edit over a changed draft", code, p, http.StatusConflict, "draft-conflict")

	if f := h.request(t, http.MethodGet, acmeDraft+"/files/"+url.PathEscape(rulePath), "", nil); string(f.body) != ruleNew {
		t.Errorf("after a conflict the draft's rule = %q, want it untouched", f.body)
	}
}

func TestJonah_RejectProposal(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := h.proposeOne(t, proposalFiles())

	for _, body := range []string{`{}`, `{"reason":"  "}`} {
		if code := h.call(t, http.MethodPost, path+"/reject", body, nil); code != http.StatusBadRequest {
			t.Errorf("reject with %s = %d, want 400", body, code)
		}
	}

	var got gen.ProposalDecided
	if code := h.call(t, http.MethodPost, path+"/reject", `{"reason":"Screenshots are attached already."}`, &got); code != http.StatusOK {
		t.Fatalf("reject = %d, want 200", code)
	}

	if p := h.getProposal(t, path); p.State != gen.ProposalStateRejected || deref(p.RejectReason) != "Screenshots are attached already." {
		t.Errorf("proposal = %+v, want rejected with the reason", p)
	}

	if n := h.audited(t, domain.AuditProposalRejected); n != 1 {
		t.Errorf("%d %s entries, want 1", n, domain.AuditProposalRejected)
	}

	for _, c := range []struct{ verb, body string }{{"/approve", ""}, {"/edit", ""}, {"/reject", `{"reason":"again"}`}} {
		code, p := problemOfCall(t, h, http.MethodPost, path+c.verb, c.body)
		wantProblem(t, c.verb+" of a decided proposal", code, p, http.StatusConflict, "proposal-decided")
	}

	code, p := problemOfCall(t, h, http.MethodPost, acmeProposals+"/99/reject", rejectBody)
	wantProblem(t, "reject of an unknown proposal", code, p, http.StatusNotFound, "proposal-not-found")
}

// "Look for improvements now" queues a manual review at once; one in flight refuses a second, a
// failed review's error shows on the overview, and a spent budget refuses another.
func TestJonah_QueueReview(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	code, p := problemOfCall(t, h, http.MethodPost, acmeJonah+"/reviews", "")
	wantProblem(t, "a review of a line that spent nothing", code, p, http.StatusConflict, "learning-budget-exhausted")

	h.kindRun(t, domain.RunKindStation, 200) // Jonah's budget is $10

	var r gen.JonahReview
	if code := h.call(t, http.MethodPost, acmeJonah+"/reviews", "", &r); code != http.StatusAccepted {
		t.Fatalf("queue review = %d, want 202", code)
	}

	if r.State != gen.JonahReviewStatePending || r.Trigger != gen.JonahReviewTriggerManual || r.Id == "" {
		t.Errorf("queued review = %+v, want a pending manual review", r)
	}

	if n := h.audited(t, domain.AuditReviewQueued); n != 1 {
		t.Errorf("%d %s entries, want 1", n, domain.AuditReviewQueued)
	}

	code, p = problemOfCall(t, h, http.MethodPost, acmeJonah+"/reviews", "")
	wantProblem(t, "a second review while one is pending", code, p, http.StatusConflict, "review-in-flight")

	claimed, err := h.store.ClaimJonahReview(inOrg(t), h.clock.Now(), time.Minute)
	if err != nil || string(claimed.ID) != r.Id {
		t.Fatalf("ClaimJonahReview = %+v, %v", claimed, err)
	}

	if _, _, err := h.store.FinishJonahReview(inOrg(t), claimed, domain.JonahReviewResult{
		State: domain.JonahFailed, Error: "the review run ended failed: out of turns", At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	var ov gen.JonahOverview
	if code := h.call(t, http.MethodGet, acmeJonah, "", &ov); code != http.StatusOK {
		t.Fatalf("overview = %d, want 200", code)
	}

	if last := val(ov.LastReview); last.State != gen.JonahReviewStateFailed || val(last.Error) != "the review run ended failed: out of turns" {
		t.Errorf("last review = %+v, want failed with its error", last)
	}

	h.jonahRun(t, 10)

	code, p = problemOfCall(t, h, http.MethodPost, acmeJonah+"/reviews", "")
	wantProblem(t, "a review over Jonah's budget", code, p, http.StatusConflict, "learning-budget-exhausted")
}

// jonahRun records a finished review run of acme that cost usd, started an hour ago.
func (h *harness) jonahRun(t *testing.T, usd float64) {
	t.Helper()

	h.kindRun(t, domain.RunKindJonah, usd)
}

// kindRun records a finished run of kind of acme that cost usd, started an hour ago.
func (h *harness) kindRun(t *testing.T, kind domain.RunKind, usd float64) {
	t.Helper()

	ctx := inOrg(t)
	at := h.clock.Now().Add(-time.Hour)

	it, err := h.store.CreateItem(ctx, domain.NewItem{FactoryID: acmeID, Title: "Review", Station: stInbox, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	id, err := h.store.StartRun(ctx, ports.NewRun{
		FactoryID: acmeID, DefinitionVersionID: h.acmeRecord(t).Current.ID, ItemID: it.ID, Station: stInbox, Agent: writerAgent,
		StartedAt: at, Kind: kind,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestJonah_Permissions(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := h.proposeOne(t, proposalFiles())

	// An API key that may read and edit acme but not publish.
	h.as(domain.Principal{
		Kind: domain.PrincipalService, Key: "019a0000-0000-7000-8000-0000000000ed", Name: "ci", Org: testOrg,
		Scopes: []domain.Permission{permFactoryRead, permDefinitionEdit}, KeyCeiling: domain.FactoryEditor,
	})

	code, p := problemOfCall(t, h, http.MethodPost, path+"/approve", "")
	wantProblem(t, "approve without definition:publish", code, p, http.StatusForbidden, "forbidden")

	if got := h.getProposal(t, path); got.State != gen.ProposalStateProposed {
		t.Fatalf("the key could read the proposal as %s, want proposed", got.State)
	}

	h.as(acmeMember(domain.FactoryViewer))

	for _, c := range []struct{ path, body string }{
		{path + "/approve", ""},
		{path + "/edit", ""},
		{path + "/reject", rejectBody},
		{path + "/undo", ""},
		{acmeJonah + "/reviews", ""},
		{acmeJonah + "/tests", keepBody(1, "inbox")},
	} {
		code, p := problemOfCall(t, h, http.MethodPost, c.path, c.body)
		wantProblem(t, "a viewer's POST "+c.path, code, p, http.StatusForbidden, "forbidden")
	}

	code, p = problemOfCall(t, h, http.MethodDelete, acmeJonah+"/tests/019a0000-0000-7000-8000-00000000dead", "")
	wantProblem(t, "a viewer's DELETE of a kept test", code, p, http.StatusForbidden, "forbidden")

	for _, read := range []string{acmeJonah, acmeProposals, path, acmeJonah + "/tests"} {
		if code := h.call(t, http.MethodGet, read, "", nil); code != http.StatusOK {
			t.Errorf("a viewer's GET %s = %d, want 200", read, code)
		}
	}

	if got := h.getProposal(t, path); got.State != gen.ProposalStateProposed {
		t.Errorf("after refusals the proposal is %s, want proposed", got.State)
	}
}

func TestJonah_Reads(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.submit(t)

	files := proposalFiles()
	stored := h.review(t, []domain.NewProposal{
		{
			Title: "Ask for a screenshot", Summary: "Four UI changes came back without one.", Edits: files,
			Evidence: []domain.ProposalEvidence{{ItemID: domain.ItemID(it.Id), Station: reviewStation, Quote: "No screenshot."}},
			Estimate: domain.ProposalEstimate{Metric: domain.MetricSentBackRate, Station: reviewStation, Before: 0.4, After: 0.2, CostPerItemUSD: 0.05},
		},
		{Title: "Tighten the rule", Edits: files[1:]},
	},
		domain.NewObservation{Summary: "UI changes lack screenshots", Detail: "4 of 6", ItemIDs: []domain.ItemID{domain.ItemID(it.Id)}, Count: 4, Proposal: 1},
		domain.NewObservation{Summary: "Tests skipped", Count: 2},
	)

	if _, err := h.store.DecideProposal(inOrg(t), acmeID, stored[1].Number, domain.ProposalDecision{
		State: domain.ProposalRejected, RejectReason: "Not now.", By: domain.LocalUser, At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	h.jonahRun(t, 1.5)
	h.kindRun(t, domain.RunKindStation, 200)

	h.wantOverview(t, stored[0].Number, it.Ref)
	h.wantProposalPages(t, stored)
	h.wantProposal(t, stored[0].Number, files, it)
}

// wantOverview checks the overview of TestJonah_Reads: proposal became the first observation,
// which rests on ref.
func (h *harness) wantOverview(t *testing.T, proposal int64, ref string) {
	t.Helper()

	var ov gen.JonahOverview
	if code := h.call(t, http.MethodGet, acmeJonah, "", &ov); code != http.StatusOK {
		t.Fatalf("overview = %d, want 200", code)
	}

	// The line spent $200 in the last 7 days: 5% of it is Jonah's $10.
	wantPolicy := gen.JonahPolicy{Enabled: true, ProposeAfter: 3, Share: 0.05, BudgetUsd: 10, ApproverRoles: []string{roleOwner, "admin", "editor"}}
	if !reflect.DeepEqual(ov.Policy, wantPolicy) {
		t.Errorf("policy = %+v, want %+v", ov.Policy, wantPolicy)
	}

	if got := fmt.Sprintf("%d %g %s", ov.Waiting, ov.MonthCostUsd, val(ov.LastReview).State); got != "1 1.5 done" {
		t.Errorf("waiting, month's cost and last review = %s, want 1 1.5 done", got)
	}

	type seen struct {
		Summary, State string
		Count, Of      int
		Proposal       int64
		Items          string
	}

	got := []seen{}
	for _, o := range ov.Observations {
		got = append(got, seen{o.Summary, string(o.State), o.Count, o.ProposeAfter, val(o.Proposal), strings.Join(o.ItemRefs, ",")})
	}

	slices.SortFunc(got, func(a, b seen) int { return strings.Compare(a.State, b.State) })

	want := []seen{{"UI changes lack screenshots", "proposed", 4, 3, proposal, ref}, {"Tests skipped", "watching", 2, 3, 0, ""}}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("observations = %+v, want %+v", got, want)
	}
}

// proposalPage lists acme's proposals with query and returns the page.
func (h *harness) proposalPage(t *testing.T, query string) gen.ProposalPage {
	t.Helper()

	var page gen.ProposalPage
	if code := h.call(t, http.MethodGet, acmeProposals+"?"+query, "", &page); code != http.StatusOK {
		t.Fatalf("list ?%s = %d, want 200", query, code)
	}

	return page
}

// numbers are page's proposal numbers and its next, 0 when it has none.
func numbers(page gen.ProposalPage) string {
	out := []string{}
	for _, p := range page.Proposals {
		out = append(out, strconv.FormatInt(p.Number, 10)+":"+string(p.State))
	}

	return fmt.Sprint(out, " next ", val(page.Next))
}

// wantProposalPages checks the list of TestJonah_Reads' stored proposals: the second is rejected.
func (h *harness) wantProposalPages(t *testing.T, stored []domain.Proposal) {
	t.Helper()

	// Newest first, one page at a time.
	first := h.proposalPage(t, "limit=1")
	if got, want := numbers(first), fmt.Sprintf("[%d:rejected] next %d", stored[1].Number, stored[1].Number); got != want {
		t.Fatalf("first page = %s, want %s", got, want)
	}

	second := h.proposalPage(t, "limit=1&after="+strconv.FormatInt(val(first.Next), 10))
	if got, want := numbers(second), fmt.Sprintf("[%d:proposed] next 0", stored[0].Number); got != want {
		t.Fatalf("second page = %s, want %s", got, want)
	}

	e := second.Proposals[0]
	got := fmt.Sprint(e.Paths, e.Agents, e.EvidenceCount, val(e.Estimate))
	want := fmt.Sprint([]string{writerFile, rulePath}, []string{writerAgent}, 1,
		gen.ProposalEstimate{Metric: gen.ProposalEstimateMetricSentBackRate, Station: reviewStation, Before: 0.4, After: 0.2, CostPerItemUsd: 0.05})

	if got != want {
		t.Errorf("entry = %s, want %s", got, want)
	}

	if got, want := numbers(h.proposalPage(t, "state=rejected")), fmt.Sprintf("[%d:rejected] next 0", stored[1].Number); got != want {
		t.Errorf("rejected = %s, want %s", got, want)
	}
}

// wantProposal checks proposal number of TestJonah_Reads, which edits files and rests on it.
func (h *harness) wantProposal(t *testing.T, number int64, files []domain.ProposalEdit, it gen.Item) {
	t.Helper()

	p := h.getProposal(t, acmeProposals+"/"+strconv.FormatInt(number, 10))
	if got := fmt.Sprint(p.MadeAgainstVersion, p.PublishesAsVersion, p.Summary != ""); got != "1 2 true" {
		t.Errorf("versions and summary = %s, want 1 2 true", got)
	}

	wantFiles := []gen.ProposalFile{
		{Path: writerFile, BaseSha256: string(domain.HashBlob([]byte(writerPrompt))), BaseContent: writerPrompt, Content: writerNew},
		{Path: rulePath, Content: ruleNew},
	}
	if !reflect.DeepEqual(p.Files, wantFiles) {
		t.Errorf("files = %+v, want %+v", p.Files, wantFiles)
	}

	wantEdits := []gen.ProposalEdit{
		{Path: writerFile, Replacements: &[]gen.Replacement{{Old: files[0].Replacements[0].Old, New: files[0].Replacements[0].New}}},
		{Path: rulePath, Content: &files[1].Content},
	}
	if !reflect.DeepEqual(p.Edits, wantEdits) {
		t.Errorf("edits = %+v, want %+v", p.Edits, wantEdits)
	}

	station := reviewStation

	wantEvidence := []gen.ProposalEvidence{{ItemRef: it.Ref, ItemTitle: it.Title, Station: &station, Quote: "No screenshot."}}
	if !reflect.DeepEqual(p.Evidence, wantEvidence) {
		t.Errorf("evidence = %+v, want %+v", p.Evidence, wantEvidence)
	}

	code, pr := problemOfCall(t, h, http.MethodGet, acmeProposals+"/99", "")
	wantProblem(t, "an unknown proposal", code, pr, http.StatusNotFound, "proposal-not-found")
}

func TestJonah_Unavailable(t *testing.T) {
	t.Parallel()

	h := newHarnessLogged(t, nil, nil, func(_ *harness, d *api.Deps) { d.Jonah = nil })

	code, p := problemOfCall(t, h, http.MethodGet, acmeJonah, "")
	wantProblem(t, "overview without a Jonah store", code, p, http.StatusServiceUnavailable, "jonah-unavailable")
}

// lineModels is the model-proposal route of line's agents.
const lineModels = "/v1/orgs/acme/factories/line/agents/"

// modelLines is jonah.ModelDefinitions over the definitions cache, with the harness's modelLine
// change, when a test sets one, applied to each line it reads.
type modelLines struct {
	*definitions.Cache
	h *harness
}

func (m modelLines) Line(ctx context.Context, id domain.DefinitionVersionID) (domain.Line, error) {
	l, err := m.Cache.Line(ctx, id)
	if err == nil && m.h.modelLine != nil {
		l.Agents = slices.Clone(l.Agents)
		m.h.modelLine(&l)
	}

	return l, err
}

// doneLineItem is a done item of line that backend built at build an hour ago, in a claim's first
// run that cost usd, sent back once at merge; the line spent lineUSD more on another item.
func (h *harness) doneLineItem(t *testing.T, usd, lineUSD float64) domain.Item {
	t.Helper()

	ctx := inOrg(t)
	at := h.clock.Now().Add(-time.Hour)
	it := h.seed(t, stBuild, domain.StateQueued, at)
	version := h.lineRecord(t).Current.ID

	for i, cost := range []float64{usd, lineUSD} {
		item := it
		if i == 1 {
			item = h.seed(t, stBuild, domain.StateQueued, at)
		}

		id, err := h.store.StartRun(ctx, ports.NewRun{
			FactoryID: lineID, DefinitionVersionID: version, ItemID: item.ID, Station: stBuild, Agent: bakeOffAgent, StartedAt: at,
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := h.store.RecordRunInput(ctx, id, domain.RunInput{Brief: "the brief", BranchHead: strings.Repeat("c", 40)}); err != nil {
			t.Fatal(err)
		}

		if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: cost}, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	if err := h.store.AppendEvents(ctx, domain.Event{
		FactoryID: lineID, ItemID: it.ID, At: at.Add(2 * time.Minute), Kind: domain.EventRejected, Station: "merge", Actor: "user:ada",
	}); err != nil {
		t.Fatal(err)
	}

	return h.move(t, it, domain.Position{Station: stDone, State: domain.StateDone}, func(tr *ports.Transition) { tr.DoneAt = h.clock.Now() })
}

// lineRecord is line's record.
func (h *harness) lineRecord(t *testing.T) ports.FactoryRecord {
	t.Helper()

	rec, err := h.defs.FactoryRecord(inOrg(t), lineID)
	if err != nil {
		t.Fatal(err)
	}

	return rec
}

func TestJonah_ModelProposalEstimate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.doneLineItem(t, 2, 998) // the line spent $1,000: Jonah's budget is $50

	var est gen.ModelProposalResult
	if code := h.call(t, http.MethodPost, lineModels+bakeOffAgent+"/model-proposal", `{"model":"`+modelOpus+`"}`, &est); code != http.StatusOK {
		t.Fatalf("estimate = %d, want 200", code)
	}

	if est.Proposal != nil {
		t.Errorf("an estimate answered a proposal: %+v", est.Proposal)
	}

	wantModelEstimate(t, est.Estimate, it)

	if page := h.proposalPageOf(t, "/v1/orgs/acme/factories/line/jonah/proposals"); len(page.Proposals) != 0 {
		t.Errorf("an estimate stored %d proposals, want none", len(page.Proposals))
	}

	var made gen.ModelProposalResult
	if code := h.call(t, http.MethodPost, lineModels+bakeOffAgent+"/model-proposal", `{"model":"`+modelOpus+`","confirm":true}`, &made); code != http.StatusCreated {
		t.Fatalf("confirm = %d, want 201", code)
	}

	if p := made.Proposal; p == nil || p.State != gen.ProposalStateProposed || len(p.Edits) != 1 || p.Edits[0].Path != yamlFile ||
		len(p.Evidence) != 1 || made.Estimate.Tries != 3 {
		t.Fatalf("confirm = %+v, want a proposed proposal of factory.yaml with its evidence and 3 tries", made)
	}

	if n := h.audited(t, domain.AuditProposalModel); n != 1 {
		t.Errorf("%d %s entries, want 1", n, domain.AuditProposalModel)
	}
}

// wantModelEstimate fails t unless e is backend's from bakeOffNow to modelOpus on it alone: sent
// back once at merge, $2, so 3 tries at $6 against the $50 of Jonah's budget.
func wantModelEstimate(t *testing.T, e gen.ModelProposalEstimate, it domain.Item) {
	t.Helper()

	if e.Agent != bakeOffAgent || e.CurrentModel != bakeOffNow || e.Model != modelOpus || len(e.Items) != 1 {
		t.Fatalf("estimate = %+v, want one item of backend from %s to %s", e, bakeOffNow, modelOpus)
	}

	want := gen.ModelProposalItem{
		ItemRef: fmt.Sprintf("line#%d", it.Number), ItemTitle: it.Title, Station: stBuild, RunId: e.Items[0].RunId, SentBack: 1,
		LineResult: "sent back once at merge, $2.00", CostUsd: 2, RunCostUsd: 2,
	}
	if e.Items[0] != want || want.RunId == "" {
		t.Errorf("item = %+v, want %+v", e.Items[0], want)
	}

	if e.Tries != 3 || e.EstimateUsd != 6 || e.BudgetLeftUsd != 50 {
		t.Errorf("estimate %d tries, $%v, $%v left; want 3, $6 and $50", e.Tries, e.EstimateUsd, e.BudgetLeftUsd)
	}
}

// proposalPageOf reads the first page of proposals at path.
func (h *harness) proposalPageOf(t *testing.T, path string) gen.ProposalPage {
	t.Helper()

	var page gen.ProposalPage
	if code := h.call(t, http.MethodGet, path, "", &page); code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, code)
	}

	return page
}

func TestJonah_ModelProposalRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.doneLineItem(t, 100, 900) // $300 of tries against a $50 budget

	for _, c := range []struct {
		what, agent, model string
		status             int
		code               string
	}{
		{"a model outside the policy", bakeOffAgent, bakeOffNow, http.StatusUnprocessableEntity, "model-not-allowed"},
		{"the agent's own model", agentFrontend, modelHaiku, http.StatusUnprocessableEntity, "model-not-allowed"},
		{"an agent with no finished item", agentFrontend, modelOpus, http.StatusUnprocessableEntity, "no-finished-items"},
		{"an unknown agent", "nobody", modelOpus, http.StatusNotFound, "agent-not-found"},
	} {
		code, p := problemOfCall(t, h, http.MethodPost, lineModels+c.agent+"/model-proposal", `{"model":"`+c.model+`","confirm":true}`)
		wantProblem(t, c.what, code, p, c.status, c.code)
	}

	code, p := problemOfCall(t, h, http.MethodPost, lineModels+bakeOffAgent+"/model-proposal", `{"model":"`+modelOpus+`"}`)
	wantProblem(t, "tries over Jonah's budget", code, p, http.StatusConflict, "learning-budget-exhausted")

	if est := val(p.ModelEstimate); est.EstimateUsd != 300 || est.BudgetLeftUsd != 50 || len(est.Items) != 1 || val(p.Estimate).Usd != 300 {
		t.Errorf("refused estimate = %+v, want $300 of tries against $50 left", p)
	}

	h.modelLine = func(l *domain.Line) {
		for i := range l.Agents {
			l.Agents[i].Connections = []string{"repo"}
		}
	}

	code, p = problemOfCall(t, h, http.MethodPost, lineModels+bakeOffAgent+"/model-proposal", `{"model":"`+modelOpus+`"}`)
	wantProblem(t, "an agent that needs a connection", code, p, http.StatusUnprocessableEntity, "agent-not-triable")

	if c := h.request(t, http.MethodPost, factoriesPath, `{"name":"support","title":"Support","start":{"kind":"blank"}}`, nil); c.status != http.StatusCreated {
		t.Fatalf("create support = %d", c.status)
	}

	code, p = problemOfCall(t, h, http.MethodPost, factoriesPath+"/support/agents/writer/model-proposal", `{"model":"`+modelOpus+`"}`)
	wantProblem(t, "a factory never published", code, p, http.StatusNotFound, "version-not-found")

	h.as(acmeMember(domain.FactoryViewer))

	code, p = problemOfCall(t, h, http.MethodPost, acmeFactory+"/agents/"+writerAgent+"/model-proposal", `{"model":"`+modelOpus+`"}`)
	wantProblem(t, "a viewer's model proposal", code, p, http.StatusForbidden, "forbidden")
}

func TestJonah_ModelProposalUnavailable(t *testing.T) {
	t.Parallel()

	h := newHarnessLogged(t, nil, nil, func(_ *harness, d *api.Deps) { d.Jonah.Models = nil })

	code, p := problemOfCall(t, h, http.MethodPost, lineModels+bakeOffAgent+"/model-proposal", `{"model":"`+modelOpus+`"}`)
	wantProblem(t, "a model proposal without Try another model", code, p, http.StatusServiceUnavailable, "jonah-unavailable")
}

func TestJonah_ProposalTried(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.doneLineItem(t, 2, 998) // the line spent $1,000: Jonah's budget is $50

	var made gen.ModelProposalResult
	if code := h.call(t, http.MethodPost, lineModels+bakeOffAgent+"/model-proposal", `{"model":"`+modelOpus+`","confirm":true}`, &made); code != http.StatusCreated {
		t.Fatalf("confirm = %d, want 201", code)
	}

	ctx := inOrg(t)

	claimed, err := h.store.ClaimPastTry(ctx, lineID, h.clock.Now(), time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}

	h.clock.Advance(90 * time.Second)

	if _, err := h.store.FinishPastTry(ctx, claimed, domain.PastTryResult{
		Verdict: domain.PastTryFixed, Reason: "The tests now cover the merge.", CostUSD: 1.25, At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	tried := h.getProposal(t, fmt.Sprintf("/v1/orgs/acme/factories/line/jonah/proposals/%d", made.Proposal.Number)).Tried
	if tried == nil || tried.State != gen.ProposalTriedStateTrying || tried.CostUsd != 1.25 || tried.Seconds != nil ||
		len(tried.KeptTests) != 0 || tried.Grader != nil || len(tried.Items) != 1 {
		t.Fatalf("tried = %+v, want one item trying at $1.25, no time, kept tests or grader yet", tried)
	}

	wantTriedItem(t, tried.Items[0], it)

	stored := h.review(t, []domain.NewProposal{{Title: "Tighten the rule", Edits: proposalFiles()}})
	if p := h.getProposal(t, fmt.Sprintf("%s/%d", acmeProposals, stored[0].Number)); p.Tried != nil {
		t.Errorf("a proposal with no tries has tried %+v", p.Tried)
	}
}

// wantTriedItem fails t unless item is it at the build station with its line result and three
// tries: the first done and fixed with its reason, the other two queued.
func wantTriedItem(t *testing.T, item gen.TriedItem, it domain.Item) {
	t.Helper()

	if item.ItemRef != fmt.Sprintf("line#%d", it.Number) || item.ItemTitle != it.Title || item.Station != stBuild ||
		val(item.LineResult) != "sent back once at merge, $2.00" || len(item.Tries) != 3 {
		t.Fatalf("item = %+v, want %s at %s with its line result and 3 tries", item, it.Title, stBuild)
	}

	want := []gen.TriedTry{
		{Attempt: 1, State: gen.TriedTryStateDone, Verdict: new(gen.TriedTryVerdictFixed), Reason: new("The tests now cover the merge."), CostUsd: 1.25},
		{Attempt: 2, State: gen.TriedTryStateQueued},
		{Attempt: 3, State: gen.TriedTryStateQueued},
	}
	if !reflect.DeepEqual(item.Tries, want) {
		t.Errorf("tries = %+v, want %+v", item.Tries, want)
	}
}
