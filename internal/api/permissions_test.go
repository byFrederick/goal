package api_test

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The outcomes of an authorization, as the API answers them.
const (
	wantAllow           = "allow"
	wantForbidden       = "forbidden"       // 403
	wantHidden          = "hidden"          // 404, as if the resource did not exist
	wantUnauthenticated = "unauthenticated" // 401
	wantReauth          = "reauth"          // 403 reauth-required
	wantBrowser         = "browser"         // 403 browser-session-required
)

func outcome(err error) string {
	switch {
	case err == nil:
		return wantAllow
	case err.Error() == "personal tokens are created in the browser, signed in there":
		return wantBrowser
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
		FactoryRoles: factoryRoles, Session: &domain.Session{ID: "s", CreatedAt: grantsNow.Add(-time.Minute), ExpiresAt: grantsNow.Add(time.Hour), Browser: true},
	}
}

func grantKey(ceiling domain.FactoryRole, factories []domain.FactoryID, scopes ...domain.Permission) domain.Principal {
	return domain.Principal{
		Kind: domain.PrincipalService, Key: "019a0000-0000-7000-8000-0000000000kk", Name: "ci", Org: grantsOrg,
		Scopes: scopes, KeyFactories: factories, KeyCeiling: ceiling,
	}
}

// grantToken is user's principal acting through a personal token with scopes.
func grantToken(user domain.Principal, scopes ...domain.Permission) domain.Principal {
	user.Session = nil
	user.Token = &domain.TokenGrant{ID: "019a0000-0000-7000-8000-0000000000tt", Name: "laptop", Scopes: scopes, ExpiresAt: grantsNow.Add(time.Hour)}

	return user
}

// Operations the role grants ask about more than once.
const (
	opGetOrg    = "getOrg"
	opDeleteOrg = "deleteOrg"
	opGetBoard  = "getBoard"
	opApprove   = "approveItem"
	opPublish   = "publishDraft"
	opCreateOrg = "createOrg"
	opListAudit = "listAudit"
	opArchive   = "archiveFactory"
	opUnarchive = "restoreFactory"
	opPurge     = "purgeFactory"
	opDelete    = "deleteFactory"
)

