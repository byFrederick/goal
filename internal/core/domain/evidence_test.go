package domain_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The evidence tests' stations and data keys.
const (
	evBuild  = "build"
	evReason = "reason"
	evText   = "text"
	evFrom   = "from"
)

// The evidence tests' line, in order, and its window.
var (
	evLine  = []string{"intake", evBuild, "review", "approve", "done"}
	evSince = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	evUntil = evSince.Add(24 * time.Hour)
)

// evAt is a moment in the window, minutes after its start.
func evAt(minutes int) time.Time { return evSince.Add(time.Duration(minutes) * time.Minute) }

// evItem is a done item of number n, finished in the window, with runs and events.
func evItem(n int64, runs []domain.Run, events ...domain.Event) domain.EvidenceItem {
	it := domain.Item{
		ID: domain.ItemID(fmt.Sprintf("item-%d", n)), Number: n, Title: fmt.Sprintf("Item %d", n), Body: "Do the thing.",
		Type: "task", Lane: "backend", State: domain.StateDone,
	}

	for i := range events {
		events[i].ID, events[i].ItemID = n*1000+int64(i), it.ID
	}

	return domain.EvidenceItem{Item: it, FinishedAt: evAt(600), Runs: runs, Events: events}
}

// person is an event a person made at station: data.actor user:ada.
func person(kind domain.EventKind, station string, at time.Time, data map[string]any) domain.Event {
	d := map[string]any{"actor": "user:ada"}
	maps.Copy(d, data)

	return domain.Event{Kind: kind, Station: station, At: at, Actor: "Ada", Data: d}
}

// agent is an event an agent or the engine made at station.
func agent(kind domain.EventKind, station, name string, at time.Time, data map[string]any) domain.Event {
	return domain.Event{Kind: kind, Station: station, At: at, Actor: name, Data: data}
}

// evRun is a finished run at station of an agent, on version, with turns and cost.
func evRun(id, station, agentName string, ver domain.DefinitionVersionID, turns int, usd float64) domain.Run {
	return domain.Run{
		ID: domain.RunID(id), Station: station, Agent: agentName, Model: "model-a", DefinitionVersionID: ver,
		Result:    domain.RunResult{Status: domain.RunSucceeded, Turns: turns, CostUSD: usd},
		StartedAt: evAt(1), EndedAt: evAt(2),
	}
}

func build(t *testing.T, in domain.EvidenceInput) map[string]string {
	t.Helper()

	if in.Since.IsZero() {
		in.Since, in.Until = evSince, evUntil
	}

	if in.Stations == nil {
		in.Stations = evLine
	}

	out := map[string]string{}
	for p, b := range domain.BuildEvidence(in) {
		out[p] = string(b)
	}

	return out
}

func expectHas(t *testing.T, name, text string, wants ...string) {
	t.Helper()

	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q:\n%s", name, w, text)
		}
	}
}

func expectLacks(t *testing.T, name, text string, unwanted ...string) {
	t.Helper()

	for _, w := range unwanted {
		if strings.Contains(text, w) {
			t.Errorf("%s has %q, want none:\n%s", name, w, text)
		}
	}
}

