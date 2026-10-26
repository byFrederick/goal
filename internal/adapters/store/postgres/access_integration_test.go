//go:build integration

package postgres

import (
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/identitytest"
)

// Invite adds an invitation as Better Auth's invite-member writes it, factory roles as the JSON
// text the identity service stores ([{"factoryId","role"}]).
func (s pgSeed) Invite(t *testing.T, org domain.OrgID, inviter domain.UserID, email string, role domain.OrgRole,
	grants []domain.FactoryGrant, status string, expires time.Time,
) string {
	t.Helper()

	var roles *string

	if len(grants) > 0 {
		type grant struct {
			FactoryID string `json:"factoryId"`
			Role      string `json:"role"`
		}

		out := make([]grant, 0, len(grants))
		for _, g := range grants {
			out = append(out, grant{FactoryID: string(g.Factory), Role: string(g.Role)})
		}

		raw, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}

		text := string(raw)
		roles = &text
	}

	var id string
	if err := s.d.auth.QueryRow(t.Context(), `INSERT INTO auth.invitation ("organizationId", email, role, status, "expiresAt", "inviterId", "factoryRoles")
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id::text`, string(org), email, string(role), status, expires, string(inviter), roles).Scan(&id); err != nil {
		t.Fatal(err)
	}

	return id
}

// Accept accepts the invitation as the identity service does: Better Auth marks it accepted and
// adds the member, then the afterAcceptInvitation hook applies its factory roles.
func (s pgSeed) Accept(t *testing.T, invitation string, user domain.UserID) {
	t.Helper()

	s.d.exec(s.d.auth, `UPDATE auth.invitation SET status = 'accepted' WHERE id = $1`, invitation)
	s.d.exec(s.d.auth, `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt")
		SELECT "organizationId", $2, role, now() FROM auth.invitation WHERE id = $1`, invitation, string(user))
	s.d.exec(s.d.auth, `SELECT goal_apply_invitation_roles($1)`, invitation)
}

func TestAccess_Conformance(t *testing.T) {
	t.Parallel()

	identitytest.RunAccess(t, func(t *testing.T) (ports.Identity, ports.Access, identitytest.AccessSeed) {
		t.Helper()

		d := newIdentityDB(t)

		return NewIdentity(d.s), NewAccess(d.s), pgSeed{d: d}
	})
}

// Each factory role change is one audit entry by whoever made it, about the member; a change to
// the same role writes nothing. A transfer is audited by goal_transfer_ownership: both role
// changes, stamped with the actor, then org.ownership_transferred.
func TestAccess_Audit(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}
	acme := s.Org(t, "Acme")
	f := s.Factory(t, acme)
	ada, bob := s.User(t, "Owner", "owner@example.com", false), s.User(t, "Bob", "bob@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, bob, domain.OrgMember)
	a := NewAccess(d.s)
	ctx := domain.WithOrg(t.Context(), acme)
	by := domain.Actor{Kind: domain.ActorUser, ID: string(ada), Name: "Owner"}
	start := len(d.logOf(acme))

	for _, role := range []domain.FactoryRole{domain.FactoryViewer, domain.FactoryApprover, domain.FactoryApprover, ""} {
		if err := a.SetFactoryRole(ctx, by, bob, f, role); err != nil {
			t.Fatal(err)
		}
	}

	member := "member:" + string(bob)
	about := func(change map[string]any) map[string]any {
		maps.Copy(change, map[string]any{"user_id": string(bob), "factory_id": string(f)})

		return change
	}
	want := []logged{
		{domain.AuditFactoryRoleGranted, by.Ref(), by.Name, member, about(map[string]any{"to": "viewer"})},
		{domain.AuditFactoryRoleChanged, by.Ref(), by.Name, member, about(map[string]any{fieldFrom: "viewer", "to": "approver"})},
		{domain.AuditFactoryRoleRemoved, by.Ref(), by.Name, member, about(map[string]any{fieldFrom: "approver"})},
	}

	if got := d.logOf(acme)[start:]; !reflect.DeepEqual(got, want) {
		t.Errorf("audit = %+v; want %+v", got, want)
	}

	s.SetRole(t, acme, bob, domain.OrgAdmin)
	start = len(d.logOf(acme))

	if err := a.TransferOwnership(ctx, by, ada, bob); err != nil {
		t.Fatal(err)
	}

	got := d.logOf(acme)[start:]
	if len(got) != 3 || got[2].action != domain.AuditOrgOwnershipTransferred || got[2].actor != by.Ref() ||
		got[2].data[fieldFrom] != string(ada) || got[2].data["to"] != string(bob) {
		t.Errorf("audit of the transfer = %+v; want two role changes, then org.ownership_transferred by Ada from Ada to Bob", got)
	}

	for _, e := range got[:2] {
		if !strings.HasSuffix(e.action, ".role_changed") || e.actor != by.Ref() {
			t.Errorf("role change %+v; want member.role_changed by Ada", e)
		}
	}
}

// Two owners handing the organization to each other's admin at once both succeed or one is
// refused, and the organization never ends up without an owner (00042's deferred trigger).
func TestAccess_ConcurrentTransfersKeepAnOwner(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}
	acme := s.Org(t, "Acme")
	ada, bob := s.User(t, "Owner", "owner@example.com", false), s.User(t, "Bob", "bob@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, bob, domain.OrgAdmin)
	a := NewAccess(d.s)
	ctx := domain.WithOrg(t.Context(), acme)

	var wg sync.WaitGroup

	errs := make([]error, 2)

	for i, pair := range [][2]domain.UserID{{ada, bob}, {ada, bob}} {
		wg.Go(func() {
			errs[i] = a.TransferOwnership(ctx, domain.Actor{Kind: domain.ActorUser, ID: string(pair[0]), Name: "x"}, pair[0], pair[1])
		})
	}

	wg.Wait()

	ok := 0

	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrNotAnOwner), errors.Is(err, domain.ErrNotAnAdmin):
		default:
			t.Errorf("concurrent transfer = %v; want success or a refusal", err)
		}
	}

	if ok != 1 || d.roleOf(acme, string(bob)) != string(domain.OrgOwner) || d.roleOf(acme, string(ada)) != string(domain.OrgAdmin) {
		t.Errorf("%d transfers succeeded, Bob %q, Ada %q; want exactly one: Bob owner, Ada admin", ok, d.roleOf(acme, string(bob)), d.roleOf(acme, string(ada)))
	}
}
