package api

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// The two-org matrix's personal tokens and the owner's CLI session (spec 3 §13, 3-12b).

// mxGone is an organization deleted after one of its members made a token there.
const mxGone domain.OrgID = "019a1c00-0000-7000-8000-00000000c0c0"

// The matrix people whose tokens and sessions these columns compare with.
const (
	mxOwnerName      = "owner"
	mxViewerName     = "viewer"
	mxApproverName   = "approver"
	mxOtherOwnerName = "other-org-owner"
	mxBearerName     = "bearer-owner"
)

// mxBearerSession is the owner's session as goal login stores it: sent as a bearer value.
const mxBearerSession = "mxbearer000000000000000000000001"

// What happens to a token's owner, membership or organization after the token is made.
const (
	mxLive       = ""
	mxDemoted    = "demoted"     // the owner is made a member, with no factory role
	mxRemoved    = "removed"     // the member leaves the organization
	mxExpired    = "expired"     // the token expired a day ago
	mxRevoked    = "revoked"     // the token was revoked
	mxOrgDeleted = "org-deleted" // the token's organization was deleted
)

// mxToken is one personal token of the matrix: of one of mxPeople (of), or of a person of its own
// (own), with its scopes, and what happened afterwards.
type mxToken struct {
	name         string // the matrix's column
	of           string // an mxPeople name
	own          mxPerson
	scopes       []domain.Permission
	after        string
	secret, hash string
	id           domain.PersonalTokenID
}

// person is the token's owner.
func (k mxToken) person() mxPerson {
	if k.of == "" {
		return k.own
	}

	for _, p := range mxPeople() {
		if p.name == k.of {
			return p
		}
	}

	panic("no matrix person " + k.of)
}

func (k mxToken) header() http.Header {
	return http.Header{"Authorization": {"Bearer " + k.secret}}
}

// mxPATScopes is every permission personal tokens may carry.
func mxPATScopes() []domain.Permission {
	all := []domain.Permission{}

	for _, name := range slices.Sorted(maps.Keys(permissionRules)) {
		if permissionRules[name].pat {
			all = append(all, domain.Permission(name))
		}
	}

	return all
}

// mxTokens are the matrix's personal tokens: an approver's with a few scopes, the owner's, a
// viewer's and the other org's owner's with every pat scope; a token whose owner was demoted, one
// whose owner left, a platform admin's; an expired, a revoked one and one of a deleted
// organization.
func mxTokens() []mxToken {
	all := mxPATScopes()
	tokens := []mxToken{
		{name: "pat-approver", of: mxApproverName, scopes: []domain.Permission{"factory:read", "item:create"}},
		{name: "pat-owner", of: mxOwnerName, scopes: all},
		{name: "pat-viewer", of: mxViewerName, scopes: all},
		{name: "pat-other-org", of: mxOtherOwnerName, scopes: all},
		{name: "pat-expired", of: mxOwnerName, scopes: all, after: mxExpired},
		{name: "pat-revoked", of: mxOwnerName, scopes: all, after: mxRevoked},
		{name: "pat-demoted", own: mxPerson{name: "demoted-owner", org: mxAcme, role: domain.OrgOwner}, scopes: all, after: mxDemoted},
		{
			name: "pat-removed", own: mxPerson{name: "removed-member", org: mxAcme, role: domain.OrgMember, factory: domain.FactoryEditor},
			scopes: all, after: mxRemoved,
		},
		{name: "pat-platform", own: mxPerson{name: "admin-member", org: mxAcme, role: domain.OrgOwner, platformAdmin: true}, scopes: all},
		{name: "pat-deleted-org", own: mxPerson{name: "gone-owner", org: mxGone, role: domain.OrgOwner}, scopes: all, after: mxOrgDeleted},
	}

	for n := range tokens {
		tokens[n].secret = fmt.Sprintf("%s%043d", personalTokenPrefix, n)
		tokens[n].hash = apiKeyHash(tokens[n].secret)
		tokens[n].id = domain.PersonalTokenID(fmt.Sprintf("019a1c00-0000-7000-8000-%012d", 200+n))

		if tokens[n].of == "" {
			tokens[n].own.user = domain.UserID(fmt.Sprintf("019a1c00-0000-7000-8000-%012d", 300+n))
		}
	}

	return tokens
}

