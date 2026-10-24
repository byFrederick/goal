package api

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"maps"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/access.golden from the current table")

// awaitingOperations are the permissions of api/permissions.yaml whose operations later phase 3
// PRs add (spec 3, §8.2 and §15). Remove a permission here in the PR that gives it an operation;
// the test fails while one is both listed and used.
var awaitingOperations = map[string]string{}

// contractOp is one operation of api/openapi.yaml as the permission tests see it.
type contractOp struct {
	scope string // x-goal-scope, "" for none
	path  string
	// factoryQuery: the operation takes a factory in its query (?factory=), which a resolver
	// checks like a path's.
	factoryQuery bool
}

// contractOps reads every operation of api/openapi.yaml by operationId.
func contractOps(t *testing.T) map[string]contractOp {
	t.Helper()

	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}

	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	out := map[string]contractOp{}

	for path, ops := range doc.Paths {
		for _, node := range ops {
			var op struct {
				ID         string `yaml:"operationId"`
				Scope      string `yaml:"x-goal-scope"`
				Parameters []struct {
					Name string `yaml:"name"`
					In   string `yaml:"in"`
				} `yaml:"parameters"`
			}

			if node.Kind != yaml.MappingNode || node.Decode(&op) != nil || op.ID == "" {
				continue
			}

			c := contractOp{scope: op.Scope, path: path}
			for _, p := range op.Parameters {
				c.factoryQuery = c.factoryQuery || (p.Name == "factory" && p.In == "query")
			}

			out[op.ID] = c
		}
	}

	return out
}

func TestPermissions_EveryOperationHasAKnownScope(t *testing.T) {
	t.Parallel()

	contract := contractOps(t)

	for id, op := range contract {
		switch _, known := permissionRules[op.scope]; {
		case op.scope == "":
			t.Errorf("operation %s has no x-goal-scope: give it one of api/permissions.yaml", id)
		case !known:
			t.Errorf("operation %s: x-goal-scope %q is not in api/permissions.yaml", id, op.scope)
		case operationScopes[id] != op.scope:
			t.Errorf("operation %s: generated scope %q, contract %q: run task api:gen", id, operationScopes[id], op.scope)
		}
	}

	for id := range operationScopes {
		if _, ok := contract[id]; !ok {
			t.Errorf("generated operation %s is not in the contract: run task api:gen", id)
		}
	}
}

func TestPermissions_EveryPermissionIsUsed(t *testing.T) {
	t.Parallel()

	used := map[string]bool{}
	for _, op := range contractOps(t) {
		used[op.scope] = true
	}

	for name := range permissionRules {
		pr, awaiting := awaitingOperations[name]

		switch {
		case used[name] && awaiting:
			t.Errorf("permission %s has an operation now: remove it from awaitingOperations (%s)", name, pr)
		case !used[name] && !awaiting:
			t.Errorf("permission %s is used by no operation: name it in an x-goal-scope or remove it", name)
		}
	}

	for name := range awaitingOperations {
		if _, ok := permissionRules[name]; !ok {
			t.Errorf("awaitingOperations names %s, which api/permissions.yaml does not have", name)
		}
	}
}

// The roles of the table are the domain's, and its factory ladder runs in the same order.
func TestPermissions_RolesMatchTheTable(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../api/permissions.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var table struct {
		OrgRoles     []domain.OrgRole     `yaml:"org_roles"`
		FactoryRoles []domain.FactoryRole `yaml:"factory_roles"`
	}

	if err := yaml.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}

	if want := []domain.OrgRole{domain.OrgOwner, domain.OrgAdmin, domain.OrgMember}; !slices.Equal(table.OrgRoles, want) {
		t.Errorf("org_roles %v, want the domain's %v", table.OrgRoles, want)
	}

	for i, lower := range table.FactoryRoles {
		for _, higher := range table.FactoryRoles[i+1:] {
			if !higher.Includes(lower) || lower.Includes(higher) {
				t.Errorf("factory_roles: %s must include %s and not the reverse", higher, lower)
			}
		}
	}

	if len(table.FactoryRoles) != 3 {
		t.Errorf("factory_roles %v, want viewer, approver, editor", table.FactoryRoles)
	}
}

