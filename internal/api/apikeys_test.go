package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	keysPath = "/v1/orgs/acme/api-keys"
	// freshViewerToken is acme's viewer signed in just now (freshToken, its owner: platform_test.go).
	freshViewerToken = "FreshViewerFreshViewerFreshVie05"
)

var secretForm = regexp.MustCompile(`^goal_sk_[A-Za-z0-9_-]{43}$`)

// keyed is the signed-in harness with fresh sessions for the owner and the viewer, as key
// management needs a sign-in from the last 10 minutes.
func keyed(t *testing.T) signedIn {
	t.Helper()

	return keyedWith(t, nil)
}

// keyedWith is keyed with goald's log written to logs when it is set.
func keyedWith(t *testing.T, logs io.Writer) signedIn {
	t.Helper()

	h := newSignedInWith(t, logs)
	h.identity.AddSession(freshToken, ownerID, domain.Session{ID: "fresh-owner", CreatedAt: t0, ExpiresAt: t0.Add(7 * 24 * time.Hour), Browser: true})
	h.identity.AddSession(freshViewerToken, viewerID, domain.Session{ID: "fresh-viewer", CreatedAt: t0, ExpiresAt: t0.Add(7 * 24 * time.Hour), Browser: true})

	return h
}

// create makes a key as the fresh owner and returns it with its secret.
func (h signedIn) create(t *testing.T, body string) (gen.CreatedApiKey, response) {
	t.Helper()

	res := h.as(t, freshToken, http.MethodPost, keysPath, body)
	if res.status != http.StatusCreated {
		t.Fatalf("create key = %d %s", res.status, res.body)
	}

	var out gen.CreatedApiKey
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatal(err)
	}

	return out, res
}

// bearer is the header that presents secret.
func bearer(secret string) http.Header {
	return http.Header{"Authorization": {"Bearer " + secret}}
}

// ciDeploy is a key of acme's factory acme that submits work, at most as an approver.
const ciDeploy = `{"name":"ci-deploy","scopes":["org:read","factory:read","item:create"],"role_ceiling":"approver",` +
	`"factories":["acme"],"expires_in_days":30}`

// Spec 3 §8.9 and §11.1 row 19: the secret appears once, with Cache-Control no-store; the key
// is stored with its grants, by who made it, and listed by its hint only.
func TestAPI_APIKeys_SecretShownOnce(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	created, res := h.create(t, ciDeploy)

	if res.header.Get("Cache-Control") != "no-store" || !secretForm.MatchString(created.Secret) {
		t.Errorf("Cache-Control %q, secret %q; want no-store and goal_sk_ with 43 characters", res.header.Get("Cache-Control"), created.Secret)
	}

	k := created.Key
	want := gen.ApiKey{
		Id: k.Id, Name: "ci-deploy", Hint: created.Secret[:12], Scopes: []string{"factory:read", "item:create", "org:read"},
		RoleCeiling: gen.Approver, Factories: &[]string{acmeID}, CreatedBy: gen.Actor{Kind: gen.ActorKindUser, Id: string(ownerID), Name: ownerName},
		CreatedAt: t0, ExpiresAt: t0.Add(30 * 24 * time.Hour),
	}

	if !reflect.DeepEqual(k, want) {
		t.Errorf("created key = %+v; want %+v", k, want)
	}

	list := h.as(t, freshToken, http.MethodGet, keysPath, "")
	if list.status != http.StatusOK || strings.Contains(string(list.body), created.Secret[12:]) || !strings.Contains(string(list.body), k.Hint) {
		t.Errorf("list = %d %s; want the key by its hint, never its secret", list.status, list.body)
	}
}

