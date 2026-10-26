package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// recoveryActor stamps what a recovery changes (spec 3 §9): 00043's member trigger audits a
// membership it adds as member.added {via: recovery}.
const recoveryActor = "system:recovery"

// ErrNotMigrated is a recovery's refusal on a database without sign-in's tables and functions.
var ErrNotMigrated = errors.New("the database has no sign-in tables yet: migrate it with goal db migrate (goal up does)")

// Recovery is what a recovery did: the person, whether it created them, and for an owner's
// recovery the organization and the role they had there before ("" for none).
type Recovery struct {
	User    domain.User
	Created bool
	Org     domain.Org
	Was     domain.OrgRole
}

// RecoverAdmin makes the person with email a platform admin (`goal db recover-admin`), creating
// them first when nobody has that email (name the email, the email verified, so they can sign in
// by code). pool connects as the database's administrator (GOAL_DATABASE_ADMIN_URL): access to
// it is the proof, so it needs neither goald nor the identity service. In one transaction it
// records platform_admin.added by system:recovery and recovery.used in the security log.
func RecoverAdmin(ctx context.Context, pool *pgxpool.Pool, email string) (Recovery, error) {
	var r Recovery

	err := recoveryTx(ctx, pool, email, func(tx pgx.Tx, addr string) error {
		var err error
		if r.User, r.Created, err = recoveredUser(ctx, tx, addr); err != nil {
			return err
		}

		var added []byte
		if err := tx.QueryRow(ctx, `SELECT goal_add_platform_admin($1, $2)`, addr, recoveryActor).Scan(&added); err != nil {
			return fmt.Errorf("add platform admin: %w", err)
		}

		return recordRecovery(ctx, tx, r, "recover-admin")
	})

	return r, err
}

// RecoverOwner makes the person with email an owner of the live organization org (its slug or
// id; `goal db recover-owner`), creating them first as RecoverAdmin does. A member becomes an
// owner, anyone else a new member as owner; the row is stamped changedBy system:recovery, so
// the organization's audit log has member.added {via: recovery} (or member.role_changed). It
// records recovery.used in the security log, all in one transaction; pool is as for
// RecoverAdmin.
func RecoverOwner(ctx context.Context, pool *pgxpool.Pool, org, email string) (Recovery, error) {
	var r Recovery

	err := recoveryTx(ctx, pool, email, func(tx pgx.Tx, addr string) error {
		var err error
		if r.Org, err = recoveredOrg(ctx, tx, org); err != nil {
			return err
		}

		if r.User, r.Created, err = recoveredUser(ctx, tx, addr); err != nil {
			return err
		}

		if r.Was, err = makeOwner(ctx, tx, r.Org.ID, r.User.ID); err != nil {
			return err
		}

		return recordRecovery(ctx, tx, r, "recover-owner")
	})

	return r, err
}

// recoveryTx checks email and that the database is migrated, then runs fn in a transaction with
// the normalized address (trimmed, lower case, as the identity service keeps it).
func recoveryTx(ctx context.Context, pool *pgxpool.Pool, email string, fn func(tx pgx.Tx, addr string) error) error {
	addr := strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(addr); err != nil || a.Address != addr {
		return fmt.Errorf("%w: %q is not an email address", domain.ErrInvalid, email)
	}

	var ready bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('auth."user"') IS NOT NULL
		AND to_regprocedure('goal_add_platform_admin(text, text)') IS NOT NULL`).Scan(&ready); err != nil {
		return fmt.Errorf("recovery: %w", err)
	}

	if !ready {
		return ErrNotMigrated
	}

	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return fn(tx, addr) }); err != nil {
		return fmt.Errorf("recovery: %w", err)
	}

	return nil
}

// recoveredUser finds the person with addr, or creates them as the identity service would for a
// verified email.
func recoveredUser(ctx context.Context, tx pgx.Tx, addr string) (domain.User, bool, error) {
	var u domain.User

	err := tx.QueryRow(ctx, `SELECT id::text, name, email FROM auth."user" WHERE lower(email) = $1`, addr).Scan(&u.ID, &u.Name, &u.Email)
	if err == nil {
		return u, false, nil
	}

	if !notFound(err) {
		return u, false, fmt.Errorf("find user: %w", err)
	}

	if err := tx.QueryRow(ctx, `INSERT INTO auth."user" (name, email, "emailVerified") VALUES ($1, $1, true)
		RETURNING id::text, name, email`, addr).Scan(&u.ID, &u.Name, &u.Email); err != nil {
		return u, false, fmt.Errorf("create user: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO auth_events (kind, user_id, subject, data) VALUES ('user.created', $1, $2, $3)`,
		string(u.ID), emailSubject(addr), map[string]any{"method": "recovery"}); err != nil {
		return u, false, fmt.Errorf("record user.created: %w", err)
	}

	return u, true, nil
}

