package domain

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Evidence is what a Jonah review reads of the line (ADR 0039 decisions 1 and 2): a folder of
// small markdown files that goald writes from the engine's own milestone events, with no model
// call. An item finished in the review's window becomes evidence when something needed help at
// any station:
//
//   - a send-back: a gate's gate_rejected (by an agent or a person, with its data.category), a
//     person's rejected, a check_failed or contract_failed, or a person's moved to an earlier
//     station in the line's order;
//   - a person's touch: a note, a question_answered, a retried, a moved, an edge_added or
//     edge_removed, or an approved after a hold (a waiting_human with a reason, which the engine
//     writes when it escalates; an arrival at a person's station writes none);
//   - a failed run: blocked, a run that failed out of turns or out of its item budget (the SDK's
//     error_max_turns and error_max_budget_usd), or a run_retried after a lost sandbox;
//   - an outlier: a run whose cost or turns is at least OutlierFactor times its station's median.
//
// A gate–person disagreement is an agent gate's gate_rejected after which a person moved the
// item to or past the gate's station, or an agent gate's gate_approved after which a person sent
// the item back from a later station to the gate or before it (a rejected, read with the moved
// event of its transition, or an upstream move). Each send-back and each pass counts once.

// Evidence file paths and limits.
const (
	EvidenceReadme   = "README.md"
	EvidenceVersions = "versions.md"
	// EvidenceTextLimit caps each reason, note, question, answer and message, in bytes.
	EvidenceTextLimit = 4 << 10
	// OutlierFactor is how many times its station's median makes a run's cost or turns an outlier,
	// and EvidenceMedianWindow the time the medians are taken over, before the window's end.
	OutlierFactor        = 3
	EvidenceMedianWindow = 30 * 24 * time.Hour
)

// EvidenceItemPath is the path of item number n's file in the evidence folder.
func EvidenceItemPath(n int64) string { return "items/" + strconv.FormatInt(n, 10) + ".md" }

// EventGatewayToolCall is a connection tool call the gateway served a run, one per call.
const EventGatewayToolCall EventKind = "gateway_tool_call"

// PerTurnKinds are the event kinds a run writes as it works, per turn or per call: never
// evidence, and left out when the store reads an item's milestone events.
var PerTurnKinds = []EventKind{
	EventAgentStarted, EventToolCall, EventToolResult, EventAgentMessage, EventUsage, EventNoteDelivered,
	EventToolDenied, EventGatewayToolCall,
}

// EvidenceItem is an item finished (done or cancelled) at FinishedAt, with its runs (oldest
// first) and its milestone events (in append order; no PerTurnKinds).
type EvidenceItem struct {
	Item       Item
	FinishedAt time.Time
	Runs       []Run
	Events     []Event
}

// StationMedian is the median cost and turns of a station's finished runs over a time, of Runs runs.
type StationMedian struct {
	Station string
	CostUSD float64
	Turns   float64
	Runs    int
}

// EvidenceInput is what BuildEvidence reads: the window, the line's station ids in order, the
// items finished in it, the stations' medians and the factory's versions (any; versions.md lists
// those published in the window, and runs name the others by number too).
type EvidenceInput struct {
	Since, Until time.Time
	Stations     []string
	Items        []EvidenceItem
	Medians      []StationMedian
	Versions     []DefinitionVersion
}

// Senders of a send-back.
const (
	senderAgent  = "agent"
	senderCheck  = "check"
	senderPerson = "person"
)

// Causes of a failed run.
const (
	causeBlocked     = "blocked"
	causeOutOfTurns  = "out_of_turns"
	causeOutOfBudget = "out_of_budget"
	causeRunRetried  = "run_retried"
)

// SDK result subtypes a failed run's error starts with (WorkerFinished.failure).
const (
	subtypeMaxTurns  = "error_max_turns"
	subtypeMaxBudget = "error_max_budget_usd"
)

// Event data keys the evidence reads.
const (
	evKeyActor    = "actor"
	evKeyReason   = "reason"
	evKeyCategory = "category"
	evKeyFrom     = "from"
	evKeyTo       = "to"
)