// Spec 3 §4.3 and §11.1 row 18: a key acts for its organization with its scopes, on its
// factories, as its ceiling at most, and never manages keys.
func TestAPI_APIKeys_ActWithTheirGrants(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	created, _ := h.create(t, ciDeploy)

	var me gen.Me
	if res := h.request(t, http.MethodGet, "/v1/me", "", bearer(created.Secret)); json.Unmarshal(res.body, &me) != nil || me.Key == nil {
		t.Fatalf("me with the key = %d %s; want the key", res.status, res.body)
	}

	if me.Kind != gen.MeKindService || me.Key.Name != "ci-deploy" || me.Key.OrgId != string(testOrg) {
		t.Errorf("me = %+v; want the key ci-deploy of acme", me)
	}

	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"submits work on its factory", http.MethodPost, acmeItems, submitFix, http.StatusCreated},
		{"sees its factory", http.MethodGet, "/v1/orgs/acme/factories/acme/board", "", http.StatusOK},
		{"does not see another factory", http.MethodGet, "/v1/orgs/acme/factories/line/board", "", http.StatusNotFound},
		{"does not decide without the scope", http.MethodPost, "/v1/orgs/acme/items/acme%231/approve", "", http.StatusForbidden},
		{"never manages keys", http.MethodGet, keysPath, "", http.StatusForbidden},
		{"is unknown to another org", http.MethodGet, "/v1/orgs/example-labs", "", http.StatusNotFound},
	} {
		if res := h.request(t, c.method, c.path, c.body, bearer(created.Secret)); res.status != c.want {
			t.Errorf("the key %s: %d %s; want %d", c.name, res.status, res.body, c.want)
		}
	}
}

// Spec 3 §11.1 row 2: a revocation and the organization's deletion hold on the next request.
func TestAPI_APIKeys_StopWhenRevoked(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	created, _ := h.create(t, ciDeploy)

	if res := h.as(t, freshToken, http.MethodDelete, keysPath+"/"+created.Key.Id, ""); res.status != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", res.status, res.body)
	}

	if res := h.request(t, http.MethodGet, "/v1/me", "", bearer(created.Secret)); res.status != http.StatusUnauthorized {
		t.Errorf("the revoked key = %d %s; want 401 at once", res.status, res.body)
	}

	var keys gen.ApiKeyList
	if err := json.Unmarshal(h.as(t, freshToken, http.MethodGet, keysPath, "").body, &keys); err != nil || len(keys.Keys) != 1 ||
		keys.Keys[0].RevokedBy == nil || keys.Keys[0].RevokedBy.Id != string(ownerID) {
		t.Errorf("keys after the revocation = %+v, %v; want ci-deploy revoked by the owner", keys, err)
	}

	other, _ := h.create(t, `{"name":"nightly","scopes":["org:read"],"role_ceiling":"viewer"}`)
	h.identity.DeleteOrg(testOrg)

	if res := h.request(t, http.MethodGet, "/v1/me", "", bearer(other.Secret)); res.status != http.StatusUnauthorized {
		t.Errorf("a deleted org's key = %d %s; want 401", res.status, res.body)
	}
}

