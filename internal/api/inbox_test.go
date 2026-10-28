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
// a blocked item, a paused one and an expedite one.
type inboxItems struct {
	decision, question, blocked, paused, expedite domain.Item
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
	s.blocked = h.seed(t, stBuild, domain.StateBlocked, t0.Add(-2*time.Hour))
	h.blockedBy(t, s.blocked, domain.EventBlocked, "checks failed")
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

	want := []string{"decision line", "question line", "andon acme", "rework_warning review", "blocked line", "blocked line", "expedite line"}
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

	if b := entryOf(got, seeded.blocked.ID).Blocked; b == nil || deref(b.Reason) != "checks failed" || b.Paused {
		t.Errorf("blocked entry = %+v; want its failure", b)
	}

	if b := entryOf(got, seeded.paused.ID).Blocked; b == nil || b.Reason != nil || !b.Paused {
		t.Errorf("paused entry = %+v; want paused without a reason", b)
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

func checkLineEntries(t *testing.T, got gen.Inbox) {
	t.Helper()

	for _, e := range got.Entries {
		switch e.Kind {
		case gen.InboxEntryKindAndon:
			if e.Andon == nil || e.Andon.Reason != andonReason || e.Item != nil {
				t.Errorf("andon entry = %+v; want acme's pull", e)
			}
		case gen.InboxEntryKindReworkWarning:
			want := gen.ReworkWarning{Station: stCheck, Rejected: 1, Verdicts: 3, Window: 5, Threshold: 0.4, TopReason: new("no evidence")}
			if e.Rework == nil || !reflect.DeepEqual(*e.Rework, want) {
				t.Errorf("rework warning = %+v; want %+v", e.Rework, want)
			}
		case gen.InboxEntryKindDecision, gen.InboxEntryKindQuestion, gen.InboxEntryKindBlocked, gen.InboxEntryKindExpedite:
		}
	}
}
