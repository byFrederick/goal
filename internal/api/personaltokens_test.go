package api_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	tokensPath       = "/v1/orgs/acme/personal-tokens" //nolint:gosec // a path, not a credential
	memberTokensPath = "/v1/orgs/acme/member-tokens"   //nolint:gosec // a path, not a credential
	// freshCLIToken is acme's owner signed in just now with goal login: a bearer session.
	freshCLIToken = "FreshCLIFreshCLIFreshCLIFresh006" //nolint:gosec // a fabricated session token
	// orgReadToken is the smallest token there is.
	orgReadToken = `{"name":"a","scopes":["org:read"]}` //nolint:gosec // a request body, not a credential
	// The problem codes of personal tokens.
	codeScopeNotAllowed = "scope-not-allowed"
	codeForbidden       = "forbidden"
)

var tokenSecretForm = regexp.MustCompile(`^goal_pat_[A-Za-z0-9_-]{43}$`)

// createToken makes a personal token with session's cookie and returns it with its secret.
func (h signedIn) createToken(t *testing.T, session, body string) (gen.CreatedPersonalToken, response) {
	t.Helper()

	res := h.as(t, session, http.MethodPost, tokensPath, body)
	if res.status != http.StatusCreated {
		t.Fatalf("create token = %d %s", res.status, res.body)
	}

	var out gen.CreatedPersonalToken
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatal(err)
	}

	return out, res
}

// laptop is a token that works: it reads, submits and notes items.
const laptop = `{"name":"laptop","scopes":["org:read","factory:read","item:create","item:operate"]}`

// Spec 3 §4.5 and §11.1 row 48: the secret appears once, with Cache-Control no-store; the token
// expires after 30 days unless asked, is its creator's, and is listed by its hint only.
func TestAPI_PersonalTokens_SecretShownOnce(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	created, res := h.createToken(t, freshToken, laptop)

	tok := created.Token
	if res.header.Get("Cache-Control") != "no-store" || !tokenSecretForm.MatchString(created.Secret) || tok.Hint != created.Secret[:13] {
		t.Errorf("Cache-Control %q, secret %q, hint %q; want no-store, goal_pat_ with 43 characters, its first 13", res.header.Get("Cache-Control"),
			created.Secret, tok.Hint)
	}

	if tok.Owner.Id != string(ownerID) || tok.Name != "laptop" || !tok.ExpiresAt.Equal(t0.Add(30*24*time.Hour)) || len(tok.Scopes) != 4 {
		t.Errorf("token = %+v; want the owner's laptop for 30 days with its four scopes", tok)
	}

	list := h.as(t, freshToken, http.MethodGet, tokensPath, "")
	if list.status != http.StatusOK || strings.Contains(string(list.body), created.Secret[13:]) || !strings.Contains(string(list.body), tok.Hint) {
		t.Errorf("list = %d %s; want the token by its hint, never its secret", list.status, list.body)
	}
}

// Spec 3 §4.5, §8.4 and §11.1 rows 40–44: a token acts as its owner in its own organization, with
// its scopes within the owner's roles; never anything else, never as a platform admin.
func TestAPI_PersonalTokens_ActAsTheirOwner(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	if err := h.identity.SetOrgRole(otherOrg, ownerID, domain.OrgMember); err != nil { // the owner belongs to example-labs too
		t.Fatal(err)
	}

	created, _ := h.createToken(t, freshToken, laptop)
	pat := bearer(created.Secret)
	checkTokenMe(t, h, pat)

	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"submits work", http.MethodPost, acmeItems, submitFix, http.StatusCreated},
		{"reads the board", http.MethodGet, "/v1/orgs/acme/factories/acme/board", "", http.StatusOK},
		{"does not decide without the scope", http.MethodPost, "/v1/orgs/acme/items/acme%231/approve", "", http.StatusForbidden},
		{"never manages keys", http.MethodGet, keysPath, "", http.StatusForbidden},
		{"never lists tokens", http.MethodGet, tokensPath, "", http.StatusForbidden},
		{"never creates a token", http.MethodPost, tokensPath, laptop, http.StatusForbidden},
		{"never reads members", http.MethodGet, "/v1/orgs/acme/members", "", http.StatusForbidden},
		{"never deletes the org", http.MethodDelete, acmePath, `{"confirm":"Acme"}`, http.StatusForbidden},
		{"never creates an org, though its owner may", http.MethodPost, orgsPath, `{"slug":"new","name":"New"}`, http.StatusForbidden},
		{"never reads the security log", http.MethodGet, "/v1/platform/auth-events", "", http.StatusForbidden},
		{"never approves a CLI sign-in", http.MethodGet, "/v1/auth/device/ABCD-EFGH", "", http.StatusForbidden},
		{"is unknown to its owner's other org", http.MethodGet, "/v1/orgs/example-labs", "", http.StatusNotFound},
	} {
		res := h.request(t, c.method, c.path, c.body, pat)
		if res.status != c.want {
			t.Errorf("the token %s: %d %s; want %d", c.name, res.status, res.body, c.want)
		}

		if p := problem403(res); p == codeReauth {
			t.Errorf("the token %s: reauth-required; want forbidden, a token never signs in again", c.name)
		}
	}
}