// Keys never hold what manages people, keys, the organization itself or the platform (spec 3,
// §4.3), and every fresh permission is out of their reach.
func TestPermissions_KeysNeverHoldThese(t *testing.T) {
	t.Parallel()

	never := []string{
		"member:read", "member:manage", "invitation:read", "api_key:read", "api_key:manage",
		"org:delete", "org:transfer", "org:manage", "org:create", "platform:read", "platform:manage",
	}

	for name, r := range permissionRules {
		if r.keys && (slices.Contains(never, name) || r.fresh) {
			t.Errorf("permission %s: keys: true, but keys never hold it", name)
		}
	}
}

// Personal tokens carry exactly spec 3 §4.5's permissions: widening the set is a reviewed change
// of this list. None is fresh, a platform admin's, or an install permission but authenticated.
func TestPermissions_PersonalTokensCarryExactlyThese(t *testing.T) {
	t.Parallel()

	want := []string{
		"andon:manage", "authenticated", "definition:edit", "definition:publish", "factory:create", string(permFactoryRead),
		"item:create", "item:decide", "item:operate", "org:read",
	}

	var got []string

	for _, name := range slices.Sorted(maps.Keys(permissionRules)) {
		r := permissionRules[name]
		if !r.pat {
			continue
		}

		got = append(got, name)

		if r.fresh || r.platformAdmin || (r.level == levelInstall && name != string(permAuthenticated)) {
			t.Errorf("permission %s: pat: true, but tokens never hold a fresh, platform or install permission", name)
		}
	}

	if !slices.Equal(got, want) {
		t.Errorf("pat: true permissions = %v, want %v", got, want)
	}
}

// The fabricated people and keys of the two-org matrix.
const (
	mxAcme  domain.OrgID     = "019a1c00-0000-7000-8000-00000000a0a0"
	mxOther domain.OrgID     = "019a1c00-0000-7000-8000-00000000b0b0"
	mxF     domain.FactoryID = "019a1c00-0000-7000-8000-0000000000f1" // acme's factory under test
	mxG     domain.FactoryID = "019a1c00-0000-7000-8000-0000000000f2" // acme's other factory
	mxH     domain.FactoryID = "019a1c00-0000-7000-8000-0000000000f3" // the other org's factory
)

var mxNow = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

// mxPrincipal is one column of principals: how a request of theirs resolves for an org, and the
// credential it carries over HTTP (nil header: none).
type mxPrincipal struct {
	name    string
	resolve func(org domain.OrgID) (domain.Principal, error)
	header  http.Header
}

// mxPerson is one person of the matrix, signed in with a session of token.
type mxPerson struct {
	name          string
	user          domain.UserID
	token         string
	org           domain.OrgID
	role          domain.OrgRole
	factory       domain.FactoryRole // on mxF
	platformAdmin bool
	stale         bool // signed in longer ago than domain.FreshFor
}

// mxPeople are the matrix's people: every org role, every factory role on mxF, a member without
// roles, a member of the other org only, a platform admin who is a member nowhere, and an owner
// whose session is not fresh.
func mxPeople() []mxPerson {
	people := []mxPerson{
		{name: "owner", org: mxAcme, role: domain.OrgOwner},
		{name: "admin", org: mxAcme, role: domain.OrgAdmin},
		{name: "viewer", org: mxAcme, role: domain.OrgMember, factory: domain.FactoryViewer},
		{name: "approver", org: mxAcme, role: domain.OrgMember, factory: domain.FactoryApprover},
		{name: "editor", org: mxAcme, role: domain.OrgMember, factory: domain.FactoryEditor},
		{name: "member-no-roles", org: mxAcme, role: domain.OrgMember},
		{name: "other-org-owner", org: mxOther, role: domain.OrgOwner},
		{name: "platform-admin", platformAdmin: true},
		{name: "stale-owner", org: mxAcme, role: domain.OrgOwner, stale: true},
	}

	for n := range people {
		people[n].user = domain.UserID(fmt.Sprintf("019a1c00-0000-7000-8000-%012d", n+1))
		people[n].token = fmt.Sprintf("mxsession%023d", n+1)
	}

	return people
}

