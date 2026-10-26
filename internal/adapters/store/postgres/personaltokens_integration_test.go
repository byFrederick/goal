//go:build integration

package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// refusedReason is the data key of why personal_token.refused refused a token.
const refusedReason = "reason"

// token creates a personal token of member user in org, named name, with scope org:read, and
// returns it with its hash.
func (d identityDB) token(org domain.OrgID, user, name string) (domain.PersonalToken, string) {
	d.t.Helper()

	hash := keyHash(d.t)

	tok, err := NewAccess(d.s).CreatePersonalToken(domain.WithOrg(d.t.Context(), org), domain.Actor{Kind: domain.ActorUser, ID: user, Name: nameCruz},
		domain.NewPersonalToken{
			Owner: domain.UserID(user), Name: name, Hint: "goal_pat_Q7mk", Hash: hash, Scopes: []domain.Permission{scopeOrgRead}, ExpiresInDays: 30,
		})
	if err != nil {
		d.t.Fatal(err)
	}

	return tok, hash
}

// Spec 3 §6.7: creating and revoking a token each append one entry in the token's transaction, by
// who did it (revoking it again appends none), and the list names the revoker as the entry does.
func TestAccess_PersonalTokensAreAudited(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner := d.user(nameCruz)
	acme := d.provision("acme-"+randomHex(t), owner)
	by := domain.Actor{Kind: domain.ActorUser, ID: owner, Name: nameCruz}
	ctx := domain.WithOrg(t.Context(), acme)
	a := NewAccess(d.s)
	tok, _ := d.token(acme, owner, "laptop")

	for range 2 {
		if err := a.RevokePersonalToken(ctx, by, tok.ID, nil); err != nil {
			t.Fatal(err)
		}
	}

	target := "personal_token:" + string(tok.ID)
	want := []logged{
		{action: domain.AuditPersonalTokenCreated, actor: by.Ref(), name: nameCruz, target: target, data: map[string]any{
			"token_id": string(tok.ID), auditName: "laptop", "user_id": owner, "scopes": []any{string(scopeOrgRead)},
			"expires_at": tok.ExpiresAt.Format(time.RFC3339Nano),
		}},
		{action: domain.AuditPersonalTokenRevoked, actor: by.Ref(), name: nameCruz, target: target, data: map[string]any{
			"token_id": string(tok.ID), auditName: "laptop", "user_id": owner,
		}},
	}

	if log := d.logOf(acme)[1:]; !reflect.DeepEqual(log, want) { // after the owner's membership
		t.Errorf("audit = %+v; want %+v", log, want)
	}

	listed, err := a.PersonalTokens(ctx, nil)
	if err != nil || len(listed) != 1 || listed[0].RevokedBy == nil || *listed[0].RevokedBy != by ||
		listed[0].Owner != (domain.User{ID: domain.UserID(owner), Name: nameCruz, Email: d.email(owner)}) {
		t.Errorf("tokens = %+v, %v; want laptop, the owner's, revoked by them", listed, err)
	}
}

// Spec 3 §6.7 and §12: a hash matching a revoked or expired token writes personal_token.refused
// to the security log at most once a minute; an unknown hash writes nothing.
func TestPersonalToken_RefusalsAreLoggedOnceAMinute(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner := d.user(nameCruz)
	acme := d.provision("acme-"+randomHex(t), owner)
	tok, hash := d.token(acme, owner, "laptop")
	id := NewIdentity(d.s)

	if err := NewAccess(d.s).RevokePersonalToken(domain.WithOrg(t.Context(), acme), domain.LocalUser, tok.ID, nil); err != nil {
		t.Fatal(err)
	}

	for _, h := range []string{hash, hash, hash, keyHash(t)} {
		if _, err := id.PersonalToken(t.Context(), h); err == nil {
			t.Fatal("a revoked or unknown token resolved")
		}
	}

	d.checkOneEvent("personal_token.refused", domain.UserID(owner), map[string]any{"token_id": string(tok.ID), refusedReason: "revoked"})
}

// Spec 3 §8.8: a revoked token's streams end at once, from a notice of its revocation, and a
// deleted organization's tokens are announced too.
func TestAccessNotices_PersonalTokenRevoked(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner := d.user(nameCruz)
	acme := d.provision("acme-"+randomHex(t), owner)
	first, _ := d.token(acme, owner, "laptop")
	second, _ := d.token(acme, owner, "desktop")

	listen, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	got := make(chan ports.AccessNotice, 8)

	go func() { _ = NewAccessNotices(d.s, nil).Listen(listen, func(n ports.AccessNotice) { got <- n }) }()

	if n := <-got; !n.Resync {
		t.Fatalf("first notice = %+v; want a resync", n)
	}

	if err := NewAccess(d.s).RevokePersonalToken(domain.WithOrg(t.Context(), acme), domain.LocalUser, first.ID, nil); err != nil {
		t.Fatal(err)
	}

	pgSeed{d: d}.DeleteOrg(t, acme)

	for _, w := range []ports.AccessNotice{
		{Kind: ports.AccessPersonalToken, OrgID: acme, UserID: domain.UserID(owner), TokenID: first.ID},
		{Kind: ports.AccessOrg, OrgID: acme},
		{Kind: ports.AccessPersonalToken, OrgID: acme, UserID: domain.UserID(owner), TokenID: second.ID},
	} {
		select {
		case n := <-got:
			if n.Kind != w.Kind || n.OrgID != w.OrgID || n.UserID != w.UserID || n.TokenID != w.TokenID {
				t.Errorf("notice = %+v; want %+v", n, w)
			}
		case <-listen.Done():
			t.Fatalf("no %+v notice", w)
		}
	}
}

// Spec 3 §6.7 and §11.1 row 50: a write made through a personal token records via: pat:<id> in
// its audit entry and its item events; the person's own writes do not.
func TestWrites_MadeWithATokenSayVia(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner := d.user(nameCruz)
	acme := d.provision("acme-"+randomHex(t), owner)
	f := pgSeed{d: d}.Factory(t, acme)
	by := domain.Actor{Kind: domain.ActorUser, ID: owner, Name: nameCruz}
	tok, _ := d.token(acme, owner, "laptop")
	key := d.key(acme, by, "ci")

	viaToken := domain.WithPrincipal(domain.WithOrg(t.Context(), acme), domain.Principal{
		Kind: domain.PrincipalUser, User: domain.UserID(owner), Org: acme, Token: &domain.TokenGrant{ID: tok.ID, Name: "laptop"},
	})

	if err := NewAccess(d.s).RevokeAPIKey(viaToken, by, key.ID); err != nil {
		t.Fatal(err)
	}

	it, err := d.s.CreateItem(viaToken, domain.NewItem{
		FactoryID: f, Type: typeTask, Title: "with a token", Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "pat:" + string(tok.ID)

	if log := d.logOf(acme); log[len(log)-1].action != domain.AuditAPIKeyRevoked || log[len(log)-1].data["via"] != want {
		t.Errorf("the revocation's entry = %+v; want via %s", log[len(log)-1], want)
	}

	if log := d.logOf(acme); log[len(log)-2].data["via"] != nil {
		t.Errorf("the key's creation, in a session = %+v; want no via", log[len(log)-2])
	}

	var via *string
	if err := d.admin.QueryRow(t.Context(), `SELECT data ->> 'via' FROM item_events WHERE item_id = $1`, string(it.ID)).Scan(&via); err != nil {
		t.Fatal(err)
	}

	if via == nil || *via != want {
		t.Errorf("the item's created event via = %v; want %s", via, want)
	}
}