// checkTokenMe checks who the owner's laptop token says it is: the person, through laptop in
// acme, with no session and no platform admin, listing acme alone.
func checkTokenMe(t *testing.T, h signedIn, pat http.Header) {
	t.Helper()

	var me gen.Me
	if res := h.request(t, http.MethodGet, "/v1/me", "", pat); json.Unmarshal(res.body, &me) != nil || me.Token == nil {
		t.Fatalf("me with the token = %d %s; want the token", res.status, res.body)
	}

	person := me.Kind == gen.MeKindUser && me.User != nil && me.User.Id == string(ownerID) && !me.PlatformAdmin && me.Session == nil
	if !person || me.Token.Name != "laptop" || me.Token.OrgId != string(testOrg) || len(me.Token.Scopes) != 4 {
		t.Errorf("me = %+v; want the owner, through laptop in acme, no session and no platform admin", me)
	}

	checkTokenOrgs(t, h, pat)
}

// checkTokenOrgs checks the owner's laptop token lists acme alone, though its owner is in
// example-labs too.
func checkTokenOrgs(t *testing.T, h signedIn, pat http.Header) {
	t.Helper()

	var orgs gen.OrgList
	if res := h.request(t, http.MethodGet, orgsPath, "", pat); json.Unmarshal(res.body, &orgs) != nil || len(orgs.Orgs) != 1 ||
		orgs.Orgs[0].Id != string(testOrg) {
		t.Errorf("orgs with the token = %d %s; want acme alone", res.status, res.body)
	}
}

// problem403 is a 403's code, "" otherwise.
func problem403(res response) string {
	var p gen.Problem
	if res.status != http.StatusForbidden || json.Unmarshal(res.body, &p) != nil || p.Code == nil {
		return ""
	}

	return *p.Code
}

// Spec 3 §6.7 and §11.1 row 50: a write made with a token says so in the audit log.
func TestAPI_PersonalTokens_WritesSayVia(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	created, _ := h.createToken(t, freshToken, laptop)
	pat := bearer(created.Secret)

	if res := h.request(t, http.MethodPost, acmeItems, submitFix, pat); res.status != http.StatusCreated {
		t.Fatalf("submit = %d %s", res.status, res.body)
	}

	if res := h.request(t, http.MethodPost, acmeNotes, noteBody(t, "check the pool"), pat); res.status != http.StatusAccepted {
		t.Fatalf("note = %d %s", res.status, res.body)
	}

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditItemNoted, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Actor.ID != string(ownerID) || entries[0].Data["via"] != "pat:"+created.Token.Id {
		t.Errorf("audit %+v; want item.noted by the owner via pat:%s", entries, created.Token.Id)
	}
}

