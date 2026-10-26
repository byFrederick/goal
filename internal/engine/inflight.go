package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// inFlightRead is how many of the factory's open items, newest first, an In flight section is
// ranked from: one ListItems page. Older open items beyond it are not listed; with at most
// domain.MaxInFlight entries shown, the newest page is what a planner needs.
const inFlightRead = 500

// openStates are the states of an item that is neither done nor cancelled.
var openStates = []domain.ItemState{
	domain.StateQueued, domain.StateReady, domain.StateInProgress,
	domain.StateWaitingHuman, domain.StateBlocked, domain.StateAssembly,
}

// inFlightNote is the `## In flight` section (ADR 0015 decision 1) for an item at a station
// that preloads domain.ContextInFlight: the factory's open items and assemblies other than the
// item, its assembly and its assembly's other parts, ranked against the item's work. An
// assembly's paths are the union of its open parts' part paths. It is empty at other stations.
func (e *Engine) inFlightNote(ctx context.Context, l domain.Line, st domain.Station, it domain.Item) (string, error) {
	if !st.Preloads(domain.ContextInFlight) {
		return "", nil
	}

	open, err := e.store.ListItems(ctx, ports.ItemQuery{FactoryID: it.FactoryID, States: openStates, Limit: inFlightRead})
	if err != nil {
		return "", fmt.Errorf("read the open items of factory %s: %w", it.FactoryID, err)
	}

	partPaths := map[domain.ItemID][]string{}
	for _, o := range open {
		if o.ParentID != "" {
			partPaths[o.ParentID] = append(partPaths[o.ParentID], o.PartPaths...)
		}
	}

	entries := make([]domain.InFlightEntry, 0, len(open))

	for _, o := range open {
		own := o.ID == it.ID || (it.ParentID != "" && (o.ID == it.ParentID || o.ParentID == it.ParentID))
		if own {
			continue
		}

		paths := o.Paths
		if o.State == domain.StateAssembly {
			paths = slices.Compact(slices.Sorted(slices.Values(partPaths[o.ID])))
		}

		entries = append(entries, domain.InFlightEntry{
			Ref: domain.ItemRef{Factory: l.Name, Number: o.Number}.String(), Title: o.Title,
			State: o.State, Station: o.Station, Paths: paths, UpdatedAt: o.UpdatedAt,
		})
	}

	return domain.InFlightSection(it.Work(), entries), nil
}