// recoveredOrg finds a live organization by slug or id; the transaction is bound to it, so
// row-level security lets an administrator that it binds read it too.
func recoveredOrg(ctx context.Context, tx pgx.Tx, ref string) (domain.Org, error) {
	var o domain.Org

	err := tx.QueryRow(ctx, `SELECT id::text, name, slug FROM auth.organization WHERE slug = $1 OR id::text = $1`, ref).
		Scan(&o.ID, &o.Name, &o.Slug)
	if notFound(err) {
		return o, fmt.Errorf("organization %q: %w", ref, ports.ErrNotFound)
	}

	if err != nil {
		return o, fmt.Errorf("find organization: %w", err)
	}

	if _, err := tx.Exec(ctx, `SELECT set_config('goal.org_id', $1, true)`, string(o.ID)); err != nil {
		return o, fmt.Errorf("bind organization: %w", err)
	}

	var live bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organizations WHERE id = $1::uuid AND deleted_at IS NULL)`,
		string(o.ID)).Scan(&live); err != nil {
		return o, fmt.Errorf("find organization: %w", err)
	}

	if !live {
		return o, fmt.Errorf("organization %q: %w", ref, ports.ErrNotFound)
	}

	return o, nil
}

// makeOwner makes user an owner of org, stamped by system:recovery, and returns their role
// before ("" when they were not a member).
func makeOwner(ctx context.Context, tx pgx.Tx, org domain.OrgID, user domain.UserID) (domain.OrgRole, error) {
	var was domain.OrgRole

	err := tx.QueryRow(ctx, `SELECT role FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2 FOR UPDATE`,
		string(org), string(user)).Scan(&was)

	switch {
	case notFound(err):
		_, err = tx.Exec(ctx, `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt", "changedBy", "changedAt")
			VALUES ($1, $2, 'owner', now(), $3, clock_timestamp())`, string(org), string(user), recoveryActor)
	case err == nil && was != domain.OrgOwner:
		_, err = tx.Exec(ctx, `UPDATE auth.member SET role = 'owner', "changedBy" = $3, "changedAt" = clock_timestamp()
			WHERE "organizationId" = $1 AND "userId" = $2`, string(org), string(user), recoveryActor)
	}

	if err != nil {
		return "", fmt.Errorf("make owner: %w", err)
	}

	return was, nil
}

// recordRecovery appends recovery.used to the security log: which command, about whom, whether
// it created them, and for an owner's recovery the organization and the role before.
func recordRecovery(ctx context.Context, tx pgx.Tx, r Recovery, command string) error {
	data := map[string]any{"command": command, "created_user": r.Created}
	if r.Org.ID != "" {
		data["org_id"], data["was"] = string(r.Org.ID), string(r.Was)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO auth_events (kind, user_id, subject, data) VALUES ('recovery.used', $1, $2, $3)`,
		string(r.User.ID), emailSubject(r.User.Email), data); err != nil {
		return fmt.Errorf("record recovery.used: %w", err)
	}

	return nil
}

// emailSubject is how the security log names an address (apps/auth/src/goal.ts emailSubject):
// base64url, unpadded, of the SHA-256 of the trimmed, lower-case address.
func emailSubject(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}
