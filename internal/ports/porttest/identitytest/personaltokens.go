package identitytest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// TokenSeed sets up what the personal token cases need beyond Seed.
type TokenSeed interface {
	Seed
	// ExpireToken makes the token with id of org expired, as if it had been created long enough
	// ago.
	ExpireToken(t *testing.T, org domain.OrgID, id domain.PersonalTokenID)
}

// RunPersonalTokens runs the personal token cases (spec 3 §4.5, §13): ports.Access creates, lists
// and revokes members' tokens, and ports.Identity resolves them to their owner as they are now,
// against a fresh implementation of both and its seed from setup.
func RunPersonalTokens(t *testing.T, setup func(t *testing.T) (ports.Identity, ports.Access, TokenSeed)) {
	t.Helper()

	cases := []struct {
		name string
		run  func(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed)
	}{
		{"ACreatedTokenResolvesAsItsOwner", testTokenOwner},
		{"TokensAreListedByOwner", testTokensListed},
		{"ARevokedTokenStopsOnTheNextLookup", testTokenRevoked},
		{"AnExpiredTokenDoesNotResolve", testTokenExpired},
		{"ARemovedMembersTokensAreGoneForGood", testTokenRemovedMember},
		{"ARoleChangeHoldsOnTheNextLookup", testTokenRoleChange},
		{"ADeletedOrgsTokensAreRevoked", testTokenDeletedOrg},
		{"NamesAreUniqueAmongTheOwnersLiveTokens", testTokenNames},
		{"TokensStayInTheirOrg", testTokenOrgs},
		{"LastUseIsRecordedAtMostOnceAMinute", testTokenLastUsed},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			id, a, s := setup(t)
			c.run(t, id, a, s)
		})
	}
}

// newToken is a token of the shape goald makes for owner, with its hash.
func newToken(t *testing.T, owner domain.UserID, name string, days int, scopes ...domain.Permission) domain.NewPersonalToken {
	t.Helper()

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}

	raw := "goal_pat_" + base64.RawURLEncoding.EncodeToString(secret)
	sum := sha256.Sum256([]byte(raw))

	if len(scopes) == 0 {
		scopes = []domain.Permission{orgRead}
	}

	return domain.NewPersonalToken{
		Owner: owner, Name: name, Hint: raw[:13], Hash: base64.RawURLEncoding.EncodeToString(sum[:]), Scopes: scopes, ExpiresInDays: days,
	}
}

// acting is how a member is recorded when they act.
func acting(user domain.UserID) domain.Actor {
	return domain.Actor{Kind: domain.ActorUser, ID: string(user), Name: "someone"}
}

func createToken(t *testing.T, a ports.Access, org domain.OrgID, n domain.NewPersonalToken) domain.PersonalToken {
	t.Helper()

	out, err := a.CreatePersonalToken(domain.WithOrg(t.Context(), org), acting(n.Owner), n)
	if err != nil {
		t.Fatalf("CreatePersonalToken(%s) = %v", n.Name, err)
	}

	return out
}

func tokens(t *testing.T, a ports.Access, org domain.OrgID, owner *domain.UserID) []domain.PersonalToken {
	t.Helper()

	out, err := a.PersonalTokens(domain.WithOrg(t.Context(), org), owner)
	if err != nil {
		t.Fatalf("PersonalTokens(%s) = %v", org, err)
	}

	return out
}

func tokenNames(ts []domain.PersonalToken) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name)
	}

	return out
}

func resolveToken(t *testing.T, id ports.Identity, hash string) domain.Principal {
	t.Helper()

	p, err := id.PersonalToken(t.Context(), hash)
	if err != nil {
		t.Fatalf("PersonalToken = %v; want it resolved", err)
	}

	return p
}

func tokenRefused(t *testing.T, id ports.Identity, hash, why string) {
	t.Helper()

	if p, err := id.PersonalToken(t.Context(), hash); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("%s token = %+v, %v; want ErrUnauthenticated", why, p, err)
	}
}

// member makes a person a member of org with role.
func member(t *testing.T, s Seed, org domain.OrgID, name string, role domain.OrgRole, platformAdmin bool) domain.UserID {
	t.Helper()

	u := s.User(t, name, name+"@example.com", platformAdmin)
	s.SetRole(t, org, u, role)

	return u
}

