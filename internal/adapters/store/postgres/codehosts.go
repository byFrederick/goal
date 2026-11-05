package postgres

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.CodeHosts = (*Store)(nil)

// The organization's code hosts (ADR 0041, decision 1) over 00127's code_hosts, as goal_app under
// row-level security. A save takes the organization's code-host lock, compares, then deletes the
// hosts it drops, upserts the rest and appends its audit entry, all in one transaction.

// CodeHosts implements ports.CodeHosts.
func (s *Store) CodeHosts(ctx context.Context) ([]domain.CodeHost, error) {
	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.CodeHostsRow, error) { return q.CodeHosts(ctx) })
	if err != nil {
		return nil, fmt.Errorf("code hosts: %w", err)
	}

	return codeHostsOf(rows), nil
}

// SetCodeHosts implements ports.CodeHosts.
func (s *Store) SetCodeHosts(ctx context.Context, by domain.Actor, hosts []domain.CodeHost) ([]domain.CodeHost, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	next := slices.Clone(hosts)
	slices.SortFunc(next, func(a, b domain.CodeHost) int { return cmp.Compare(a.Host, b.Host) })

	err := s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockCodeHosts(ctx); err != nil {
			return err
		}

		rows, err := q.CodeHosts(ctx)
		if err != nil {
			return err
		}

		was := codeHostsOf(rows)
		if slices.Equal(was, next) {
			return nil
		}

		names := make([]string, 0, len(next))
		for _, h := range next {
			names = append(names, h.Host)
		}

		if err := q.DeleteCodeHostsNotIn(ctx, names); err != nil {
			return err
		}

		for _, h := range next {
			if err := q.PutCodeHost(ctx, db.PutCodeHostParams{Host: h.Host, Kind: string(h.Kind), CreatedBy: by.Ref()}); err != nil {
				return codeHostError(err)
			}
		}

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditOrgCodeHostsSet, TargetKind: domain.AuditTargetOrg, TargetID: string(org),
			Data: domain.CodeHostsAuditData(next, was),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("set code hosts: %w", err)
	}

	return next, nil
}

func codeHostsOf(rows []db.CodeHostsRow) []domain.CodeHost {
	out := make([]domain.CodeHost, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.CodeHost{Host: r.Host, Kind: domain.CodeHostKind(r.Kind)})
	}

	return out
}

// codeHostError names a host the table's CHECKs refused, which domain.CheckCodeHost lets through
// only when the two disagree.
func codeHostError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == checkViolation {
		return fmt.Errorf("%s: %w", pgErr.ConstraintName, domain.ErrCodeHostInvalid)
	}

	return err
}
