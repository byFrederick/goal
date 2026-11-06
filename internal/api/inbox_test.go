package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// blockedBy appends the event that blocked an item, with its reason ("" for none).
func (h *harness) blockedBy(t *testing.T, it domain.Item, kind domain.EventKind, reason string) {
	t.Helper()

	data := map[string]any{}
	if reason != "" {
		data["reason"] = reason
	}

	h.blockedWith(t, it, kind, data)
}

// blockedWith appends the event of kind that blocked an item, with data.
func (h *harness) blockedWith(t *testing.T, it domain.Item, kind domain.EventKind, data map[string]any) {
	t.Helper()

	if err := h.store.AppendEvents(inOrg(t), domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: t0, Kind: kind, Station: it.Station, Actor: domain.ActorEngine, Data: data,
	}); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) inbox(t *testing.T, query string) gen.Inbox {
	t.Helper()

	return decode[gen.Inbox](t, h.request(t, http.MethodGet, "/v1/orgs/acme/inbox"+query, "", nil), http.StatusOK)
}

// inboxItems seeds one item of each kind of the line factory: a decision with a brief, a question,
// a blocked item, one blocked with a code, a paused one and an expedite one.
type inboxItems struct {
	decision, question, toolApproval, blocked, coded, paused, expedite domain.Item
}

func (h *harness) seedInboxItems(t *testing.T) inboxItems {
	t.Helper()

	var s inboxItems

	s.decision = h.move(t, h.seed(t, stBuild, domain.StateInProgress, t0.Add(-3*time.Hour)),
		domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
			tr.OutputStation, tr.Output = stBuild, json.RawMessage(`{"summary": "Adds the flag.", "risk": "Low",
				"pull_request": "https://github.com/acme/app/pull/3", "tests": 12}`)
		})
	s.question = h.asked(t, "Organization", "Factory")
	s.toolApproval = h.toolAsked(t, true)
	s.blocked = h.seed(t, stBuild, domain.StateBlocked, t0.Add(-2*time.Hour))
	h.blockedBy(t, s.blocked, domain.EventBlocked, "checks failed")
	s.coded = h.seed(t, stBuild, domain.StateBlocked, t0.Add(-90*time.Minute))
	h.blockedWith(t, s.coded, domain.EventBlocked, map[string]any{
		"reason": "version 1 names no runtime.modelKey", "code": domain.BlockModelKeyMissing,
	})
	s.paused = h.seed(t, stBuild, domain.StateBlocked, t0.Add(-time.Hour))
	h.blockedBy(t, s.paused, domain.EventBlocked, "checks failed")
	h.blockedBy(t, s.paused, domain.EventPaused, "")

	var err error

	s.expedite, err = h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: lineID, Type: typeFeature, Title: "hotfix", Station: stBuild, State: domain.StateQueued,
		Class: domain.ClassExpedite, CreatedAt: t0.Add(-30 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// The inbox has one entry per thing that waits on a person, of every kind E knows, in kind order.
func TestAPI_InboxEntriesOfEveryKind(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishReview(t)
	h.andon(t, "review")                                                                         // a read by name loads the new factory, as a definition notice does in goald
	h.verdicts(t, t0.Add(-time.Hour), map[bool][]string{true: {"no evidence"}, false: {"", ""}}) // 1 of 3: one more reaches 2 of 5

	seeded := h.seedInboxItems(t)
	h.pullAndon(t) // acme's line stops

	got := h.inbox(t, "")

	kinds := []string{}
	for _, e := range got.Entries {
		kinds = append(kinds, string(e.Kind)+" "+e.Factory.Name)
	}

	want := []string{"decision line", "question line", "tool_approval line", "andon acme", "rework_warning review", "blocked line", "blocked line", "blocked line", "expedite line"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("entries = %v; want %v", kinds, want)
	}

	checkItemEntries(t, got, seeded)
	checkLineEntries(t, got)

	// A stopped line warns no more: its pull is the entry.
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/factories/review/andon", `{"reason":"stop"}`, nil); code != http.StatusCreated {
		t.Fatalf("pull review = %d", code)
	}

	if got := h.inbox(t, "?factory=review"); len(got.Entries) != 1 || got.Entries[0].Kind != gen.InboxEntryKindAndon {
		t.Errorf("review's inbox while stopped = %+v; want its andon only", got.Entries)
	}
}

// entryOf finds the entry of an item.
func entryOf(in gen.Inbox, item domain.ItemID) gen.InboxEntry {
	for _, e := range in.Entries {
		if e.Item != nil && e.Item.Id == string(item) {
			return e
		}
	}

	return gen.InboxEntry{}
}