// An item's file holds its title, body, type and lane, each run with its station, agent, model,
// version, turns, cost, outcome and pinned knowledge, each send-back with its sender, category
// and reason, its questions with their answers, its notes and its touches, and no per-turn event.
func TestEvidence_ItemFiles(t *testing.T) {
	t.Parallel()

	runs := []domain.Run{
		evRun("r1", evBuild, "engineer", "ver-3", 12, 0.4),
		evRun("r2", "review", "reviewer", "ver-3", 3, 0.1),
		evRun("r3", evBuild, "engineer", "ver-unknown", 5, 0.2),
	}
	runs[2].Result.Status = domain.RunFailed

	it := evItem(7, runs,
		agent(domain.EventKnowledgePinned, evBuild, "engineer", evAt(1), map[string]any{
			"snapshots": []any{map[string]any{"source": "handbook", "snapshot": "snap-1"}},
		}),
		agent(domain.EventToolCall, evBuild, "engineer", evAt(2), map[string]any{"tool": "Bash", "input": "TOOL-INPUT"}),
		agent(domain.EventGateRejected, "review", "reviewer", evAt(3), map[string]any{evReason: "No tests for the handler.\nAdd one.", "category": "missing-tests"}),
		agent(domain.EventQuestionAsked, evBuild, "engineer", evAt(4), map[string]any{"question": "Which port?", "agent": "engineer"}),
		person(domain.EventQuestionAnswered, evBuild, evAt(5), map[string]any{"answer": "Use 8080."}),
		person(domain.EventNote, evBuild, evAt(6), map[string]any{evText: "Mind the timeout."}),
	)
	it.Events[0].RunID = "r1"
	it.Item.Origin = domain.OriginPlan

	files := build(t, domain.EvidenceInput{
		Items:    []domain.EvidenceItem{it},
		Versions: []domain.DefinitionVersion{{ID: "ver-3", Number: 3, State: domain.VersionPublished, PublishedAt: evSince.Add(-time.Hour)}},
	})

	f, ok := files["items/7.md"]
	if !ok {
		t.Fatalf("no items/7.md among %v", keys(files))
	}

	expectHas(t, "items/7.md", f,
		"# #7 Item 7", "- Type: task", "- Lane: backend", "- Origin: plan", "> Do the thing.",
		"| r1 | build | engineer | model-a | v3 | 12 | $0.40 | finished | handbook (snap-1) |",
		"| r2 | review | reviewer | model-a | v3 | 3 | $0.10 | sent_back | none |",
		"| r3 | build | engineer | model-a | ver-unknown | 5 | $0.20 | failed | none |",
		"gate_rejected at review", "- Sender: agent reviewer", "- Category: missing-tests",
		"> No tests for the handler.\n> Add one.",
		"> Which port?", "Answered by Ada", "> Use 8080.",
		"Note by Ada at build", "> Mind the timeout.",
		"| note | build | Ada |", "| question_answered | build | Ada |",
	)
	expectLacks(t, "items/7.md", f, "TOOL-INPUT", "tool_call")
}

