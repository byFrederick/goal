package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The watch on every published change (ADR 0039 decisions 14 and 15): on versions, on a
// proposal, as a Needs you entry when a proposal's own number got worse, and Undo this change.

// errUndoOpen is the refusal of an undo while one of the proposal is proposed, published or applied.
var errUndoOpen = errors.New("an undo of this proposal is already proposed, published or applied")

// watchProblem answers Undo this change's refusals.
func watchProblem(err error) (gen.Problem, bool) {
	var code string

	switch {
	case errors.Is(err, errUndoOpen):
		code = "undo-open"
	case errors.Is(err, domain.ErrProposalNotPublished):
		code = "proposal-not-published"
	case errors.Is(err, domain.ErrUndoConflict):
		code = "undo-conflict"
	default:
		return gen.Problem{}, false
	}

	p := detailed(http.StatusConflict, err)
	p.Code = &code

	return p, true
}

func (s *server) UndoProposal(ctx context.Context, req gen.UndoProposalRequestObject) (gen.UndoProposalResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if _, err := s.proposal(ctx, rec.ID, req.Number); err != nil {
		return nil, err
	}

	undo, err := s.jonah.Decisions.Undo(ctx, rec.ID, req.Number)
	if errors.Is(err, ports.ErrConflict) {
		return nil, fmt.Errorf("undo proposal %d: %w", req.Number, errUndoOpen)
	}

	if err != nil {
		return nil, err
	}

	out, err := s.proposalOut(ctx, rec, undo)
	if err != nil {
		return nil, err
	}

	return gen.UndoProposal201JSONResponse(out), nil
}

// watchOut is w as the API shows it.
func watchOut(w domain.Watch) gen.Watch {
	out := gen.Watch{
		Station: w.Station, Metric: gen.WatchMetric(w.Metric), Verdict: gen.WatchVerdict(w.Verdict), OpenedAt: w.OpenedAt,
		Before: gen.WatchSide{Items: w.Before.N, Value: w.Before.Value}, After: gen.WatchSide{Items: w.After.N, Value: w.After.Value},
	}

	if w.Interval != nil {
		out.Interval = &gen.WatchInterval{Low: w.Interval.Low, High: w.Interval.High}
	}

	if !w.ClosedAt.IsZero() {
		at := w.ClosedAt
		out.ClosedAt = &at
	}

	if w.ProposalNumber > 0 {
		n := w.ProposalNumber
		out.Proposal = &n
	}

	return out
}

// watches reads the watches the API shows; nil when this goald serves no Jonah store.
func (s *server) watches() ports.Watches {
	if s.jonah == nil {
		return nil
	}

	return s.jonah.Watches
}

// versionWatches are the watches of f's versions, by version: empty for every version asked
// when this goald keeps none.
func (s *server) versionWatches(ctx context.Context, f domain.FactoryID, versions []domain.DefinitionVersionID) (map[domain.DefinitionVersionID][]gen.Watch, error) {
	out := make(map[domain.DefinitionVersionID][]gen.Watch, len(versions))
	for _, v := range versions {
		out[v] = []gen.Watch{}
	}

	ws := s.watches()
	if ws == nil || len(versions) == 0 {
		return out, nil
	}

	list, err := ws.VersionWatches(ctx, f, versions)
	if err != nil {
		return nil, fmt.Errorf("read watches: %w", err)
	}

	for _, w := range list {
		out[w.VersionID] = append(out[w.VersionID], watchOut(w))
	}

	return out, nil
}

// withWatches sets each published version's watches.
func (s *server) withWatches(ctx context.Context, f domain.FactoryID, versions []gen.Version) error {
	ids := make([]domain.DefinitionVersionID, 0, len(versions))

	for _, v := range versions {
		if v.State == gen.VersionStatePublished {
			ids = append(ids, domain.DefinitionVersionID(v.Id))
		}
	}

	by, err := s.versionWatches(ctx, f, ids)
	if err != nil {
		return err
	}

	for i, v := range versions {
		if ws, ok := by[domain.DefinitionVersionID(v.Id)]; ok {
			versions[i].Watches = &ws
		}
	}

	return nil
}

// proposalWatch is the watch of p's own number, nil before there is one.
func (s *server) proposalWatch(ctx context.Context, p domain.Proposal) (*gen.Watch, error) {
	ws := s.watches()
	if ws == nil || p.PublishedVersionID == "" {
		return nil, nil
	}

	list, err := ws.ProposalWatches(ctx, []domain.ProposalID{p.ID})
	if err != nil {
		return nil, fmt.Errorf("read the watch of proposal %d: %w", p.Number, err)
	}

	i := slices.IndexFunc(list, func(w domain.Watch) bool {
		return w.Metric == p.Estimate.Metric && w.Station == p.Estimate.Station
	})
	if i < 0 {
		return nil, nil
	}

	list[i].ProposalNumber = p.Number
	out := watchOut(list[i])

	return &out, nil
}

// watchEntries are the Jonah proposals of the factories whose own number got worse and that
// no undo reverses yet (ADR 0039 decision 15): one watch_worse entry each, about no item. A
// person's publish never raises one.
func (w workInbox) watchEntries(ctx context.Context, factories []*config.Factory, limit int) ([]gen.InboxEntry, error) {
	ws := w.s.watches()
	if ws == nil {
		return nil, nil
	}

	list, err := ws.WorseProposalWatches(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("read worse watches: %w", err)
	}

	out := []gen.InboxEntry{}

	for _, watch := range list {
		i := slices.IndexFunc(factories, func(f *config.Factory) bool { return domain.FactoryID(f.Metadata.ID) == watch.FactoryID })
		if i < 0 || watch.ProposalID == "" {
			continue
		}

		e := entryOf(factories[i], gen.InboxEntryKindWatchWorse, string(watch.ProposalID), watch.UpdatedAt)
		e.Station = optional(watch.Station)
		wo := watchOut(watch)
		e.Watch = &wo
		out = append(out, e)
	}

	return out, nil
}
