package jonah

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
)

// reviewOutput is a review run's output, as goal-agent's jonah mode returns it
// (apps/worker/src/jonah.ts JonahReply).
type reviewOutput struct {
	Observations []outputObservation `json:"observations"`
	Proposals    []outputProposal    `json:"proposals"`
}

// outputObservation names its proposal by index in proposals, from 0; nil when it has none. ID
// is the open observation of observations.md it updates; empty for a new one. NotJonahs marks a
// cause that is not a definition problem.
type outputObservation struct {
	ID        string   `json:"id"`
	Summary   string   `json:"summary"`
	Detail    string   `json:"detail"`
	Items     []string `json:"items"`
	Count     int      `json:"count"`
	Proposal  *int     `json:"proposal"`
	NotJonahs bool     `json:"not_jonahs"`
}

type outputProposal struct {
	Title    string           `json:"title"`
	Summary  string           `json:"summary"`
	Edits    []outputEdit     `json:"edits"`
	Evidence []outputEvidence `json:"evidence"`
	Estimate outputEstimate   `json:"estimate"`
}

// outputEdit is one file's edit: replacements of an existing file, or the whole content of a
// new one.
type outputEdit struct {
	Path         string               `json:"path"`
	Replacements []domain.Replacement `json:"replacements"`
	Content      *string              `json:"content"`
}

// outputEvidence names a sent-back event by its id, as a decimal string (gateway jonahEvent).
type outputEvidence struct {
	Event  string `json:"event"`
	Reason string `json:"reason"`
}

// outputEstimate has no before: goald computes it.
type outputEstimate struct {
	Metric    domain.ProposalMetric `json:"metric"`
	Station   string                `json:"station"`
	After     float64               `json:"after"`
	CostDelta float64               `json:"cost_per_item_usd_delta"`
}

// parseReviewOutput reads a review run's output.
func parseReviewOutput(raw json.RawMessage) (reviewOutput, error) {
	var out reviewOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return reviewOutput{}, fmt.Errorf("the review's output does not parse: %w", err)
	}

	return out, nil
}

// reviewInput is what a review's output is checked against: the review, its policy's
// proposeAfter, the files of the version it read, the sent-back events of its window, the
// factory's open observations and proposals, the models the organization's policy allows, and
// the definition check.
type reviewInput struct {
	review       domain.JonahReview
	proposeAfter int
	files        map[string][]byte
	events       []domain.SentBack
	observations []domain.Observation
	proposals    []domain.Proposal
	models       []string
	check        DefinitionCheck
}

// Reasons a proposal is dropped, as `jonah proposal dropped` logs them.
const (
	dropUnicode   = "unicode"
	dropPath      = "path"
	dropEdit      = "edit"
	dropApply     = "apply"
	dropExists    = "exists"
	dropFactory   = "factory_path"
	dropCheck     = "check"
	dropEvidence  = "evidence"
	dropThreshold = "threshold"
	dropInvalid   = "invalid"
)

// maxQuote is the most a piece of evidence's quote keeps: the store's 500 characters.
const maxQuote = 500

// checkOutput keeps out's proposals that pass every check (ADR 0039 decisions 5, 6 and 20) and
// returns them with every observation, numbered onto the kept proposals. It drops a proposal,
// telling drop its index, reason and detail, for: an invisible or bidirectional character in its
// title, summary or edits; a path outside the allowlist; an edit with both or neither of
// replacements and content; an old that does not occur exactly once in the version's file, or
// replacements of a file it lacks; whole content for a file it has; a factory.yaml change at a
// path no proposal may change; a blocking problem config.Check finds in the edited files that the
// version does not have; evidence naming an event outside the window's; on a threshold review, no
// observation naming it whose count, with the open observation's it updates, reaches
// proposeAfter; or anything else the store would refuse. A kept proposal supersedes the open
// proposals its edits overlap. An observation naming no open observation is a new one; one whose
// proposal was dropped is watching. Each proposal's estimate has no before yet.
func checkOutput(in reviewInput, out reviewOutput, drop func(i int, reason, detail string)) ([]domain.NewProposal, []domain.NewObservation) {
	events, items := windowIndex(in.events)
	open := make(map[string]domain.Observation, len(in.observations))

	for _, o := range in.observations {
		open[string(o.ID)] = o
	}

	sighted := make([]bool, len(out.Proposals))

	for _, o := range out.Observations {
		if i, ok := in.names(o, open, len(sighted)); ok {
			sighted[i] = true
		}
	}

	var (
		kept    []domain.NewProposal
		number  = make([]int, len(out.Proposals))
		checker = definitionChecker{in: in}
	)

	for i, p := range out.Proposals {
		n, reason, detail := checkProposal(&checker, events, p)
		if reason == "" && in.review.Trigger == domain.JonahThreshold && !sighted[i] {
			reason, detail = dropThreshold, fmt.Sprintf("no observation of it reaches %d", in.proposeAfter)
		}

		if reason != "" {
			drop(i, reason, detail)

			continue
		}

		for _, o := range in.proposals {
			if domain.EditsOverlap(o.Edits, n.Edits, in.files) {
				n.Supersedes = append(n.Supersedes, o.ID)
			}
		}

		kept = append(kept, n)
		number[i] = len(kept)
	}

	return kept, observationsOf(in, out, open, number, items)
}

