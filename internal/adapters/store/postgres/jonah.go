package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.JonahStore = (*Store)(nil)

// proposalNumberKey is 00075's key on a factory's proposal numbers.
const proposalNumberKey = "jonah_proposals_number_key"

// QueueJonahReview implements ports.JonahStore over 00075's jonah_reviews: the insert does
// nothing when the factory has a review in flight (jonah_reviews_in_flight_key), and that review
// is returned with ErrConflict.
func (s *Store) QueueJonahReview(ctx context.Context, n domain.NewJonahReview) (domain.JonahReview, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.JonahReview{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue jonah review: %w", err)
	}

	p := db.InsertJonahReviewParams{
		Trigger: string(n.Trigger), WindowStart: n.WindowStart, WindowEnd: n.WindowEnd, CreatedBy: n.By.Ref(), CreatedAt: n.At,
	}

	var err error

	if p.ID, err = newID(); err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue jonah review: %w", err)
	}

	if p.FactoryID, err = parseID("factory", string(n.FactoryID)); err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue jonah review: %w", err)
	}

	if p.VersionID, err = parseID("definition version", string(n.VersionID)); err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue jonah review: %w", err)
	}

	var (
		row      db.JonahReview
		inFlight bool
	)

	err = s.inTx(ctx, func(q *db.Queries) error {
		row, err = q.InsertJonahReview(ctx, p)
		if !notFound(err) {
			return err
		}

		inFlight = true
		row, err = q.InFlightJonahReview(ctx, p.FactoryID)

		return err
	})

	switch {
	case isForeignKeyViolation(err):
		return domain.JonahReview{}, fmt.Errorf("queue jonah review of factory %s: factory or version: %w", n.FactoryID, ports.ErrNotFound)
	case err != nil:
		return domain.JonahReview{}, fmt.Errorf("queue jonah review of factory %s: %w", n.FactoryID, err)
	case inFlight:
		return jonahReviewFrom(row), fmt.Errorf("queue jonah review: factory %s has review %s in flight: %w", n.FactoryID, row.ID, ports.ErrConflict)
	default:
		return jonahReviewFrom(row), nil
	}
}

// ClaimJonahReview implements ports.JonahStore with FOR UPDATE SKIP LOCKED.
func (s *Store) ClaimJonahReview(ctx context.Context, now time.Time, lease time.Duration) (domain.JonahReview, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.JonahReview{}, domain.ErrNoOrg
	}

	row, err := fetch(ctx, s.inTx, func(q *db.Queries) (db.JonahReview, error) {
		return q.ClaimJonahReview(ctx, db.ClaimJonahReviewParams{Until: now.Add(lease), Now: now})
	})

	switch {
	case notFound(err):
		return domain.JonahReview{}, fmt.Errorf("claim jonah review: %w", ports.ErrNotFound)
	case err != nil:
		return domain.JonahReview{}, fmt.Errorf("claim jonah review: %w", err)
	default:
		return jonahReviewFrom(row), nil
	}
}

// FinishJonahReview implements ports.JonahStore: the fenced update, the factory's next numbers
// and the inserts in one transaction.
func (s *Store) FinishJonahReview(ctx context.Context, claimed domain.JonahReview, r domain.JonahReviewResult) (domain.JonahReview, []domain.Proposal, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.JonahReview{}, nil, domain.ErrNoOrg
	}

	if err := r.Check(); err != nil {
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w", claimed.ID, err)
	}

	id, err := uuid.Parse(string(claimed.ID))
	if err != nil || claimed.LeaseUntil.IsZero() {
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w", claimed.ID, ports.ErrLeaseLost)
	}

	run, err := optionalID("run", string(r.RunID))
	if err != nil {
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w", claimed.ID, err)
	}

	var (
		review    db.JonahReview
		proposals []domain.Proposal
	)

	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error

		review, err = q.FinishJonahReview(ctx, db.FinishJonahReviewParams{
			State: string(r.State), Error: r.Error, RunID: run, FinishedAt: r.At, ID: id, LeaseUntil: claimed.LeaseUntil,
		})
		if err != nil {
			return err
		}

		proposals, err = insertReviewOutput(ctx, q, review, r)

		return err
	})

	switch {
	case notFound(err):
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w", claimed.ID, ports.ErrLeaseLost)
	case violates(err, proposalNumberKey):
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: proposal numbers: %w", claimed.ID, ports.ErrConflict)
	case isCheckViolationErr(err):
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w: %w", claimed.ID, domain.ErrInvalid, err)
	case isForeignKeyViolation(err):
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: run: %w", claimed.ID, ports.ErrNotFound)
	case err != nil:
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w", claimed.ID, err)
	default:
		return jonahReviewFrom(review), proposals, nil
	}
}

