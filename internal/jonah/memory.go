package jonah

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
)

// memoryFile is the factory's memory at the root of its definition (ADR 0038 decision 6). Only
// the review's facts reach it, through memoryProposal; a definition edit of it is dropped.
const memoryFile = domain.MemoryProposalPath

// outputFact is a fact the review proposes for memory (ADR 0039 decision 7): the words of a
// person's event, the items (#<number>) whose events it quotes, and the texts of the facts it
// replaces.
type outputFact struct {
	Text     string   `json:"text"`
	Items    []string `json:"items"`
	Replaces []string `json:"replaces"`
}

// memoryEntry is one entry of memory.yaml, as memory.schema.json defines it.
type memoryEntry struct {
	Text  string   `json:"text"`
	From  []string `json:"from,omitempty"`
	Added string   `json:"added"`
}

// Reasons a fact is dropped, as `jonah fact dropped` logs them.
const (
	factEmpty    = "empty"
	factTooLong  = "too_long"
	factUnicode  = "unicode"
	factItems    = "items"
	factItem     = "item"
	factUnquoted = "unquoted"
	factExists   = "exists"
	factReplaces = "replaces"
	factLimit    = "limit"
	factEvidence = "evidence"
	factMemory   = "memory"
	factCheck    = "check"
)

// maxMemoryEvidence is the most evidence a proposal holds, as domain.NewProposal.Check allows.
const maxMemoryEvidence = 50

// memoryProposal is the one proposal of memory.yaml a review's facts make (ADR 0039 decision 7),
// and whether there is one. It builds on the version's memory.yaml and on the newest open
// proposal of memory.yaml that still applies, carrying that one's evidence and superseding every
// open proposal it overlaps. It keeps a fact, in order, only when its text, trimmed, is at most
// domain.MaxMemoryText characters with no hidden character, memory does not have it yet, it
// names at least one item, every item it names is one of the window's evidence items with a
// person's event (domain.PersonWords) that holds the text, whitespace runs collapsed, every
// replaced text is an entry of memory, and the file stays within domain.MaxMemoryEntries and
// domain.MaxMemoryBytes. A kept fact becomes an entry from its items (<factory>#<number>), added
// today, and one piece of evidence per quoted event. The file is checked as a proposal's edits
// are; drop is told each dropped fact's index, reason and detail, never its text.
func memoryProposal(c *definitionChecker, facts []outputFact, drop func(i int, reason, detail string)) (domain.NewProposal, bool) {
	if len(facts) == 0 {
		return domain.NewProposal{}, false
	}

	m, err := readMemoryBase(c.in)
	if err != nil {
		dropAll(facts, drop, factMemory, err.Error())

		return domain.NewProposal{}, false
	}

	n := domain.NewProposal{Evidence: m.evidence}
	entries, kept := m.current, 0
	items := evidenceIndex(c.in.evidence)

	for i, f := range facts {
		next, evidence, reason, detail := c.in.addFact(entries, items, f)
		if reason == "" && len(n.Evidence)+len(evidence) > maxMemoryEvidence {
			reason, detail = factEvidence, fmt.Sprintf("the proposal would cite more than %d events", maxMemoryEvidence)
		}

		if reason != "" {
			drop(i, reason, detail)

			continue
		}

		entries, n.Evidence = next, append(n.Evidence, evidence...)
		kept++
	}

	if kept == 0 {
		return domain.NewProposal{}, false
	}

	n.Edits = []domain.ProposalEdit{m.edit(encodeMemory(entries))}
	n.Title, n.Summary = memoryTitle(m.before, entries)

	if err := finishMemory(c, &n); err != nil {
		dropAll(facts, drop, factCheck, err.Error())

		return domain.NewProposal{}, false
	}

	return n, true
}

// memoryBase is what a memory proposal builds on: the version's memory.yaml (present says
// whether it has one) and its entries, and the entries and evidence of the open memory proposal
// it carries, when there is one.
type memoryBase struct {
	version  []byte
	present  bool
	before   []memoryEntry
	current  []memoryEntry
	evidence []domain.ProposalEvidence
}

func readMemoryBase(in reviewInput) (memoryBase, error) {
	m := memoryBase{}
	m.version, m.present = in.files[memoryFile]

	current := string(m.version)
	if open, after, ok := openMemory(in, m.present); ok {
		current, m.evidence = after, slices.Clone(open.Evidence)
	}

	var err error
	if m.before, err = readMemory(in.check, m.version, m.present); err != nil {
		return memoryBase{}, err
	}

	if m.current, err = readMemory(in.check, []byte(current), m.present || current != ""); err != nil {
		return memoryBase{}, err
	}

	return m, nil
}

