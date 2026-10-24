package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var (
	_ ports.Tenancy = (*Store)(nil)
	_ ports.Audit   = (*Store)(nil)
)

// DefaultOrgID is the organization 00005 creates and every row from before organizations moved
// into ("default", "Default").
const DefaultOrgID domain.OrgID = "019a1c00-0000-7000-8000-000000000001"

// SQLSTATEs the tenancy methods map to domain.ErrInvalid.
const checkViolation = "23514"

// Orgs implements ports.Tenancy.
func (s *Store) Orgs(ctx context.Context) ([]domain.OrgSummary, error) {
	rows, err := s.q.Orgs(ctx)
	if err != nil {
		return nil, fmt.Errorf("orgs: %w", err)
	}

	out := make([]domain.OrgSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.OrgSummary{
			Org:          orgOf(r.ID, r.Slug, r.Name, r.Mark, r.MaxSlots, r.CreatedAt),
			FactoryCount: int(r.FactoryCount),
		})
	}

	return out, nil
}

// Org implements ports.Tenancy.
func (s *Store) Org(ctx context.Context, ref string) (domain.Org, error) {
	r, err := s.q.OrgByRef(ctx, ref)
	if notFound(err) {
		return domain.Org{}, fmt.Errorf("org %q: %w", ref, ports.ErrNotFound)
	}

	if err != nil {
		return domain.Org{}, fmt.Errorf("org %q: %w", ref, err)
	}

	return orgOf(r.ID, r.Slug, r.Name, r.Mark, r.MaxSlots, r.CreatedAt), nil
}

// ActiveOrgs implements ports.Tenancy.
func (s *Store) ActiveOrgs(ctx context.Context) ([]domain.OrgID, error) {
	ids, err := s.q.ActiveOrgs(ctx)
	if err != nil {
		return nil, fmt.Errorf("active orgs: %w", err)
	}

	out := make([]domain.OrgID, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.OrgID(id.String()))
	}

	return out, nil
}

// CreateOrg implements ports.Tenancy. The new organization's id binds its own transaction, so
// the insert and its audit entry pass row-level security like any other write.
func (s *Store) CreateOrg(ctx context.Context, by domain.Actor, n ports.NewOrg) (domain.Org, error) {
	if n.Mark == "" {
		n.Mark = domain.DefaultMark(n.Name)
	}

	if err := errors.Join(domain.CheckSlug(n.Slug), domain.CheckOrgName(n.Name), domain.CheckMark(n.Mark)); err != nil {
		return domain.Org{}, err
	}

	id, err := newID()
	if err != nil {
		return domain.Org{}, err
	}

	var o domain.Org

	err = s.inTx(domain.WithOrg(ctx, domain.OrgID(id.String())), func(q *db.Queries) error {
		row, err := q.InsertOrg(ctx, db.InsertOrgParams{ID: id, Slug: n.Slug, Name: n.Name, Mark: n.Mark})
		if err != nil {
			return ruleError(err, n.Slug)
		}

		o = orgFrom(row)

		return appendAudit(ctx, q, orgEntry(by, domain.AuditOrgCreated, o, map[string]any{"slug": o.Slug, "name": o.Name}))
	})
	if err != nil {
		return domain.Org{}, fmt.Errorf("create org %s: %w", n.Slug, err)
	}

	return o, nil
}

// UpdateOrg implements ports.Tenancy. The organization's row is locked first, then its
// factories' caps are read and changed in the same transaction.
func (s *Store) UpdateOrg(ctx context.Context, by domain.Actor, u ports.OrgUpdate) (domain.Org, error) {
	var o domain.Org

	err := s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.CurrentOrg(ctx)
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		o = orgFrom(row)

		slots, err := factorySlots(ctx, q)
		if err != nil {
			return err
		}

		c, err := u.Apply(o, slots)
		if err != nil || len(c.Audit) == 0 {
			return err
		}

		row, err = q.UpdateOrg(ctx, db.UpdateOrgParams{
			Slug: c.Org.Slug, Name: c.Org.Name, Mark: c.Org.Mark, MaxSlots: optionalCap(c.Org.MaxSlots),
		})
		if err != nil {
			return ruleError(err, c.Org.Slug)
		}

		for id, n := range c.FactorySlots {
			fid, err := parseID("factory", string(id))
			if err != nil {
				return err
			}

			if err := q.SetFactorySlots(ctx, db.SetFactorySlotsParams{ID: fid, MaxSlots: optionalCap(n)}); err != nil {
				return fmt.Errorf("factory %s: %w", id, ruleError(err, ""))
			}
		}

		o = orgFrom(row)

		return appendAudit(ctx, q, orgEntry(by, domain.AuditOrgUpdated, o, c.Audit))
	})
	if err != nil {
		return domain.Org{}, fmt.Errorf("update org: %w", err)
	}

	return o, nil
}