// helpTouches are the kinds of a person's event that make a touch, besides an approval after a hold.
var helpTouches = []EventKind{EventNote, EventQuestionAnswered, EventRetried, EventMoved, EventEdgeAdded, EventEdgeRemoved}

type evSendBack struct {
	Kind     EventKind
	Station  string
	Sender   string
	Actor    string
	Category string
	Reason   string
	At       time.Time
}

type evFailure struct {
	Cause   string
	Station string
	Run     RunID
	Reason  string
	At      time.Time
}

type evOutlier struct {
	Run     RunID
	Station string
	What    string
	Value   string
	Times   float64
	Median  string
}

type evQuestion struct {
	Station, Asker, Question string
	At                       time.Time
	Answered                 bool
	AnsweredBy, Answer       string
}

type evNote struct {
	Station, By, Text string
	At                time.Time
}

// evItem is what one item's evidence holds.
type evItem struct {
	EvidenceItem

	runs      []TeardownRun
	causes    map[RunID]string
	knowledge map[RunID][]string
	sendBacks []evSendBack
	touches   []Event
	failures  []evFailure
	outliers  []evOutlier
	questions []evQuestion
	notes     []evNote
	// movedPast and passSentBack count the item's gate–person disagreements, by gate station.
	movedPast, passSentBack map[string]int
}

func (x *evItem) needsHelp() bool {
	return len(x.sendBacks) > 0 || len(x.touches) > 0 || len(x.failures) > 0 || len(x.outliers) > 0
}

// why names what made the item evidence.
func (x *evItem) why() string {
	var out []string

	for _, w := range []struct {
		n    int
		name string
	}{{len(x.sendBacks), "send-back"}, {len(x.touches), "touch"}, {len(x.failures), "failed run"}, {len(x.outliers), "outlier"}} {
		if w.n > 0 {
			out = append(out, w.name)
		}
	}

	return strings.Join(out, ", ")
}

// BuildEvidence writes the evidence folder of in: README.md, versions.md and items/<number>.md
// for each item finished in [in.Since, in.Until) that needed help, by path. It reads nothing
// else and calls nothing.
func BuildEvidence(in EvidenceInput) map[string][]byte {
	medians := map[string]StationMedian{}
	for _, m := range in.Medians {
		medians[m.Station] = m
	}

	numbers := map[DefinitionVersionID]int{}

	for _, v := range in.Versions {
		if v.Number > 0 {
			numbers[v.ID] = v.Number
		}
	}

	files := map[string][]byte{}

	var (
		finished int
		evidence []*evItem
	)

	for _, it := range in.Items {
		if !inEvidenceWindow(it.FinishedAt, in.Since, in.Until) {
			continue
		}

		finished++

		x := readItem(it, in.Stations, medians)
		if !x.needsHelp() {
			continue
		}

		evidence = append(evidence, x)
		files[EvidenceItemPath(it.Item.Number)] = []byte(itemFile(x, numbers))
	}

	slices.SortFunc(evidence, func(a, b *evItem) int { return cmp.Compare(a.Item.Number, b.Item.Number) })

	files[EvidenceReadme] = []byte(readme(in, finished, evidence))
	files[EvidenceVersions] = []byte(versionsFile(in))

	return files
}

func inEvidenceWindow(at, since, until time.Time) bool { return !at.Before(since) && at.Before(until) }

// readItem reads one item's evidence from its runs and events.
func readItem(it EvidenceItem, line []string, medians map[string]StationMedian) *evItem {
	x := &evItem{
		EvidenceItem: it, causes: map[RunID]string{}, knowledge: map[RunID][]string{},
		movedPast: map[string]int{}, passSentBack: map[string]int{},
	}
	x.runs, _ = teardownRuns(it.Runs, it.Events, it.FinishedAt)

	for _, r := range it.Runs {
		x.readRun(r, medians)
	}

	held := map[string]bool{}

	for _, e := range it.Events {
		x.readEvent(e, line, held)
	}

	x.readDisagreements(line)

	return x
}

