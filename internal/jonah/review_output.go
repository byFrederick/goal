package jonah

import (
	"encoding/json"
	"fmt"
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

// outputObservation names its proposal by index in proposals, from 0; nil when it has none.
type outputObservation struct {
	Summary  string   `json:"summary"`
	Detail   string   `json:"detail"`
	Items    []string `json:"items"`
	Count    int      `json:"count"`
	Proposal *int     `json:"proposal"`
}

type outputProposal struct {
	Title    string                `json:"title"`
	Summary  string                `json:"summary"`
	Files    []domain.ProposalFile `json:"files"`
	Evidence []outputEvidence      `json:"evidence"`
	Estimate outputEstimate        `json:"estimate"`
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
// proposeAfter, the files of the version it read and the sent-back events of its window.
type reviewInput struct {
	review       domain.JonahReview
	proposeAfter int
	files        map[string][]byte
	events       []domain.SentBack
}

// Reasons a proposal is dropped, as `jonah proposal dropped` logs them.
const (
	dropPath      = "path"
	dropBase      = "base_sha256"
	dropSize      = "size"
	dropEvidence  = "evidence"
	dropThreshold = "threshold"
	dropInvalid   = "invalid"
)

// maxQuote is the most a piece of evidence's quote keeps: the store's 500 characters.
const maxQuote = 500

// checkOutput keeps out's proposals that pass every check (ADR 0034 decisions 3 to 5) and
// returns them with every observation, numbered onto the kept proposals. It drops a proposal,
// telling drop its index and reason, for: a path outside the allowlist; a base_sha256 that is not
// the file's SHA-256 in the version (empty only for a file the version lacks); contents over
// domain.MaxProposalFilesBytes; evidence naming an event outside the window's; on a threshold
// review, no observation naming it with a count of at least proposeAfter; or anything else the
// store would refuse. An observation whose proposal was dropped is watching. Each proposal's
// estimate has no before yet.
func checkOutput(in reviewInput, out reviewOutput, drop func(i int, reason string)) ([]domain.NewProposal, []domain.NewObservation) {
	events := make(map[string]domain.SentBack, len(in.events))
	items := make(map[string]domain.ItemID, len(in.events))

	for _, e := range in.events {
		events[strconv.FormatInt(e.EventID, 10)] = e
		items[strconv.FormatInt(e.ItemNumber, 10)] = e.ItemID
	}

	seen := make([]bool, len(out.Proposals))

	for _, o := range out.Observations {
		if o.Proposal != nil && *o.Proposal >= 0 && *o.Proposal < len(seen) && o.Count >= in.proposeAfter {
			seen[*o.Proposal] = true
		}
	}

	var (
		kept   []domain.NewProposal
		number = make([]int, len(out.Proposals))
	)

	for i, p := range out.Proposals {
		n, reason := checkProposal(in, events, p)
		if reason == "" && in.review.Trigger == domain.JonahThreshold && !seen[i] {
			reason = dropThreshold
		}

		if reason != "" {
			drop(i, reason)

			continue
		}

		kept = append(kept, n)
		number[i] = len(kept)
	}

	observations := make([]domain.NewObservation, 0, len(out.Observations))

	for _, o := range out.Observations {
		n := domain.NewObservation{Summary: o.Summary, Detail: o.Detail, ItemIDs: itemIDs(items, o.Items), Count: o.Count}
		if o.Proposal != nil && *o.Proposal >= 0 && *o.Proposal < len(number) &&
			(in.review.Trigger != domain.JonahThreshold || o.Count >= in.proposeAfter) {
			n.Proposal = number[*o.Proposal]
		}

		if n.Check() != nil {
			continue
		}

		observations = append(observations, n)
	}

	return kept, observations
}

// checkProposal is p as a proposal to store, or why it is dropped.
func checkProposal(in reviewInput, events map[string]domain.SentBack, p outputProposal) (domain.NewProposal, string) {
	total := 0

	for _, f := range p.Files {
		if !domain.ProposalPathAllowed(f.Path) {
			return domain.NewProposal{}, dropPath
		}

		content, ok := in.files[f.Path]
		if ok && f.BaseSHA256 != string(domain.HashBlob(content)) || !ok && f.BaseSHA256 != "" {
			return domain.NewProposal{}, dropBase
		}

		total += len(f.Content)
	}

	if total > domain.MaxProposalFilesBytes {
		return domain.NewProposal{}, dropSize
	}

	evidence := make([]domain.ProposalEvidence, 0, len(p.Evidence))

	for _, e := range p.Evidence {
		s, ok := events[e.Event]
		if !ok {
			return domain.NewProposal{}, dropEvidence
		}

		evidence = append(evidence, domain.ProposalEvidence{ItemID: s.ItemID, EventID: s.EventID, Station: s.Station, Quote: truncate(e.Reason, maxQuote)})
	}

	n := domain.NewProposal{
		Title: p.Title, Summary: p.Summary, Files: p.Files, Evidence: evidence,
		Estimate: domain.ProposalEstimate{Metric: p.Estimate.Metric, Station: p.Estimate.Station, After: p.Estimate.After, CostPerItemUSD: p.Estimate.CostDelta},
	}

	if n.Check() != nil {
		return domain.NewProposal{}, dropInvalid
	}

	return n, ""
}

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