func checkItemEntries(t *testing.T, got gen.Inbox, seeded inboxItems) {
	t.Helper()

	brief := decode[gen.ApprovalBrief](t, response{status: http.StatusOK, body: []byte(`{"summary": "Adds the flag.",
		"facts": [{"key": "risk", "value": "Low"}, {"key": "tests", "value": "12"}],
		"pull_request": "https://github.com/acme/app/pull/3", "spend_usd": 0}`)}, http.StatusOK)
	if got := entryOf(got, seeded.decision.ID).Approval; got == nil || !reflect.DeepEqual(*got, brief) {
		t.Errorf("decision brief = %+v; want %+v", got, brief)
	}

	checkQuestionEntry(t, entryOf(got, seeded.question.ID), seeded.question)
	checkToolApprovalEntry(t, entryOf(got, seeded.toolApproval.ID), seeded.toolApproval)

	if b := entryOf(got, seeded.blocked.ID).Blocked; b == nil || deref(b.Reason) != "checks failed" || b.Code != nil || b.Paused {
		t.Errorf("blocked entry = %+v; want its failure without a code", b)
	}

	if b := entryOf(got, seeded.coded.ID).Blocked; b == nil || deref(b.Code) != domain.BlockModelKeyMissing || deref(b.Reason) == "" {
		t.Errorf("coded blocked entry = %+v; want code %s with its reason", b, domain.BlockModelKeyMissing)
	}

	if b := entryOf(got, seeded.paused.ID).Blocked; b == nil || b.Reason != nil || b.Code != nil || !b.Paused {
		t.Errorf("paused entry = %+v; want paused without a reason or code", b)
	}

	if e := entryOf(got, seeded.expedite.ID); e.Kind != gen.InboxEntryKindExpedite || !e.Since.Equal(seeded.expedite.CreatedAt) {
		t.Errorf("expedite entry = %+v; want the hotfix since it was created", e)
	}
}

// checkQuestionEntry checks the entry of an item whose agent asked: its question, without a brief.
func checkQuestionEntry(t *testing.T, e gen.InboxEntry, it domain.Item) {
	t.Helper()

	if e.Kind != gen.InboxEntryKindQuestion || e.Id != "question:"+string(it.ID) || e.Question == nil ||
		e.Question.Text != question || e.Question.Agent != agBackend || e.Approval != nil || e.Action != nil {
		t.Errorf("question entry = %+v; want the open question without a brief", e)
	}
}

// checkToolApprovalEntry checks the entry of an item whose agent asked to call a tool: the call,
// by the agent's role name, without a brief or a question.
func checkToolApprovalEntry(t *testing.T, e gen.InboxEntry, it domain.Item) {
	t.Helper()

	if e.Kind != gen.InboxEntryKindToolApproval || e.Id != "tool_approval:"+string(it.ID) || e.ToolApproval == nil ||
		e.ToolApproval.Connection != approvalConnection || e.ToolApproval.Tool != approvalTool ||
		asJSON(t, e.ToolApproval.Input) != approvalInput || e.ToolApproval.Agent != agBackend || e.ToolApproval.RunId != askingRun ||
		!e.ToolApproval.AskedAt.Equal(e.Since) || e.Question != nil || e.Approval != nil || e.Action != nil {
		t.Errorf("tool approval entry = %+v; want the open call without a brief", e)
	}
}

func checkLineEntries(t *testing.T, got gen.Inbox) {
	t.Helper()

	for _, e := range got.Entries {
		switch e.Kind {
		case gen.InboxEntryKindWatchWorse: // TestInbox_WatchWorseOnlyForProposalsOwnMetric checks these
		case gen.InboxEntryKindAndon:
			if e.Andon == nil || e.Andon.Reason != andonReason || e.Item != nil {
				t.Errorf("andon entry = %+v; want acme's pull", e)
			}
		case gen.InboxEntryKindReworkWarning:
			want := gen.ReworkWarning{Station: stCheck, Rejected: 1, Verdicts: 3, Window: 5, Threshold: 0.4, TopReason: new("no evidence")}
			if e.Rework == nil || !reflect.DeepEqual(*e.Rework, want) {
				t.Errorf("rework warning = %+v; want %+v", e.Rework, want)
			}
		case gen.InboxEntryKindDecision, gen.InboxEntryKindQuestion, gen.InboxEntryKindToolApproval, gen.InboxEntryKindBlocked,
			gen.InboxEntryKindExpedite, gen.InboxEntryKindConversion, gen.InboxEntryKindKnowledgeSource,
			gen.InboxEntryKindCheckFailing, gen.InboxEntryKindCheckLimit: // TestInbox_CheckEntries checks these
		}
	}
}

