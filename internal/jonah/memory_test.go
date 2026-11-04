package jonah_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// fact mirrors the worker's Fact: a memory fact the review proposes.
type fact struct {
	Text     string   `json:"text"`
	Items    []string `json:"items"`
	Replaces []string `json:"replaces,omitempty"`
}

// memoryEntry is one memory.yaml entry, as config.ProposalCheck.MemoryJSON reads it.
type memoryEntry struct {
	Text  string   `json:"text"`
	From  []string `json:"from,omitempty"`
	Added string   `json:"added"`
}

// Event data keys and an item type the memory tests use.
const (
	keyText   = "text"
	keyReason = "reason"
	taskType  = "task"
)

// phones is a person's fact that holds beyond its item (ADR 0039 decision 7).
const phones = "Phones are 390 wide; check 375 too"

// ada is the person whose events the memory tests record.
var ada = domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}

// byAda is an event of kind at build whose data is data with Ada as its actor.
func byAda(kind domain.EventKind, data map[string]any) domain.Event {
	data["actor"] = ada.Ref()

	return domain.Event{Kind: kind, Station: stBuild, Actor: ada.Name, Data: data}
}

// finishedWith records a new item with events, then moves it to done at at, and returns it.
func (w *reviewWorld) finishedWith(at time.Time, events ...domain.Event) domain.Item {
	w.t.Helper()

	it, err := w.store.CreateItem(w.ctx(), domain.NewItem{
		FactoryID: reviewFactory, Title: "a finished item", Type: taskType, Station: stBuild, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at.Add(-time.Hour),
	})
	if err != nil {
		w.t.Fatalf("CreateItem: %v", err)
	}

	for i, e := range events {
		e.FactoryID, e.ItemID, e.At = reviewFactory, it.ID, at.Add(-time.Duration(len(events)-i)*time.Minute)
		if err := w.store.AppendEvents(w.ctx(), e); err != nil {
			w.t.Fatalf("AppendEvents: %v", err)
		}
	}

	if _, err := w.store.Transition(w.ctx(), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: "done", State: domain.StateDone}, At: at, DoneAt: at,
	}); err != nil {
		w.t.Fatalf("Transition: %v", err)
	}

	return it
}

// eventOf is the id of it's first event of kind.
func (w *reviewWorld) eventOf(it domain.Item, kind domain.EventKind) int64 {
	w.t.Helper()

	events, err := w.store.ItemEvents(w.ctx(), it.ID, 0, 100)
	if err != nil {
		w.t.Fatalf("ItemEvents: %v", err)
	}

	for _, e := range events {
		if e.Kind == kind {
			return e.ID
		}
	}

	w.t.Fatalf("item %s has no %s event", it.ID, kind)

	return 0
}

// ref is it's ref as a review names it.
func ref(it domain.Item) string { return "#" + strconv.FormatInt(it.Number, 10) }

// memoryOf is the memory proposal's file: its edit applied to the version's memory.yaml, read as
// entries, after config.Check found no blocking problem in the version with it.
func (w *reviewWorld) memoryOf(p domain.Proposal) []memoryEntry {
	w.t.Helper()

	if len(p.Edits) != 1 || p.Edits[0].Path != config.MemoryFile {
		w.t.Fatalf("memory proposal edits %+v, want memory.yaml only", p.Edits)
	}

	before, present := w.defs.files[config.MemoryFile]

	after, err := p.Edits[0].Apply(before, present)
	if err != nil {
		w.t.Fatalf("apply the memory proposal: %v", err)
	}

	files := maps.Clone(w.defs.files)

	files[config.MemoryFile] = []byte(after)

	if problems := (config.ProposalCheck{}).Problems(files); len(problems) != 0 {
		w.t.Errorf("the version with the proposal's memory.yaml has problems %v:\n%s", problems, after)
	}

	raw, err := config.ProposalCheck{}.MemoryJSON([]byte(after))
	if err != nil {
		w.t.Fatalf("MemoryJSON: %v", err)
	}

	var entries []memoryEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		w.t.Fatal(err)
	}

	return entries
}

// factDrops are the reasons of the `jonah fact dropped` records, in order, each of which must name review.
func (w *reviewWorld) factDrops(review domain.JonahReviewID) []string {
	w.t.Helper()

	var out []string

	for _, rec := range w.logged("jonah fact dropped") {
		reason, ok := rec["reason"].(string)
		if rec["review_id"] != string(review) || !ok {
			w.t.Errorf("dropped record %v, want review_id %s and a reason", rec, review)
		}

		out = append(out, reason)
	}

	return out
}

