package jonah

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// TryAgainStore is what Try again reads and writes; *postgres.Store and *porttest.Store satisfy
// it, as ports.Store, ports.JonahStore and ports.PastTries.
type TryAgainStore interface {
	Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error)
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (domain.JonahSpend, error)
	ProposalPastTries(ctx context.Context, proposal domain.ProposalID) ([]domain.PastTry, error)
	AddPastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, adds []domain.PastTryAdd, at time.Time) ([]domain.PastTry, error)
	Run(ctx context.Context, id domain.RunID) (domain.Run, error)
}

// ProposedLines reads what a proposal changes; TryDefinitions satisfies it.
type ProposedLines interface {
	ProposedLine(ctx context.Context, version domain.DefinitionVersionID, edits []domain.ProposalEdit) (domain.Line, []string, error)
}

// TryAgainDeps are the ports Try again works through. Log may be nil.
type TryAgainDeps struct {
	Store       TryAgainStore
	Definitions ProposedLines
	Audit       ports.Audit
	Clock       ports.Clock
	Log         *slog.Logger
}

// TryAgain adds tries to an open proposal (ADR 0039 decision 11), as the person in ctx.
type TryAgain struct {
	d   TryAgainDeps
	log *slog.Logger
}

// NewTryAgain returns Try again over d.
func NewTryAgain(d TryAgainDeps) *TryAgain {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &TryAgain{d: d, log: log}
}

// TryAgainEstimate is what Try again adds and costs: per item the proposal was tried on, its
// station and the tries it adds, each priced at the item's original run's cost, and what is left
// of Jonah's budget (ADR 0039 decision 16).
type TryAgainEstimate struct {
	Number        int64
	Items         []TryAgainItem
	Tries         int
	EstimateUSD   float64
	BudgetLeftUSD float64
}

// TryAgainItem is one item's share of a TryAgainEstimate.
type TryAgainItem struct {
	ItemID     domain.ItemID
	Station    string
	Tries      int
	RunCostUSD float64
}

// Try estimates Try again of f's proposal number and, with confirm, queues the tries: one more per
// item the proposal was tried on, or domain.ModelTriesPerItem more for a model proposal, each a
// copy of the item's latest try at the attempts after it (ports.PastTries.AddPastTries), queued
// like the proposal's first ones. Confirmed, it is audited. policy is the factory's Jonah policy
// now, whose share of the line's spend over BudgetWindow bounds the cost.
//
// It refuses with ports.ErrNotFound for no such proposal, ports.ErrProposalDecided for one not
// proposed, domain.ErrProposalStale when its edits no longer apply, domain.ErrProposalNotTriable
// when it reaches no station that can be tried (a memory-only or flow-only one),
// domain.ErrProposalNotTried when it has no tries yet, and domain.ErrLearningBudgetExhausted, with
// the estimate, when the estimate is more than what is left of Jonah's budget.
func (a *TryAgain) Try(ctx context.Context, f domain.FactoryID, number int64, policy domain.JonahPolicy, confirm bool) (TryAgainEstimate, error) {
	p, err := a.d.Store.Proposal(ctx, f, number)
	if err != nil {
		return TryAgainEstimate{}, fmt.Errorf("try again: read proposal %d: %w", number, err)
	}

	if p.State != domain.ProposalProposed {
		return TryAgainEstimate{}, fmt.Errorf("try again: proposal %d is %s: %w", number, p.State, ports.ErrProposalDecided)
	}

	if err := a.triable(ctx, p); err != nil {
		return TryAgainEstimate{}, err
	}

	est, adds, err := a.estimate(ctx, p, policy)
	if err != nil || !confirm {
		return est, err
	}

	return est, a.queue(ctx, p, est, adds)
}