// session is when the person signed in, and until when, by the matrix's clock.
func (p mxPerson) session() domain.Session {
	created := mxNow.Add(-time.Minute)
	if p.stale {
		created = mxNow.Add(-domain.FreshFor - time.Minute)
	}

	return domain.Session{ID: "session-" + p.name, CreatedAt: created, ExpiresAt: mxNow.Add(7 * 24 * time.Hour), Browser: true}
}

// cookie is the person's session cookie as Better Auth signs it.
func (p mxPerson) cookie() http.Header {
	return http.Header{"Cookie": {sessionCookie + "=" + p.token + ".c2lnbmF0dXJl"}}
}

// mxIdentity fabricates the matrix's principals through the Identity fake, so unknown, expired
// and revoked credentials fail as the port says they must; the fake resolves their credentials
// over HTTP too.
func mxIdentity(t *testing.T) (*porttest.Identity, []mxPrincipal) {
	t.Helper()

	id := porttest.NewIdentity(func() time.Time { return mxNow })
	id.AddOrg(domain.Org{ID: mxAcme, Slug: "acme", Name: "Acme"})
	id.AddOrg(domain.Org{ID: mxOther, Slug: "other", Name: "Other"})

	must := func(err error) {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}
	}

	out := []mxPrincipal{{name: "anonymous", resolve: func(domain.OrgID) (domain.Principal, error) { return domain.Principal{}, nil }}}

	for _, p := range mxPeople() {
		must(id.AddUser(porttest.IdentityUser{ID: p.user, Name: p.name, Email: p.name + "@example.com", PlatformAdmin: p.platformAdmin}))
		id.AddSession(p.token, p.user, p.session())

		if p.org != "" {
			must(id.SetOrgRole(p.org, p.user, p.role))
		}

		if p.factory != "" {
			must(id.GrantFactoryRole(p.org, p.user, mxF, p.factory))
		}

		out = append(out, mxPrincipal{name: p.name, header: p.cookie(), resolve: func(org domain.OrgID) (domain.Principal, error) {
			return id.Session(t.Context(), p.token, org)
		}})
	}

	for _, k := range mxKeys() {
		key := k.key
		if k.expired {
			key.ExpiresAt = mxNow
		}

		id.AddKey(k.hash, key)

		if k.revoked {
			id.RevokeKey(key.ID)
		}

		out = append(out, mxPrincipal{name: key.Name, header: k.header(), resolve: func(domain.OrgID) (domain.Principal, error) {
			return id.APIKey(t.Context(), k.hash)
		}})
	}

	return id, append(out, mxTokenPrincipals(t, id)...)
}

// mxKey is one API key of the matrix, with its secret and hash.
type mxKey struct {
	secret, hash     string
	key              porttest.IdentityKey
	expired, revoked bool
}

func (k mxKey) header() http.Header {
	return http.Header{"Authorization": {"Bearer " + k.secret}}
}

