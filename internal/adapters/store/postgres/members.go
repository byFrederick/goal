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

// The people of an organization (spec 3, PR 3-11), on Access (access.go): the members and
// invitations of the transaction's organization through 00045's definers (goal_app cannot read
// schema auth), factory roles under row-level security, and the ownership transfer through
// 00042's goal_transfer_ownership. Each call is one transaction bound to the organization in ctx.

// grantRow is a factory role as the definers' jsonb has it.
type grantRow struct {
	FactoryID string `json:"factory_id"`
	Role      string `json:"role"`
}

// memberRow is one entry of goal_members' jsonb.
type memberRow struct {
	MemberID     string     `json:"member_id"`
	UserID       string     `json:"user_id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	Role         string     `json:"role"`
	JoinedAt     time.Time  `json:"joined_at"`
	LastActiveAt *time.Time `json:"last_active_at"`
	FactoryRoles []grantRow `json:"factory_roles"`
}

// invitationRow is one entry of goal_invitations' jsonb. Its factory roles are the identity
// service's JSON, [{"factoryId","role"}].
type invitationRow struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	FactoryRoles []struct {
		FactoryID string `json:"factoryId"`
		Role      string `json:"role"`
	} `json:"factory_roles"`
	Inviter struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"inviter"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Members implements ports.Access.
func (a *Access) Members(ctx context.Context) ([]domain.Member, error) {
	var rows []memberRow

	err := a.s.read(ctx, func(q *db.Queries) error {
		var err error

		rows, err = members(ctx, q)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("members: %w", err)
	}

	out := make([]domain.Member, 0, len(rows))
	for _, r := range rows {
		m := domain.Member{
			ID: r.MemberID, User: domain.User{ID: domain.UserID(r.UserID), Name: r.Name, Email: r.Email}, Role: domain.OrgRole(r.Role),
			FactoryRoles: make([]domain.FactoryGrant, 0, len(r.FactoryRoles)), JoinedAt: r.JoinedAt.UTC(),
		}
		if r.LastActiveAt != nil {
			m.LastActiveAt = r.LastActiveAt.UTC()
		}

		for _, g := range r.FactoryRoles {
			m.FactoryRoles = append(m.FactoryRoles, domain.FactoryGrant{Factory: domain.FactoryID(g.FactoryID), Role: domain.FactoryRole(g.Role)})
		}

		out = append(out, m)
	}

	return out, nil
}

func members(ctx context.Context, q *db.Queries) ([]memberRow, error) {
	raw, err := q.Members(ctx)
	if err != nil {
		return nil, err
	}

	var rows []memberRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return rows, nil
}

// Invitations implements ports.Access.
func (a *Access) Invitations(ctx context.Context) ([]domain.Invitation, error) {
	var raw []byte

	err := a.s.read(ctx, func(q *db.Queries) error {
		var err error

		raw, err = q.Invitations(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("invitations: %w", err)
	}

	var rows []invitationRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("invitations: decode: %w", err)
	}

	out := make([]domain.Invitation, 0, len(rows))
	for _, r := range rows {
		inv := domain.Invitation{
			ID: r.ID, Email: r.Email, Role: domain.OrgRole(r.Role), FactoryRoles: make([]domain.FactoryGrant, 0, len(r.FactoryRoles)),
			Inviter:   domain.User{ID: domain.UserID(r.Inviter.ID), Name: r.Inviter.Name},
			CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(),
		}

		for _, g := range r.FactoryRoles {
			inv.FactoryRoles = append(inv.FactoryRoles, domain.FactoryGrant{Factory: domain.FactoryID(g.FactoryID), Role: domain.FactoryRole(g.Role)})
		}

		out = append(out, inv)
	}

	return out, nil
}

// SetFactoryRole implements ports.Access: in one transaction it checks the person is a member
// (not an owner or admin) and the factory is the organization's, locks the member's role on it,
// writes the change and its audit entry. The same role again changes nothing and is not audited.
func (a *Access) SetFactoryRole(ctx context.Context, by domain.Actor, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) error {
	if role != "" && !role.Valid() {
		return fmt.Errorf("factory role %q: %w", role, domain.ErrInvalid)
	}

	uid, err := parseID("member", string(user))
	if err != nil {
		return err
	}

	fid, err := parseID("factory", string(f))
	if err != nil {
		return err
	}

	err = a.s.inTx(ctx, func(q *db.Queries) error {
		if err := checkFactoryRoleHolder(ctx, q, user); err != nil {
			return err
		}

		if found, err := q.FactoryExists(ctx, fid); err != nil || !found {
			return orElse(err, fmt.Errorf("factory %s: %w", f, ports.ErrNotFound))
		}

		return putFactoryRole(ctx, q, by, factoryRoleChange{user: user, f: f, uid: uid, fid: fid, role: role})
	})
	if err != nil {
		return fmt.Errorf("set factory role: %w", err)
	}

	return nil
}

// factoryRoleChange is one member's new role on one factory ("" removes it).
type factoryRoleChange struct {
	user     domain.UserID
	f        domain.FactoryID
	uid, fid uuid.UUID
	role     domain.FactoryRole
}

// putFactoryRole locks the member's role on the factory, writes c and its audit entry
// (factory_role.granted, changed or removed); the same role again writes nothing.
func putFactoryRole(ctx context.Context, q *db.Queries, by domain.Actor, c factoryRoleChange) error {
	prior, err := q.LockFactoryRole(ctx, db.LockFactoryRoleParams{FactoryID: c.fid, UserID: c.uid})
	if err != nil && !notFound(err) {
		return err
	}

	if domain.FactoryRole(prior) == c.role {
		return nil
	}

	data := map[string]any{"user_id": string(c.user), "factory_id": string(c.f)}
	action := domain.AuditFactoryRoleGranted
	put := db.PutFactoryRoleParams{FactoryID: c.fid, UserID: c.uid, Role: string(c.role), GrantedBy: by.Ref()}

	switch {
	case c.role == "":
		action, data["from"] = domain.AuditFactoryRoleRemoved, prior
		err = q.DeleteFactoryRole(ctx, db.DeleteFactoryRoleParams{FactoryID: c.fid, UserID: c.uid})
	case prior != "":
		action, data["from"], data["to"] = domain.AuditFactoryRoleChanged, prior, string(c.role)
		err = q.PutFactoryRole(ctx, put)
	default:
		data["to"] = string(c.role)
		err = q.PutFactoryRole(ctx, put)
	}

	if err != nil {
		return err
	}

	return appendAudit(ctx, q, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetMember, TargetID: string(c.user), FactoryID: c.f, Data: data,
	})
}

// checkFactoryRoleHolder refuses a factory role to anyone but a member of the transaction's
// organization: someone else is not found, owners and admins are editors everywhere already.
func checkFactoryRoleHolder(ctx context.Context, q *db.Queries, user domain.UserID) error {
	rows, err := members(ctx, q)
	if err != nil {
		return err
	}

	for _, m := range rows {
		if m.UserID != string(user) {
			continue
		}

		if domain.OrgRole(m.Role) != domain.OrgMember {
			return fmt.Errorf("%s of the organization: %w", m.Role, domain.ErrRoleFromOrg)
		}

		return nil
	}

	return fmt.Errorf("member %s: %w", user, ports.ErrNotFound)
}

// orElse is err when there is one, else fallback.
func orElse(err, fallback error) error {
	if err != nil {
		return err
	}

	return fallback
}

// TransferOwnership implements ports.Access through goal_transfer_ownership, which locks the
// organization's members, swaps the two roles stamped with by and audits the transfer, all in
// this transaction; the owner rule (00042's deferred trigger) is checked at commit.
func (a *Access) TransferOwnership(ctx context.Context, by domain.Actor, from, to domain.UserID) error {
	fromID, err := parseID("owner", string(from))
	if err != nil {
		return fmt.Errorf("transfer ownership: %w", domain.ErrNotAnOwner)
	}

	toID, err := parseID("admin", string(to))
	if err != nil {
		return fmt.Errorf("transfer ownership: %w", domain.ErrNotAnAdmin)
	}

	err = a.s.inTx(ctx, func(q *db.Queries) error {
		return q.TransferOwnership(ctx, db.TransferOwnershipParams{
			FromUser: fromID, ToUser: toID, ByRef: by.Ref(), RequestID: optional(domain.RequestIDFrom(ctx)),
		})
	})
	if err != nil {
		return fmt.Errorf("transfer ownership: %w", transferError(err))
	}

	return nil
}

// transferError names the rule goal_transfer_ownership or the owner trigger refused.
func transferError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == "P0002": // no_data_found: the organization is gone
		return ports.ErrNotFound
	case pgErr.ConstraintName == "goal_transfer_to_admin":
		return domain.ErrNotAnAdmin
	case pgErr.ConstraintName == "goal_transfer_from_owner":
		return domain.ErrNotAnOwner
	case pgErr.ConstraintName == "member_keeps_an_owner":
		return domain.ErrLastOwner
	default:
		return err
	}
}
