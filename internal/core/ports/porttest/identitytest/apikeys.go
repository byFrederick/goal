package identitytest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// KeySeed sets up what the API key cases need beyond Seed.
type KeySeed interface {
	Seed
	// ExpireKey makes the key with id of org expired, as if it had been created long enough ago.
	ExpireKey(t *testing.T, org domain.OrgID, id domain.APIKeyID)
}

// RunAPIKeys runs the API key cases (spec 3 §4.3, §13): ports.Access creates, lists and revokes
// an organization's keys, and ports.Identity resolves them, against a fresh implementation of
// both and its seed from setup.
func RunAPIKeys(t *testing.T, setup func(t *testing.T) (ports.Identity, ports.Access, KeySeed)) {
	t.Helper()

	cases := []struct {
		name string
		run  func(t *testing.T, id ports.Identity, a ports.Access, s KeySeed)
	}{
		{"ACreatedKeyResolvesWithItsGrants", testKeyGrants},
		{"KeysAreListedAsCreated", testKeyListed},
		{"ARevokedKeyStopsOnTheNextLookup", testKeyRevoked},
		{"AnExpiredKeyDoesNotResolve", testKeyExpired},
		{"ADeletedOrgsKeysAreRevoked", testKeyDeletedOrg},
		{"NamesAreUniqueAmongTheOrgsLiveKeys", testKeyNames},
		{"KeysStayInTheirOrg", testKeyOrgs},
		{"LastUseIsRecordedAtMostOnceAMinute", testKeyLastUsed},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			id, a, s := setup(t)
			c.run(t, id, a, s)
		})
	}
}

// creator is who creates and revokes the cases' keys.
var creator = domain.LocalUser

// orgRead is the scope most cases' keys carry.
const orgRead domain.Permission = "org:read"

// newKey is a key of the shape goald makes, with its hash.
func newKey(t *testing.T, name string, scopes []domain.Permission, ceiling domain.FactoryRole, factories []domain.FactoryID, days int) domain.NewAPIKey {
	t.Helper()

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}

	raw := "goal_sk_" + base64.RawURLEncoding.EncodeToString(secret)
	sum := sha256.Sum256([]byte(raw))

	return domain.NewAPIKey{
		Name: name, Hint: raw[:12], Hash: base64.RawURLEncoding.EncodeToString(sum[:]), Scopes: scopes, Ceiling: ceiling,
		Factories: factories, ExpiresInDays: days,
	}
}

func create(t *testing.T, a ports.Access, org domain.OrgID, k domain.NewAPIKey) domain.APIKey {
	t.Helper()

	out, err := a.CreateAPIKey(domain.WithOrg(t.Context(), org), creator, k)
	if err != nil {
		t.Fatalf("CreateAPIKey(%s) = %v", k.Name, err)
	}

	return out
}

func list(t *testing.T, a ports.Access, org domain.OrgID) []domain.APIKey {
	t.Helper()

	keys, err := a.APIKeys(domain.WithOrg(t.Context(), org))
	if err != nil {
		t.Fatalf("APIKeys(%s) = %v", org, err)
	}

	return keys
}

func resolve(t *testing.T, id ports.Identity, hash string) domain.Principal {
	t.Helper()

	p, err := id.APIKey(t.Context(), hash)
	if err != nil {
		t.Fatalf("APIKey = %v; want it resolved", err)
	}

	return p
}

func refused(t *testing.T, id ports.Identity, hash, why string) {
	t.Helper()

	if p, err := id.APIKey(t.Context(), hash); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("%s key = %+v, %v; want ErrUnauthenticated", why, p, err)
	}
}

// near reports whether a is within a minute of b: the store dates keys by its own clock.
func near(a, b time.Time) bool { return a.Sub(b).Abs() < time.Minute }