// Spec 3 §4.5 and §11.1 rows 47 and 49: creation needs the browser's fresh cookie session (a CLI
// session, a key or a token is refused), scopes tokens may carry that the creator holds, a free
// name and an expiry of at most 90 days; nothing is created otherwise.
func TestAPI_PersonalTokens_Refusals(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	h.identity.AddSession(freshCLIToken, ownerID, domain.Session{ID: "fresh-cli", CreatedAt: t0, ExpiresAt: t0.Add(7 * 24 * time.Hour)})
	h.createToken(t, freshToken, laptop)

	key, _ := h.create(t, `{"name":"ci","scopes":["org:read"],"role_ceiling":"viewer"}`)
	pat, _ := h.createToken(t, freshToken, `{"name":"other","scopes":["org:read"]}`)

	for _, c := range []struct {
		name   string
		header http.Header
		body   string
		want   int
		code   string
	}{
		{"the CLI's fresh session", bearer(freshCLIToken), orgReadToken, 403, "browser-session-required"},
		// goald does not check the cookie's signature (it holds no Better Auth secret): how the
		// session was signed in decides, not how its token arrives (review M1).
		{"the CLI's fresh session as a cookie with a forged signature", cookie(freshCLIToken), orgReadToken, 403, "browser-session-required"},
		{"an API key", bearer(key.Secret), orgReadToken, 403, codeForbidden},
		{"a personal token", bearer(pat.Secret), orgReadToken, 403, codeForbidden},
		{"a stale browser session", cookie(ownerToken), orgReadToken, 403, codeReauth},
		{"a scope tokens never carry", cookie(freshToken), `{"name":"a","scopes":["member:read"]}`, 400, codeScopeNotAllowed},
		{"a fresh scope", cookie(freshToken), `{"name":"a","scopes":["org:delete"]}`, 400, codeScopeNotAllowed},
		{"an unknown scope", cookie(freshToken), `{"name":"a","scopes":["nope:nope"]}`, 400, codeScopeNotAllowed},
		{"a scope the creator lacks", cookie(freshViewerToken), `{"name":"a","scopes":["item:decide"]}`, 400, codeScopeNotAllowed},
		{"an org scope the creator lacks", cookie(freshViewerToken), `{"name":"a","scopes":["factory:create"]}`, 400, codeScopeNotAllowed},
		{"a taken name, case ignored", cookie(freshToken), `{"name":"LAPTOP","scopes":["org:read"]}`, 409, "personal-token-name-taken"},
		{"no scope", cookie(freshToken), `{"name":"a","scopes":[]}`, 400, ""},
		{"an empty name", cookie(freshToken), `{"name":" ","scopes":["org:read"]}`, 400, ""},
		{"over 90 days", cookie(freshToken), `{"name":"a","scopes":["org:read"],"expires_in_days":91}`, 400, ""},
		{"no days", cookie(freshToken), `{"name":"a","scopes":["org:read"],"expires_in_days":0}`, 400, ""},
	} {
		res := h.request(t, http.MethodPost, tokensPath, c.body, c.header)
		if p := problem(t, res); res.status != c.want || (c.code != "" && (p.Code == nil || *p.Code != c.code)) {
			t.Errorf("%s: %d %s; want %d %s", c.name, res.status, res.body, c.want, c.code)
		}
	}

	var list gen.PersonalTokenList
	if err := json.Unmarshal(h.as(t, freshToken, http.MethodGet, tokensPath, "").body, &list); err != nil || len(list.Tokens) != 2 {
		t.Errorf("tokens = %+v, %v; want laptop and other alone", list, err)
	}

	// A viewer's token gets what the viewer holds: a factory scope held on one factory is enough.
	if res := h.as(t, freshViewerToken, http.MethodPost, tokensPath, `{"name":"v","scopes":["factory:read"],"expires_in_days":7}`); res.status != http.StatusCreated {
		t.Errorf("the viewer's token = %d %s; want 201", res.status, res.body)
	}
}

// Spec 3 §4.5: the owner revokes their own token without signing in again and only their own; a
// revocation holds on the next request.
func TestAPI_PersonalTokens_RevokeYourOwn(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	mine, _ := h.createToken(t, freshToken, laptop)

	if res := h.as(t, viewerToken, http.MethodDelete, tokensPath+"/"+mine.Token.Id, ""); res.status != http.StatusNotFound {
		t.Errorf("the viewer revoking the owner's token = %d %s; want 404", res.status, res.body)
	}

	// ownerToken signed in an hour ago: no fresh sign-in needed.
	if res := h.as(t, ownerToken, http.MethodDelete, tokensPath+"/"+mine.Token.Id, ""); res.status != http.StatusNoContent {
		t.Fatalf("revoke own = %d %s; want 204 without signing in again", res.status, res.body)
	}

	if res := h.request(t, http.MethodGet, "/v1/me", "", bearer(mine.Secret)); res.status != http.StatusUnauthorized {
		t.Errorf("the revoked token = %d %s; want 401 at once", res.status, res.body)
	}
}

// Spec 3 §4.5: owners and admins list and revoke every member's tokens, with a fresh sign-in;
// members do not.
func TestAPI_PersonalTokens_MembersTokens(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	h.createToken(t, freshToken, laptop)
	theirs, _ := h.createToken(t, freshViewerToken, `{"name":"laptop","scopes":["org:read"]}`)

	if res := h.as(t, viewerToken, http.MethodGet, memberTokensPath, ""); res.status != http.StatusForbidden {
		t.Errorf("a member listing members' tokens = %d; want 403", res.status)
	}

	var all gen.PersonalTokenList
	if err := json.Unmarshal(h.as(t, ownerToken, http.MethodGet, memberTokensPath, "").body, &all); err != nil || len(all.Tokens) != 2 {
		t.Fatalf("members' tokens = %+v, %v; want both", all, err)
	}

	if res := h.as(t, ownerToken, http.MethodDelete, memberTokensPath+"/"+theirs.Token.Id, ""); problem403(res) != codeReauth {
		t.Errorf("revoking a member's token stale = %d %s; want reauth-required", res.status, res.body)
	}

	if res := h.as(t, freshToken, http.MethodDelete, memberTokensPath+"/"+theirs.Token.Id, ""); res.status != http.StatusNoContent {
		t.Fatalf("revoke the member's = %d %s", res.status, res.body)
	}

	if res := h.request(t, http.MethodGet, "/v1/me", "", bearer(theirs.Secret)); res.status != http.StatusUnauthorized {
		t.Errorf("the member's revoked token = %d; want 401", res.status)
	}

	var own gen.PersonalTokenList
	if err := json.Unmarshal(h.as(t, viewerToken, http.MethodGet, tokensPath, "").body, &own); err != nil || len(own.Tokens) != 1 ||
		own.Tokens[0].RevokedBy == nil || own.Tokens[0].RevokedBy.Id != string(ownerID) {
		t.Errorf("the viewer's tokens = %+v, %v; want theirs, revoked by the owner", own, err)
	}
}

