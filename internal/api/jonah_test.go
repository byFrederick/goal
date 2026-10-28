package api_test

import (
	"net/http"
	"net/url"
	"strconv"
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
	acmeJonah     = "/v1/orgs/acme/factories/acme/jonah"
	acmeProposals = acmeJonah + "/proposals"
	rulePath      = "rules/evidence.md"
	writerNew     = "# writer\nAttach a screenshot.\n"
	ruleNew       = "Show evidence.\n"
)

var (
	_ api.JonahDecisions = (*jonah.Decider)(nil)
	_ jonah.Authoring    = (*api.JonahAuthoring)(nil)
	_ api.JonahReads     = ports.JonahStore(nil)
)

// val is *p, or T's zero value for nil.
func val[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}

// proposalFiles edit acme's writer prompt as v1 has it and add a rule.
func proposalFiles() []domain.ProposalFile {
	return []domain.ProposalFile{
		{Path: writerFile, BaseSHA256: string(domain.HashBlob([]byte(writerPrompt))), Content: writerNew},
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
func (h *harness) proposeOne(t *testing.T, files []domain.ProposalFile) string {
	t.Helper()

	p := h.review(t, []domain.NewProposal{{Title: "Ask for a screenshot", Files: files}})

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

	t.Run("changed base", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		files := proposalFiles()
		files[0].BaseSHA256 = string(domain.HashBlob([]byte("# writer, as it was once\n")))
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

	// The draft's copy of the writer's prompt changed: a second proposal of it conflicts.
	second := h.proposeOne(t, proposalFiles()[:1])

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

	for _, c := range []struct{ verb, body string }{{"approve", ""}, {"edit", ""}, {"reject", `{"reason":"again"}`}} {
		code, p := problemOfCall(t, h, http.MethodPost, path+"/"+c.verb, c.body)
		wantProblem(t, c.verb+" of a decided proposal", code, p, http.StatusConflict, "proposal-decided")
	}

	code, p := problemOfCall(t, h, http.MethodPost, acmeProposals+"/99/reject", `{"reason":"no"}`)
	wantProblem(t, "reject of an unknown proposal", code, p, http.StatusNotFound, "proposal-not-found")
}

func TestJonah_QueueReview(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var got gen.JonahReview
	if code := h.call(t, http.MethodPost, acmeJonah+"/reviews", "", &got); code != http.StatusAccepted {
		t.Fatalf("queue review = %d, want 202", code)
	}

	if got.State != gen.JonahReviewStatePending || got.Trigger != gen.JonahReviewTriggerManual {
		t.Errorf("review = %+v, want pending and manual", got)
	}

	if n := h.audited(t, domain.AuditReviewQueued); n != 1 {
		t.Errorf("%d %s entries, want 1", n, domain.AuditReviewQueued)
	}

	code, p := problemOfCall(t, h, http.MethodPost, acmeJonah+"/reviews", "")
	wantProblem(t, "a second review", code, p, http.StatusConflict, "review-in-flight")

	// A factory whose review runs cost the weekly budget (default $10) over the last 7 days.
	h = newHarness(t)
	h.jonahRun(t, 10)

	code, p = problemOfCall(t, h, http.MethodPost, acmeJonah+"/reviews", "")
	wantProblem(t, "a review over budget", code, p, http.StatusConflict, "learning-budget-exhausted")
}

// jonahRun records a finished review run of acme that cost usd, started an hour ago.
func (h *harness) jonahRun(t *testing.T, usd float64) {
	t.Helper()

	ctx := inOrg(t)
	at := h.clock.Now().Add(-time.Hour)

	it, err := h.store.CreateItem(ctx, domain.NewItem{FactoryID: acmeID, Title: "Review", Station: stInbox, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	id, err := h.store.StartRun(ctx, ports.NewRun{
		FactoryID: acmeID, DefinitionVersionID: h.acmeRecord(t).Current.ID, ItemID: it.ID, Station: stInbox, Agent: writerAgent,
		StartedAt: at, Kind: domain.RunKindJonah,
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
		Scopes: []domain.Permission{"factory:read", "definition:edit"}, KeyCeiling: domain.FactoryEditor,
	})

	code, p := problemOfCall(t, h, http.MethodPost, path+"/approve", "")
	wantProblem(t, "approve without definition:publish", code, p, http.StatusForbidden, "forbidden")

	if got := h.getProposal(t, path); got.State != gen.ProposalStateProposed {
		t.Fatalf("the key could read the proposal as %s, want proposed", got.State)
	}

	h.as(acmeMember(domain.FactoryViewer))

	for _, c := range []struct{ path, body string }{
		{path + "/approve", ""}, {path + "/edit", ""}, {path + "/reject", `{"reason":"no"}`}, {acmeJonah + "/reviews", ""},
	} {
		code, p := problemOfCall(t, h, http.MethodPost, c.path, c.body)
		wantProblem(t, "a viewer's POST "+c.path, code, p, http.StatusForbidden, "forbidden")
	}

	for _, read := range []string{acmeJonah, acmeProposals, path} {
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
			Title: "Ask for a screenshot", Summary: "Four UI changes came back without one.", Files: files,
			Evidence: []domain.ProposalEvidence{{ItemID: domain.ItemID(it.Id), Station: "review", Quote: "No screenshot."}},
			Estimate: domain.ProposalEstimate{Metric: domain.MetricSentBackRate, Station: "review", Before: 0.4, After: 0.2, CostPerItemUSD: 0.05},
		},
		{Title: "Tighten the rule", Files: files[1:]},
	},
		domain.NewObservation{Summary: "UI changes lack screenshots", Detail: "4 of 6", ItemIDs: []domain.ItemID{domain.ItemID(it.Id)}, Count: 4, Proposal: 1},
		domain.NewObservation{Summary: "Tests skipped", Count: 3},
	)

	if _, err := h.store.DecideProposal(inOrg(t), acmeID, stored[1].Number, domain.ProposalDecision{
		State: domain.ProposalRejected, RejectReason: "Not now.", By: domain.LocalUser, At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	h.jonahRun(t, 1.5)

	var ov gen.JonahOverview
	if code := h.call(t, http.MethodGet, acmeJonah, "", &ov); code != http.StatusOK {
		t.Fatalf("overview = %d, want 200", code)
	}

	if ov.Policy.ProposeAfter != 5 || ov.Policy.WeeklyUsd != 10 || !ov.Policy.Enabled || strings.Join(ov.Policy.ApproverRoles, ",") != "owner,admin,editor" {
		t.Errorf("policy = %+v, want the defaults approved by owner, admin and editor", ov.Policy)
	}

	if ov.Waiting != 1 || ov.MonthCostUsd != 1.5 || ov.LastReview == nil || ov.LastReview.State != gen.JonahReviewStateDone {
		t.Errorf("overview = %+v, want 1 waiting, $1.50 this month and the last review done", ov)
	}

	if len(ov.Observations) != 2 {
		t.Fatalf("observations = %+v, want 2", ov.Observations)
	}

	for _, o := range ov.Observations {
		switch o.Summary {
		case "UI changes lack screenshots":
			if o.State != "proposed" || val(o.Proposal) != stored[0].Number || len(o.ItemRefs) != 1 || o.ItemRefs[0] != it.Ref || o.Count != 4 || o.ProposeAfter != 5 {
				t.Errorf("proposed observation = %+v, want proposal %d resting on %s", o, stored[0].Number, it.Ref)
			}
		default:
			if o.State != "watching" || o.Proposal != nil || o.Count != 3 {
				t.Errorf("watched observation = %+v, want watching 3 of 5", o)
			}
		}
	}

	// The list: newest first, by state, one page at a time.
	var page gen.ProposalPage
	if code := h.call(t, http.MethodGet, acmeProposals+"?limit=1", "", &page); code != http.StatusOK {
		t.Fatalf("list = %d, want 200", code)
	}

	if len(page.Proposals) != 1 || page.Proposals[0].Number != stored[1].Number || page.Next == nil {
		t.Fatalf("first page = %+v, want proposal %d and a next page", page, stored[1].Number)
	}

	after := *page.Next
	page = gen.ProposalPage{}

	if code := h.call(t, http.MethodGet, acmeProposals+"?limit=1&after="+strconv.FormatInt(after, 10), "", &page); code != http.StatusOK {
		t.Fatalf("second page = %d, want 200", code)
	}

	if len(page.Proposals) != 1 || page.Next != nil {
		t.Fatalf("second page = %+v, want the last proposal and no next", page)
	}

	e := page.Proposals[0]
	if e.Number != stored[0].Number || strings.Join(e.Paths, ",") != writerFile+","+rulePath || strings.Join(e.Agents, ",") != writerAgent ||
		e.EvidenceCount != 1 || e.Estimate == nil || e.Estimate.Before != 0.4 || e.Estimate.CostPerItemUsd != 0.05 {
		t.Errorf("entry = %+v, want its paths, the writer, 1 piece of evidence and its estimate", e)
	}

	page = gen.ProposalPage{}

	if code := h.call(t, http.MethodGet, acmeProposals+"?state=rejected", "", &page); code != http.StatusOK || len(page.Proposals) != 1 ||
		page.Proposals[0].State != gen.ProposalStateRejected {
		t.Errorf("rejected = %d %+v, want the one rejected", code, page)
	}

	// The proposal: base and new content, evidence and versions.
	p := h.getProposal(t, acmeProposals+"/"+strconv.FormatInt(stored[0].Number, 10))
	if p.MadeAgainstVersion != 1 || p.PublishesAsVersion != 2 || len(p.Files) != 2 || p.Summary == "" {
		t.Fatalf("proposal = %+v, want 2 files from v1 to v2", p)
	}

	if f := p.Files[0]; f.BaseContent != writerPrompt || f.Content != writerNew || f.BaseSha256 != files[0].BaseSHA256 {
		t.Errorf("edited file = %+v, want v1's prompt and the new one", f)
	}

	if f := p.Files[1]; f.BaseContent != "" || f.BaseSha256 != "" || f.Content != ruleNew {
		t.Errorf("new file = %+v, want no base", f)
	}

	if len(p.Evidence) != 1 || p.Evidence[0].ItemRef != it.Ref || p.Evidence[0].ItemTitle != it.Title || p.Evidence[0].Quote != "No screenshot." {
		t.Errorf("evidence = %+v, want %s with its title and quote", p.Evidence, it.Ref)
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