func testKeyGrants(t *testing.T, id ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	f := s.Factory(t, acme)
	scopes := []domain.Permission{orgRead, "factory:read", "item:create"}
	n := newKey(t, "ci-deploy", scopes, domain.FactoryApprover, []domain.FactoryID{f}, 30)
	k := checkCreated(t, a, acme, n)

	want := domain.Principal{
		Kind: domain.PrincipalService, Key: k.ID, Name: "ci-deploy", Org: acme, Scopes: scopes,
		KeyFactories: []domain.FactoryID{f}, KeyCeiling: domain.FactoryApprover, KeyExpiresAt: k.ExpiresAt,
	}
	if p := resolve(t, id, n.Hash); !reflect.DeepEqual(p, want) {
		t.Errorf("principal = %+v; want the key's grants %+v", p, want)
	}

	all := newKey(t, "nightly", []domain.Permission{orgRead}, domain.FactoryViewer, nil, 90)
	k = checkCreated(t, a, acme, all)
	want = domain.Principal{
		Kind: domain.PrincipalService, Key: k.ID, Name: "nightly", Org: acme, Scopes: []domain.Permission{orgRead},
		KeyCeiling: domain.FactoryViewer, KeyExpiresAt: k.ExpiresAt,
	}

	if p := resolve(t, id, all.Hash); !reflect.DeepEqual(p, want) {
		t.Errorf("an unrestricted key = %+v; want %+v, every factory (nil)", p, want)
	}
}

// checkCreated creates n in org and checks the key it answers: n's grants, by the creator, dated
// now and expiring its days later.
func checkCreated(t *testing.T, a ports.Access, org domain.OrgID, n domain.NewAPIKey) domain.APIKey {
	t.Helper()

	k := create(t, a, org, n)
	want := domain.APIKey{
		ID: k.ID, Name: n.Name, Hint: n.Hint, Scopes: n.Scopes, Ceiling: n.Ceiling, Factories: n.Factories, CreatedBy: creator,
		CreatedAt: k.CreatedAt, ExpiresAt: k.CreatedAt.Add(time.Duration(n.ExpiresInDays) * 24 * time.Hour),
	}

	if k.ID == "" || !near(k.CreatedAt, now()) || !reflect.DeepEqual(k, want) {
		t.Errorf("created = %+v; want %+v, created now", k, want)
	}

	return k
}

func testKeyListed(t *testing.T, _ ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	f := s.Factory(t, acme)
	first := create(t, a, acme, newKey(t, "ci-deploy", []domain.Permission{"factory:read"}, domain.FactoryViewer, []domain.FactoryID{f}, 30))
	second := create(t, a, acme, newKey(t, "nightly", []domain.Permission{orgRead}, domain.FactoryEditor, nil, 90))

	if keys := list(t, a, acme); !reflect.DeepEqual(keys, []domain.APIKey{second, first}) {
		t.Errorf("keys = %+v; want nightly, then ci-deploy (newest first), as created", keys)
	}
}

func testKeyRevoked(t *testing.T, id ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	n := newKey(t, "ci", []domain.Permission{orgRead}, domain.FactoryViewer, nil, 90)
	k := create(t, a, acme, n)
	resolve(t, id, n.Hash)

	ctx := domain.WithOrg(t.Context(), acme)
	if err := a.RevokeAPIKey(ctx, creator, k.ID); err != nil {
		t.Fatal(err)
	}

	refused(t, id, n.Hash, "a revoked")

	other := domain.Actor{Kind: domain.ActorSystem, ID: "test", Name: "test"}
	if err := a.RevokeAPIKey(ctx, other, k.ID); err != nil {
		t.Errorf("revoking a revoked key = %v; want nothing to change", err)
	}

	keys := list(t, a, acme)
	if len(keys) != 1 || keys[0].RevokedAt == nil || !near(*keys[0].RevokedAt, now()) || keys[0].RevokedBy == nil ||
		keys[0].RevokedBy.Ref() != creator.Ref() {
		t.Errorf("keys = %+v; want ci revoked now by its first revoker", keys)
	}

	if err := a.RevokeAPIKey(ctx, creator, "00000000-0000-7000-8000-00000000dead"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("revoking an unknown key = %v; want ErrNotFound", err)
	}
}

func testKeyExpired(t *testing.T, id ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	n := newKey(t, "ci", []domain.Permission{orgRead}, domain.FactoryViewer, nil, 1)
	k := create(t, a, acme, n)
	s.ExpireKey(t, acme, k.ID)
	refused(t, id, n.Hash, "an expired")

	for _, days := range []int{0, 91} {
		if _, err := a.CreateAPIKey(domain.WithOrg(t.Context(), acme), creator, newKey(t, "x", n.Scopes, domain.FactoryViewer, nil, days)); err == nil {
			t.Errorf("a key expiring in %d days was created; want it refused", days)
		}
	}
}