// readRun records a run that failed out of turns or budget, and an outlier run.
func (x *evItem) readRun(r Run, medians map[string]StationMedian) {
	if r.Result.Status == RunFailed {
		cause := ""

		switch {
		case strings.HasPrefix(r.Result.Error, subtypeMaxTurns):
			cause = causeOutOfTurns
		case strings.HasPrefix(r.Result.Error, subtypeMaxBudget):
			cause = causeOutOfBudget
		}

		if cause != "" {
			x.causes[r.ID] = cause
			x.failures = append(x.failures, evFailure{Cause: cause, Station: r.Station, Run: r.ID, Reason: r.Result.Error, At: r.EndedAt})
		}
	}

	m, ok := medians[r.Station]
	if !ok {
		return
	}

	if m.CostUSD > 0 && r.Result.CostUSD >= OutlierFactor*m.CostUSD {
		x.outliers = append(x.outliers, evOutlier{
			Run: r.ID, Station: r.Station, What: "cost", Value: usd(r.Result.CostUSD), Times: r.Result.CostUSD / m.CostUSD, Median: usd(m.CostUSD),
		})
	}

	if m.Turns > 0 && float64(r.Result.Turns) >= OutlierFactor*m.Turns {
		x.outliers = append(x.outliers, evOutlier{
			Run: r.ID, Station: r.Station, What: "turns", Value: strconv.Itoa(r.Result.Turns), Times: float64(r.Result.Turns) / m.Turns,
			Median: strconv.FormatFloat(m.Turns, 'f', -1, 64),
		})
	}
}

// readEvent records what e adds to the item's evidence; held says, by station, whether the
// item's last waiting_human there was a hold.
func (x *evItem) readEvent(e Event, line []string, held map[string]bool) {
	if read, ok := eventReaders[e.Kind]; ok {
		read(x, e, line, held)
	}

	if byPerson(e) && (slices.Contains(helpTouches, e.Kind) || (e.Kind == EventApproved && held[e.Station])) {
		x.touches = append(x.touches, e)
	}
}

// eventReaders read what an event of their kind adds to an item's evidence (readEvent).
var eventReaders = map[EventKind]func(x *evItem, e Event, line []string, held map[string]bool){
	EventGateRejected: func(x *evItem, e Event, _ []string, _ map[string]bool) {
		sender := senderAgent
		if byPerson(e) {
			sender = senderPerson
		}

		x.sendBack(e, sender, e.Station, cmp.Or(dataText(e, evKeyCategory), string(e.Kind)), dataText(e, evKeyReason))
	},
	EventRejected: func(x *evItem, e Event, _ []string, _ map[string]bool) {
		if byPerson(e) {
			x.sendBack(e, senderPerson, e.Station, string(e.Kind), dataText(e, evKeyReason))
		}
	},
	EventCheckFailed: func(x *evItem, e Event, _ []string, _ map[string]bool) {
		x.sendBack(e, senderCheck, e.Station, string(e.Kind), checkReason(e))
	},
	EventContractFailed: func(x *evItem, e Event, _ []string, _ map[string]bool) {
		x.sendBack(e, senderCheck, e.Station, string(e.Kind), dataText(e, "error"))
	},
	EventMoved: func(x *evItem, e Event, line []string, _ map[string]bool) {
		from, to := cmp.Or(dataText(e, evKeyFrom), e.Station), dataText(e, evKeyTo)
		if byPerson(e) && linePlace(line, to) < linePlace(line, from) {
			x.sendBack(e, senderPerson, from, string(e.Kind), dataText(e, evKeyReason))
		}
	},
	EventWaitingHuman: func(_ *evItem, e Event, _ []string, held map[string]bool) {
		held[e.Station] = dataText(e, evKeyReason) != ""
	},
	EventBlocked:    func(x *evItem, e Event, _ []string, _ map[string]bool) { x.fail(e, causeBlocked) },
	EventRunRetried: func(x *evItem, e Event, _ []string, _ map[string]bool) { x.fail(e, causeRunRetried) },
	EventKnowledgePinned: func(x *evItem, e Event, _ []string, _ map[string]bool) {
		x.knowledge[e.RunID] = append(x.knowledge[e.RunID], pinnedSnapshots(e)...)
	},
	EventQuestionAsked: func(x *evItem, e Event, _ []string, _ map[string]bool) {
		x.questions = append(x.questions, evQuestion{
			Station: e.Station, Asker: cmp.Or(dataText(e, "agent"), e.Actor), Question: dataText(e, "question"), At: e.At,
		})
	},
	EventQuestionAnswered: func(x *evItem, e Event, _ []string, _ map[string]bool) { x.answer(e) },
	EventNote: func(x *evItem, e Event, _ []string, _ map[string]bool) {
		if byPerson(e) {
			x.notes = append(x.notes, evNote{Station: e.Station, By: e.Actor, Text: dataText(e, "text"), At: e.At})
		}
	},
}