// Only an item that needed help gets a file: a send-back, a person's touch, a failed run or an
// outlier; a clean pass, a planned approval on arrival and an item finished outside the window
// get none.
func TestEvidence_OnlyItemsThatNeededHelp(t *testing.T) {
	t.Parallel()

	run := func(id string) []domain.Run { return []domain.Run{evRun(id, evBuild, "engineer", "", 10, 1)} }
	failed := func(id, why string) []domain.Run {
		r := run(id)
		r[0].Result.Status, r[0].Result.Error = domain.RunFailed, why

		return r
	}

	cases := []struct {
		name   string
		runs   []domain.Run
		events []domain.Event
		want   bool
	}{
		{"an agent gate's rejection", nil, []domain.Event{agent(domain.EventGateRejected, "review", "reviewer", evAt(1), map[string]any{evReason: "x"})}, true},
		{"a person gate's rejection", nil, []domain.Event{person(domain.EventGateRejected, "review", evAt(1), map[string]any{evReason: "x"})}, true},
		{"a person's reject", nil, []domain.Event{person(domain.EventRejected, "approve", evAt(1), map[string]any{evReason: "x"})}, true},
		{"a failed check", nil, []domain.Event{agent(domain.EventCheckFailed, evBuild, domain.ActorEngine, evAt(1), map[string]any{"command": "go test", "exit_code": 1})}, true},
		{"a failed contract", nil, []domain.Event{agent(domain.EventContractFailed, evBuild, domain.ActorEngine, evAt(1), map[string]any{"error": "bad"})}, true},
		{"a person's move upstream", nil, []domain.Event{person(domain.EventMoved, "approve", evAt(1), map[string]any{evFrom: "approve", "to": evBuild, evReason: "x"})}, true},
		{"a person's move downstream", nil, []domain.Event{person(domain.EventMoved, evBuild, evAt(1), map[string]any{evFrom: evBuild, "to": "review"})}, true},
		{"a note", nil, []domain.Event{person(domain.EventNote, evBuild, evAt(1), map[string]any{evText: "x"})}, true},
		{"an answer", nil, []domain.Event{person(domain.EventQuestionAnswered, evBuild, evAt(1), map[string]any{"answer": "x"})}, true},
		{"a retry", nil, []domain.Event{person(domain.EventRetried, evBuild, evAt(1), nil)}, true},
		{"a dependency added", nil, []domain.Event{person(domain.EventEdgeAdded, evBuild, evAt(1), map[string]any{"needs": "item-9"})}, true},
		{"a dependency removed", nil, []domain.Event{person(domain.EventEdgeRemoved, evBuild, evAt(1), map[string]any{"needs": "item-9"})}, true},
		{"an approval after a hold", nil, []domain.Event{
			agent(domain.EventWaitingHuman, "review", domain.ActorEngine, evAt(1), map[string]any{evReason: "rejected after 3 reworks (max 3)"}),
			person(domain.EventApproved, "review", evAt(2), map[string]any{"planned": false}),
		}, true},
		{"a blocked item", nil, []domain.Event{agent(domain.EventBlocked, evBuild, domain.ActorEngine, evAt(1), map[string]any{evReason: "sandbox failed"})}, true},
		{"a run out of turns", failed("r-turns", "error_max_turns: reached 40 turns"), nil, true},
		{"a run out of its budget", failed("r-budget", "error_max_budget_usd"), nil, true},
		{"a run retried after a lost sandbox", nil, []domain.Event{agent(domain.EventRunRetried, evBuild, domain.ActorEngine, evAt(1), map[string]any{evReason: "sandbox lost"})}, true},
		{"a clean pass", run("r-clean"), []domain.Event{
			agent(domain.EventGateApproved, "review", "reviewer", evAt(1), nil),
			agent(domain.EventWaitingHuman, "approve", domain.ActorEngine, evAt(2), nil),
			person(domain.EventApproved, "approve", evAt(3), map[string]any{"planned": true}),
			agent(domain.EventDone, "done", domain.ActorEngine, evAt(4), nil),
		}, false},
		{"a run that failed for another reason", failed("r-other", "worker finished without output"), nil, false},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			n := int64(i + 1)
			files := build(t, domain.EvidenceInput{
				Items:   []domain.EvidenceItem{evItem(n, c.runs, c.events...)},
				Medians: []domain.StationMedian{{Station: evBuild, CostUSD: 1, Turns: 10, Runs: 20}},
			})

			if _, got := files[fmt.Sprintf("items/%d.md", n)]; got != c.want {
				t.Errorf("file for %s: %v, want %v (files %v)", c.name, got, c.want, keys(files))
			}
		})
	}

	late := evItem(99, nil, person(domain.EventNote, evBuild, evAt(1), map[string]any{evText: "x"}))
	late.FinishedAt = evUntil

	if files := build(t, domain.EvidenceInput{Items: []domain.EvidenceItem{late}}); len(files) != 2 {
		t.Errorf("files for an item finished at the window's end = %v, want only README.md and versions.md", keys(files))
	}
}

// A run is an outlier at three times its station's median cost or turns, not below; a station
// with no median has none.
func TestEvidence_OutlierAtThreeTimesMedian(t *testing.T) {
	t.Parallel()

	medians := []domain.StationMedian{{Station: evBuild, CostUSD: 1, Turns: 10, Runs: 30}}

	cases := []struct {
		name    string
		station string
		turns   int
		usd     float64
		want    bool
	}{
		{"just under on both", evBuild, 29, 2.99, false},
		{"three times the cost", evBuild, 10, 3, true},
		{"three times the turns", evBuild, 30, 1, true},
		{"a station without a median", "review", 300, 300, false},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			n := int64(i + 1)
			it := evItem(n, []domain.Run{evRun("r", c.station, "engineer", "", c.turns, c.usd)})
			files := build(t, domain.EvidenceInput{Items: []domain.EvidenceItem{it}, Medians: medians})

			f, got := files[fmt.Sprintf("items/%d.md", n)]
			if got != c.want {
				t.Fatalf("outlier file: %v, want %v", got, c.want)
			}

			if got {
				expectHas(t, "item", f, "## Outliers", "the station's 30-day median")
			}
		})
	}
}