// FactorySlots implements ports.Tenancy.
func (s *Store) FactorySlots(ctx context.Context) (map[domain.FactoryID]int, error) {
	var out map[domain.FactoryID]int

	err := s.read(ctx, func(q *db.Queries) error {
		var err error

		out, err = factorySlots(ctx, q)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("factory slots: %w", err)
	}

	return out, nil
}

// factorySlots reads every factory of the transaction's organization with its cap (0: none).
func factorySlots(ctx context.Context, q *db.Queries) (map[domain.FactoryID]int, error) {
	rows, err := q.FactorySlots(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[domain.FactoryID]int, len(rows))
	for _, r := range rows {
		out[domain.FactoryID(r.ID.String())] = capOf(r.MaxSlots)
	}

	return out, nil
}

// DeleteOrg implements ports.Tenancy.
func (s *Store) DeleteOrg(ctx context.Context, by domain.Actor, now time.Time) error {
	err := s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.CurrentOrg(ctx)
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		if err := q.SoftDeleteOrg(ctx, &now); err != nil {
			return err
		}

		o := orgFrom(row)

		return appendAudit(ctx, q, orgEntry(by, domain.AuditOrgDeleted, o, map[string]any{"slug": o.Slug, "name": o.Name}))
	})
	if err != nil {
		return fmt.Errorf("delete org: %w", err)
	}

	return nil
}