// edit is the version's memory.yaml replaced whole by content, or content as a new file.
func (m memoryBase) edit(content string) domain.ProposalEdit {
	if m.present {
		return domain.ProposalEdit{Path: memoryFile, Replacements: []domain.Replacement{{Old: string(m.version), New: content}}}
	}

	return domain.ProposalEdit{Path: memoryFile, Content: content}
}

// finishMemory checks n's file as a proposal's edits are, and makes n supersede the open
// proposals it overlaps.
func finishMemory(c *definitionChecker, n *domain.NewProposal) error {
	if reason, detail := c.edits(n.Edits); reason != "" {
		return fmt.Errorf("%s: %s", reason, detail)
	}

	for _, o := range c.in.proposals {
		if domain.EditsOverlap(o.Edits, n.Edits, c.in.files) {
			n.Supersedes = append(n.Supersedes, o.ID)
		}
	}

	return n.Check()
}

// openMemory is the newest open proposal of memory.yaml alone that still applies to the
// version's file, and the file after it.
func openMemory(in reviewInput, present bool) (domain.Proposal, string, bool) {
	for _, o := range in.proposals { // newest first
		if len(o.Edits) != 1 || o.Edits[0].Path != memoryFile {
			continue
		}

		if after, err := o.Edits[0].Apply(in.files[memoryFile], present); err == nil {
			return o, after, true
		}
	}

	return domain.Proposal{}, "", false
}

// readMemory is raw's entries; none when the file is absent or empty.
func readMemory(check DefinitionCheck, raw []byte, present bool) ([]memoryEntry, error) {
	if !present || strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}

	doc, err := check.MemoryJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", memoryFile, err)
	}

	var entries []memoryEntry
	if err := json.Unmarshal(doc, &entries); err != nil {
		return nil, fmt.Errorf("%s is not a list of facts: %w", memoryFile, err)
	}

	return entries, nil
}

func dropAll(facts []outputFact, drop func(i int, reason, detail string), reason, detail string) {
	for i := range facts {
		drop(i, reason, detail)
	}
}

// addFact is entries with f added and the facts it replaces removed, and f's evidence; or why f
// is dropped and the detail.
func (in reviewInput) addFact(entries []memoryEntry, items map[string]domain.EvidenceItem, f outputFact) ([]memoryEntry, []domain.ProposalEvidence, string, string) {
	text := strings.TrimSpace(f.Text)
	if reason, detail := checkFactText(entries, text); reason != "" {
		return nil, nil, reason, detail
	}

	from, evidence, reason, detail := in.quotes(items, f.Items, text)
	if reason != "" {
		return nil, nil, reason, detail
	}

	next := slices.Clone(entries)

	for _, old := range f.Replaces {
		i := slices.IndexFunc(next, func(e memoryEntry) bool { return e.Text == old })
		if i < 0 {
			return nil, nil, factReplaces, "a fact it replaces is not in memory"
		}

		next = slices.Delete(next, i, i+1)
	}

	next = append(next, memoryEntry{Text: text, From: from, Added: in.today.UTC().Format(time.DateOnly)})

	if size := len(encodeMemory(next)); len(next) > domain.MaxMemoryEntries || size > domain.MaxMemoryBytes {
		return nil, nil, factLimit, fmt.Sprintf("memory would hold %d entries and %d bytes, more than %d or %d",
			len(next), size, domain.MaxMemoryEntries, domain.MaxMemoryBytes)
	}

	return next, evidence, "", ""
}

// checkFactText is why text may not be a fact of memory with entries, and the detail; "" when
// it may.
func checkFactText(entries []memoryEntry, text string) (string, string) {
	switch l := utf8.RuneCountInString(text); {
	case l == 0:
		return factEmpty, "the fact has no text"
	case l > domain.MaxMemoryText:
		return factTooLong, fmt.Sprintf("the fact is %d characters, more than %d", l, domain.MaxMemoryText)
	}

	if r, ok := domain.HiddenCharacter(text); ok {
		return factUnicode, fmt.Sprintf("it holds the invisible or bidirectional character %U", r)
	}

	if hasEntry(entries, text) {
		return factExists, "memory already has the fact"
	}

	return "", ""
}