// Reasons, notes and answers are kept whole up to 4 KB and cut at a rune boundary past it, with
// a marker.
func TestEvidence_TruncatesAt4KB(t *testing.T) {
	t.Parallel()

	whole := strings.Repeat("a", 4096)
	long := strings.Repeat("b", 4095) + "é" + "TAIL" // é straddles the 4 KB boundary

	it := evItem(1, nil,
		person(domain.EventRejected, "approve", evAt(1), map[string]any{evReason: long}),
		person(domain.EventNote, evBuild, evAt(2), map[string]any{evText: whole}),
	)

	f := build(t, domain.EvidenceInput{Items: []domain.EvidenceItem{it}})["items/1.md"]

	expectHas(t, "item", f, "> "+whole+"\n", "> "+strings.Repeat("b", 4095)+"\n", "Cut at 4 KB of 4101 bytes.")
	expectLacks(t, "item", f, "TAIL", "Cut at 4 KB of 4096")

	if !utf8.ValidString(f) {
		t.Error("the file is not valid UTF-8")
	}
}

// README.md counts send-backs by station, by sender and by category, touches by kind, failed
// runs by cause and the items with a file.
func TestEvidence_ReadmeCounts(t *testing.T) {
	t.Parallel()

	items := []domain.EvidenceItem{
		evItem(1, nil,
			agent(domain.EventGateRejected, "review", "reviewer", evAt(1), map[string]any{evReason: "x", "category": "missing-tests"}),
			agent(domain.EventGateRejected, "review", "reviewer", evAt(2), map[string]any{evReason: "y", "category": "missing-tests"}),
			agent(domain.EventCheckFailed, evBuild, domain.ActorEngine, evAt(3), map[string]any{"command": "go vet", "exit_code": 1}),
			person(domain.EventNote, evBuild, evAt(4), map[string]any{evText: "x"}),
		),
		evItem(2, nil,
			person(domain.EventRejected, "approve", evAt(1), map[string]any{evReason: "wrong"}),
			person(domain.EventNote, evBuild, evAt(2), map[string]any{evText: "x"}),
			person(domain.EventRetried, evBuild, evAt(3), nil),
			agent(domain.EventRunRetried, evBuild, domain.ActorEngine, evAt(4), map[string]any{evReason: "sandbox lost"}),
		),
		evItem(3, nil, agent(domain.EventGateApproved, "review", "reviewer", evAt(1), nil)),
	}

	readme := build(t, domain.EvidenceInput{Items: items})["README.md"]

	expectHas(t, "README.md", readme,
		"2 of the 3 items that finished",
		"## Send-backs by station\n\n| Station | Send-backs |\n| --- | --- |\n| review | 2 |\n| approve | 1 |\n| build | 1 |\n",
		"## Send-backs by sender\n\n| Sender | Send-backs |\n| --- | --- |\n| agent | 2 |\n| check | 1 |\n| person | 1 |\n",
		"## Send-backs by category\n\n| Category | Send-backs |\n| --- | --- |\n| missing-tests | 2 |\n| check_failed | 1 |\n| rejected | 1 |\n",
		"## Touches by kind\n\n| Kind | Touches |\n| --- | --- |\n| note | 2 |\n| retried | 1 |\n",
		"## Failed runs by cause\n\n| Cause | Runs |\n| --- | --- |\n| run_retried | 1 |\n",
		"| [#1](items/1.md) | Item 1 |", "| [#2](items/2.md) | Item 2 |",
	)
	expectLacks(t, "README.md", readme, "items/3.md")
}