// triable refuses p unless what it changes reaches a station that can be tried.
func (a *TryAgain) triable(ctx context.Context, p domain.Proposal) error {
	line, reached, err := a.d.Definitions.ProposedLine(ctx, p.VersionID, p.Edits)
	if errors.Is(err, domain.ErrEditDoesNotApply) {
		return fmt.Errorf("try again: proposal %d: %w: %w", p.Number, domain.ErrProposalStale, err)
	}

	if err != nil {
		return fmt.Errorf("try again: read what proposal %d changes: %w", p.Number, err)
	}

	if !slices.ContainsFunc(reached, func(st string) bool { return stationTriable(line, st) }) {
		return fmt.Errorf("try again: proposal %d: %w", p.Number, domain.ErrProposalNotTriable)
	}

	return nil
}

// estimate is what Try again of p adds, priced, with the adds to queue.
func (a *TryAgain) estimate(ctx context.Context, p domain.Proposal, policy domain.JonahPolicy) (TryAgainEstimate, []domain.PastTryAdd, error) {
	tries, err := a.d.Store.ProposalPastTries(ctx, p.ID)
	if err != nil {
		return TryAgainEstimate{}, nil, fmt.Errorf("try again: read the tries of proposal %d: %w", p.Number, err)
	}

	tried, ok := TriedOf(tries, domain.JonahBudget{})
	if !ok {
		return TryAgainEstimate{}, nil, fmt.Errorf("try again: proposal %d: %w", p.Number, domain.ErrProposalNotTried)
	}

	per := 1
	if p.Agent != "" {
		per = domain.ModelTriesPerItem
	}

	est := TryAgainEstimate{Number: p.Number}
	adds := make([]domain.PastTryAdd, 0, len(tried.Items))

	for _, it := range tried.Items {
		latest := it.Tries[len(it.Tries)-1]

		run, err := a.d.Store.Run(ctx, latest.OriginalRunID)
		if err != nil {
			return TryAgainEstimate{}, nil, fmt.Errorf("try again: read run %s of item %s: %w", latest.OriginalRunID, it.ItemID, err)
		}

		est.Items = append(est.Items, TryAgainItem{ItemID: it.ItemID, Station: latest.Station, Tries: per, RunCostUSD: run.Result.CostUSD})
		est.Tries += per
		est.EstimateUSD += float64(per) * run.Result.CostUSD
		adds = append(adds, domain.PastTryAdd{ItemID: it.ItemID, Tries: per})
	}

	spend, err := a.d.Store.JonahSpendSince(ctx, p.FactoryID, a.d.Clock.Now().UTC().Add(-BudgetWindow))
	if err != nil {
		return TryAgainEstimate{}, nil, fmt.Errorf("try again: read spend: %w", err)
	}

	b := policy.Budget(spend)
	est.BudgetLeftUSD = max(b.BudgetUSD-b.SpentUSD, 0)

	if est.EstimateUSD > est.BudgetLeftUSD {
		return est, nil, fmt.Errorf("try again: the estimate $%.2f is more than the $%.2f left of Jonah's budget: %w", est.EstimateUSD,
			est.BudgetLeftUSD, domain.ErrLearningBudgetExhausted)
	}

	return est, adds, nil
}

// queue adds p's tries and audits it.
func (a *TryAgain) queue(ctx context.Context, p domain.Proposal, est TryAgainEstimate, adds []domain.PastTryAdd) error {
	by, err := actor(ctx)
	if err != nil {
		return err
	}

	now := a.d.Clock.Now().UTC()

	queued, err := a.d.Store.AddPastTries(ctx, p.FactoryID, p.ID, adds, now)
	if err != nil {
		return fmt.Errorf("try again: proposal %d: %w", p.Number, err)
	}

	if err := a.d.Audit.Append(ctx, domain.AuditEntry{
		At: now, Actor: by, Action: domain.AuditProposalTried, TargetKind: domain.AuditTargetProposal, TargetID: string(p.ID), FactoryID: p.FactoryID,
		Data: map[string]any{"number": p.Number, "tries": len(queued), "estimate_usd": est.EstimateUSD},
	}); err != nil {
		return fmt.Errorf("audit proposal %d: %w", p.Number, err)
	}

	a.log.InfoContext(ctx, "jonah tries added", slog.String("factory", string(p.FactoryID)), slog.Int64("proposal", p.Number),
		slog.Int("tries", len(queued)))

	return nil
}