// A fact that quotes, whitespace aside, a person's note, answer or send-back reason on each item
// it names becomes a memory proposal: the fact with the items it came from and today's date, and
// one piece of evidence per quoted event (ADR 0039 decision 7).
func TestMemoryFacts_KeepsAPersonsQuote(t *testing.T) {
	t.Parallel()

	for name, e := range map[string]domain.Event{
		"note":   byAda(domain.EventNote, map[string]any{keyText: "On #12: phones are odd.  " + strings.ReplaceAll(phones, " ", "\n  ") + "."}),
		"answer": byAda(domain.EventQuestionAnswered, map[string]any{"answer": "Yes. " + phones}),
		"reject": byAda(domain.EventRejected, map[string]any{keyReason: phones + ", and the button is clipped"}),
		"gate":   byAda(domain.EventGateRejected, map[string]any{keyReason: phones, "category": "ui"}),
		"move":   byAda(domain.EventMoved, map[string]any{keyReason: phones, "from": stShip, "to": stBuild}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := newReviewWorld(t)
			it := w.finishedWith(rt0.Add(-time.Hour), e)
			other := w.finishedWith(rt0.Add(-2*time.Hour), byAda(domain.EventNote, map[string]any{keyText: phones}))

			r := w.queue(domain.JonahManual)
			w.answer(output{Facts: []fact{{Text: " " + phones + " ", Items: []string{ref(it), ref(other)}}}})

			if n := w.work(); n != 1 {
				t.Fatalf("Work finished %d, want 1", n)
			}

			if drops := w.factDrops(r.ID); len(drops) != 0 {
				t.Errorf("facts dropped: %v", drops)
			}

			ps := w.proposals()
			if len(ps) != 1 || ps[0].Title != "Add 1 fact to memory" || ps[0].ReviewID != r.ID {
				t.Fatalf("proposals = %+v, want one memory proposal", ps)
			}

			want := []memoryEntry{{Text: phones, From: []string{"acme-review#" + ref(it)[1:], "acme-review#" + ref(other)[1:]}, Added: "2026-10-01"}}
			if got := w.memoryOf(ps[0]); !slices.EqualFunc(got, want, sameEntry) {
				t.Errorf("memory = %+v, want %+v", got, want)
			}

			wantEvidence := []domain.ProposalEvidence{
				{ItemID: it.ID, EventID: w.eventOf(it, e.Kind), Station: stBuild, Quote: phones},
				{ItemID: other.ID, EventID: w.eventOf(other, domain.EventNote), Station: stBuild, Quote: phones},
			}
			if !slices.Equal(ps[0].Evidence, wantEvidence) {
				t.Errorf("evidence = %+v, want %+v", ps[0].Evidence, wantEvidence)
			}
		})
	}
}

func sameEntry(a, b memoryEntry) bool {
	return a.Text == b.Text && a.Added == b.Added && slices.Equal(a.From, b.From)
}

