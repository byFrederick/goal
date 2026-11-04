package jonah

import (
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// TriedState is where a proposal's tries of past items are, as its page shows them (ADR 0039
// decision 11).
type TriedState string

// Tried states: budget used while a try is still queued and Jonah's budget is used up, so none
// is claimed; otherwise trying while a try is queued or running, and done once every try is done.
const (
	TriedTrying     TriedState = "trying"
	TriedDone       TriedState = "done"
	TriedBudgetUsed TriedState = "budget_used"
)

// Tried is how a proposal did on its past items: its tries by item, what they cost and how long
// they took.
type Tried struct {
	State TriedState
	// Items are the items tried, in the order their tries were queued.
	Items   []TriedItem
	CostUSD float64
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
		out             = Tried{State: TriedDone}
		at              = map[domain.ItemID]int{}
		queued, running bool
		first, last     time.Time
	)

	for _, t := range tries {
		i, ok := at[t.ItemID]
		if !ok {
			i, at[t.ItemID] = len(out.Items), len(out.Items)
			out.Items = append(out.Items, TriedItem{ItemID: t.ItemID, Station: t.Station})
		}

		out.Items[i].Tries = append(out.Items[i].Tries, t)
		out.CostUSD += t.CostUSD
		queued = queued || t.State == domain.PastTryQueued
		running = running || t.State == domain.PastTryRunning

		if first.IsZero() || t.CreatedAt.Before(first) {
			first = t.CreatedAt
		}

		if t.FinishedAt.After(last) {
			last = t.FinishedAt
		}
	}

	for _, it := range out.Items {
		slices.SortStableFunc(it.Tries, func(a, b domain.PastTry) int { return a.Attempt - b.Attempt })
	}

	switch {
	case queued && budget.Exhausted():
		out.State = TriedBudgetUsed
	case queued || running:
		out.State = TriedTrying
	default:
		out.Duration = last.Sub(first)
	}

	return out, true
}
