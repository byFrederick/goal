package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// followed resolves the item refs an output lists in field, the property the line maps to
// flow.dependencies.after (ADR 0015), to the open items of the item's factory the work must
// follow. It only reads: a ref that is malformed, of another factory, unknown, done or
// cancelled, the item itself or its assembly, or one that already needs the item (an edge to it
// would close a cycle) is domain.ErrBadAfter naming the ref, so the output fails and nothing is
// written.
func (e *Engine) followed(ctx context.Context, c claimed, output []byte, field string) ([]domain.Item, error) {
	refs, err := domain.AfterFrom(output, field)
	if err != nil {
		return nil, err
	}

	items := make([]domain.Item, 0, len(refs))

	for _, ref := range refs {
		if ref.Factory != c.line.Name && ref.Factory != string(c.line.FactoryID) {
			return nil, fmt.Errorf("%w: %s is of another factory than %s", domain.ErrBadAfter, ref, c.line.Name)
		}

		it, err := e.store.ItemByNumber(ctx, c.line.FactoryID, ref.Number)
		if errors.Is(err, ports.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s is not an item", domain.ErrBadAfter, ref)
		}

		if err != nil {
			return nil, fmt.Errorf("after %s: %w", ref, err)
		}

		switch {
		case closed(it):
			return nil, fmt.Errorf("%w: %s is %s", domain.ErrBadAfter, ref, it.State)
		case it.ID == c.item.ID:
			return nil, fmt.Errorf("%w: %s is this item", domain.ErrBadAfter, ref)
		case it.ID == c.item.ParentID:
			return nil, fmt.Errorf("%w: %s is this item's assembly", domain.ErrBadAfter, ref)
		}

		cycle, err := e.reaches(ctx, it.ID, c.item.ID)
		if err != nil {
			return nil, fmt.Errorf("after %s: %w", ref, err)
		}

		if cycle {
			return nil, fmt.Errorf("%w: %s already waits for this item: %w", domain.ErrBadAfter, ref, domain.ErrCycle)
		}

		if !slices.ContainsFunc(items, func(i domain.Item) bool { return i.ID == it.ID }) {
			items = append(items, it)
		}
	}

	return items, nil
}

// reaches reports whether from needs to, directly or through other items.
func (e *Engine) reaches(ctx context.Context, from, to domain.ItemID) (bool, error) {
	seen := map[domain.ItemID]bool{from: true}
	next := []domain.ItemID{from}

	for len(next) > 0 {
		id := next[0]
		next = next[1:]

		edges, err := e.store.Edges(ctx, id)
		if err != nil {
			return false, fmt.Errorf("edges of %s: %w", id, err)
		}

		for _, edge := range edges.Needs {
			if edge.Needs == to {
				return true, nil
			}

			if !seen[edge.Needs] {
				seen[edge.Needs] = true
				next = append(next, edge.Needs)
			}
		}
	}

	return false, nil
}

// follow adds a linked edge by the agent from a single item to each item its output follows,
// once the output passed its checks and before finish moves the item, while the lease still
// keeps it from claims; a plan's root parts get
// theirs in Assemble instead. An edge an earlier attempt added (ports.ErrConflict) counts as
// added, so a retry is idempotent. followed checked for cycles outside the factory's lock, so an
// edge added since can still close one: that fails the output as an *outputError.
func (e *Engine) follow(ctx context.Context, c claimed, chk checked) error {
	if len(chk.plan) > 0 {
		return nil
	}

	for _, it := range chk.after {
		_, err := e.store.AddEdge(ctx, domain.NewEdge{
			Item: c.item.ID, Needs: it.ID, Kind: domain.EdgeLinked, By: agentActor(c), At: e.clock.Now(),
		})

		switch {
		case err == nil, errors.Is(err, ports.ErrConflict):
		case errors.Is(err, domain.ErrCycle):
			return &outputError{err: fmt.Errorf("%w: %s#%d already waits for this item: %w", domain.ErrBadAfter, c.line.Name, it.Number, err)}
		default:
			return fmt.Errorf("follow %s#%d: %w", c.line.Name, it.Number, err)
		}
	}

	return nil
}

// agentActor is the claim's agent as an actor, as the gateway's principal for its run is.
func agentActor(c claimed) domain.Actor {
	return domain.Actor{Kind: domain.ActorAgent, ID: c.agent.Name, Name: c.agent.Name}
}

func itemIDs(items []domain.Item) []domain.ItemID {
	ids := make([]domain.ItemID, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}

	return ids
}