// A gate–person disagreement is an item a person moved to or past a gate's station after the gate
// sent it back, or a gate's pass a person later sent back to or before the gate.
func TestEvidence_GatePersonDisagreements(t *testing.T) {
	t.Parallel()

	reject := func(at int) domain.Event {
		return agent(domain.EventGateRejected, "review", "reviewer", evAt(at), map[string]any{evReason: "x"})
	}
	pass := func(at int) domain.Event { return agent(domain.EventGateApproved, "review", "reviewer", evAt(at), nil) }
	move := func(at int, from, to string) domain.Event {
		return person(domain.EventMoved, from, evAt(at), map[string]any{evFrom: from, "to": to, evReason: "x"})
	}
	rejectBy := func(at int, from, to string) []domain.Event {
		return []domain.Event{
			person(domain.EventRejected, from, evAt(at), map[string]any{evReason: "x"}),
			agent(domain.EventMoved, from, "Ada", evAt(at), map[string]any{evFrom: from, "to": to}),
		}
	}

	items := []domain.EvidenceItem{
		evItem(1, nil, reject(1), move(2, evBuild, "approve")),                                 // moved past the send-back
		evItem(2, nil, reject(1), move(2, evBuild, "review")),                                  // moved to the gate without rework
		evItem(3, nil, reject(1), pass(2), move(3, evBuild, "approve")),                        // the gate passed it itself first
		evItem(4, nil, append([]domain.Event{pass(1)}, rejectBy(2, "approve", evBuild)...)...), // its pass sent back
		evItem(5, nil, pass(1), move(2, "approve", "review")),                                  // moved back to the gate
		evItem(6, nil, pass(1), move(2, "approve", evBuild), move(3, "approve", evBuild)),      // each pass counts once
		evItem(7, nil, append([]domain.Event{pass(1)}, rejectBy(2, "intake", "intake")...)...), // a reject before the gate
	}

	readme := build(t, domain.EvidenceInput{Items: items})["README.md"]

	expectHas(t, "README.md", readme,
		"## Gate–person disagreements\n\n| Gate station | Moved past its send-back | Its pass sent back | Items |\n| --- | --- | --- | --- |\n"+
			"| review | 2 | 3 | #1, #2, #4, #5, #6 |\n",
	)
}

// versions.md lists the versions published in the window, oldest first, with who published each
// and its message: not one published before it, at its end or a draft.
func TestEvidence_Versions(t *testing.T) {
	t.Parallel()

	published := func(n int, at time.Time, msg string) domain.DefinitionVersion {
		return domain.DefinitionVersion{
			ID: domain.DefinitionVersionID(fmt.Sprintf("v-%d", n)), Number: n, State: domain.VersionPublished, Message: msg,
			PublishedBy: domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}, PublishedAt: at,
		}
	}

	versions := []domain.DefinitionVersion{
		published(9, evAt(90), "Ask the reviewer for tests"),
		published(4, evSince.Add(-time.Second), "Before the window"),
		published(8, evAt(30), "Merge step on Approve spec"),
		published(10, evUntil, "At the end"),
		{ID: "draft", State: domain.VersionDraft, Message: "Draft"},
	}

	got := build(t, domain.EvidenceInput{Versions: versions})["versions.md"]

	v8 := strings.Index(got, "## v8")
	v9 := strings.Index(got, "## v9")

	if v8 < 0 || v9 < v8 {
		t.Errorf("versions.md lists v8 at %d and v9 at %d, want v8 then v9:\n%s", v8, v9, got)
	}

	expectHas(t, "versions.md", got, "Published "+evAt(30).Format(time.RFC3339)+" by Ada.", "> Merge step on Approve spec", "> Ask the reviewer for tests")
	expectLacks(t, "versions.md", got, "Before the window", "At the end", "Draft")
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}
