package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Watches keeps the watch on every published change (ADR 0039 decisions 14 and 15) and reads
// the station outcomes it compares, for the organization in ctx; *postgres.Store implements it.
type Watches interface {
	// StationOutcomes is each item's outcome at station on each of versions, oldest first: the
	// item's runs at the station on one version, from the first until its first run there on
	// another version, once none of them runs and the item left the station. The outcome is
	// credited to that version, whatever version runs elsewhere.
	StationOutcomes(ctx context.Context, factory domain.FactoryID, station string, versions []domain.DefinitionVersionID) ([]domain.StationOutcome, error)
	// SaveWatch writes w, keyed by its factory, version, station and metric, while that watch is
	// open; saved is false when it closed before, and then nothing changes.
	SaveWatch(ctx context.Context, w domain.Watch) (saved bool, err error)
	// VersionWatches are the watches of the factory's versions, by version, station and metric.
	VersionWatches(ctx context.Context, factory domain.FactoryID, versions []domain.DefinitionVersionID) ([]domain.Watch, error)
	// ProposalWatches are the watches of proposals' own numbers.
	ProposalWatches(ctx context.Context, proposals []domain.ProposalID) ([]domain.Watch, error)
	// WorseProposalWatches are the organization's watches of a proposal's own number whose
	// verdict is worse and that no undo proposal (proposed, published or applied) reverses yet,
	// most recently updated first, at most limit, each with its ProposalNumber.
	WorseProposalWatches(ctx context.Context, limit int) ([]domain.Watch, error)
}