// Permissions the grants ask about more than once.
const (
	scopeOrgRead    domain.Permission = "org:read"
	permTokenManage domain.Permission = "personal_token:manage"
	permTokenRead   domain.Permission = "personal_token:read"
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

	tests := []grant{
		{name: "anonymous reads health", p: domain.Principal{}, operation: "getHealth", want: wantAllow},
		{name: "anonymous lists nothing", p: domain.Principal{}, operation: "listOrgs", want: wantUnauthenticated},
		{name: "anonymous inside an org", p: domain.Principal{}, operation: opGetOrg, org: grantsOrg, want: wantUnauthenticated},
		{name: "an operation the table lacks is refused", p: owner, operation: "launchMissiles", org: grantsOrg, want: wantForbidden},

		{name: "owner deletes the org", p: owner, operation: opDeleteOrg, org: grantsOrg, want: wantAllow},
		{name: "admin cannot delete the org", p: admin, operation: opDeleteOrg, org: grantsOrg, want: wantForbidden},
		{name: "admin cannot transfer the org", p: admin, permission: "org:transfer", org: grantsOrg, want: wantForbidden},
		{name: "admin manages settings", p: admin, operation: "updateOrgSettings", org: grantsOrg, want: wantAllow},
		{name: "admin reads the audit log", p: admin, operation: opListAudit, org: grantsOrg, want: wantAllow},
		{name: "member cannot read the audit log", p: member, operation: opListAudit, org: grantsOrg, want: wantForbidden},
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
		{name: "viewer reads the judges", p: viewer, operation: "listJudges", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "approver cannot change a judge", p: approver, operation: "updateJudge", org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "approver cannot try a judge", p: approver, operation: "tryJudge", org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "editor adds a judge", p: editor, operation: "createJudge", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "approver cannot edit the draft", p: approver, operation: "saveDraftSection", org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "editor publishes", p: editor, operation: opPublish, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "editor pauses the factory", p: editor, operation: "pauseFactory", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "editor includes approver", p: editor, operation: "rejectItem", org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "admin edits every factory", p: admin, operation: opPublish, org: grantsOrg, factory: grantsG, want: wantAllow},
		{name: "owner archives a factory", p: owner, operation: opArchive, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "owner restores a factory", p: owner, operation: opUnarchive, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "admin archives a factory", p: admin, operation: opArchive, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "admin restores a factory", p: admin, operation: opUnarchive, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "editor cannot archive the factory", p: editor, operation: opArchive, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "editor cannot restore the factory", p: editor, operation: opUnarchive, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "owner purges a factory", p: owner, operation: opPurge, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "admin purges a factory", p: admin, operation: opPurge, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "editor cannot purge the factory", p: editor, operation: opPurge, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "editor deletes a factory never published", p: editor, operation: opDelete, org: grantsOrg, factory: grantsF, want: wantAllow},

		{name: "a member of another org does not see this one", p: owner, operation: opGetOrg, org: grantsOther, want: wantHidden},
		{name: "a platform admin is no member", p: platformAdmin, operation: opGetOrg, org: grantsOrg, want: wantHidden},
		{name: "a platform admin creates orgs", p: platformAdmin, operation: opCreateOrg, want: wantAllow},
		{name: "a person who is not a platform admin creates orgs too", p: owner, operation: opCreateOrg, want: wantAllow},

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

	checkGrants(t, tests)
}

// TestPermissions_PersonalTokenGrants pins spec 3 §4.5 and §8.4 on fabricated principals: a
// personal token holds its scopes within its owner's roles now, never a fresh, member, key,
// device or platform permission; only a session signed in through the browser creates one.
func TestPermissions_PersonalTokenGrants(t *testing.T) {
	t.Parallel()

	owner := grantUser(domain.OrgOwner, nil)
	admin := grantUser(domain.OrgAdmin, nil)
	member := grantUser(domain.OrgMember, nil)
	viewer := grantUser(domain.OrgMember, map[domain.FactoryID]domain.FactoryRole{grantsF: domain.FactoryViewer})
	approver := grantUser(domain.OrgMember, map[domain.FactoryID]domain.FactoryRole{grantsF: domain.FactoryApprover})
	stale := grantUser(domain.OrgOwner, nil)
	stale.Session = &domain.Session{CreatedAt: grantsNow.Add(-domain.FreshFor - time.Second), ExpiresAt: grantsNow.Add(time.Hour), Browser: true}
	key := grantKey(domain.FactoryApprover, nil, scopeOrgRead, "factory:read", "item:decide")
	everything := []domain.Permission{
		scopeOrgRead, "factory:read", "item:create", "item:decide", "item:operate", "andon:manage", "definition:edit",
		"definition:publish", "factory:create", "org:delete", "member:manage", "api_key:manage", "device:read", permTokenManage,
		permTokenRead, "org:create", "platform:read",
	}
	ownerToken := grantToken(owner, everything...)
	adminOfPlatform := grantUser(domain.OrgOwner, nil)
	adminOfPlatform.PlatformAdmin = true
	// The CLI's session: signed in by goal login's device flow (Session.Browser false), however
	// its token is sent.
	bearer := grantUser(domain.OrgOwner, nil)
	bearer.Session.Browser = false

	checkGrants(t, []grant{
		{name: "token decides within its scopes", p: grantToken(approver, "item:decide"), operation: opApprove, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "token without the scope", p: grantToken(approver, "factory:read"), operation: opApprove, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "token beyond its owner's role", p: grantToken(viewer, "item:decide"), operation: opApprove, org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "token of a member without a role", p: grantToken(member, "factory:read"), operation: opGetBoard, org: grantsOrg, factory: grantsF, want: wantHidden},
		{name: "token hides an invisible factory before its scopes", p: grantToken(member), operation: opApprove, org: grantsOrg, factory: grantsF, want: wantHidden},
		{name: "token of another org", p: ownerToken, operation: opGetOrg, org: grantsOther, want: wantHidden},
		{name: "token's owner says who they are without a scope", p: grantToken(viewer), operation: "getMe", want: wantAllow},
		{name: "token of an owner publishes when scoped", p: ownerToken, operation: opPublish, org: grantsOrg, factory: grantsF, want: wantAllow},
		{name: "token never deletes the org, never asked to sign in again", p: ownerToken, operation: opDeleteOrg, org: grantsOrg, want: wantForbidden},
		{name: "token never manages members", p: ownerToken, permission: "member:manage", org: grantsOrg, want: wantForbidden},
		{name: "token never manages keys", p: ownerToken, permission: "api_key:manage", org: grantsOrg, want: wantForbidden},
		{name: "token never reads the audit log", p: ownerToken, operation: opListAudit, org: grantsOrg, want: wantForbidden},
		{name: "token never pauses a factory", p: ownerToken, operation: "pauseFactory", org: grantsOrg, factory: grantsF, want: wantForbidden},
		{name: "token never creates a token", p: ownerToken, permission: permTokenManage, org: grantsOrg, want: wantForbidden},
		{name: "token never lists tokens", p: ownerToken, permission: permTokenRead, org: grantsOrg, want: wantForbidden},
		{name: "token never approves a CLI sign-in", p: ownerToken, permission: "device:read", want: wantForbidden},
		{name: "token of a platform admin cannot create orgs", p: grantToken(adminOfPlatform, everything...), operation: opCreateOrg, want: wantForbidden},
		{name: "token of a platform admin cannot read the security log", p: grantToken(adminOfPlatform, everything...), permission: "platform:read", want: wantForbidden},

		{name: "a person creates a token in the browser", p: owner, permission: permTokenManage, org: grantsOrg, want: wantAllow},
		{name: "a member creates a token", p: member, permission: permTokenManage, org: grantsOrg, want: wantAllow},
		{name: "a stale browser session signs in again to create one", p: stale, permission: permTokenManage, org: grantsOrg, want: wantReauth},
		{name: "the CLI's session never creates one", p: bearer, permission: permTokenManage, org: grantsOrg, want: wantBrowser},
		{name: "the CLI's session still revokes one", p: bearer, permission: permTokenRead, org: grantsOrg, want: wantAllow},
		{name: "a key never creates one", p: key, permission: permTokenManage, org: grantsOrg, want: wantForbidden},
		{name: "a stale session revokes its own without signing in again", p: stale, operation: "revokePersonalToken", org: grantsOrg, want: wantAllow},
		{name: "a member cannot list members' tokens", p: member, operation: "listMemberTokens", org: grantsOrg, want: wantForbidden},
		{name: "an admin revokes members' tokens", p: admin, operation: "revokeMemberToken", org: grantsOrg, want: wantAllow},
	})
}

// grant is one authorization question and the answer the API gives it.
type grant struct {
	name       string
	p          domain.Principal
	operation  string
	permission domain.Permission // instead of the operation's scope
	org        domain.OrgID
	factory    domain.FactoryID
	want       string
}

// checkGrants asks the table's Authorizer each question.
func checkGrants(t *testing.T, tests []grant) {
	t.Helper()

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
