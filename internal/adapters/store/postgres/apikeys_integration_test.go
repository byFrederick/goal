//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// scopeOrgRead is the test keys' scope.
const scopeOrgRead domain.Permission = "org:read"

// keyHash is a test key's stored hash.
func keyHash(t *testing.T) string {
	t.Helper()

	sum := sha256.Sum256([]byte(randomHex(t)))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Spec 3 §6.8: creating and revoking a key each append one entry in the key's transaction, by the
// person who did it (revoking it again appends none), and the list names them as the entries do.
func TestAccess_APIKeysAreAudited(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner := d.user(nameCruz)
	acme := d.provision("acme-"+randomHex(t), owner)
	by := domain.Actor{Kind: domain.ActorUser, ID: owner, Name: nameCruz}
	ctx := domain.WithOrg(t.Context(), acme)
	a := NewAccess(d.s)
	k := d.key(acme, by, "ci-deploy")

	for range 2 {
		if err := a.RevokeAPIKey(ctx, by, k.ID); err != nil {
			t.Fatal(err)
		}
	}

	target := "api_key:" + string(k.ID)
	want := []logged{
		{action: domain.AuditAPIKeyCreated, actor: by.Ref(), name: nameCruz, target: target, data: map[string]any{
			"key_id": string(k.ID), "name": "ci-deploy", "scopes": []any{string(scopeOrgRead), "item:create"}, "role_ceiling": "approver",
			auditFactories: nil, "expires_at": k.ExpiresAt.Format(time.RFC3339Nano),
		}},
		{action: domain.AuditAPIKeyRevoked, actor: by.Ref(), name: nameCruz, target: target, data: map[string]any{
			"key_id": string(k.ID), "name": "ci-deploy",
		}},
	}

	if log := d.logOf(acme)[1:]; !reflect.DeepEqual(log, want) { // after the owner's membership
		t.Errorf("audit = %+v; want %+v", log, want)
	}

	keys, err := a.APIKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0].RevokedBy == nil {
		t.Fatalf("keys = %+v, %v; want ci-deploy, revoked", keys, err)
	}

	if got := []domain.Actor{keys[0].CreatedBy, *keys[0].RevokedBy}; !reflect.DeepEqual(got, []domain.Actor{by, by}) {
		t.Errorf("listed creator and revoker = %+v; want %+v for both", got, by)
	}
}

// A key's actions written by the identity migrations' definers are named "<key name> (API key)",
// as goald names its own (00046's goal_append_audit).
func TestAppendAudit_NamesAPIKeys(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.provision("acme-"+randomHex(t), d.user(nameCruz))
	k := d.key(acme, domain.LocalUser, "ci-deploy")

	d.exec(d.admin, `SELECT goal_append_audit($1::uuid, $2, 'test.done', 'org', $3, '{}', NULL)`, string(acme), "service:"+string(k.ID), string(acme))

	if log := d.logOf(acme); log[len(log)-1].actor != "service:"+string(k.ID) || log[len(log)-1].name != "ci-deploy (API key)" {
		t.Errorf("a key's definer entry = %+v; want it named ci-deploy (API key)", log[len(log)-1])
	}
}

// key creates an API key named name of org by by, with scopes org:read and item:create.
func (d identityDB) key(org domain.OrgID, by domain.Actor, name string) domain.APIKey {
	d.t.Helper()

	k, err := NewAccess(d.s).CreateAPIKey(domain.WithOrg(d.t.Context(), org), by, domain.NewAPIKey{
		Name: name, Hint: "goal_sk_Ab3x", Hash: keyHash(d.t), Scopes: []domain.Permission{scopeOrgRead, "item:create"},
		Ceiling: domain.FactoryApprover, ExpiresInDays: 30,
	})
	if err != nil {
		d.t.Fatal(err)
	}

	return k
}

// Spec 3 §8.8: a revoked key's streams end at once, from a notice of its revocation, and a
// deleted organization's keys are announced too.
func TestAccessNotices_KeyRevoked(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}
	acme := s.Org(t, "Acme")
	ctx := domain.WithOrg(t.Context(), acme)
	a := NewAccess(d.s)

	first, err := a.CreateAPIKey(ctx, domain.LocalUser, domain.NewAPIKey{
		Name: "ci", Hint: "goal_sk_Ab3x", Hash: keyHash(t), Scopes: []domain.Permission{scopeOrgRead}, Ceiling: domain.FactoryViewer, ExpiresInDays: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := a.CreateAPIKey(ctx, domain.LocalUser, domain.NewAPIKey{
		Name: "nightly", Hint: "goal_sk_Q7mz", Hash: keyHash(t), Scopes: []domain.Permission{scopeOrgRead}, Ceiling: domain.FactoryViewer, ExpiresInDays: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	listen, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	got := make(chan ports.AccessNotice, 8)

	go func() { _ = NewAccessNotices(d.s, nil).Listen(listen, func(n ports.AccessNotice) { got <- n }) }()

	if n := <-got; !n.Resync {
		t.Fatalf("first notice = %+v; want a resync", n)
	}

	if err := a.RevokeAPIKey(ctx, domain.LocalUser, first.ID); err != nil {
		t.Fatal(err)
	}

	s.DeleteOrg(t, acme)

	for _, w := range []ports.AccessNotice{
		{Kind: ports.AccessAPIKey, OrgID: acme, KeyID: first.ID},
		{Kind: ports.AccessOrg, OrgID: acme},
		{Kind: ports.AccessAPIKey, OrgID: acme, KeyID: second.ID},
	} {
		select {
		case n := <-got:
			if n.Kind != w.Kind || n.OrgID != w.OrgID || n.KeyID != w.KeyID {
				t.Errorf("notice = %+v; want %+v", n, w)
			}
		case <-listen.Done():
			t.Fatalf("no %+v notice", w)
		}
	}
}
