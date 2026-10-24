package api_test

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// The outcomes of an authorization, as the API answers them.
const (
	wantAllow           = "allow"
	wantForbidden       = "forbidden"       // 403
	wantHidden          = "hidden"          // 404, as if the resource did not exist
	wantUnauthenticated = "unauthenticated" // 401
	wantReauth          = "reauth"          // 403 reauth-required
)

func outcome(err error) string {
	switch {
	case err == nil:
		return wantAllow
	case errors.Is(err, domain.ErrUnauthenticated):
		return wantUnauthenticated
	case errors.Is(err, domain.ErrReauthRequired):
		return wantReauth
	case errors.Is(err, ports.ErrNotFound):
		return wantHidden
	default:
		return wantForbidden
	}
}

const (
	grantsOrg   domain.OrgID     = "019a0000-0000-7000-8000-00000000000a"
	grantsOther domain.OrgID     = "019a0000-0000-7000-8000-00000000000b"
	grantsF     domain.FactoryID = "019a0000-0000-7000-8000-0000000000f1"
	grantsG     domain.FactoryID = "019a0000-0000-7000-8000-0000000000f2"
)

// grantsNow is the clock of the role grants; fresh sessions started a minute before it.
var grantsNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func grantUser(role domain.OrgRole, factoryRoles map[domain.FactoryID]domain.FactoryRole) domain.Principal {
	return domain.Principal{
		Kind: domain.PrincipalUser, User: "019a0000-0000-7000-8000-0000000000aa", Name: "Ada", Org: grantsOrg, OrgRole: role,
		FactoryRoles: factoryRoles, Session: &domain.Session{ID: "s", CreatedAt: grantsNow.Add(-time.Minute), ExpiresAt: grantsNow.Add(time.Hour)},
	}
}

func grantKey(ceiling domain.FactoryRole, factories []domain.FactoryID, scopes ...domain.Permission) domain.Principal {
	return domain.Principal{
		Kind: domain.PrincipalService, Key: "019a0000-0000-7000-8000-0000000000kk", Name: "ci", Org: grantsOrg,
		Scopes: scopes, KeyFactories: factories, KeyCeiling: ceiling,
	}
}

// Operations the role grants ask about more than once.
const (
	opGetOrg    = "getOrg"
	opDeleteOrg = "deleteOrg"
	opGetBoard  = "getBoard"
	opApprove   = "approveItem"
	opPublish   = "publishDraft"
	opCreateOrg = "createOrg"
)

