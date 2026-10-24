package domain

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// ComponentKind is a kind of time an item spends on its way to done, as the lead-time bridge
// attributes it.
type ComponentKind string

// Component kinds, in the order the bridge breaks ties.
const (
	// ComponentWaiting is time queued or ready at a station, in the item's first round.
	ComponentWaiting ComponentKind = "waiting"
	// ComponentRework is any time in a rework round, by the station that sent the item back.
	ComponentRework ComponentKind = "rework"
	// ComponentWaitingOnYou is time waiting for a person's decision, in the first round.
	ComponentWaitingOnYou ComponentKind = "waiting_on_you"
	// ComponentBlocked is time blocked at a station, in the first round.
	ComponentBlocked ComponentKind = "blocked"
	// ComponentWorking is time in progress at a station, in the first round.
	ComponentWorking ComponentKind = "working"
	// ComponentOther gathers the bridge's steps too small to show on their own.
	ComponentOther ComponentKind = "other"
)

var componentOrder = []ComponentKind{
	ComponentWaiting, ComponentRework, ComponentWaitingOnYou, ComponentBlocked, ComponentWorking, ComponentOther,
}

// Component is one kind of time at one station ("" for waiting on you and other).
type Component struct {
	Kind    ComponentKind
	Station string
}

// ComponentOf is where a span's time goes: rework by the station that sent the item back when
// the span is in a rework round, else its state at its station.
func ComponentOf(sp Span) Component {
	if sp.ReworkBy != "" {
		return Component{Kind: ComponentRework, Station: sp.ReworkBy}
	}

	switch sp.State {
	case StateWaitingHuman:
		return Component{Kind: ComponentWaitingOnYou}
	case StateBlocked:
		return Component{Kind: ComponentBlocked, Station: sp.Station}
	case StateInProgress:
		return Component{Kind: ComponentWorking, Station: sp.Station}
	case StateQueued, StateReady, StateDone, StateCancelled:
		// Waiting; an item done or cancelled has no span.
	}

	return Component{Kind: ComponentWaiting, Station: sp.Station}
}

// ItemTime is an item done in a window: its lead time (created to done) and its time in each
// component.
type ItemTime struct {
	Item     ItemID
	LeadTime time.Duration
	Times    map[Component]time.Duration
}

// Bridge explains the gap between the actual p85 lead time and the target, largest step
// first; its steps sum exactly to the gap.
type Bridge struct {
	Steps []BridgeStep
}

// BridgeStep is one component's part of the gap.
type BridgeStep struct {
	Component Component
	Duration  time.Duration
	// Share is Duration over the gap.
	Share float64
	// Items counts the items over the target with time in the component.
	Items int
}

// Bridge rules (E §6.5).
const (
	// bridgeMinItems is how many items done the bridge needs.
	bridgeMinItems = 5
	// bridgeMinShare is the share of the gap under which a step merges into other.
	bridgeMinShare = 0.03
)

// BridgeLeadTime attributes the gap between the actual p85 lead time and the target to the
// components of time of the items done. Items over the target are slow, the rest fast; each
// component's excess is its mean time over slow items less its mean over fast items (with no
// fast items: its whole mean for a delay, nothing for working), negatives dropped, and its step
// is its excess's share of the gap. Steps under 3% of the gap merge into other. It returns no
// steps when actual ≤ target or fewer than 5 items were done.
func BridgeLeadTime(items []ItemTime, target, actualP85 time.Duration) Bridge {
	gap := actualP85 - target
	if gap <= 0 || len(items) < bridgeMinItems {
		return Bridge{}
	}

	var slow, fast []ItemTime

	for _, it := range items {
		if it.LeadTime > target {
			slow = append(slow, it)
		} else {
			fast = append(fast, it)
		}
	}

	excess := map[Component]float64{}
	total := 0.0

	for _, c := range componentsOf(slow) {
		e := meanOf(slow, c)

		switch {
		case len(fast) > 0:
			e -= meanOf(fast, c)
		case c.Kind == ComponentWorking:
			e = 0
		}

		if e > 0 {
			excess[c] = e
			total += e
		}
	}

	if total == 0 {
		return Bridge{Steps: []BridgeStep{{Component: Component{Kind: ComponentOther}, Duration: gap, Share: 1, Items: len(slow)}}}
	}

	return Bridge{Steps: steps(excess, total, gap, slow)}
}

// steps splits gap by excess (over total), merging the small steps into other, and gives the
// rounding to the largest step so the steps sum exactly to gap.
func steps(excess map[Component]float64, total float64, gap time.Duration, slow []ItemTime) []BridgeStep {
	var (
		out    []BridgeStep
		merged []Component
		rest   float64
	)

	for c, e := range excess {
		share := e / total
		if share < bridgeMinShare {
			merged = append(merged, c)
			rest += share

			continue
		}

		out = append(out, BridgeStep{Component: c, Share: share, Items: slowWith(slow, c)})
	}

	slices.SortFunc(out, func(a, b BridgeStep) int {
		return cmp.Or(cmp.Compare(b.Share, a.Share), compareComponents(a.Component, b.Component))
	})

	if len(merged) > 0 {
		out = append(out, BridgeStep{Component: Component{Kind: ComponentOther}, Share: rest, Items: slowWith(slow, merged...)})
	}

	var sum time.Duration

	for i := range out {
		out[i].Duration = time.Duration(math.Round(float64(gap) * out[i].Share))
		sum += out[i].Duration
	}

	out[0].Duration += gap - sum

	return out
}

func compareComponents(a, b Component) int {
	return cmp.Or(
		cmp.Compare(slices.Index(componentOrder, a.Kind), slices.Index(componentOrder, b.Kind)),
		cmp.Compare(a.Station, b.Station),
	)
}

// componentsOf lists every component the items have time in, in a stable order.
func componentsOf(items []ItemTime) []Component {
	seen := map[Component]bool{}

	var out []Component

	for _, it := range items {
		for c := range it.Times {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}

	slices.SortFunc(out, compareComponents)

	return out
}

// meanOf is the mean time in c over items, in nanoseconds.
func meanOf(items []ItemTime, c Component) float64 {
	var total float64
	for _, it := range items {
		total += float64(it.Times[c])
	}

	return total / float64(len(items))
}

// slowWith counts the items with time in any of cs.
func slowWith(items []ItemTime, cs ...Component) int {
	n := 0

	for _, it := range items {
		if slices.ContainsFunc(cs, func(c Component) bool { return it.Times[c] > 0 }) {
			n++
		}
	}

	return n
}

// Percentile interpolates linearly between the closest ranks of ds, as PostgreSQL's
// percentile_cont does; no values give zero.
func Percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}

	sorted := slices.Sorted(slices.Values(ds))
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)

	return sorted[lo] + time.Duration(math.Round((pos-float64(lo))*float64(sorted[hi]-sorted[lo])))
}