// Spec 3 §4.3: scopes keys never carry, scopes above the ceiling or not held by the creator,
// malformed requests, a taken name, an unknown factory and a stale session are refused, and
// nothing is created.
func TestAPI_APIKeys_Refusals(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	h.create(t, `{"name":"ci","scopes":["org:read"],"role_ceiling":"viewer"}`)

	for _, c := range []struct {
		name, token, body string
		want              int
		code              string
	}{
		{"a scope keys never carry", freshToken, `{"name":"a","scopes":["member:read"],"role_ceiling":"editor"}`, 400, "scope-not-allowed"},
		{"an unknown scope", freshToken, `{"name":"a","scopes":["nope:nope"],"role_ceiling":"editor"}`, 400, "scope-not-allowed"},
		{"a scope above the ceiling", freshToken, `{"name":"a","scopes":["definition:publish"],"role_ceiling":"approver"}`, 400, "scope-not-allowed"},
		{"a taken name, case ignored", freshToken, `{"name":"CI","scopes":["org:read"],"role_ceiling":"viewer"}`, 409, "api-key-name-taken"},
		{"an unknown factory", freshToken, `{"name":"a","scopes":["factory:read"],"role_ceiling":"viewer","factories":["nope"]}`, 404, "factory-not-found"},
		{"no factory named", freshToken, `{"name":"a","scopes":["factory:read"],"role_ceiling":"viewer","factories":[]}`, 400, ""},
		{"no scope", freshToken, `{"name":"a","scopes":[],"role_ceiling":"viewer"}`, 400, ""},
		{"an empty name", freshToken, `{"name":"  ","scopes":["org:read"],"role_ceiling":"viewer"}`, 400, ""},
		{"over 90 days", freshToken, `{"name":"a","scopes":["org:read"],"role_ceiling":"viewer","expires_in_days":91}`, 400, ""},
		{"an unknown ceiling", freshToken, `{"name":"a","scopes":["org:read"],"role_ceiling":"owner"}`, 400, ""},
		{"a stale session", ownerToken, `{"name":"a","scopes":["org:read"],"role_ceiling":"viewer"}`, 403, "reauth-required"},
		{"a member", freshViewerToken, `{"name":"a","scopes":["org:read"],"role_ceiling":"viewer"}`, 403, "forbidden"},
	} {
		res := h.as(t, c.token, http.MethodPost, keysPath, c.body)
		if p := problem(t, res); res.status != c.want || (c.code != "" && (p.Code == nil || *p.Code != c.code)) {
			t.Errorf("%s: %d %s; want %d %s", c.name, res.status, res.body, c.want, c.code)
		}
	}

	var keys gen.ApiKeyList
	if err := json.Unmarshal(h.as(t, freshToken, http.MethodGet, keysPath, "").body, &keys); err != nil || len(keys.Keys) != 1 {
		t.Errorf("keys = %+v, %v; want ci alone", keys, err)
	}

	if res := h.as(t, freshToken, http.MethodDelete, keysPath+"/00000000-0000-7000-8000-ffffffffffff", ""); res.status != http.StatusNotFound {
		t.Errorf("revoking an unknown key = %d; want 404", res.status)
	}
}

// Spec 3 §8.8: a key's stream ends when the key is revoked and when it expires.
func TestAPI_APIKeys_StreamsEndWithTheKey(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	revoked, _ := h.create(t, `{"name":"revoked","scopes":["org:read"],"role_ceiling":"viewer"}`)
	expiring, _ := h.create(t, `{"name":"expiring","scopes":["org:read"],"role_ceiling":"viewer","expires_in_days":1}`)

	events := h.streamWith(t, bearer(revoked.Secret))
	h.access.Publish(ports.AccessNotice{Kind: ports.AccessAPIKey, OrgID: testOrg, KeyID: domain.APIKeyID(expiring.Key.Id)})
	h.access.Publish(ports.AccessNotice{Kind: ports.AccessAPIKey, OrgID: testOrg, KeyID: domain.APIKeyID(revoked.Key.Id)})

	if !ended(events) {
		t.Error("the stream went on after its key was revoked")
	}

	events = h.streamWith(t, bearer(expiring.Secret))
	h.clock.Advance(24 * time.Hour)

	if !ended(events) {
		t.Error("the stream went on past its key's expiry")
	}
}

// Under GOAL_AUTH=none the local user manages the install's keys.
func TestAPI_APIKeys_LocalUser(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	res := h.request(t, http.MethodPost, keysPath, `{"name":"ci","scopes":["factory:read"],"role_ceiling":"viewer"}`, nil)
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}

	var created gen.CreatedApiKey
	if err := json.Unmarshal(res.body, &created); err != nil || created.Key.CreatedBy.Id != domain.LocalUser.ID || created.Key.Factories != nil {
		t.Errorf("created = %+v, %v; want a key of every factory by the local user", created, err)
	}
}
