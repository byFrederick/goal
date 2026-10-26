package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Invite adds an invitation of org by inviter, in status ("pending", "accepted", "canceled", …).
// inv.ID, Inviter.ID, Email, Role, FactoryRoles and ExpiresAt are kept; the inviter's name is the
// user's; CreatedAt is now.
func (i *Identity) Invite(org domain.OrgID, inv domain.Invitation, status string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	o, ok := i.orgs[org]
	if !ok {
		return fmt.Errorf("organization %s: %w", org, ports.ErrNotFound)
	}

	if u := i.users[inv.Inviter.ID]; u != nil {
		inv.Inviter.Name = u.Name
	}

	inv.CreatedAt = i.now()
	inv.FactoryRoles = slices.Clone(inv.FactoryRoles)
	o.invitations = append(o.invitations, &identityInvitation{invitation: inv, status: status})

	return nil
}

// Accept has user accept a pending invitation as the identity service does: it is accepted, user
// becomes a member with its role, and its factory roles of factories that still exist are given.
func (i *Identity) Accept(invitation string, user domain.UserID) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, o := range i.orgs {
		for _, inv := range o.invitations {
			if inv.invitation.ID != invitation {
				continue
			}

			if inv.status != "pending" || !i.now().Before(inv.invitation.ExpiresAt) {
				return fmt.Errorf("invitation %s: %w", invitation, ports.ErrConflict)
			}

			inv.status = "accepted"
			o.members[user], o.joined[user] = inv.invitation.Role, i.now()

			for _, g := range inv.invitation.FactoryRoles {
				if slices.Contains(o.factories, g.Factory) {
					o.grant(user, g.Factory, g.Role)
				}
			}

			return nil
		}
	}

	return fmt.Errorf("invitation %s: %w", invitation, ports.ErrNotFound)
}

// org is the live organization in ctx.
func (i *Identity) org(ctx context.Context) (*identityOrg, error) {
	id, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	o, ok := i.orgs[id]
	if !ok || o.deleted {
		return nil, fmt.Errorf("organization %s: %w", id, ports.ErrNotFound)
	}

	return o, nil
}

// Members implements ports.Access.
func (i *Identity) Members(ctx context.Context) ([]domain.Member, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	o, err := i.org(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]domain.Member, 0, len(o.members))

	for user, role := range o.members {
		u := i.users[user]
		if u == nil {
			continue
		}

		out = append(out, domain.Member{
			ID: "member-" + string(user), User: domain.User{ID: u.ID, Name: u.Name, Email: u.Email}, Role: role,
			FactoryRoles: grants(o.factoryRoles[user]), JoinedAt: o.joined[user], LastActiveAt: i.lastActive(user),
		})
	}

	slices.SortFunc(out, func(a, b domain.Member) int {
		return cmp.Or(cmp.Compare(a.User.Name, b.User.Name), cmp.Compare(a.User.ID, b.User.ID))
	})

	return out, nil
}

// lastActive is when user's newest session began (Better Auth's updatedAt of a session the fake
// never extends); zero without one.
func (i *Identity) lastActive(user domain.UserID) time.Time {
	var last time.Time

	for _, s := range i.sessions {
		if s.user == user && s.session.CreatedAt.After(last) {
			last = s.session.CreatedAt
		}
	}

	return last
}

// grants are factory roles by factory id.
func grants(roles map[domain.FactoryID]domain.FactoryRole) []domain.FactoryGrant {
	out := make([]domain.FactoryGrant, 0, len(roles))
	for _, f := range slices.Sorted(maps.Keys(roles)) {
		out = append(out, domain.FactoryGrant{Factory: f, Role: roles[f]})
	}

	return out
}

// Invitations implements ports.Access.
func (i *Identity) Invitations(ctx context.Context) ([]domain.Invitation, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	o, err := i.org(ctx)
	if err != nil {
		return nil, err
	}

	out := []domain.Invitation{}

	for _, inv := range o.invitations {
		if inv.status == "pending" && i.now().Before(inv.invitation.ExpiresAt) {
			v := inv.invitation
			v.FactoryRoles = slices.Clone(v.FactoryRoles)
			out = append(out, v)
		}
	}

	return out, nil
}

// SetFactoryRole implements ports.Access.
func (i *Identity) SetFactoryRole(ctx context.Context, _ domain.Actor, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if role != "" && !role.Valid() {
		return fmt.Errorf("factory role %q: %w", role, domain.ErrInvalid)
	}

	o, err := i.org(ctx)
	if err != nil {
		return err
	}

	switch o.members[user] {
	case "":
		return fmt.Errorf("member %s: %w", user, ports.ErrNotFound)
	case domain.OrgOwner, domain.OrgAdmin:
		return fmt.Errorf("member %s: %w", user, domain.ErrRoleFromOrg)
	case domain.OrgMember:
	}

	if !slices.Contains(o.factories, f) {
		return fmt.Errorf("factory %s: %w", f, ports.ErrNotFound)
	}

	o.grant(user, f, role)

	return nil
}

// grant gives user role on f; "" removes it.
func (o *identityOrg) grant(user domain.UserID, f domain.FactoryID, role domain.FactoryRole) {
	if role == "" {
		delete(o.factoryRoles[user], f)

		return
	}

	if o.factoryRoles[user] == nil {
		o.factoryRoles[user] = map[domain.FactoryID]domain.FactoryRole{}
	}

	o.factoryRoles[user][f] = role
}

// TransferOwnership implements ports.Access.
func (i *Identity) TransferOwnership(ctx context.Context, _ domain.Actor, from, to domain.UserID) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	o, err := i.org(ctx)
	if err != nil {
		return err
	}

	if o.members[from] != domain.OrgOwner {
		return fmt.Errorf("transfer from %s: %w", from, domain.ErrNotAnOwner)
	}

	if o.members[to] != domain.OrgAdmin {
		return fmt.Errorf("transfer to %s: %w", to, domain.ErrNotAnAdmin)
	}

	o.members[from], o.members[to] = domain.OrgAdmin, domain.OrgOwner

	return nil
}
