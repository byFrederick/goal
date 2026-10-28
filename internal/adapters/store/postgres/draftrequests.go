package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.DraftRequests = (*DraftRequests)(nil)

// draftRequestOpenKey is 00078's partial unique index: one queued or running request per factory.
const draftRequestOpenKey = "draft_requests_one_open_idx"

// DraftRequests implements ports.DraftRequests over 00078's draft_requests, as goal_app under
// row-level security: every statement sees the organization in ctx, but the claim and the
// requeue, which run 00078's definers in no organization.
type DraftRequests struct {
	s *Store
}

// NewDraftRequests returns the draft requests on s's pool.
func NewDraftRequests(s *Store) *DraftRequests { return &DraftRequests{s: s} }

// Create implements ports.DraftRequests.
func (d *DraftRequests) Create(ctx context.Context, n domain.NewDraftRequest) (domain.DraftRequest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.DraftRequest{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.DraftRequest{}, err
	}

	factory, err := parseID("factory", string(n.Factory))
	if err != nil {
		return domain.DraftRequest{}, err
	}

	id, err := newID()
	if err != nil {
		return domain.DraftRequest{}, err
	}

	var agent *string
	if n.Agent != "" {
		agent = &n.Agent
	}

	r, err := fetch(ctx, d.s.inTx, func(q *db.Queries) (db.DraftRequest, error) {
		return q.InsertDraftRequest(ctx, db.InsertDraftRequestParams{
			ID: id, FactoryID: factory, Requester: n.Requester.Ref(), Text: n.Text, Agent: agent,
		})
	})
	if err != nil {
		return domain.DraftRequest{}, fmt.Errorf("create draft request: %w", draftRequestError(err, n.Factory))
	}

	return draftRequestFrom(r), nil
}

// draftRequestError maps a refused insert: the open request index to domain.ErrDraftRequestOpen,
// a factory the organization does not have to ports.ErrNotFound.
func draftRequestError(err error, factory domain.FactoryID) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case violates(err, draftRequestOpenKey):
		return fmt.Errorf("factory %s: %w", factory, domain.ErrDraftRequestOpen)
	case pgErr.Code == foreignKeyViolation:
		return fmt.Errorf("factory %s: %w", factory, ports.ErrNotFound)
	default:
		return err
	}
}

// Get implements ports.DraftRequests.
func (d *DraftRequests) Get(ctx context.Context, id domain.DraftRequestID) (domain.DraftRequest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.DraftRequest{}, domain.ErrNoOrg
	}

	rid, err := parseID("draft request", string(id))
	if err != nil {
		return domain.DraftRequest{}, err
	}

	r, err := fetch(ctx, d.s.read, func(q *db.Queries) (db.DraftRequest, error) { return q.DraftRequest(ctx, rid) })
	if notFound(err) {
		return domain.DraftRequest{}, fmt.Errorf("draft request %s: %w", id, ports.ErrNotFound)
	}

	if err != nil {
		return domain.DraftRequest{}, fmt.Errorf("get draft request %s: %w", id, err)
	}

	return draftRequestFrom(r), nil
}

// List implements ports.DraftRequests.
func (d *DraftRequests) List(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.DraftRequest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	if limit < 1 || limit > domain.MaxDraftRequestList {
		return nil, fmt.Errorf("list %d draft requests, 1 to %d: %w", limit, domain.MaxDraftRequestList, domain.ErrInvalid)
	}

	fid, err := uuid.Parse(string(factory))
	if err != nil {
		fid = uuid.Nil // a factory that is not a UUID has no requests, as no factory has the nil id
	}

	rows, err := fetch(ctx, d.s.read, func(q *db.Queries) ([]db.DraftRequest, error) {
		return q.DraftRequests(ctx, db.DraftRequestsParams{FactoryID: fid, MaxRows: int32(min(limit, domain.MaxDraftRequestList))}) //nolint:gosec // bounded by min
	})
	if err != nil {
		return nil, fmt.Errorf("list draft requests of factory %s: %w", factory, err)
	}

	out := make([]domain.DraftRequest, 0, len(rows))
	for _, r := range rows {
		out = append(out, draftRequestFrom(r))
	}

	return out, nil
}