func testTokenOwner(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	f := s.Factory(t, acme)
	ada := member(t, s, acme, "ada", domain.OrgMember, true) // a platform admin
	s.SetFactoryRole(t, acme, ada, f, domain.FactoryApprover)

	n := newToken(t, ada, "laptop", 7, "factory:read", "item:create")
	created := createToken(t, a, acme, n)

	if created.Owner.ID != ada || created.Hint != n.Hint || !near(created.ExpiresAt, now().Add(7*24*time.Hour)) {
		t.Errorf("created = %+v; want ada's laptop for 7 days", created)
	}

	p := resolveToken(t, id, n.Hash)
	if p.Kind != domain.PrincipalUser || p.User != ada || p.Name != "ada" || p.Email != "ada@example.com" || p.Org != acme {
		t.Errorf("principal = %+v; want ada in acme", p)
	}

	if p.OrgRole != domain.OrgMember || p.FactoryRoles[f] != domain.FactoryApprover {
		t.Errorf("roles = %s %v; want member, approver of f", p.OrgRole, p.FactoryRoles)
	}

	if p.PlatformAdmin || p.Session != nil {
		t.Errorf("principal = %+v; want no platform admin and no session through a token", p)
	}

	checkGrant(t, p.Token, created)
}

// checkGrant checks a resolved token's grant is created's: its id, name, scopes and expiry.
func checkGrant(t *testing.T, g *domain.TokenGrant, created domain.PersonalToken) {
	t.Helper()

	if g == nil || g.ID != created.ID || g.Name != created.Name || !slices.Equal(g.Scopes, created.Scopes) || !g.ExpiresAt.Equal(created.ExpiresAt) {
		t.Errorf("grant = %+v; want %s's, with its scopes and expiry", g, created.Name)
	}
}

func testTokensListed(t *testing.T, _ ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := member(t, s, acme, "ada", domain.OrgOwner, false)
	bo := member(t, s, acme, "bo", domain.OrgMember, false)

	createToken(t, a, acme, newToken(t, ada, "first", 30))
	old := createToken(t, a, acme, newToken(t, ada, "old", 30))
	createToken(t, a, acme, newToken(t, bo, "bo's", 30))

	if err := a.RevokePersonalToken(domain.WithOrg(t.Context(), acme), acting(ada), old.ID, &ada); err != nil {
		t.Fatal(err)
	}

	if got := tokenNames(tokens(t, a, acme, &ada)); len(got) != 2 || got[1] != "old" {
		t.Errorf("ada's tokens = %v; want first, then the revoked old, and never bo's", got)
	}

	all := tokens(t, a, acme, nil)
	if got := tokenNames(all); len(got) != 3 || got[len(got)-1] != "old" {
		t.Errorf("every member's tokens = %v; want all three, the revoked one last", got)
	}

	for _, tok := range all {
		if tok.Owner.Name == "" || tok.Owner.Email == "" {
			t.Errorf("token %s: owner %+v; want them named", tok.Name, tok.Owner)
		}
	}
}

func testTokenRevoked(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := member(t, s, acme, "ada", domain.OrgOwner, false)
	bo := member(t, s, acme, "bo", domain.OrgMember, false)
	n := newToken(t, ada, "laptop", 30)
	tok := createToken(t, a, acme, n)
	ctx := domain.WithOrg(t.Context(), acme)

	if err := a.RevokePersonalToken(ctx, acting(bo), tok.ID, &bo); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("bo revoking ada's token = %v; want ErrNotFound", err)
	}

	resolveToken(t, id, n.Hash)

	if err := a.RevokePersonalToken(ctx, acting(ada), tok.ID, &ada); err != nil {
		t.Fatal(err)
	}

	tokenRefused(t, id, n.Hash, "a revoked")

	if err := a.RevokePersonalToken(ctx, acting(bo), tok.ID, nil); err != nil {
		t.Errorf("revoking a revoked token = %v; want no change", err)
	}

	got := tokens(t, a, acme, &ada)
	if len(got) != 1 || got[0].RevokedAt == nil || got[0].RevokedBy == nil || got[0].RevokedBy.ID != string(ada) {
		t.Errorf("tokens = %+v; want laptop revoked by ada, the first revocation kept", got)
	}

	if err := a.RevokePersonalToken(ctx, acting(ada), "019a0000-0000-7000-8000-ffffffffffff", nil); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("revoking an unknown token = %v; want ErrNotFound", err)
	}
}

func testTokenExpired(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := member(t, s, acme, "ada", domain.OrgOwner, false)
	n := newToken(t, ada, "laptop", 30)
	tok := createToken(t, a, acme, n)
	s.ExpireToken(t, acme, tok.ID)

	tokenRefused(t, id, n.Hash, "an expired")

	if _, err := a.CreatePersonalToken(domain.WithOrg(t.Context(), acme), acting(ada), newToken(t, ada, "long", 91)); err == nil {
		t.Error("a token of 91 days was created; want it refused")
	}
}

func testTokenRemovedMember(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	member(t, s, acme, "owner", domain.OrgOwner, false)
	bo := member(t, s, acme, "bo", domain.OrgMember, false)
	n := newToken(t, bo, "laptop", 30)
	createToken(t, a, acme, n)

	s.SetRole(t, acme, bo, "")
	tokenRefused(t, id, n.Hash, "a removed member's")

	s.SetRole(t, acme, bo, domain.OrgMember) // invited back
	tokenRefused(t, id, n.Hash, "a re-invited member's old")

	if got := tokens(t, a, acme, nil); len(got) != 0 {
		t.Errorf("tokens after the removal = %v; want none: removal deletes them", tokenNames(got))
	}
}

