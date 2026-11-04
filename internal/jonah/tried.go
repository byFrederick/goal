package jonah

import (
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// TriedState is where a proposal's tries of past items are, as its page shows them (ADR 0039
// decision 11).
type TriedState string

// Tried states: budget used while a try is not done and Jonah's budget is used up, so none is
// claimed and none is graded; otherwise trying while a try is queued or running, and done once
// every try is done.
const (
	TriedTrying     TriedState = "trying"
	TriedDone       TriedState = "done"
	TriedBudgetUsed TriedState = "budget_used"
)

// Tried is how a proposal did on its past items: its tries by item, its kept tests' tries (ADR
// 0039 decision 12), what they cost and how long they took.
type Tried struct {
	State TriedState
	// Items are the items the proposal cites that were tried, in the order their tries were queued.
	Items []TriedItem
	// KeptTests are its tries of kept tests, in the order they were queued.
	KeptTests []domain.PastTry
	CostUSD   float64
	// Duration is from the first try queued to the last one finished; zero until every try is done.
	Duration time.Duration
}

// TriedItem is one past item's tries, by attempt.
type TriedItem struct {
	ItemID  domain.ItemID
	Station string
	Tries   []domain.PastTry
}

// TriedOf groups tries, oldest first as ports.PastTries reads them, by item, with budget as
// Jonah's budget over domain.JonahBudgetWindow now; false when there are none.
func TriedOf(tries []domain.PastTry, budget domain.JonahBudget) (Tried, bool) {
	if len(tries) == 0 {
		return Tried{}, false
	}

	var (
		out         = Tried{State: triedState(tries, budget)}
		at          = map[domain.ItemID]int{}
		first, last time.Time
	)

	for _, t := range tries {
		out.CostUSD += t.CostUSD

		if first.IsZero() || t.CreatedAt.Before(first) {
			first = t.CreatedAt
		}

		if t.FinishedAt.After(last) {
			last = t.FinishedAt
		}

		if t.KeptTestID != "" {
			out.KeptTests = append(out.KeptTests, t)

			continue
		}

		i, ok := at[t.ItemID]
		if !ok {
			i, at[t.ItemID] = len(out.Items), len(out.Items)
			out.Items = append(out.Items, TriedItem{ItemID: t.ItemID, Station: t.Station})
		}

		out.Items[i].Tries = append(out.Items[i].Tries, t)
	}

	for _, it := range out.Items {
		slices.SortStableFunc(it.Tries, func(a, b domain.PastTry) int { return a.Attempt - b.Attempt })
	}

	if out.State == TriedDone {
		out.Duration = last.Sub(first)
	}

	return out, true
}

// triedState is where tries are with budget as Jonah's budget now.
func triedState(tries []domain.PastTry, budget domain.JonahBudget) TriedState {
	in := func(st domain.PastTryState) bool {
		return slices.ContainsFunc(tries, func(t domain.PastTry) bool { return t.State == st })
	}

	switch open := in(domain.PastTryQueued) || in(domain.PastTryRunning); {
	case open && budget.Exhausted():
		return TriedBudgetUsed
	case open:
		return TriedTrying
	default:
		return TriedDone
	}
}