// fail records e, a blocked or run_retried event, as a failed run of cause.
func (x *evItem) fail(e Event, cause string) {
	x.failures = append(x.failures, evFailure{Cause: cause, Station: e.Station, Run: e.RunID, Reason: dataText(e, evKeyReason), At: e.At})
}

func (x *evItem) sendBack(e Event, sender, station, category, reason string) {
	x.sendBacks = append(x.sendBacks, evSendBack{
		Kind: e.Kind, Station: station, Sender: sender, Actor: e.Actor, Category: category, Reason: reason, At: e.At,
	})
}

// answer pairs an answer with the latest unanswered question, or records it alone.
func (x *evItem) answer(e Event) {
	for i := len(x.questions) - 1; i >= 0; i-- {
		if !x.questions[i].Answered {
			x.questions[i].Answered, x.questions[i].AnsweredBy, x.questions[i].Answer = true, e.Actor, dataText(e, "answer")
			return
		}
	}

	x.questions = append(x.questions, evQuestion{Station: e.Station, At: e.At, Answered: true, AnsweredBy: e.Actor, Answer: dataText(e, "answer")})
}

// readDisagreements counts the item's gate–person disagreements (the rules at the top of this file).
func (x *evItem) readDisagreements(line []string) {
	rejected, passed := map[string]bool{}, map[string]bool{}
	events := x.Events

	for i, e := range events {
		person := byPerson(e)

		switch {
		case e.Kind == EventGateRejected && !person:
			rejected[e.Station] = true
		case e.Kind == EventGateApproved && !person:
			delete(rejected, e.Station)
			passed[e.Station] = true
		case e.Kind == EventMoved && person:
			from, to := cmp.Or(dataText(e, evKeyFrom), e.Station), dataText(e, evKeyTo)
			x.crossed(rejected, line, from, to)
			x.sentBackPast(passed, line, from, to)
		case e.Kind == EventRejected && person:
			x.sentBackPast(passed, line, e.Station, rejectTarget(events[i+1:], e))
		}
	}
}

// crossed counts and clears each gate send-back in rejected that a person's move went to or past:
// the move leaves the gate's station or one before it for the gate's station or one after it.
func (x *evItem) crossed(rejected map[string]bool, line []string, from, to string) {
	f, t := linePlace(line, from), linePlace(line, to)

	for _, g := range slices.Sorted(maps.Keys(rejected)) {
		if p := linePlace(line, g); f <= p && p <= t && f < t {
			x.movedPast[g]++
			delete(rejected, g)
		}
	}
}

// sentBackPast counts and clears each gate pass in passed that a person's send-back undid: one
// that leaves a station after the gate's for the gate's or one before it. An unknown target counts.
func (x *evItem) sentBackPast(passed map[string]bool, line []string, from, to string) {
	f := linePlace(line, from)

	for _, g := range slices.Sorted(maps.Keys(passed)) {
		p := linePlace(line, g)
		if f > p && (to == "" || linePlace(line, to) <= p) {
			x.passSentBack[g]++
			delete(passed, g)
		}
	}
}

// rejectTarget is where a person's rejected sent the item: the to of the moved event its
// transition wrote (at the same moment, from its station); "" when there is none.
func rejectTarget(later []Event, rej Event) string {
	for _, e := range later {
		if !e.At.Equal(rej.At) {
			break
		}

		if e.Kind == EventMoved && cmp.Or(dataText(e, evKeyFrom), e.Station) == rej.Station {
			return dataText(e, evKeyTo)
		}
	}

	return ""
}

// byPerson reports whether a person made e (data.actor user:…).
func byPerson(e Event) bool {
	return strings.HasPrefix(dataText(e, evKeyActor), personRef)
}

// dataText is e's data at key as a string, "" when it is not one.
func dataText(e Event, key string) string {
	s, _ := e.Data[key].(string)
	return s
}