// insertReviewOutput stores r's proposals, numbered after the factory's last, and its
// observations, linked to the proposals they became.
func insertReviewOutput(ctx context.Context, q *db.Queries, review db.JonahReview, r domain.JonahReviewResult) ([]domain.Proposal, error) {
	last, err := q.LastProposalNumber(ctx, review.FactoryID)
	if err != nil {
		return nil, fmt.Errorf("last proposal number: %w", err)
	}

	out := make([]domain.Proposal, 0, len(r.Proposals))
	ids := make([]uuid.UUID, 0, len(r.Proposals))

	for i, n := range r.Proposals {
		p, err := insertProposalParams(review, last+int64(i)+1, n, r.At)
		if err != nil {
			return nil, err
		}

		row, err := q.InsertProposal(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("insert proposal %d: %w", p.Number, err)
		}

		got, err := proposalFrom(row)
		if err != nil {
			return nil, err
		}

		out, ids = append(out, got), append(ids, row.ID)
	}

	for _, n := range r.Observations {
		p := db.InsertObservationParams{
			FactoryID: review.FactoryID, ReviewID: review.ID, Summary: n.Summary, Detail: n.Detail, ItemIds: []uuid.UUID{},
			Count: clampLimit(n.Count), State: string(domain.ObservationWatching), CreatedAt: r.At,
		}

		if p.ID, err = newID(); err != nil {
			return nil, err
		}

		for _, it := range n.ItemIDs {
			item, err := parseID("item", string(it))
			if err != nil {
				return nil, fmt.Errorf("observation: %w: %w", domain.ErrInvalid, err)
			}

			p.ItemIds = append(p.ItemIds, item)
		}

		if n.Proposal > 0 {
			p.State, p.ProposalID = string(domain.ObservationProposed), &ids[n.Proposal-1]
		}

		if _, err := q.InsertObservation(ctx, p); err != nil {
			return nil, fmt.Errorf("insert observation: %w", err)
		}
	}

	return out, nil
}

func insertProposalParams(review db.JonahReview, number int64, n domain.NewProposal, at time.Time) (db.InsertProposalParams, error) {
	p := db.InsertProposalParams{
		FactoryID: review.FactoryID, Number: number, ReviewID: review.ID, VersionID: review.VersionID, Title: n.Title,
		Summary: n.Summary, Paths: n.Paths(), CreatedAt: at,
	}

	var err error

	if p.ID, err = newID(); err != nil {
		return p, err
	}

	evidence := n.Evidence
	if evidence == nil {
		evidence = []domain.ProposalEvidence{}
	}

	if p.Files, err = json.Marshal(n.Files); err != nil {
		return p, fmt.Errorf("proposal files: %w", err)
	}

	if p.Evidence, err = json.Marshal(evidence); err != nil {
		return p, fmt.Errorf("proposal evidence: %w", err)
	}

	if p.Estimate, err = json.Marshal(n.Estimate); err != nil {
		return p, fmt.Errorf("proposal estimate: %w", err)
	}

	return p, nil
}

