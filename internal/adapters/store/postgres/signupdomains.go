package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.SignupDomains = (*Store)(nil)

// Sign-up by domain (ADR 0023, decision 7): goald reads the organization's row of
// org_signup_domains under row-level security and changes it only through 00081's
// goal_set_signup_domain, which checks the owner, their own email, the webmail list and other
// organizations' claims in the same transaction as the change and its audit entry.

// SignupDomain implements ports.SignupDomains.
func (s *Store) SignupDomain(ctx context.Context) (domain.SignupDomain, bool, error) {
	var (
		out domain.SignupDomain
		set bool
	)

	err := s.read(ctx, func(q *db.Queries) error {
		row, err := q.SignupDomain(ctx)
		if notFound(err) {
			return nil
		}

		if err != nil {
			return err
		}

		by, err := domain.ActorFromRef(row.SetBy)
		if err != nil {
			return err
		}

		out, set = domain.SignupDomain{Domain: row.Domain, JoinRole: domain.FactoryRole(row.JoinRole), SetBy: by, SetAt: row.SetAt.UTC()}, true

		return nil
	})
	if err != nil {
		return domain.SignupDomain{}, false, fmt.Errorf("sign-up domain: %w", err)
	}

	return out, set, nil
}

// SetSignupDomain implements ports.SignupDomains. An actor who is no user (the local user, an
// API key) has no sign-in email, so no domain is theirs.
func (s *Store) SetSignupDomain(ctx context.Context, by domain.Actor, d string, role domain.FactoryRole) (domain.SignupDomain, error) {
	user, err := uuid.Parse(by.ID)
	if by.Kind != domain.ActorUser || err != nil {
		return domain.SignupDomain{}, fmt.Errorf("set sign-up domain: %w", domain.ErrSignupDomainNotYours)
	}

	if err := s.setSignupDomain(ctx, by, user, &d, role); err != nil {
		return domain.SignupDomain{}, fmt.Errorf("set sign-up domain %q: %w", d, err)
	}

	out, _, err := s.SignupDomain(ctx)

	return out, err
}

// ClearSignupDomain implements ports.SignupDomains.
func (s *Store) ClearSignupDomain(ctx context.Context, by domain.Actor) error {
	user, err := uuid.Parse(by.ID)
	if by.Kind != domain.ActorUser || err != nil {
		return fmt.Errorf("clear sign-up domain: %w", domain.ErrNotAnOwner)
	}

	if err := s.setSignupDomain(ctx, by, user, nil, domain.FactoryViewer); err != nil {
		return fmt.Errorf("clear sign-up domain: %w", err)
	}

	return nil
}

func (s *Store) setSignupDomain(ctx context.Context, by domain.Actor, user uuid.UUID, d *string, role domain.FactoryRole) error {
	err := s.inTx(ctx, func(q *db.Queries) error {
		return q.SetSignupDomain(ctx, db.SetSignupDomainParams{
			ActingUser: user, Domain: d, JoinRole: string(role), ByRef: by.Ref(), RequestID: optional(domain.RequestIDFrom(ctx)),
		})
	})

	return signupDomainError(err)
}

// signupDomainError names the rule goal_set_signup_domain or the table refused.
func signupDomainError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == "P0002": // no_data_found: the organization is gone
		return ports.ErrNotFound
	case pgErr.ConstraintName == "goal_signup_domain_owner":
		return domain.ErrNotAnOwner
	case pgErr.ConstraintName == "goal_signup_domain_not_yours":
		return domain.ErrSignupDomainNotYours
	case pgErr.ConstraintName == "goal_signup_domain_public":
		return domain.ErrSignupDomainPublic
	case pgErr.ConstraintName == "goal_signup_domain_taken",
		pgErr.Code == uniqueViolation && pgErr.ConstraintName == "org_signup_domains_domain_key":
		return domain.ErrSignupDomainTaken
	case pgErr.Code == checkViolation:
		return fmt.Errorf("%s: %w", pgErr.ConstraintName, domain.ErrSignupDomainInvalid)
	default:
		return err
	}
}