func testKeyDeletedOrg(t *testing.T, id ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	n := newKey(t, "ci", []domain.Permission{orgRead}, domain.FactoryViewer, nil, 90)
	create(t, a, acme, n)
	s.DeleteOrg(t, acme)
	refused(t, id, n.Hash, "a deleted org's")

	keys := list(t, a, acme)
	if len(keys) != 1 || keys[0].RevokedBy == nil || keys[0].RevokedBy.Ref() != "system:org-deleted" {
		t.Errorf("keys of the deleted org = %+v; want ci revoked by system:org-deleted", keys)
	}
}

func testKeyNames(t *testing.T, _ ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme, labs := s.Org(t, "Acme"), s.Org(t, "Labs")
	scopes := []domain.Permission{orgRead}
	k := create(t, a, acme, newKey(t, "CI", scopes, domain.FactoryViewer, nil, 90))

	if _, err := a.CreateAPIKey(domain.WithOrg(t.Context(), acme), creator, newKey(t, "ci", scopes, domain.FactoryViewer, nil, 90)); !errors.Is(err, domain.ErrAPIKeyNameTaken) {
		t.Errorf("a second live ci = %v; want ErrAPIKeyNameTaken", err)
	}

	create(t, a, labs, newKey(t, "ci", scopes, domain.FactoryViewer, nil, 90))

	if err := a.RevokeAPIKey(domain.WithOrg(t.Context(), acme), creator, k.ID); err != nil {
		t.Fatal(err)
	}

	create(t, a, acme, newKey(t, "ci", scopes, domain.FactoryViewer, nil, 90))
}

func testKeyOrgs(t *testing.T, id ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme, labs := s.Org(t, "Acme"), s.Org(t, "Labs")
	labsFactory := s.Factory(t, labs)
	n := newKey(t, "ci", []domain.Permission{orgRead}, domain.FactoryViewer, nil, 90)
	k := create(t, a, acme, n)

	if keys := list(t, a, labs); len(keys) != 0 {
		t.Errorf("Labs lists %+v; want none of Acme's keys", keys)
	}

	if err := a.RevokeAPIKey(domain.WithOrg(t.Context(), labs), creator, k.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Labs revoking Acme's key = %v; want ErrNotFound", err)
	}

	resolve(t, id, n.Hash)

	_, err := a.CreateAPIKey(domain.WithOrg(t.Context(), acme), creator,
		newKey(t, "labs", n.Scopes, domain.FactoryViewer, []domain.FactoryID{labsFactory}, 90))
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("an Acme key for a Labs factory = %v; want ErrNotFound", err)
	}

	_, err = a.CreateAPIKey(domain.WithOrg(t.Context(), acme), creator,
		newKey(t, "unknown", n.Scopes, domain.FactoryViewer, []domain.FactoryID{"00000000-0000-7000-8000-00000000dead"}, 1))
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a key for an unknown factory = %v; want ErrNotFound", err)
	}

	if _, err := a.APIKeys(t.Context()); err == nil {
		t.Error("listing keys outside an organization succeeded; want an error")
	}
}

func testKeyLastUsed(t *testing.T, id ports.Identity, a ports.Access, s KeySeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	n := newKey(t, "ci", []domain.Permission{orgRead}, domain.FactoryViewer, nil, 90)
	create(t, a, acme, n)

	if keys := list(t, a, acme); keys[0].LastUsedAt != nil {
		t.Errorf("an unused key's last use = %v; want none", keys[0].LastUsedAt)
	}

	resolve(t, id, n.Hash)

	first := list(t, a, acme)[0].LastUsedAt
	if first == nil || !near(*first, now()) {
		t.Fatalf("last use after a lookup = %v; want now", first)
	}

	resolve(t, id, n.Hash)

	if again := list(t, a, acme)[0].LastUsedAt; again == nil || !again.Equal(*first) {
		t.Errorf("last use after a second lookup within a minute = %v; want %v unchanged", again, first)
	}
}