// A factory Goal could not convert to secrets and environment files (ADR 0037 decision 19) is a
// conversion entry naming each problem, since the attempt; a converted factory has none.
func TestInbox_Conversion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	problems := []string{
		"connection aws-prod uses the aws kind, which Goal no longer converts: remove it from the factory",
		"connection docs does not exist in the organization: remove it from the factory",
	}

	for _, c := range []ports.FactoryConversion{
		{Factory: lineID, From: "0b6f1c9e-0000-7000-8000-000000000101", Problems: problems, At: t0.Add(-time.Hour)},
		{Factory: acmeID, From: "0b6f1c9e-0000-7000-8000-000000000102", To: "0b6f1c9e-0000-7000-8000-000000000103", At: t0},
	} {
		if _, err := h.conversions.Record(inOrg(t), c); err != nil {
			t.Fatal(err)
		}
	}

	var got []gen.InboxEntry

	for _, e := range h.inbox(t, "").Entries {
		if e.Kind == gen.InboxEntryKindConversion {
			got = append(got, e)
		}
	}

	if len(got) != 1 {
		t.Fatalf("conversion entries %+v; want the refused factory's only", got)
	}

	e := got[0]
	if e.Id != "conversion:"+lineID || e.Factory.Id != lineID || !e.Since.Equal(t0.Add(-time.Hour)) || e.Item != nil {
		t.Errorf("entry %+v; want conversion:%s since the attempt, about no item", e, lineID)
	}

	want := "Connection aws-prod uses the aws kind, which Goal no longer converts: remove it from the factory. " +
		"Connection docs does not exist in the organization: remove it from the factory."
	if e.Conversion == nil || e.Conversion.Reason != want || !reflect.DeepEqual(e.Conversion.Problems, problems) {
		t.Errorf("conversion %+v; want reason %q and each problem", e.Conversion, want)
	}
}

// Needs you shows a check trigger whose last three checks failed, and one whose check skipped
// something over the limit today, once.
func TestInbox_CheckEntries(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	day := h.clock.Now().UTC().Truncate(24 * time.Hour)

	h.check(t, day.Add(time.Hour), `{"file": [{"key": "a", "title": "a", "brief": "b"}, {"key": "b", "title": "b", "brief": "b"},
		{"key": "c", "title": "c", "brief": "b"}]}`, "")
	h.check(t, day.Add(2*time.Hour), `{"file": [{"key": "d", "title": "d", "brief": "b"}, {"key": "e", "title": "e", "brief": "b"},
		{"key": "f", "title": "f", "brief": "b"}]}`, "")

	got := h.checkEntries(t)
	expectLimitEntry(t, got[gen.InboxEntryKindCheckLimit], day)

	if len(got[gen.InboxEntryKindCheckFailing]) != 0 {
		t.Errorf("failing entries = %+v; want none yet", got[gen.InboxEntryKindCheckFailing])
	}

	for i := range 3 {
		h.check(t, day.Add(time.Duration(3+i)*time.Hour), "", "the check run ended failed: boom")
	}

	if f := h.checkEntries(t)[gen.InboxEntryKindCheckFailing]; len(f) != 1 || f[0].Id != "check_failing:"+acmeID+":"+incidentsName ||
		f[0].TriggerCheck.Reason != "the check run ended failed: boom" {
		t.Errorf("failing entries = %+v; want one with the newest reason", f)
	}
}

// checkEntries are the inbox's check entries by kind.
func (h *harness) checkEntries(t *testing.T) map[gen.InboxEntryKind][]gen.InboxEntry {
	t.Helper()

	got := map[gen.InboxEntryKind][]gen.InboxEntry{}

	for _, e := range h.inbox(t, "").Entries {
		if e.Kind == gen.InboxEntryKindCheckFailing || e.Kind == gen.InboxEntryKindCheckLimit {
			got[e.Kind] = append(got[e.Kind], e)
		}
	}

	return got
}

// expectLimitEntry checks that l is one check_limit entry for new-incidents on day, since its
// first check over the limit.
func expectLimitEntry(t *testing.T, l []gen.InboxEntry, day time.Time) {
	t.Helper()

	if len(l) != 1 || l[0].Id != "check_limit:"+acmeID+":"+incidentsName+":"+day.Format(time.DateOnly) ||
		l[0].TriggerCheck == nil || l[0].TriggerCheck.Trigger != incidentsName || !l[0].Since.Equal(day.Add(time.Hour+time.Minute)) {
		t.Errorf("limit entries = %+v; want one for today, since the first check over the limit", l)
	}
}