func testTokenRoleChange(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	f := s.Factory(t, acme)
	member(t, s, acme, "owner", domain.OrgOwner, false)
	bo := member(t, s, acme, "bo", domain.OrgOwner, false)
	n := newToken(t, bo, "laptop", 30, "factory:read")
	createToken(t, a, acme, n)

	if p := resolveToken(t, id, n.Hash); p.OrgRole != domain.OrgOwner {
		t.Fatalf("role = %s; want owner", p.OrgRole)
	}

	s.SetRole(t, acme, bo, domain.OrgMember)
	s.SetFactoryRole(t, acme, bo, f, domain.FactoryViewer)

	if p := resolveToken(t, id, n.Hash); p.OrgRole != domain.OrgMember || p.FactoryRoles[f] != domain.FactoryViewer {
		t.Errorf("after the demotion: %s %v; want member, viewer of f at once", p.OrgRole, p.FactoryRoles)
	}

	s.SetFactoryRole(t, acme, bo, f, "")

	if p := resolveToken(t, id, n.Hash); len(p.FactoryRoles) != 0 {
		t.Errorf("after the factory role went: %v; want none", p.FactoryRoles)
	}
}

func testTokenDeletedOrg(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := member(t, s, acme, "ada", domain.OrgOwner, false)
	n := newToken(t, ada, "laptop", 30)
	createToken(t, a, acme, n)

	s.DeleteOrg(t, acme)
	tokenRefused(t, id, n.Hash, "a deleted org's")
}

func testTokenNames(t *testing.T, _ ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := member(t, s, acme, "ada", domain.OrgOwner, false)
	bo := member(t, s, acme, "bo", domain.OrgMember, false)
	ctx := domain.WithOrg(t.Context(), acme)
	first := createToken(t, a, acme, newToken(t, ada, "laptop", 30))

	if _, err := a.CreatePersonalToken(ctx, acting(ada), newToken(t, ada, "LapTop", 30)); !errors.Is(err, domain.ErrPersonalTokenNameTaken) {
		t.Errorf("a second laptop of ada's = %v; want ErrPersonalTokenNameTaken", err)
	}

	createToken(t, a, acme, newToken(t, bo, "laptop", 30)) // another member's may share it

	if err := a.RevokePersonalToken(ctx, acting(ada), first.ID, &ada); err != nil {
		t.Fatal(err)
	}

	createToken(t, a, acme, newToken(t, ada, "laptop", 30)) // a revoked token's name is free
}

func testTokenOrgs(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme, labs := s.Org(t, "Acme"), s.Org(t, "Labs")
	ada := member(t, s, acme, "ada", domain.OrgOwner, false)
	s.SetRole(t, labs, ada, domain.OrgOwner)
	stranger := s.User(t, "stranger", "stranger@example.com", false)

	n := newToken(t, ada, "laptop", 30)
	tok := createToken(t, a, acme, n)

	if got := tokens(t, a, labs, nil); len(got) != 0 {
		t.Errorf("labs' tokens = %v; want none of acme's", tokenNames(got))
	}

	if err := a.RevokePersonalToken(domain.WithOrg(t.Context(), labs), acting(ada), tok.ID, nil); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("revoking acme's token in labs = %v; want ErrNotFound", err)
	}

	if p := resolveToken(t, id, n.Hash); p.Org != acme {
		t.Errorf("the token's org = %s; want acme, its own", p.Org)
	}

	if _, err := a.CreatePersonalToken(domain.WithOrg(t.Context(), acme), acting(stranger), newToken(t, stranger, "x", 30)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a token of a non-member = %v; want ErrNotFound", err)
	}

	if _, err := a.PersonalTokens(t.Context(), nil); err == nil {
		t.Error("listing tokens outside an organization succeeded; want an error")
	}
}

func testTokenLastUsed(t *testing.T, id ports.Identity, a ports.Access, s TokenSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := member(t, s, acme, "ada", domain.OrgOwner, false)
	n := newToken(t, ada, "laptop", 30)
	createToken(t, a, acme, n)

	if got := tokens(t, a, acme, nil); got[0].LastUsedAt != nil {
		t.Errorf("an unused token's last use = %v; want none", got[0].LastUsedAt)
	}

	resolveToken(t, id, n.Hash)

	first := tokens(t, a, acme, nil)[0].LastUsedAt
	if first == nil || !near(*first, now()) {
		t.Fatalf("last use after a lookup = %v; want now", first)
	}

	resolveToken(t, id, n.Hash)

	if again := tokens(t, a, acme, nil)[0].LastUsedAt; again == nil || !again.Equal(*first) {
		t.Errorf("last use after a second lookup within a minute = %v; want %v unchanged", again, first)
	}
}