// JonahReviews implements ports.JonahStore.
func (s *Store) JonahReviews(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.JonahReview, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, fmt.Errorf("jonah reviews: %w", err)
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.JonahReview, error) {
		return q.LatestJonahReviews(ctx, db.LatestJonahReviewsParams{FactoryID: f, MaxReviews: clampLimit(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("jonah reviews of factory %s: %w", factory, err)
	}

	out := make([]domain.JonahReview, 0, len(rows))
	for _, r := range rows {
		out = append(out, jonahReviewFrom(r))
	}

	return out, nil
}

// Proposals implements ports.JonahStore.
func (s *Store) Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(q.Factory))
	if err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}

	p := db.ProposalsParams{FactoryID: f, MaxProposals: clampLimit(q.Limit)}

	if q.State != "" {
		state := string(q.State)
		p.State = &state
	}

	if q.Before > 0 {
		p.Before = &q.Before
	}

	rows, err := fetch(ctx, s.read, func(dq *db.Queries) ([]db.JonahProposal, error) { return dq.Proposals(ctx, p) })
	if err != nil {
		return nil, fmt.Errorf("proposals of factory %s: %w", q.Factory, err)
	}

	out := make([]domain.Proposal, 0, len(rows))

	for _, r := range rows {
		got, err := proposalFrom(r)
		if err != nil {
			return nil, err
		}

		out = append(out, got)
	}

	return out, nil
}

// Proposal implements ports.JonahStore.
func (s *Store) Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.Proposal{}, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("proposal %d: %w", number, err)
	}

	row, err := fetch(ctx, s.read, func(q *db.Queries) (db.JonahProposal, error) {
		return q.ProposalByNumber(ctx, db.ProposalByNumberParams{FactoryID: f, Number: number})
	})

	switch {
	case notFound(err):
		return domain.Proposal{}, fmt.Errorf("proposal %d of factory %s: %w", number, factory, ports.ErrNotFound)
	case err != nil:
		return domain.Proposal{}, fmt.Errorf("proposal %d of factory %s: %w", number, factory, err)
	default:
		return proposalFrom(row)
	}
}

// DecideProposal implements ports.JonahStore: an update while the proposal is proposed, and
// when it changes nothing, a read that tells a decided proposal from a missing one.
func (s *Store) DecideProposal(ctx context.Context, factory domain.FactoryID, number int64, d domain.ProposalDecision) (domain.Proposal, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.Proposal{}, domain.ErrNoOrg
	}

	if err := d.Check(); err != nil {
		return domain.Proposal{}, fmt.Errorf("decide proposal %d: %w", number, err)
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("decide proposal %d: %w", number, err)
	}

	published, err := optionalID("definition version", string(d.PublishedVersionID))
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("decide proposal %d: %w", number, err)
	}

	by := d.By.Ref()

	var (
		row     db.JonahProposal
		decided bool
	)

	err = s.inTx(ctx, func(q *db.Queries) error {
		row, err = q.DecideProposal(ctx, db.DecideProposalParams{
			State: string(d.State), PublishedVersionID: published, RejectReason: d.RejectReason, DecidedBy: &by, DecidedAt: d.At,
			FactoryID: f, Number: number,
		})
		if !notFound(err) {
			return err
		}

		if _, err = q.ProposalByNumber(ctx, db.ProposalByNumberParams{FactoryID: f, Number: number}); err == nil {
			decided = true
		}

		return err
	})

	switch {
	case decided:
		return domain.Proposal{}, fmt.Errorf("decide proposal %d of factory %s: %w", number, factory, ports.ErrProposalDecided)
	case notFound(err), isForeignKeyViolation(err):
		return domain.Proposal{}, fmt.Errorf("decide proposal %d of factory %s: %w", number, factory, ports.ErrNotFound)
	case err != nil:
		return domain.Proposal{}, fmt.Errorf("decide proposal %d of factory %s: %w", number, factory, err)
	default:
		return proposalFrom(row)
	}
}