// checkReason is a failed check's command, exit code and output.
func checkReason(e Event) string {
	reason := "`" + dataText(e, keyCommand) + "`"
	if code, ok := e.Data["exit_code"]; ok {
		reason += fmt.Sprintf(" exited %v", code)
	}

	if out := dataText(e, "output"); out != "" {
		reason += "\n\n" + out
	}

	return reason
}

// pinnedSnapshots are a knowledge_pinned event's snapshots as "source (snapshot)".
func pinnedSnapshots(e Event) []string {
	var out []string

	add := func(m map[string]any) {
		src, _ := m["source"].(string)
		snap, _ := m["snapshot"].(string)

		if src != "" {
			out = append(out, src+" ("+snap+")")
		}
	}

	switch v := e.Data["snapshots"].(type) {
	case []any:
		for _, s := range v {
			if m, ok := s.(map[string]any); ok {
				add(m)
			}
		}
	case []map[string]any:
		for _, m := range v {
			add(m)
		}
	}

	return out
}

// itemFile is one item's file.
func itemFile(x *evItem, numbers map[DefinitionVersionID]int) string {
	var b strings.Builder

	it := x.Item
	fmt.Fprintf(&b, "# #%d %s\n\n", it.Number, oneLine(it.Title))
	fmt.Fprintf(&b, "- Type: %s\n- Lane: %s\n- Origin: %s\n- Finished: %s, %s\n- Evidence: %s\n\n", orNone(it.Type), orNone(it.Lane),
		orNone(string(it.Origin)), it.State, stamped(x.FinishedAt), x.why())
	b.WriteString("## Body\n\n")
	b.WriteString(quoted(it.Body, 0))

	x.writeRuns(&b, numbers)
	x.writeSendBacks(&b)
	x.writeFailures(&b)
	x.writeOutliers(&b)
	x.writeQuestions(&b)
	x.writeNotes(&b)
	x.writeTouches(&b)

	return b.String()
}

func (x *evItem) writeRuns(b *strings.Builder, numbers map[DefinitionVersionID]int) {
	b.WriteString("\n## Runs\n\n")

	if len(x.runs) == 0 {
		b.WriteString("None.\n")
		return
	}

	b.WriteString("| Run | Station | Agent | Model | Version | Turns | Cost | Outcome | Knowledge |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")

	for i, r := range x.runs {
		outcome := string(r.Outcome)
		if c := x.causes[r.ID]; c != "" {
			outcome += " (" + c + ")"
		}

		knowledge := strings.Join(x.knowledge[r.ID], ", ")
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %d | %s | %s | %s |\n", cell(string(r.ID)), cell(r.Station), cell(r.Agent), cell(r.Model),
			versionName(x.Runs[i].DefinitionVersionID, numbers), r.Turns, usd(r.CostUSD), outcome, cell(cmp.Or(knowledge, "none")))
	}
}

func (x *evItem) writeSendBacks(b *strings.Builder) {
	if len(x.sendBacks) == 0 {
		return
	}

	b.WriteString("\n## Send-backs\n")

	for i, s := range x.sendBacks {
		sender := s.Sender
		if s.Sender != senderCheck {
			sender += " " + oneLine(s.Actor)
		}

		fmt.Fprintf(b, "\n### %d. %s at %s\n\n- Sender: %s\n- Category: %s\n- At: %s\n\n", i+1, s.Kind, s.Station, sender, oneLine(s.Category), stamped(s.At))
		b.WriteString(quoted(s.Reason, EvidenceTextLimit))
	}
}

func (x *evItem) writeFailures(b *strings.Builder) {
	if len(x.failures) == 0 {
		return
	}

	b.WriteString("\n## Failed runs\n")

	for i, f := range x.failures {
		fmt.Fprintf(b, "\n### %d. %s at %s\n\n- Run: %s\n- At: %s\n\n", i+1, f.Cause, f.Station, orNone(string(f.Run)), stamped(f.At))
		b.WriteString(quoted(f.Reason, EvidenceTextLimit))
	}
}

