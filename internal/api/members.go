package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// People are the organizations' people, part of Access: their members and pending invitations,
// factory roles and the transfer of ownership, each for the organization in ctx. Inviting,
// changing an organization role and removing are the identity service's
// (/api/auth/organization/*), so its owner rules apply to them.
type People interface {
	Members(ctx context.Context) ([]domain.Member, error)
	Invitations(ctx context.Context) ([]domain.Invitation, error)
	SetFactoryRole(ctx context.Context, by domain.Actor, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) error
	TransferOwnership(ctx context.Context, by domain.Actor, from, to domain.UserID) error
}

// permMemberManage changes people: invitations, roles, removals, factory roles.
const permMemberManage domain.Permission = "member:manage"

// errNoSignIn refuses what needs a signed-in person: under GOAL_AUTH=none nobody owns anything.
var errNoSignIn = fmt.Errorf("%w: only a signed-in owner can do this", errForbidden)

// ListMembers lists the organization's people with their roles, their factory roles on the
// factories the caller sees, and what the caller may change about each: the identity service
// would refuse the rest (spec 3 §8.6), so the UI can say why before anyone tries.
func (s *server) ListMembers(ctx context.Context, _ gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	members, err := s.access.Members(ctx)
	if err != nil {
		return nil, err
	}

	p := principalOf(ctx)
	owners := 0

	for _, m := range members {
		if m.Role == domain.OrgOwner {
			owners++
		}
	}

	// Membership changes go to the identity service, which only runs with sign-in on.
	manages := s.auth.Mode == AuthSession && s.holds(ctx, permMemberManage)
	visible := map[domain.FactoryID]bool{}
	out := gen.ListMembers200JSONResponse{Members: make([]gen.Member, 0, len(members))}

	for _, m := range members {
		// Only an owner touches an owner, and never the last one (Better Auth's rules and Goal's).
		changeable := manages && (m.Role != domain.OrgOwner || (p.OrgRole == domain.OrgOwner && owners > 1))
		you := p.Kind == domain.PrincipalUser && m.User.ID == p.User
		row := memberOut(m, you, changeable)

		for _, g := range m.FactoryRoles {
			if _, known := visible[g.Factory]; !known {
				visible[g.Factory] = s.visible(ctx, g.Factory)
			}

			if visible[g.Factory] {
				row.FactoryRoles = append(row.FactoryRoles, grantOut(g))
			}
		}

		out.Members = append(out.Members, row)
	}

	return out, nil
}

// memberOut is a member as the contract has it, without factory roles yet.
func memberOut(m domain.Member, you, changeable bool) gen.Member {
	row := gen.Member{
		MemberId: m.ID, User: gen.User{Id: string(m.User.ID), Name: m.User.Name, Email: m.User.Email}, Role: gen.OrgRole(m.Role),
		FactoryRoles: []gen.FactoryRoleGrant{}, JoinedAt: m.JoinedAt, You: you, CanChangeRole: changeable, CanRemove: changeable && !you,
	}

	if !m.LastActiveAt.IsZero() {
		at := m.LastActiveAt
		row.LastActiveAt = &at
	}

	return row
}

// holds reports whether the request's principal holds the organization permission, counting a
// fresh-only one held with an older session (the change then asks to sign in again).
func (s *server) holds(ctx context.Context, permission domain.Permission) bool {
	err := s.ask(ctx, permission, "")

	return err == nil || errors.Is(err, domain.ErrReauthRequired)
}

func grantOut(g domain.FactoryGrant) gen.FactoryRoleGrant {
	return gen.FactoryRoleGrant{FactoryId: string(g.Factory), Role: gen.FactoryRole(g.Role)}
}

// ListInvitations lists the organization's pending invitations.
func (s *server) ListInvitations(ctx context.Context, _ gen.ListInvitationsRequestObject) (gen.ListInvitationsResponseObject, error) {
	invitations, err := s.access.Invitations(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListInvitations200JSONResponse{Invitations: make([]gen.Invitation, 0, len(invitations))}

	for _, inv := range invitations {
		row := gen.Invitation{
			Id: inv.ID, Email: inv.Email, Role: gen.OrgRole(inv.Role), FactoryRoles: make([]gen.FactoryRoleGrant, 0, len(inv.FactoryRoles)),
			Inviter: gen.Inviter{Id: string(inv.Inviter.ID), Name: inv.Inviter.Name}, CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt,
		}

		for _, g := range inv.FactoryRoles {
			row.FactoryRoles = append(row.FactoryRoles, grantOut(g))
		}

		out.Invitations = append(out.Invitations, row)
	}

	return out, nil
}

// SetFactoryRole gives a member a role on a factory of the organization (by name or id).
func (s *server) SetFactoryRole(ctx context.Context, req gen.SetFactoryRoleRequestObject) (gen.SetFactoryRoleResponseObject, error) {
	role := domain.FactoryRole(req.Body.Role)
	if !role.Valid() {
		return nil, fmt.Errorf("%w: factory role %q is not viewer, approver or editor", errInvalid, role)
	}

	f, err := s.changeFactoryRole(ctx, req.User, req.Factory, role)
	if err != nil {
		return nil, err
	}

	return gen.SetFactoryRole200JSONResponse(grantOut(domain.FactoryGrant{Factory: f, Role: role})), nil
}

// RemoveFactoryRole takes a member's role on a factory away.
func (s *server) RemoveFactoryRole(ctx context.Context, req gen.RemoveFactoryRoleRequestObject) (gen.RemoveFactoryRoleResponseObject, error) {
	if _, err := s.changeFactoryRole(ctx, req.User, req.Factory, ""); err != nil {
		return nil, err
	}

	return gen.RemoveFactoryRole204Response{}, nil
}

// changeFactoryRole resolves the factory (unknown and invisible answer alike) and sets user's role
// on it; "" removes it. It returns the factory's id.
func (s *server) changeFactoryRole(ctx context.Context, user, factory string, role domain.FactoryRole) (domain.FactoryID, error) {
	actor, err := actorOf(ctx, "change a factory role")
	if err != nil {
		return "", err
	}

	rec, err := s.definition(ctx, factory)
	if err != nil {
		return "", err
	}

	if err := s.access.SetFactoryRole(ctx, actor, domain.UserID(user), rec.ID, role); err != nil {
		return "", err
	}

	return rec.ID, nil
}

// TransferOwnership hands the organization to one of its admins: the caller, an owner, becomes an
// admin in the same change. It answers the organization as the caller now sees it.
func (s *server) TransferOwnership(ctx context.Context, req gen.TransferOwnershipRequestObject) (gen.TransferOwnershipResponseObject, error) {
	actor, err := actorOf(ctx, "transfer ownership")
	if err != nil {
		return nil, err
	}

	p := principalOf(ctx)
	if s.auth.Mode == AuthNone || p.Kind != domain.PrincipalUser {
		return nil, errNoSignIn
	}

	o := orgOf(ctx)
	if req.Body.Confirm != o.Name {
		return nil, errConfirmMismatch
	}

	if err := s.access.TransferOwnership(ctx, actor, p.User, domain.UserID(req.Body.To)); err != nil {
		return nil, err
	}

	p.OrgRole = domain.OrgAdmin
	ctx = domain.WithPrincipal(ctx, p)

	n, err := s.visibleCount(ctx)
	if err != nil {
		return nil, err
	}

	return gen.TransferOwnership200JSONResponse(s.orgOut(ctx, o, n)), nil
}