// Observations implements ports.JonahStore.
func (s *Store) Observations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, fmt.Errorf("observations: %w", err)
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.JonahObservation, error) {
		return q.LatestObservations(ctx, db.LatestObservationsParams{FactoryID: f, MaxObservations: clampLimit(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("observations of factory %s: %w", factory, err)
	}

	out := make([]domain.Observation, 0, len(rows))

	for _, r := range rows {
		o := domain.Observation{
			ID: domain.ObservationID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()),
			ReviewID: domain.JonahReviewID(r.ReviewID.String()), Summary: r.Summary, Detail: r.Detail, ItemIDs: []domain.ItemID{},
			Count: int(r.Count), State: domain.ObservationState(r.State), CreatedAt: r.CreatedAt.UTC(),
		}

		for _, it := range r.ItemIds {
			o.ItemIDs = append(o.ItemIDs, domain.ItemID(it.String()))
		}

		if r.ProposalID != nil {
			o.ProposalID = domain.ProposalID(r.ProposalID.String())
		}

		out = append(out, o)
	}

	return out, nil
}

// JonahSpendSince implements ports.JonahStore.
func (s *Store) JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return 0, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return 0, fmt.Errorf("jonah spend: %w", err)
	}

	usd, err := fetch(ctx, s.read, func(q *db.Queries) (float64, error) {
		return q.JonahSpendSince(ctx, db.JonahSpendSinceParams{FactoryID: f, Since: since})
	})
	if err != nil {
		return 0, fmt.Errorf("jonah spend of factory %s: %w", factory, err)
	}

	return usd, nil
}

// SentBackSince implements ports.JonahStore over item_touches.
func (s *Store) SentBackSince(ctx context.Context, factory domain.FactoryID, since time.Time) (int, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return 0, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return 0, fmt.Errorf("sent back: %w", err)
	}

	n, err := fetch(ctx, s.read, func(q *db.Queries) (int64, error) {
		return q.SentBackSince(ctx, db.SentBackSinceParams{FactoryID: f, Since: since})
	})
	if err != nil {
		return 0, fmt.Errorf("sent back in factory %s: %w", factory, err)
	}

	return int(n), nil
}

func jonahReviewFrom(r db.JonahReview) domain.JonahReview {
	out := domain.JonahReview{
		ID: domain.JonahReviewID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()),
		VersionID: domain.DefinitionVersionID(r.VersionID.String()), Trigger: domain.JonahTrigger(r.Trigger),
		State: domain.JonahReviewState(r.State), Attempts: int(r.Attempts), WindowStart: r.WindowStart.UTC(),
		WindowEnd: r.WindowEnd.UTC(), Error: r.Error, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt.UTC(),
	}

	if r.LeaseUntil != nil {
		out.LeaseUntil = r.LeaseUntil.UTC()
	}

	if r.RunID != nil {
		out.RunID = domain.RunID(r.RunID.String())
	}

	if r.FinishedAt != nil {
		out.FinishedAt = r.FinishedAt.UTC()
	}

	return out
}

func proposalFrom(r db.JonahProposal) (domain.Proposal, error) {
	out := domain.Proposal{
		ID: domain.ProposalID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()), Number: r.Number,
		ReviewID: domain.JonahReviewID(r.ReviewID.String()), VersionID: domain.DefinitionVersionID(r.VersionID.String()),
		State: domain.ProposalState(r.State), Title: r.Title, Summary: r.Summary, RejectReason: r.RejectReason,
		DecidedBy: deref(r.DecidedBy), CreatedAt: r.CreatedAt.UTC(),
	}

	if err := json.Unmarshal(r.Files, &out.Files); err != nil {
		return out, fmt.Errorf("proposal %d files: %w", r.Number, err)
	}

	if err := json.Unmarshal(r.Evidence, &out.Evidence); err != nil {
		return out, fmt.Errorf("proposal %d evidence: %w", r.Number, err)
	}

	if len(out.Evidence) == 0 {
		out.Evidence = nil
	}

	if err := json.Unmarshal(r.Estimate, &out.Estimate); err != nil {
		return out, fmt.Errorf("proposal %d estimate: %w", r.Number, err)
	}

	if r.PublishedVersionID != nil {
		out.PublishedVersionID = domain.DefinitionVersionID(r.PublishedVersionID.String())
	}

	if r.DecidedAt != nil {
		out.DecidedAt = r.DecidedAt.UTC()
	}

	return out, nil
}

// clampLimit is limit as an int32 column or a query's LIMIT: never below 0.
func clampLimit(limit int) int32 { return int32(min(max(limit, 0), 1<<31-1)) }

// isCheckViolationErr reports whether a CHECK constraint refused a row.
func isCheckViolationErr(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	return ok && pgErr.Code == checkViolation
}