func (x *evItem) writeOutliers(b *strings.Builder) {
	if len(x.outliers) == 0 {
		return
	}

	b.WriteString("\n## Outliers\n\n")

	for _, o := range x.outliers {
		fmt.Fprintf(b, "- Run %s at %s: %s %s, %.1fx the station's 30-day median of %s\n", o.Run, o.Station, o.What, o.Value, o.Times, o.Median)
	}
}

func (x *evItem) writeQuestions(b *strings.Builder) {
	if len(x.questions) == 0 {
		return
	}

	b.WriteString("\n## Questions\n")

	for i, q := range x.questions {
		fmt.Fprintf(b, "\n### %d. Asked at %s by %s, %s\n\n", i+1, q.Station, orNone(oneLine(q.Asker)), stamped(q.At))
		b.WriteString(quoted(q.Question, EvidenceTextLimit))

		if !q.Answered {
			b.WriteString("\nNot answered.\n")
			continue
		}

		fmt.Fprintf(b, "\nAnswered by %s:\n\n", oneLine(q.AnsweredBy))
		b.WriteString(quoted(q.Answer, EvidenceTextLimit))
	}
}

func (x *evItem) writeNotes(b *strings.Builder) {
	if len(x.notes) == 0 {
		return
	}

	b.WriteString("\n## Notes\n")

	for i, n := range x.notes {
		fmt.Fprintf(b, "\n### %d. Note by %s at %s, %s\n\n", i+1, oneLine(n.By), n.Station, stamped(n.At))
		b.WriteString(quoted(n.Text, EvidenceTextLimit))
	}
}

func (x *evItem) writeTouches(b *strings.Builder) {
	if len(x.touches) == 0 {
		return
	}

	b.WriteString("\n## Touches\n\n| At | Kind | Station | By |\n| --- | --- | --- | --- |\n")

	for _, e := range x.touches {
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", stamped(e.At), e.Kind, cell(e.Station), cell(e.Actor))
	}
}

// readme is README.md: the window, the counts and the items with a file.
func readme(in EvidenceInput, finished int, items []*evItem) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Evidence\n\nItems that finished from %s to %s and needed help: %d of the %d items that finished. Each has a file in items/.\n",
		stamped(in.Since), stamped(in.Until), len(items), finished)

	byStation, bySender, byCategory, touches, failures, outliers := counter{}, counter{}, counter{}, counter{}, counter{}, counter{}

	for _, x := range items {
		for _, s := range x.sendBacks {
			byStation.add(s.Station)
			bySender.add(s.Sender)
			byCategory.add(s.Category)
		}

		for _, e := range x.touches {
			touches.add(string(e.Kind))
		}

		for _, f := range x.failures {
			failures.add(f.Cause)
		}

		for _, o := range x.outliers {
			outliers.add(o.Station)
		}
	}

	byStation.write(&b, "Send-backs by station", "Station", "Send-backs")
	bySender.write(&b, "Send-backs by sender", "Sender", "Send-backs")
	byCategory.write(&b, "Send-backs by category", "Category", "Send-backs")
	touches.write(&b, "Touches by kind", "Kind", "Touches")
	failures.write(&b, "Failed runs by cause", "Cause", "Runs")
	outliers.write(&b, "Outlier runs by station", "Station", "Runs")
	writeDisagreements(&b, items)

	b.WriteString("\n## Items\n\n")

	if len(items) == 0 {
		b.WriteString("None.\n")
		return b.String()
	}

	b.WriteString("| Item | Title | Evidence |\n| --- | --- | --- |\n")

	for _, x := range items {
		fmt.Fprintf(&b, "| [#%d](%s) | %s | %s |\n", x.Item.Number, EvidenceItemPath(x.Item.Number), cell(x.Item.Title), x.why())
	}

	return b.String()
}