// observationsOf are out's observations to store: each with the open observation it names, when
// it names one, and number[i], the stored number of the proposal of index i it became.
func observationsOf(in reviewInput, out reviewOutput, open map[string]domain.Observation, number []int, items map[string]domain.ItemID) []domain.NewObservation {
	observations := make([]domain.NewObservation, 0, len(out.Observations))

	for _, o := range out.Observations {
		n := domain.NewObservation{Summary: o.Summary, Detail: o.Detail, ItemIDs: itemIDs(items, o.Items), Count: o.Count, NotJonahs: o.NotJonahs}
		if _, ok := open[o.ID]; ok {
			n.ID = domain.ObservationID(o.ID)
		}

		if i, ok := in.names(o, open, len(number)); ok {
			n.Proposal = number[i]
		}

		if n.Check() == nil {
			observations = append(observations, n)
		}
	}

	return observations
}

// names is the index of the proposal o names among n, and whether o may make it one: never as
// not Jonah's, and on a threshold review only when o's count, with the count of the open
// observation it updates, is proposeAfter or more (decision 5).
func (in reviewInput) names(o outputObservation, open map[string]domain.Observation, n int) (int, bool) {
	if o.Proposal == nil || *o.Proposal < 0 || *o.Proposal >= n || o.NotJonahs {
		return 0, false
	}

	count := o.Count
	if prior, ok := open[o.ID]; ok {
		count += prior.Count
	}

	return *o.Proposal, in.review.Trigger != domain.JonahThreshold || count >= in.proposeAfter
}

// windowIndex is the window's events by their id and its items by their number, as decimal
// strings.
func windowIndex(sent []domain.SentBack) (map[string]domain.SentBack, map[string]domain.ItemID) {
	events := make(map[string]domain.SentBack, len(sent))
	items := make(map[string]domain.ItemID, len(sent))

	for _, e := range sent {
		events[strconv.FormatInt(e.EventID, 10)] = e
		items[strconv.FormatInt(e.ItemNumber, 10)] = e.ItemID
	}

	return events, items
}

// checkProposal is p as a proposal to store, or why it is dropped and the detail.
func checkProposal(checker *definitionChecker, events map[string]domain.SentBack, p outputProposal) (domain.NewProposal, string, string) {
	n := domain.NewProposal{
		Title: p.Title, Summary: p.Summary,
		Estimate: domain.ProposalEstimate{Metric: p.Estimate.Metric, Station: p.Estimate.Station, After: p.Estimate.After, CostPerItemUSD: p.Estimate.CostDelta},
	}

	for _, e := range p.Edits {
		if !domain.ProposalPathAllowed(e.Path) {
			return domain.NewProposal{}, dropPath, e.Path + " is not a file a proposal may edit"
		}

		if (e.Content == nil) == (len(e.Replacements) == 0) {
			return domain.NewProposal{}, dropEdit, e.Path + " needs replacements or, for a new file, its content"
		}

		edit := domain.ProposalEdit{Path: e.Path, Replacements: e.Replacements}
		if e.Content != nil {
			edit.Content = *e.Content
		}

		n.Edits = append(n.Edits, edit)
	}

	if r, ok := n.HiddenCharacter(); ok {
		return domain.NewProposal{}, dropUnicode, fmt.Sprintf("it holds the invisible or bidirectional character %U", r)
	}

	if reason, detail := checker.edits(n.Edits); reason != "" {
		return domain.NewProposal{}, reason, detail
	}

	for _, e := range p.Evidence {
		s, ok := events[e.Event]
		if !ok {
			return domain.NewProposal{}, dropEvidence, "event " + e.Event + " is not one of the window's"
		}

		n.Evidence = append(n.Evidence, domain.ProposalEvidence{ItemID: s.ItemID, EventID: s.EventID, Station: s.Station, Quote: truncate(e.Reason, maxQuote)})
	}

	if err := n.Check(); err != nil {
		return domain.NewProposal{}, dropInvalid, err.Error()
	}

	return n, "", ""
}

// definitionChecker checks a review's edits against the files of the version it read, finding
// the version's own blocking problems once.
type definitionChecker struct {
	in   reviewInput
	base map[string]bool
}

// edits is why edits do not apply to the version or give a definition a proposal may not make,
// and the detail; "" when they apply.
func (c *definitionChecker) edits(edits []domain.ProposalEdit) (string, string) {
	files := maps.Clone(c.in.files)

	for _, e := range edits {
		content, ok := c.in.files[e.Path]
		if e.Whole() && ok {
			return dropExists, e.Path + " exists: give replacements of it, not its whole content"
		}

		applied, err := e.Apply(content, ok)
		if err != nil {
			return dropApply, err.Error()
		}

		files[e.Path] = []byte(applied)
	}

	if after, ok := files[factoryFile]; ok && !bytes.Equal(after, c.in.files[factoryFile]) {
		if problem := factoryProblem(c.in.check, c.in.files[factoryFile], after, c.in.models); problem != "" {
			return dropFactory, problem
		}
	}

	if c.base == nil {
		c.base = map[string]bool{}
		for _, p := range c.in.check.Problems(c.in.files) {
			c.base[p] = true
		}
	}

	for _, p := range c.in.check.Problems(files) {
		if !c.base[p] {
			return dropCheck, p
		}
	}

	return "", ""
}

// factoryFile is the definition's factory.yaml, whose paths a proposal may change only within
// the allowlist.
const factoryFile = "factory.yaml"

// itemIDs are the items of refs (#<number>, as the review's events name them) among the
// window's; a ref to no such item is left out.
func itemIDs(items map[string]domain.ItemID, refs []string) []domain.ItemID {
	out := []domain.ItemID{}

	for _, ref := range refs {
		if id, ok := items[strings.TrimPrefix(ref, "#")]; ok {
			out = append(out, id)
		}
	}

	return out
}