// PurgeDeletedOrgs implements ports.Tenancy: in one transaction it takes the purge lock (or
// returns nothing when another instance holds it), locks up to limit deleted organizations and
// purges each with goal_purge_org, then calls also. The transaction commits only when every
// organization of the batch is purged.
func (s *Store) PurgeDeletedOrgs(ctx context.Context, cutoff time.Time, limit int,
	also func(context.Context, domain.OrgID) error,
) ([]domain.OrgID, error) {
	var out []domain.OrgID

	err := s.systemTx(ctx, func(q *db.Queries) error {
		locked, err := q.TryLockOrgPurge(ctx)
		if err != nil || !locked {
			return err
		}

		ids, err := q.DeletedOrgs(ctx, db.DeletedOrgsParams{Before: cutoff, MaxOrgs: int32(min(limit, 1000))}) //nolint:gosec // capped
		if err != nil {
			return err
		}

		for _, id := range ids {
			org := domain.OrgID(id.String())

			if err := q.PurgeOrg(ctx, id); err != nil {
				return fmt.Errorf("org %s: %w", id, err)
			}

			if also != nil {
				if err := also(ctx, org); err != nil {
					return fmt.Errorf("org %s: %w", id, err)
				}
			}

			out = append(out, org)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("purge deleted orgs: %w", err)
	}

	return out, nil
}

// Append implements ports.Audit.
func (s *Store) Append(ctx context.Context, e domain.AuditEntry) error {
	if err := domain.CheckAuditAction(e.Action); err != nil {
		return err
	}

	if err := s.inTx(ctx, func(q *db.Queries) error { return appendAudit(ctx, q, e) }); err != nil {
		return fmt.Errorf("append audit entry: %w", err)
	}

	return nil
}

// AuditLog implements ports.Audit.
func (s *Store) AuditLog(ctx context.Context, aq ports.AuditQuery) ([]domain.AuditEntry, error) {
	var (
		out     []domain.AuditEntry
		factory *uuid.UUID
	)

	if aq.FactoryID != "" {
		id, ok := parseOptional(string(aq.FactoryID))
		if !ok {
			return out, nil // a factory id no factory could have matches nothing
		}

		factory = &id
	}

	err := s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.AuditLog(ctx, db.AuditLogParams{
			Before: aq.Before, Action: aq.Action, TargetKind: aq.TargetKind, TargetID: aq.TargetID,
			FactoryID: factory, MaxEntries: int64(aq.Limit),
		})
		if err != nil {
			return err
		}

		out = make([]domain.AuditEntry, 0, len(rows))
		for _, r := range rows {
			e, err := auditFrom(r)
			if err != nil {
				return err
			}

			out = append(out, e)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("audit log: %w", err)
	}

	return out, nil
}

// appendAudit appends e to the audit log of the transaction's organization (org_id defaults to
// goal_org()), with the id of the API request in ctx when there is one. Every adapter file that
// changes something appends its entry with it, in the same transaction as the change.
func appendAudit(ctx context.Context, q *db.Queries, e domain.AuditEntry) error {
	var factory *uuid.UUID

	if e.FactoryID != "" {
		id, err := uuid.Parse(string(e.FactoryID))
		if err != nil {
			return fmt.Errorf("audit entry factory %q: %w", e.FactoryID, ports.ErrNotFound)
		}

		factory = &id
	}

	data := e.Data
	if data == nil {
		data = map[string]any{}
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("audit entry data: %w", err)
	}

	err = q.InsertAudit(ctx, db.InsertAuditParams{
		At: optionalTime(e.At), ActorKind: string(e.Actor.Kind), ActorID: e.Actor.ID, ActorName: e.Actor.Name,
		Action: e.Action, TargetKind: e.TargetKind, TargetID: e.TargetID, FactoryID: factory, Data: raw,
		RequestID: optional(domain.RequestIDFrom(ctx)),
	})
	if err != nil {
		return fmt.Errorf("append audit entry %s: %w", e.Action, ruleError(err, ""))
	}

	return nil
}

// parseOptional parses a UUID filter; false means the filter names nothing.
func parseOptional(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)

	return id, err == nil
}

func orgEntry(by domain.Actor, action string, o domain.Org, data map[string]any) domain.AuditEntry {
	return domain.AuditEntry{Actor: by, Action: action, TargetKind: domain.AuditTargetOrg, TargetID: string(o.ID), Data: data}
}

// ruleError maps a rejected organization row to domain.ErrInvalid: the slug index (a slug is
// unique among live and deleted organizations until purge) and the CHECK constraints.
func ruleError(err error, slug string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == "organizations_slug_key":
		return fmt.Errorf("slug %q: %w", slug, domain.ErrSlugTaken)
	case pgErr.Code == checkViolation:
		return fmt.Errorf("%s: %w", pgErr.ConstraintName, domain.ErrInvalid)
	default:
		return err
	}
}

func orgOf(id uuid.UUID, slug, name, mark string, maxSlots *int32, created time.Time) domain.Org {
	return domain.Org{
		ID: domain.OrgID(id.String()), Slug: slug, Name: name, Mark: mark, MaxSlots: capOf(maxSlots), CreatedAt: created.UTC(),
	}
}

func orgFrom(r db.Organization) domain.Org {
	return orgOf(r.ID, r.Slug, r.Name, r.Mark, r.MaxSlots, r.CreatedAt)
}

// capOf reads a max_slots column: NULL is no cap, 0.
func capOf(n *int32) int {
	if n == nil {
		return 0
	}

	return int(*n)
}

// optionalCap writes a max_slots column: no cap (0) is NULL. Caps are checked to be at most
// domain.MaxSlotCap before they get here.
func optionalCap(n int) *int32 {
	if n == 0 {
		return nil
	}

	v := int32(min(n, domain.MaxSlotCap)) //nolint:gosec // capped

	return &v
}

func auditFrom(r db.AuditLog) (domain.AuditEntry, error) {
	data := map[string]any{}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return domain.AuditEntry{}, fmt.Errorf("audit entry %d: data: %w", r.ID, err)
	}

	e := domain.AuditEntry{
		ID: r.ID, At: r.At.UTC(), Actor: domain.Actor{Kind: domain.ActorKind(r.ActorKind), ID: r.ActorID, Name: r.ActorName},
		Action: r.Action, TargetKind: r.TargetKind, TargetID: r.TargetID, Data: data,
	}

	if r.FactoryID != nil {
		e.FactoryID = domain.FactoryID(r.FactoryID.String())
	}

	return e, nil
}