// A fact that quotes no person's event on an item of the window's evidence is dropped and logged:
// an agent's send-back, a failed check's output, an item outside the window, a paraphrase, text
// past memory's limit for an entry, a hidden character, a fact memory already has, and a
// replacement of a fact memory does not have (ADR 0039 decisions 7 and 20).
func TestMemoryFacts_RefusesUnquoted(t *testing.T) {
	t.Parallel()

	agentGate := domain.Event{Kind: domain.EventGateRejected, Station: stBuild, Actor: "reviewer", Data: map[string]any{"actor": "agent:reviewer", keyReason: phones}}
	check := domain.Event{Kind: domain.EventCheckFailed, Station: stBuild, Data: map[string]any{"command": "task lint", "output": phones}}
	long := strings.Repeat("a", domain.MaxMemoryText+1)
	hidden := "Phones are 390\u200b wide"

	for name, tc := range map[string]struct {
		events []domain.Event
		old    bool
		fact   fact
		reason string
	}{
		"agent's send-back": {events: []domain.Event{agentGate}, fact: fact{Text: phones}, reason: "unquoted"},
		"check output":      {events: []domain.Event{check}, fact: fact{Text: phones}, reason: "unquoted"},
		"outside window":    {events: []domain.Event{byAda(domain.EventNote, map[string]any{keyText: phones})}, old: true, fact: fact{Text: phones}, reason: "item"},
		"paraphrase": {
			events: []domain.Event{byAda(domain.EventNote, map[string]any{keyText: phones})},
			fact:   fact{Text: "Check phones at 375 as well as 390"}, reason: "unquoted",
		},
		"too long":  {events: []domain.Event{byAda(domain.EventNote, map[string]any{keyText: long})}, fact: fact{Text: long}, reason: "too_long"},
		"hidden":    {events: []domain.Event{byAda(domain.EventNote, map[string]any{keyText: hidden})}, fact: fact{Text: hidden}, reason: reasonUnicode},
		"in memory": {events: []domain.Event{byAda(domain.EventNote, map[string]any{keyText: "Use acme for samples"})}, fact: fact{Text: "Use acme for samples"}, reason: "exists"},
		"no items":  {fact: fact{Text: phones}, reason: "items"},
		"replaces what memory lacks": {
			events: []domain.Event{byAda(domain.EventNote, map[string]any{keyText: phones})},
			fact:   fact{Text: phones, Replaces: []string{"no such fact"}}, reason: "replaces",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := newReviewWorld(t)
			w.defs.files[config.MemoryFile] = []byte("- text: Use acme for samples\n  added: 2026-09-01\n")

			at := rt0.Add(-time.Hour)
			if tc.old {
				at = rt0.Add(-jonah30Days - time.Hour)
			}

			f := tc.fact
			if tc.events != nil {
				f.Items = []string{ref(w.finishedWith(at, tc.events...))}
			}

			r := w.queue(domain.JonahManual)
			w.answer(output{Facts: []fact{f}})

			if n := w.work(); n != 1 {
				t.Fatalf("Work finished %d, want 1", n)
			}

			if got := w.reviews()[0]; got.ID != r.ID || got.State != domain.JonahDone {
				t.Fatalf("review = %+v, want done", got)
			}

			if ps := w.proposals(); len(ps) != 0 {
				t.Errorf("proposals = %+v, want none", ps)
			}

			if got := w.factDrops(r.ID); !slices.Equal(got, []string{tc.reason}) {
				t.Errorf("facts dropped for %v, want [%s]", got, tc.reason)
			}

			// The first 40 characters, so a leak of the long fact's start is caught too.
			if text := tc.fact.Text[:min(len(tc.fact.Text), 40)]; strings.Contains(w.log.String(), text) {
				t.Errorf("the log holds the fact's text %q: %s", text, w.log)
			}
		})
	}
}

// priorProposal stores p as the open proposal of an earlier review a minute ago, and returns it.
func (w *reviewWorld) priorProposal(p domain.NewProposal) domain.Proposal {
	w.t.Helper()

	at := w.clock.Now().Add(-time.Minute)

	if _, err := w.store.QueueJonahReview(w.ctx(), domain.NewJonahReview{
		FactoryID: reviewFactory, VersionID: reviewVersion, Trigger: domain.JonahManual, WindowStart: at.Add(-time.Hour), WindowEnd: at,
		By: domain.LocalUser, At: at,
	}); err != nil {
		w.t.Fatal(err)
	}

	claimed, err := w.store.ClaimJonahReview(w.ctx(), at, time.Minute)
	if err != nil {
		w.t.Fatal(err)
	}

	_, stored, err := w.store.FinishJonahReview(w.ctx(), claimed, domain.JonahReviewResult{State: domain.JonahDone, At: at, Proposals: []domain.NewProposal{p}})
	if err != nil || len(stored) != 1 {
		w.t.Fatalf("FinishJonahReview = %+v, %v; want the prior proposal", stored, err)
	}

	return stored[0]
}

// memoryFile is memory.yaml with entries "fact 1" to "fact n", each from acme-review#1.
func memoryFile(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("- text: fact " + strconv.Itoa(i+1) + "\n  from: [acme-review#1]\n  added: 2026-09-01\n")
	}

	return b.String()
}

// The facts of TestReviews_FinishProposesMemoryFacts: one an open proposal carries, and two of
// people's events.
const (
	carried  = "Use acme.example.com in samples"
	lint     = "Run the linter before pushing"
	branches = "Ask before deleting a branch"
)