// mxKeys are the matrix's API keys: one of acme with a few scopes, one restricted to acme's
// other factory with every scope keys may carry, one of the other org, an expired and a revoked
// one. Live keys expire 90 days after the matrix's clock.
func mxKeys() []mxKey {
	all := []domain.Permission{}

	for _, name := range slices.Sorted(maps.Keys(permissionRules)) {
		if permissionRules[name].keys {
			all = append(all, domain.Permission(name))
		}
	}

	keys := []mxKey{
		{key: porttest.IdentityKey{Name: "key-scoped", Org: mxAcme, Scopes: []domain.Permission{"org:read", "factory:read", "item:decide"}, Ceiling: domain.FactoryApprover}},
		{key: porttest.IdentityKey{Name: "key-restricted", Org: mxAcme, Scopes: all, Factories: []domain.FactoryID{mxG}, Ceiling: domain.FactoryEditor}},
		{key: porttest.IdentityKey{Name: "key-other-org", Org: mxOther, Scopes: all, Ceiling: domain.FactoryEditor}},
		{key: porttest.IdentityKey{Name: "key-expired", Org: mxAcme, Scopes: all, Ceiling: domain.FactoryEditor}, expired: true},
		{key: porttest.IdentityKey{Name: "key-revoked", Org: mxAcme, Scopes: all, Ceiling: domain.FactoryEditor}, revoked: true},
	}

	for n := range keys {
		keys[n].secret = fmt.Sprintf("%s%043d", apiKeyPrefix, n)
		keys[n].hash = apiKeyHash(keys[n].secret)
		keys[n].key.ID = domain.APIKeyID(fmt.Sprintf("019a1c00-0000-7000-8000-%012d", 100+n))
		keys[n].key.ExpiresAt = mxNow.Add(90 * 24 * time.Hour)
	}

	return keys
}

// resolverHidden is what a resolver answers for an operation's unknown or invisible resource,
// nil when the operation passes no resolver.
func resolverHidden(op contractOp) error {
	switch {
	case strings.Contains(op.path, "{item}"):
		return errItemNotFound
	case strings.Contains(op.path, "{run}"):
		return errRunNotFound
	case strings.Contains(op.path, "{factory}"), op.factoryQuery:
		return errFactoryNotFound
	default:
		return nil
	}
}

// mxAnswer is what the API answers principal p for op in org: the middleware's pass, then the
// resolver's with the org's factory.
func mxAnswer(t *testing.T, authz Authorizer, id string, op contractOp, p mxPrincipal, org domain.OrgID) gen.Problem {
	t.Helper()

	// A public operation reads no credential: a stale cookie must not keep anyone from the
	// health check or the sign-in page's configuration.
	var principal domain.Principal

	if op.scope != string(permPublic) {
		var err error
		if principal, err = p.resolve(org); err != nil {
			return problemOf(err)
		}
	}

	r := AccessRequest{Principal: principal, Operation: id, Org: org}
	if err := refusal(authz.Authorize(t.Context(), r), errOrgNotFound); err != nil {
		return problemOf(err)
	}

	if hidden := resolverHidden(op); hidden != nil {
		r.Factory = map[domain.OrgID]domain.FactoryID{mxAcme: mxF, mxOther: mxH}[org]
		if err := refusal(authz.Authorize(t.Context(), r), hidden); err != nil {
			return problemOf(err)
		}
	}

	return gen.Problem{Status: 200}
}

func statusOf(p gen.Problem) string {
	if p.Code == nil {
		return strconv.Itoa(p.Status)
	}

	return fmt.Sprintf("%d %s", p.Status, *p.Code)
}