// writeDisagreements writes the gate–person disagreements by gate station, with their items.
func writeDisagreements(b *strings.Builder, items []*evItem) {
	type row struct {
		past, back int
		items      []string
	}

	rows := map[string]*row{}

	for _, x := range items {
		for _, g := range slices.Sorted(maps.Keys(x.movedPast)) {
			r := cmp.Or(rows[g], &row{})
			r.past += x.movedPast[g]
			r.items, rows[g] = appendRef(r.items, x.Item.Number), r
		}

		for _, g := range slices.Sorted(maps.Keys(x.passSentBack)) {
			r := cmp.Or(rows[g], &row{})
			r.back += x.passSentBack[g]
			r.items, rows[g] = appendRef(r.items, x.Item.Number), r
		}
	}

	b.WriteString("\n## Gate–person disagreements\n\n")

	if len(rows) == 0 {
		b.WriteString("None.\n")
		return
	}

	b.WriteString("| Gate station | Moved past its send-back | Its pass sent back | Items |\n| --- | --- | --- | --- |\n")

	for _, g := range slices.Sorted(maps.Keys(rows)) {
		r := rows[g]
		fmt.Fprintf(b, "| %s | %d | %d | %s |\n", cell(g), r.past, r.back, strings.Join(r.items, ", "))
	}
}

func appendRef(refs []string, n int64) []string {
	ref := "#" + strconv.FormatInt(n, 10)
	if slices.Contains(refs, ref) {
		return refs
	}

	return append(refs, ref)
}

// versionsFile is versions.md: the versions published in the window, oldest first.
func versionsFile(in EvidenceInput) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Versions published from %s to %s\n", stamped(in.Since), stamped(in.Until))

	var published []DefinitionVersion

	for _, v := range in.Versions {
		if v.State == VersionPublished && v.Number > 0 && inEvidenceWindow(v.PublishedAt, in.Since, in.Until) {
			published = append(published, v)
		}
	}

	slices.SortFunc(published, func(a, b DefinitionVersion) int { return cmp.Compare(a.Number, b.Number) })

	if len(published) == 0 {
		b.WriteString("\nNone.\n")
		return b.String()
	}

	for _, v := range published {
		by := cmp.Or(v.PublishedBy.Name, v.PublishedBy.Ref())
		fmt.Fprintf(&b, "\n## v%d\n\nPublished %s by %s.\n\n", v.Number, stamped(v.PublishedAt), oneLine(by))
		b.WriteString(quoted(v.Message, EvidenceTextLimit))
	}

	return b.String()
}

// counter counts keys for a README table.
type counter map[string]int

func (c counter) add(key string) { c[key]++ }

// write writes c as a table under title, the highest count first, then by key; "None." when empty.
func (c counter) write(b *strings.Builder, title, key, count string) {
	fmt.Fprintf(b, "\n## %s\n\n", title)

	if len(c) == 0 {
		b.WriteString("None.\n")
		return
	}

	fmt.Fprintf(b, "| %s | %s |\n| --- | --- |\n", key, count)

	ks := slices.Collect(maps.Keys(c))
	slices.SortFunc(ks, func(a, b string) int { return cmp.Or(cmp.Compare(c[b], c[a]), cmp.Compare(a, b)) })

	for _, k := range ks {
		fmt.Fprintf(b, "| %s | %d |\n", cell(k), c[k])
	}
}

// quoted is text as a markdown block quote, each line verbatim after "> ", cut at limit bytes
// (0: whole) at a rune boundary with a line saying so; "None." for no text.
func quoted(text string, limit int) string {
	if text == "" {
		return "None.\n"
	}

	whole := len(text)
	cut := limit > 0 && whole > limit

	if cut {
		end := limit
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}

		text = text[:end]
	}

	var b strings.Builder

	for line := range strings.SplitSeq(text, "\n") {
		if line == "" {
			b.WriteString(">\n")
			continue
		}

		b.WriteString("> " + line + "\n")
	}

	if cut {
		fmt.Fprintf(&b, "\nCut at 4 KB of %d bytes.\n", whole)
	}

	return b.String()
}

// cell is s for a table cell: on one line, its pipes escaped.
func cell(s string) string { return strings.ReplaceAll(oneLine(s), "|", `\|`) }

func orNone(s string) string { return cmp.Or(s, "none") }

func stamped(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func usd(v float64) string { return "$" + strconv.FormatFloat(v, 'f', 2, 64) }

// versionName is a run's version as v<number> when known, else its id, else "unknown".
func versionName(id DefinitionVersionID, numbers map[DefinitionVersionID]int) string {
	if n, ok := numbers[id]; ok {
		return "v" + strconv.Itoa(n)
	}

	return cell(cmp.Or(string(id), "unknown"))
}