// quotes are, for each item of refs, its ref as memory's from writes it and the evidence of its
// event that quotes text; or why the fact is dropped and the detail.
func (in reviewInput) quotes(items map[string]domain.EvidenceItem, refs []string, text string) ([]string, []domain.ProposalEvidence, string, string) {
	if len(refs) == 0 {
		return nil, nil, factItems, "the fact names no item"
	}

	from := make([]string, 0, len(refs))
	evidence := make([]domain.ProposalEvidence, 0, len(refs))

	for _, r := range refs {
		it, ok := items[strings.TrimPrefix(r, "#")]
		if !ok {
			return nil, nil, factItem, fmt.Sprintf("%q is not one of the window's evidence items", truncate(r, 40))
		}

		e, ok := quotedEvent(it, text)
		if !ok {
			return nil, nil, factUnquoted, fmt.Sprintf("no note, answer or send-back reason of a person on #%d holds the fact", it.Item.Number)
		}

		ref := in.factoryName + "#" + strconv.FormatInt(it.Item.Number, 10)
		if slices.Contains(from, ref) {
			continue
		}

		from = append(from, ref)
		evidence = append(evidence, domain.ProposalEvidence{ItemID: it.Item.ID, EventID: e.ID, Station: e.Station, Quote: text})
	}

	return from, evidence, "", ""
}

func hasEntry(entries []memoryEntry, text string) bool {
	return slices.ContainsFunc(entries, func(e memoryEntry) bool { return e.Text == text })
}

// evidenceIndex is items by their number, as a decimal string.
func evidenceIndex(items []domain.EvidenceItem) map[string]domain.EvidenceItem {
	out := make(map[string]domain.EvidenceItem, len(items))
	for _, it := range items {
		out[strconv.FormatInt(it.Item.Number, 10)] = it
	}

	return out
}

// quotedEvent is it's first event whose person's words hold text, whitespace runs collapsed in
// both.
func quotedEvent(it domain.EvidenceItem, text string) (domain.Event, bool) {
	want := collapse(text)

	for _, e := range it.Events {
		if words, ok := domain.PersonWords(e); ok && strings.Contains(collapse(words), want) {
			return e, true
		}
	}

	return domain.Event{}, false
}

// collapse is s with each run of whitespace one space, trimmed.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// encodeMemory is entries as memory.yaml: each string as a JSON string, which YAML reads as a
// double-quoted scalar. Comments of the file it replaces are not kept.
func encodeMemory(entries []memoryEntry) string {
	if len(entries) == 0 {
		return "[]\n"
	}

	var b strings.Builder

	for _, e := range entries {
		b.WriteString("- text: " + quote(e.Text) + "\n")

		if len(e.From) > 0 {
			refs := make([]string, len(e.From))
			for i, r := range e.From {
				refs[i] = quote(r)
			}

			b.WriteString("  from: [" + strings.Join(refs, ", ") + "]\n")
		}

		b.WriteString("  added: " + quote(e.Added) + "\n")
	}

	return b.String()
}

func quote(s string) string {
	raw, _ := json.Marshal(s) // a string always marshals

	return string(raw)
}

// memoryTitle is the memory proposal's title and summary: the facts it adds to the version's
// memory and those it removes.
func memoryTitle(before, after []memoryEntry) (string, string) {
	var added, removed []string

	for _, e := range after {
		if !hasEntry(before, e.Text) {
			added = append(added, e.Text)
		}
	}

	for _, e := range before {
		if !hasEntry(after, e.Text) {
			removed = append(removed, e.Text)
		}
	}

	title := fmt.Sprintf("Add %d %s to memory", len(added), plural(len(added), "fact"))
	if len(removed) > 0 {
		title += fmt.Sprintf(" and remove %d", len(removed))
	}

	var b strings.Builder

	b.WriteString("Facts quoted from people's notes, answers and send-back reasons, checked by goald.\n\nAdds:\n")

	for _, t := range added {
		b.WriteString("- " + t + "\n")
	}

	if len(removed) > 0 {
		b.WriteString("\nRemoves:\n")

		for _, t := range removed {
			b.WriteString("- " + t + "\n")
		}
	}

	return title, truncateRunes(b.String(), maxMemorySummary)
}

// maxMemorySummary is the most of a proposal's summary the store takes, in characters.
const maxMemorySummary = 4000

func plural(n int, word string) string {
	if n == 1 {
		return word
	}

	return word + "s"
}

// truncateRunes is s cut to at most n characters.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	return string([]rune(s)[:n])
}