// TestAuthorize_TwoOrgMatrix asks the real Authorizer every operation of the contract for every
// kind of principal in both organizations (spec 3, §13), as the API's two passes would, and
// keeps every answer in testdata/access.golden so a permission change is a reviewed diff (go
// test ./internal/api -run TwoOrgMatrix -update rewrites it). The properties that must hold
// whatever the table says are checked here too.
func TestAuthorize_TwoOrgMatrix(t *testing.T) {
	t.Parallel()

	authz := NewPermissions(porttest.NewClock(mxNow))
	_, principals := mxIdentity(t)
	contract := contractOps(t)

	var golden bytes.Buffer

	golden.WriteString("# Generated by TestAuthorize_TwoOrgMatrix (go test ./internal/api -run TwoOrgMatrix -update).\n")
	golden.WriteString("# operation, principal, then the answer in acme and in the other org (install: no org).\n")

	byName := map[string]mxPrincipal{}
	for _, p := range principals {
		byName[p.name] = p
	}

	for _, id := range slices.Sorted(maps.Keys(contract)) {
		op := contract[id]
		orgs := []domain.OrgID{""}

		if strings.HasPrefix(op.path, "/v1/orgs/{org}") {
			orgs = []domain.OrgID{mxAcme, mxOther}
		}

		for _, p := range principals {
			cols := []string{}

			for _, org := range orgs {
				answer := mxAnswer(t, authz, id, op, p, org)
				checkMatrixProperties(t, id, op, p.name, org, answer)
				checkTokenProperties(t, id, op, p.name, org, answer, func(person string) gen.Problem {
					return mxAnswer(t, authz, id, op, byName[person], org)
				})
				cols = append(cols, statusOf(answer))
			}

			if len(orgs) == 1 {
				cols = []string{"install: " + cols[0]}
			}

			fmt.Fprintf(&golden, "%-20s %-16s %s\n", id, p.name, strings.Join(padded(cols), " "))
		}
	}

	const path = "testdata/access.golden"

	if *updateGolden {
		if err := os.WriteFile(path, golden.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: run go test ./internal/api -run TwoOrgMatrix -update", err)
	}

	if !bytes.Equal(golden.Bytes(), want) {
		t.Errorf("the answers differ from %s: review the change, then run go test ./internal/api -run TwoOrgMatrix -update", path)
	}
}

func padded(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = fmt.Sprintf("%-24s", c)
	}

	out[len(out)-1] = strings.TrimRight(out[len(out)-1], " ")

	return out
}

// checkMatrixProperties checks what holds whatever the table says: strangers learn nothing, an
// invisible factory or item answers exactly as an unknown one, no 401 or 404 has detail (a 403
// names the permission missing).
func checkMatrixProperties(t *testing.T, id string, op contractOp, principal string, org domain.OrgID, answer gen.Problem) {
	t.Helper()

	where := fmt.Sprintf("%s by %s in %s", id, principal, org)

	if (answer.Status == 401 || answer.Status == 404) && answer.Detail != nil {
		t.Errorf("%s: %d with detail %q, want a fixed body", where, answer.Status, *answer.Detail)
	}

	unauthenticated := slices.Contains([]string{
		"anonymous", "key-expired", "key-revoked", "pat-expired", "pat-revoked", "pat-removed", "pat-deleted-org",
	}, principal)
	if unauthenticated && op.scope != string(permPublic) && answer.Status != 401 {
		t.Errorf("%s: %d, want 401", where, answer.Status)
	}

	if mxStranger(principal, org) && !reflect.DeepEqual(answer, problemOf(errOrgNotFound)) {
		t.Errorf("%s: %+v, want exactly the unknown organization's body", where, answer)
	}

	checkInvisible(t, where, op, principal, org, answer)
}

// checkInvisible checks the principals whose factory is invisible: every operation on it
// answers as an unknown one, at least when it is a factory permission (an org permission may be
// refused before the resolver, and so may a permission a personal token never carries: that
// refusal comes from the token alone, before any factory is looked up).
func checkInvisible(t *testing.T, where string, op contractOp, principal string, org domain.OrgID, answer gen.Problem) {
	t.Helper()

	invisible := org == mxAcme && slices.Contains([]string{"member-no-roles", "key-restricted", "pat-demoted"}, principal)
	reached := answer.Status == 404 || (permissionRules[op.scope].level == levelFactory &&
		(!strings.HasPrefix(principal, "pat-") || permissionRules[op.scope].pat))

	if hidden := resolverHidden(op); hidden != nil && invisible && reached && !reflect.DeepEqual(answer, problemOf(hidden)) {
		t.Errorf("%s: %+v, want exactly the unknown resource's body %+v", where, answer, problemOf(hidden))
	}
}