// mxTokenPrincipals adds the matrix's tokens, their own people and the owner's CLI session to
// the fake, and returns their columns.
func mxTokenPrincipals(t *testing.T, id *porttest.Identity) []mxPrincipal {
	t.Helper()

	must := func(err error) {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}
	}

	owner := mxPeople()[0]
	id.AddSession(mxBearerSession, owner.user, owner.session())
	out := []mxPrincipal{{
		name: mxBearerName, header: http.Header{"Authorization": {"Bearer " + mxBearerSession}},
		resolve: func(org domain.OrgID) (domain.Principal, error) {
			p, err := id.Session(t.Context(), mxBearerSession, org)
			if p.Session != nil {
				p.Session.Bearer = true
			}

			return p, err
		},
	}}

	id.AddOrg(domain.Org{ID: mxGone, Slug: "gone", Name: "Gone"})

	for _, k := range mxTokens() {
		p := k.person()
		if k.of == "" {
			must(id.AddUser(porttest.IdentityUser{ID: p.user, Name: p.name, Email: p.name + "@example.com", PlatformAdmin: p.platformAdmin}))
			must(id.SetOrgRole(p.org, p.user, p.role))

			if p.factory != "" {
				must(id.GrantFactoryRole(p.org, p.user, mxF, p.factory))
			}
		}

		must(id.AddToken(k.hash, porttest.IdentityToken{
			ID: k.id, Org: p.org, User: p.user, Name: k.name, Hint: k.secret[:13], Scopes: k.scopes,
			CreatedAt: mxNow.Add(-time.Hour), ExpiresAt: mxNow.Add(30 * 24 * time.Hour),
		}))

		switch k.after {
		case mxDemoted:
			must(id.SetOrgRole(p.org, p.user, domain.OrgMember))
		case mxRemoved:
			must(id.SetOrgRole(p.org, p.user, ""))
		case mxExpired:
			id.ExpireToken(k.id)
		case mxRevoked:
			id.RevokeToken(k.id)
		case mxOrgDeleted:
			id.DeleteOrg(p.org)
		}

		out = append(out, mxPrincipal{name: k.name, header: k.header(), resolve: func(domain.OrgID) (domain.Principal, error) {
			return id.PersonalToken(t.Context(), k.hash)
		}})
	}

	return out
}

// mxTokenOwners are the tokens whose owner is a person of the matrix with a session of their own:
// a token never answers more than that session.
var mxTokenOwners = map[string]string{
	"pat-approver": mxApproverName, "pat-owner": mxOwnerName, "pat-viewer": mxViewerName, "pat-other-org": mxOtherOwnerName,
}

// checkTokenProperties checks what holds for personal tokens whatever the table says (spec 3 §8.4,
// §13): a token passes only a pat permission, never a fresh one, never a platform route or
// creating an organization, never more than its owner's own session, and is never asked to sign
// in again; neither a token nor the CLI's bearer session creates a token.
func checkTokenProperties(t *testing.T, id string, op contractOp, principal string, org domain.OrgID, answer gen.Problem,
	sessionAnswer func(person string) gen.Problem,
) {
	t.Helper()

	where := fmt.Sprintf("%s by %s in %s", id, principal, org)
	token := strings.HasPrefix(principal, "pat-")

	if (token || principal == mxBearerName) && id == "createPersonalToken" && answer.Status == http.StatusOK {
		t.Errorf("%s: passed, want a token never made with a token or a CLI session", where)
	}

	if !token || op.scope == string(permPublic) {
		return
	}

	if problem := tokenAnswerProblem(op.scope, answer); problem != "" {
		t.Errorf("%s: %s", where, problem)
	}

	if person, ok := mxTokenOwners[principal]; ok && answer.Status == http.StatusOK && sessionAnswer(person).Status != http.StatusOK {
		t.Errorf("%s: passed, but %s's own session does not", where, person)
	}
}

// tokenAnswerProblem is what is wrong with a token's answer for an operation needing scope, ""
// when nothing is.
func tokenAnswerProblem(scope string, answer gen.Problem) string {
	ru := permissionRules[scope]
	passed := answer.Status == http.StatusOK

	switch {
	case answer.Code != nil && *answer.Code == "reauth-required":
		return "reauth-required, want a token never asked to sign in again"
	case passed && (!ru.pat || ru.fresh || ru.platformAdmin):
		return "passed " + scope + ", which tokens never hold"
	case passed && (strings.HasPrefix(scope, "platform:") || scope == "org:create"):
		return "passed a platform permission"
	default:
		return ""
	}
}
