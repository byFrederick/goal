package api_test

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// approvedWithWatch stores a proposal whose own number is the send-back rate at review, approves
// it as v2 and returns its path and the stored proposal.
func (h *harness) approvedWithWatch(t *testing.T) (string, domain.Proposal) {
	t.Helper()

	stored := h.review(t, []domain.NewProposal{{
		Title: "Ask for a screenshot", Files: proposalFiles(),
		Estimate: domain.ProposalEstimate{Metric: domain.MetricSentBackRate, Station: reviewStation, Before: 0.3, After: 0.1},
	}})
	path := acmeProposals + "/" + strconv.FormatInt(stored[0].Number, 10)

	if code := h.call(t, http.MethodPost, path+"/approve", "", nil); code != http.StatusOK {
		t.Fatalf("approve = %d, want 200", code)
	}

	p, err := h.store.Proposal(inOrg(t), acmeID, stored[0].Number)
	if err != nil {
		t.Fatal(err)
	}

	return path, p
}

// saveWatch stores a watch of v at station on metric with verdict, for proposal when set.
func (h *harness) saveWatch(t *testing.T, v domain.DefinitionVersionID, proposal domain.ProposalID, station string, metric domain.ProposalMetric, verdict domain.WatchVerdict) {
	t.Helper()

	now := h.clock.Now()
	if _, err := h.store.SaveWatch(inOrg(t), domain.Watch{
		FactoryID: acmeID, VersionID: v, ProposalID: proposal, Station: station, Metric: metric,
		Before: domain.WatchSide{N: 60, Value: 0.1}, After: domain.WatchSide{N: 60, Value: 0.35},
		Interval: &domain.WatchInterval{Low: 0.12, High: 0.38}, Verdict: verdict, OpenedAt: now.Add(-time.Hour), UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func worseEntries(in gen.Inbox) []gen.InboxEntry {
	var out []gen.InboxEntry

	for _, e := range in.Entries {
		if e.Kind == gen.InboxEntryKindWatchWorse {
			out = append(out, e)
		}
	}

	return out
}

// Only a proposal's own number that got worse comes to Needs you, about no item, with Undo
// this change; a person's publish that got worse does not, and the entry goes once an undo is
// written.
func TestInbox_WatchWorseOnlyForProposalsOwnMetric(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path, p := h.approvedWithWatch(t)

	h.saveWatch(t, p.PublishedVersionID, p.ID, reviewStation, domain.MetricSentBackRate, domain.WatchWorse)
	h.saveWatch(t, p.PublishedVersionID, "", stBuild, domain.MetricCostPerItem, domain.WatchWorse) // a person's publish's

	wantWorseEntry(t, worseEntries(h.inbox(t, "")), p)

	if got := h.getProposal(t, path); got.Watch == nil || got.Watch.Verdict != gen.Worse || got.Watch.Station != reviewStation {
		t.Errorf("the proposal's watch = %+v, want its own number, worse", got.Watch)
	}

	var undo gen.Proposal
	if code := h.call(t, http.MethodPost, path+"/undo", "", &undo); code != http.StatusCreated {
		t.Fatalf("undo = %d, want 201", code)
	}

	if val(undo.Undoes) != p.Number || undo.State != gen.ProposalStateProposed || len(undo.Files) != 1 || undo.Files[0].Content != writerPrompt {
		t.Errorf("undo = %+v, want a proposed undo of %d giving the writer's prompt back", undo, p.Number)
	}

	if worse := worseEntries(h.inbox(t, "")); len(worse) != 0 {
		t.Errorf("watch_worse entries after the undo = %+v, want none", worse)
	}

	code, prob := problemOfCall(t, h, http.MethodPost, path+"/undo", "")
	wantProblem(t, "a second undo", code, prob, http.StatusConflict, "undo-open")
}

// wantWorseEntry checks that worse is one watch_worse entry of p's own number at review.
func wantWorseEntry(t *testing.T, worse []gen.InboxEntry, p domain.Proposal) {
	t.Helper()

	if len(worse) != 1 {
		t.Fatalf("watch_worse entries = %+v, want the proposal's own number only", worse)
	}

	e := worse[0]
	if e.Item != nil || e.Id != "watch_worse:"+string(p.ID) || val(e.Station) != reviewStation || e.Watch == nil {
		t.Fatalf("entry = %+v, want proposal %s's watch at review about no item", e, p.ID)
	}

	if w := *e.Watch; val(w.Proposal) != p.Number || w.Metric != gen.WatchMetricSentBackRate || w.Before.Items != 60 ||
		w.After.Value != 0.35 || w.Interval == nil || w.Interval.Low != 0.12 || w.Verdict != gen.Worse {
		t.Errorf("entry's watch = %+v, want proposal %d's worse send-back rate with its sides and interval", w, p.Number)
	}
}

// Versions carry their watches, and Undo this change refuses a proposal never published.
func TestJonah_VersionWatchesAndUndoRefusal(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, p := h.approvedWithWatch(t)
	h.saveWatch(t, p.PublishedVersionID, "", stBuild, domain.MetricSentBackRate, domain.WatchTooEarly)

	page := decode[gen.VersionPage](t, h.request(t, http.MethodGet, "/v1/orgs/acme/factories/acme/versions", "", nil), http.StatusOK)

	i := slices.IndexFunc(page.Versions, func(v gen.Version) bool { return val(v.Number) == 2 })
	if i < 0 || page.Versions[i].Watches == nil || len(*page.Versions[i].Watches) != 1 || (*page.Versions[i].Watches)[0].Station != stBuild {
		t.Fatalf("versions = %+v, want v2 with its watch at build", page.Versions)
	}

	if v1 := page.Versions[slices.IndexFunc(page.Versions, func(v gen.Version) bool { return val(v.Number) == 1 })]; v1.Watches == nil || len(*v1.Watches) != 0 {
		t.Errorf("v1's watches = %v, want none", v1.Watches)
	}

	detail := decode[gen.VersionDetail](t, h.request(t, http.MethodGet, "/v1/orgs/acme/factories/acme/versions/2", "", nil), http.StatusOK)
	if detail.Watches == nil || len(*detail.Watches) != 1 {
		t.Errorf("v2's watches = %v, want one", detail.Watches)
	}

	fresh := h.proposeOne(t, proposalFiles())
	code, prob := problemOfCall(t, h, http.MethodPost, fresh+"/undo", "")
	wantProblem(t, "undo of a proposal never published", code, prob, http.StatusConflict, "proposal-not-published")
}