// mxStranger reports whether the principal has no standing in org: not a member, a platform
// admin who is not one, a key of the other org.
func mxStranger(principal string, org domain.OrgID) bool {
	strangers := map[domain.OrgID][]string{
		mxAcme: {"other-org-owner", "platform-admin", "key-other-org", "pat-other-org"},
		mxOther: {
			"owner", "admin", "viewer", "approver", "editor", "member-no-roles", "stale-owner", "key-scoped", "key-restricted",
			mxBearerName, "pat-approver", "pat-owner", "pat-viewer", "pat-demoted", "pat-platform",
		},
	}

	return slices.Contains(strangers[org], principal)
}

// The visibility helpers answer by the principal's roles, and AllowAll (GOAL_AUTH=none) sees
// and may do everything.
func TestServer_VisibleAndCan(t *testing.T) {
	t.Parallel()

	viewer := domain.Principal{
		Kind: domain.PrincipalUser, User: "u", Org: mxAcme, OrgRole: domain.OrgMember,
		FactoryRoles: map[domain.FactoryID]domain.FactoryRole{mxF: domain.FactoryViewer},
	}
	stale := domain.Principal{
		Kind: domain.PrincipalUser, User: "o", Org: mxAcme, OrgRole: domain.OrgOwner,
		Session: &domain.Session{CreatedAt: mxNow.Add(-time.Hour)},
	}

	tests := []struct {
		name     string
		authz    Authorizer // Permissions when nil
		p        domain.Principal
		f        domain.FactoryID
		visible  bool
		can      []domain.Permission // exactly, when set
		canHolds domain.Permission
	}{
		{name: "viewer on their factory", p: viewer, f: mxF, visible: true, can: []domain.Permission{"factory:read"}},
		{name: "viewer on another", p: viewer, f: mxG, visible: false, can: []domain.Permission{}},
		{name: "member's org permissions", p: viewer, can: []domain.Permission{"member:read", "org:read", "personal_token:read"}},
		{name: "a stale owner may still be offered deleting", p: stale, canHolds: "org:delete"},
		{name: "local user under none", authz: AllowAll{}, p: domain.LocalPrincipal, f: mxG, visible: true, canHolds: "definition:publish"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &server{authz: NewPermissions(porttest.NewClock(mxNow))}
			if tt.authz != nil {
				s.authz = tt.authz
			}

			c := domain.WithOrg(domain.WithPrincipal(t.Context(), tt.p), mxAcme)
			can := s.can(c, tt.f)

			if tt.f != "" && s.visible(c, tt.f) != tt.visible {
				t.Errorf("visible(%s) = %v, want %v", tt.f, !tt.visible, tt.visible)
			}

			if tt.can != nil && !slices.Equal(can, tt.can) {
				t.Errorf("can(%q) = %v, want %v", tt.f, can, tt.can)
			}

			if tt.canHolds != "" && !slices.Contains(can, tt.canHolds) {
				t.Errorf("can(%q) = %v, want it to hold %s", tt.f, can, tt.canHolds)
			}
		})
	}
}

// refusal hides what the principal may not see behind the resolver's own unknown-resource error.
func TestRefusal_AnswersAsTheAPIDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err, want error
	}{
		{err: nil, want: nil},
		{err: errHidden, want: errFactoryNotFound},
		{err: domain.ErrUnauthenticated, want: domain.ErrUnauthenticated},
		{err: domain.ErrReauthRequired, want: domain.ErrReauthRequired},
		{err: errDenied, want: errForbidden},
	}

	for _, tt := range tests {
		if got := refusal(tt.err, errFactoryNotFound); !errors.Is(got, tt.want) || (tt.want == nil) != (got == nil) {
			t.Errorf("refusal(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