// TestPermissions_RoleGrants pins the product rules of spec 3 §4.2–§4.3 and §8.4 on fabricated
// principals: who may do what, and what an invisible factory or another org looks like.
func TestPermissions_RoleGrants(t *testing.T) {
	t.Parallel()

	owner := grantUser(domain.OrgOwner, nil)
	admin := grantUser(domain.OrgAdmin, nil)
	member := grantUser(domain.OrgMember, nil)
	viewer := grantUser(domain.OrgMember, map[domain.FactoryID]domain.FactoryRole{grantsF: domain.FactoryViewer})
	approver := grantUser(domain.OrgMember, map[domain.FactoryID]domain.FactoryRole{grantsF: domain.FactoryApprover})
	editor := grantUser(domain.OrgMember, map[domain.FactoryID]domain.FactoryRole{grantsF: domain.FactoryEditor})
	stale := grantUser(domain.OrgOwner, nil)
	stale.Session = &domain.Session{CreatedAt: grantsNow.Add(-domain.FreshFor - time.Second), ExpiresAt: grantsNow.Add(time.Hour)}
	noSession := grantUser(domain.OrgOwner, nil)
	noSession.Session = nil
	platformAdmin := grantUser("", nil)
	platformAdmin.PlatformAdmin = true
	key := grantKey(domain.FactoryApprover, nil, "org:read", "factory:read", "item:decide", "member:manage", "org:delete")

	tests := []struct {
		name       string
		p          domain.Principal
		operation  string
		permission domain.Permission // instead of the operation's scope
		org        domain.OrgID
		factory    domain.FactoryID
		want       string
	}{
		{name: "anonymous reads health", p: domain.Principal{}, operation: "getHealth", want: wantAllow},
		{name: "anonymous lists nothing", p: domain.Principal{}, operation: "listOrgs", want: wantUnauthenticated},
		{name: "anonymous inside an org", p: domain.Principal{}, operation: opGetOrg, org: grantsOrg, want: wantUnauthenticated},
		{name: "an operation the table lacks is refused", p: owner, operation: "launchMissiles", org: grantsOrg, want: wantForbidden},

		{name: "owner deletes the org", p: owner, operation: opDeleteOrg, org: grantsOrg, want: wantAllow},
		{name: "admin cannot delete the org", p: admin, operation: opDeleteOrg, org: grantsOrg, want: wantForbidden},
		{name: "admin cannot transfer the org", p: admin, permission: "org:transfer", org: grantsOrg, want: wantForbidden},
		{name: "admin manages settings", p: admin, operation: "updateOrgSettings", org: grantsOrg, want: wantAllow},
		{name: "admin reads the audit log", p: admin, operation: "listAudit", org: grantsOrg, want: wantAllow},
		{name: "member cannot read the audit log", p: member, operation: "listAudit", org: grantsOrg, want: wantForbidden},
		{name: "member cannot manage settings", p: member, operation: "updateOrgSettings", org: grantsOrg, want: wantForbidden},
		{name: "member reads the org", p: member, operation: opGetOrg, org: grantsOrg, want: wantAllow},
		{name: "member cannot create a factory", p: member, operation: "createFactory", org: grantsOrg, want: wantForbidden},
		{name: "admin creates a factory", p: admin, operation: "createFactory", org: grantsOrg, want: wantAllow},

		{name: "member without a role cannot see the factory", p: member, operation: opGetBoard, org: grantsOrg, factory: grantsF, want: wantHidden},
		{name: "member without a role passes the middleware", p: member, operation: opGetBoard, org: grantsOrg, want: wantAllow},
		{name: "viewer reads the board", p: viewer, operation: opGetBoard, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "viewer cannot decide", p: viewer, operation: opApprove, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "viewer cannot pull the andon", p: viewer, operation: "pullAndon", org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "viewer sees only their factory", p: viewer, operation: opGetBoard, org: grantsOrg, factory: grantsG, want: wantHidden},
		{name: "approver decides", p: approver, operation: opApprove, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "approver submits work", p: approver, operation: "submitItem", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "approver pulls the andon", p: approver, operation: "pullAndon", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "approver cannot publish", p: approver, operation: opPublish, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "approver cannot edit the draft", p: approver, operation: "saveDraftSection", org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "editor publishes", p: editor, operation: opPublish, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "editor pauses the factory", p: editor, operation: "pauseFactory", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "editor includes approver", p: editor, operation: "rejectItem", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "admin edits every factory", p: admin, operation: opPublish, org: grantsOrg, factory: grantsG, want: wantAllow},

		{name: "a member of another org does not see this one", p: owner, operation: opGetOrg, org: grantsOther, want: wantHidden},
		{name: "a platform admin is no member", p: platformAdmin, operation: opGetOrg, org: grantsOrg, want: wantHidden},
		{name: "a platform admin creates orgs", p: platformAdmin, operation: opCreateOrg, want: wantAllow},
		{name: "an owner who is not a platform admin cannot", p: owner, operation: opCreateOrg, want: wantForbidden},

		{name: "a stale session cannot delete the org", p: stale, operation: opDeleteOrg, org: grantsOrg, want: wantReauth},
		{name: "a stale session still reads", p: stale, operation: opGetOrg, org: grantsOrg, want: wantAllow},
		{name: "no session is not fresh", p: noSession, operation: opDeleteOrg, org: grantsOrg, want: wantReauth},

		{name: "key decides within its scopes", p: key, operation: opApprove, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "key without the scope", p: key, operation: "submitItem", org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "key above its ceiling", p: grantKey(domain.FactoryViewer, nil, "item:decide"), operation: opApprove, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "no key holds member:manage", p: key, permission: "member:manage", org: grantsOrg, want: wantForbidden},
		{name: "no key deletes the org", p: key, operation: opDeleteOrg, org: grantsOrg, want: wantForbidden},
		{name: "a key lists its orgs", p: key, operation: "listOrgs", want: wantAllow},
		{name: "a key cannot create orgs", p: key, operation: opCreateOrg, want: wantForbidden},
		{name: "a key restricted to another factory", p: grantKey(domain.FactoryEditor, []domain.FactoryID{grantsG}, "factory:read"), operation: opGetBoard, org: grantsOrg, factory: grantsF, want: wantHidden},
		{name: "a key of another org", p: key, operation: opGetOrg, org: grantsOther, want: wantHidden},
	}

	authz := api.NewPermissions(porttest.NewClock(grantsNow))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := authz.Authorize(t.Context(), api.AccessRequest{
				Principal: tt.p, Operation: tt.operation, Permission: tt.permission, Org: tt.org, Factory: tt.factory,
			})
			if got := outcome(err); got != tt.want {
				t.Errorf("Authorize = %s (%v), want %s", got, err, tt.want)
			}
		})
	}
}