// Claim implements ports.DraftRequests.
func (d *DraftRequests) Claim(ctx context.Context, now, until time.Time) (*domain.DraftRequestClaim, error) {
	var out *domain.DraftRequestClaim

	err := d.s.systemTx(ctx, func(q *db.Queries) error {
		c, err := q.ClaimDraftRequest(ctx, db.ClaimDraftRequestParams{Now: now, Until: until})
		if notFound(err) {
			return nil
		}

		if err != nil {
			return err
		}

		if err := q.SetOrg(ctx, c.OrgID.String()); err != nil {
			return fmt.Errorf("set org: %w", err)
		}

		r, err := q.DraftRequest(ctx, c.ID)
		if err != nil {
			return fmt.Errorf("read draft request %s: %w", c.ID, err)
		}

		out = &domain.DraftRequestClaim{
			Org: domain.OrgID(c.OrgID.String()), Request: draftRequestFrom(r),
			Lease: domain.DraftRequestLease{Request: domain.DraftRequestID(c.ID.String()), Token: c.LeaseHolder.String()},
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim draft request: %w", err)
	}

	return out, nil
}

// draftLease parses l. A malformed one holds nothing: ErrLeaseLost.
func draftLease(l domain.DraftRequestLease) (id, holder uuid.UUID, err error) {
	id, err = uuid.Parse(string(l.Request))
	if err == nil {
		holder, err = uuid.Parse(l.Token)
	}

	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, fmt.Errorf("draft request %s: %w", l.Request, ports.ErrLeaseLost)
	}

	return id, holder, nil
}

// Heartbeat implements ports.DraftRequests.
func (d *DraftRequests) Heartbeat(ctx context.Context, l domain.DraftRequestLease, now, until time.Time) error {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.ErrNoOrg
	}

	id, holder, err := draftLease(l)
	if err != nil {
		return err
	}

	n, err := fetch(ctx, d.s.inTx, func(q *db.Queries) (int64, error) {
		return q.HeartbeatDraftRequest(ctx, db.HeartbeatDraftRequestParams{Until: until, ID: id, LeaseHolder: holder, Now: now})
	})
	if err != nil {
		return fmt.Errorf("heartbeat draft request %s: %w", l.Request, err)
	}

	if n == 0 {
		return fmt.Errorf("draft request %s: %w", l.Request, ports.ErrLeaseLost)
	}

	return nil
}

// Finish implements ports.DraftRequests.
func (d *DraftRequests) Finish(ctx context.Context, l domain.DraftRequestLease, r domain.DraftRequestResult) error {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.ErrNoOrg
	}

	if err := r.Check(); err != nil {
		return err
	}

	id, holder, err := draftLease(l)
	if err != nil {
		return err
	}

	paths := r.ChangedPaths
	if paths == nil {
		paths = []string{} // the column is NOT NULL
	}

	n, err := fetch(ctx, d.s.inTx, func(q *db.Queries) (int64, error) {
		return q.FinishDraftRequest(ctx, db.FinishDraftRequestParams{
			Status: string(r.Status), Summary: cutChars(r.Summary, domain.MaxDraftRequestSummary), ChangedPaths: paths,
			CostUsd: r.CostUSD, Error: cutChars(r.Error, domain.MaxDraftRequestError),
			Turns:      int32(min(r.Turns, domain.MaxDraftRequestTurns)), //nolint:gosec // bounded by min
			FinishedAt: r.At, ID: id, LeaseHolder: holder,
		})
	})
	if err != nil {
		return fmt.Errorf("finish draft request %s: %w", l.Request, err)
	}

	if n == 0 {
		return fmt.Errorf("draft request %s: %w", l.Request, ports.ErrLeaseLost)
	}

	return nil
}

// RequeueExpired implements ports.DraftRequests.
func (d *DraftRequests) RequeueExpired(ctx context.Context, now time.Time) (int, error) {
	var n int32

	err := d.s.systemTx(ctx, func(q *db.Queries) error {
		var err error

		n, err = q.RequeueDraftRequests(ctx, now)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("requeue draft requests: %w", err)
	}

	return int(n), nil
}

func draftRequestFrom(r db.DraftRequest) domain.DraftRequest {
	requester, err := domain.ActorFromRef(r.Requester)
	if err != nil {
		requester = domain.Actor{Kind: domain.ActorSystem, ID: r.Requester, Name: r.Requester}
	}

	return domain.DraftRequest{
		ID: domain.DraftRequestID(r.ID.String()), Org: domain.OrgID(r.OrgID.String()), Factory: domain.FactoryID(r.FactoryID.String()),
		Requester: requester, Text: r.Text, Agent: deref(r.Agent), Status: domain.DraftRequestStatus(r.Status),
		Summary: r.Summary, ChangedPaths: r.ChangedPaths, CostUSD: r.CostUsd, Turns: int(r.Turns), Error: r.Error,
		CreatedAt: r.CreatedAt.UTC(), StartedAt: utcPtr(r.StartedAt), FinishedAt: utcPtr(r.FinishedAt),
		LeaseExpiresAt: utcPtr(r.LeaseExpiresAt),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	u := t.UTC()

	return &u
}

// cutChars keeps the first n characters of s, as char_length counts them.
func cutChars(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	return string([]rune(s)[:n])
}