// Spec 3 §11.1 rows 38 and 39: the owner's current roles hold on the next request: a demotion
// narrows the token, a removal ends it for good, and so does the organization's deletion.
func TestAPI_PersonalTokens_FollowTheirOwnersRoles(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	if err := h.identity.SetOrgRole(testOrg, viewerID, domain.OrgAdmin); err != nil {
		t.Fatal(err)
	}

	created, _ := h.createToken(t, freshViewerToken, `{"name":"laptop","scopes":["org:read","factory:read","item:create"]}`)
	pat := bearer(created.Secret)

	if res := h.request(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", pat); res.status != http.StatusOK {
		t.Fatalf("an admin's token on line = %d %s; want 200", res.status, res.body)
	}

	if err := h.identity.SetOrgRole(testOrg, viewerID, domain.OrgMember); err != nil { // back to a viewer of acme only
		t.Fatal(err)
	}

	if res := h.request(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", pat); res.status != http.StatusNotFound {
		t.Errorf("the demoted owner's token on line = %d; want 404 at once", res.status)
	}

	if res := h.request(t, http.MethodPost, acmeItems, submitFix, pat); res.status != http.StatusForbidden {
		t.Errorf("the demoted owner's token submitting on acme = %d; want 403, a viewer's", res.status)
	}

	if err := h.identity.SetOrgRole(testOrg, viewerID, ""); err != nil {
		t.Fatal(err)
	}

	if err := h.identity.SetOrgRole(testOrg, viewerID, domain.OrgMember); err != nil { // invited back
		t.Fatal(err)
	}

	if res := h.request(t, http.MethodGet, "/v1/me", "", pat); res.status != http.StatusUnauthorized {
		t.Errorf("a removed member's token after a re-invitation = %d; want 401", res.status)
	}

	owners, _ := h.createToken(t, freshToken, laptop)
	h.identity.DeleteOrg(testOrg)

	if res := h.request(t, http.MethodGet, "/v1/me", "", bearer(owners.Secret)); res.status != http.StatusUnauthorized {
		t.Errorf("a deleted org's token = %d; want 401", res.status)
	}
}

// Spec 3 §8.8: a token's stream ends when the token is revoked, when it expires and when its
// owner's membership changes.
func TestAPI_PersonalTokens_StreamsEndWithTheToken(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	revoked, _ := h.createToken(t, freshToken, orgReadToken)
	expiring, _ := h.createToken(t, freshToken, `{"name":"expiring","scopes":["org:read"],"expires_in_days":1}`)
	member, _ := h.createToken(t, freshViewerToken, `{"name":"member","scopes":["org:read"]}`)

	events := h.streamWith(t, bearer(revoked.Secret))
	h.access.Publish(ports.AccessNotice{Kind: ports.AccessPersonalToken, OrgID: testOrg, UserID: ownerID, TokenID: domain.PersonalTokenID(expiring.Token.Id)})
	h.access.Publish(ports.AccessNotice{Kind: ports.AccessPersonalToken, OrgID: testOrg, UserID: ownerID, TokenID: domain.PersonalTokenID(revoked.Token.Id)})

	if !ended(events) {
		t.Error("the stream went on after its token was revoked")
	}

	events = h.streamWith(t, bearer(member.Secret))
	h.access.Publish(ports.AccessNotice{Kind: ports.AccessMember, OrgID: testOrg, UserID: viewerID})

	if !ended(events) {
		t.Error("the stream went on after its owner's membership changed")
	}

	events = h.streamWith(t, bearer(expiring.Secret))
	h.clock.Advance(24 * time.Hour)

	if !ended(events) {
		t.Error("the stream went on past its token's expiry")
	}
}

// Under GOAL_AUTH=none nobody signs in, so nobody has personal tokens.
func TestAPI_PersonalTokens_LocalUser(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var list gen.PersonalTokenList
	if res := h.request(t, http.MethodGet, tokensPath, "", nil); json.Unmarshal(res.body, &list) != nil || len(list.Tokens) != 0 {
		t.Errorf("list = %d %s; want none", res.status, res.body)
	}

	if res := h.request(t, http.MethodPost, tokensPath, laptop, nil); res.status != http.StatusForbidden {
		t.Errorf("create = %d %s; want 403: personal tokens need sign-in", res.status, res.body)
	}
}