// A review's facts make one proposal of memory.yaml: it builds on the open memory proposal and
// supersedes it, removes the facts the new ones replace, and drops the facts past memory's 50
// entries. A definition proposal's edit of memory.yaml is dropped (ADR 0039 decision 7).
func TestReviews_FinishProposesMemoryFacts(t *testing.T) {
	t.Parallel()

	w := newReviewWorld(t)
	version := memoryFile(48)
	w.defs.files[config.MemoryFile] = []byte(version)

	withCarried := version + "- text: " + carried + "\n  added: 2026-09-30\n"
	prior := w.priorProposal(domain.NewProposal{
		Title: "Add 1 fact to memory", Edits: []domain.ProposalEdit{{Path: config.MemoryFile, Replacements: []domain.Replacement{{Old: version, New: withCarried}}}},
		Evidence: []domain.ProposalEvidence{{ItemID: "item-0", Quote: carried}},
	})

	first := w.finishedWith(rt0.Add(-3*time.Hour), byAda(domain.EventNote, map[string]any{keyText: phones}))
	second := w.finishedWith(rt0.Add(-2*time.Hour), byAda(domain.EventQuestionAnswered, map[string]any{"answer": lint}))
	third := w.finishedWith(rt0.Add(-time.Hour), byAda(domain.EventRejected, map[string]any{keyReason: branches}))

	r := w.queue(domain.JonahManual)
	w.answer(output{
		Proposals: []proposal{{
			Title: "Remember phones", Summary: "x", Edits: []fileEdit{{Path: config.MemoryFile, Replacements: []domain.Replacement{{Old: "fact 1\n", New: "Phones\n"}}}},
			Estimate: estimate{Metric: domain.MetricSentBackRate, Station: stBuild},
		}},
		Facts: []fact{
			{Text: phones, Items: []string{ref(first)}, Replaces: []string{"fact 1"}},
			{Text: lint, Items: []string{ref(second)}},
			{Text: branches, Items: []string{ref(third)}},
		},
	})

	if n := w.work(); n != 1 {
		t.Fatalf("Work finished %d, want 1", n)
	}

	if got := w.drops(r.ID); !slices.Equal(got, []dropped{{0, reasonPath}}) {
		t.Errorf("proposals dropped = %+v, want the definition edit of memory.yaml", got)
	}

	if got := w.factDrops(r.ID); !slices.Equal(got, []string{"limit"}) {
		t.Errorf("facts dropped = %v, want the third past the limit", got)
	}

	ps := w.proposals()
	if len(ps) != 2 || ps[1].ID != prior.ID || ps[1].State != domain.ProposalSuperseded {
		t.Fatalf("proposals = %+v, want the new one and the prior one superseded", ps)
	}

	p := ps[0]
	if p.Title != "Add 3 facts to memory and remove 1" || p.State != domain.ProposalProposed || p.Estimate.Metric != "" {
		t.Errorf("memory proposal = %+v", p)
	}

	if len(p.Edits) != 1 || len(p.Edits[0].Replacements) != 1 || p.Edits[0].Replacements[0].Old != version {
		t.Errorf("memory proposal edits = %+v, want one whole-file replacement of the version's memory.yaml", p.Edits)
	}

	w.expectFinishedMemory(p, first, second)
}

// expectFinishedMemory checks TestReviews_FinishProposesMemoryFacts's memory proposal p: fact 2
// to fact 48 kept as they were, then the carried fact and the facts of first and second, with
// the carried evidence and theirs.
func (w *reviewWorld) expectFinishedMemory(p domain.Proposal, first, second domain.Item) {
	t := w.t
	t.Helper()

	got := w.memoryOf(p)
	if len(got) != domain.MaxMemoryEntries {
		t.Fatalf("memory has %d entries, want %d", len(got), domain.MaxMemoryEntries)
	}

	if got[0].Text != "fact 2" || got[46].Text != "fact 48" || !slices.Equal(got[0].From, []string{"acme-review#1"}) || got[0].Added != "2026-09-01" {
		t.Errorf("kept entries begin %+v and end %+v, want fact 2 to fact 48 as they were", got[0], got[46])
	}

	want := []memoryEntry{
		{Text: carried, Added: "2026-09-30"},
		{Text: phones, From: []string{"acme-review" + ref(first)}, Added: "2026-10-01"},
		{Text: lint, From: []string{"acme-review" + ref(second)}, Added: "2026-10-01"},
	}
	if !slices.EqualFunc(got[47:], want, sameEntry) {
		t.Errorf("new entries = %+v, want %+v", got[47:], want)
	}

	wantEvidence := []domain.ProposalEvidence{
		{ItemID: "item-0", Quote: carried},
		{ItemID: first.ID, EventID: w.eventOf(first, domain.EventNote), Station: stBuild, Quote: phones},
		{ItemID: second.ID, EventID: w.eventOf(second, domain.EventQuestionAnswered), Station: stBuild, Quote: lint},
	}
	if !slices.Equal(p.Evidence, wantEvidence) {
		t.Errorf("evidence = %+v, want %+v", p.Evidence, wantEvidence)
	}
}
