package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// The CLI sign-ins of the device tests: one from this browser's address (the test client's), one
// from elsewhere, one expired.
const (
	cliClient   = "goal-cli"
	remoteCLIIP = "203.0.113.7"
)

func newDeviceHarness(t *testing.T) signedIn {
	t.Helper()

	h := newSignedIn(t)
	h.identity.AddDeviceRequest("ABCD-EFGH", domain.DeviceRequest{
		ClientID: cliClient, ClientName: "Goal CLI", RequestedAt: t0.Add(-time.Minute), ExpiresAt: t0.Add(9 * time.Minute),
		IP: "127.0.0.1", UserAgent: "goal/1.4 (darwin)",
	})
	h.identity.AddDeviceRequest("WXYZ2345", domain.DeviceRequest{
		ClientID: cliClient, ClientName: "Goal CLI", ExpiresAt: t0.Add(9 * time.Minute), IP: remoteCLIIP,
	})
	h.identity.AddDeviceRequest("GONEGONE", domain.DeviceRequest{ClientID: cliClient, ExpiresAt: t0.Add(-time.Second)})

	return h
}

// Spec 3 §8.9 and §10.2 (Device-Approve), §11.1 row 20: a signed-in person reads a pending CLI
// sign-in by the code they typed, dashes and case aside, with whether it came from their own
// network.
func TestAPI_DeviceRequest(t *testing.T) {
	t.Parallel()

	h := newDeviceHarness(t)

	got := decode[gen.DeviceRequest](t, h.as(t, viewerToken, http.MethodGet, "/v1/auth/device/abcd-efgh", ""), http.StatusOK)
	if got.ClientId != cliClient || got.ClientName != "Goal CLI" || got.RequestedAt == nil || !got.RequestedAt.Equal(t0.Add(-time.Minute)) ||
		!got.ExpiresAt.Equal(t0.Add(9*time.Minute)) || deref(got.Ip) != "127.0.0.1" || deref(got.UserAgent) != "goal/1.4 (darwin)" || !got.SameIp {
		t.Errorf("the request from this browser's address = %+v; want it whole, on the same network", got)
	}

	other := decode[gen.DeviceRequest](t, h.as(t, ownerToken, http.MethodGet, "/v1/auth/device/WXYZ-2345", ""), http.StatusOK)
	if other.SameIp || deref(other.Ip) != remoteCLIIP || other.RequestedAt != nil || other.UserAgent != nil {
		t.Errorf("the request from elsewhere = %+v; want another network, without the times and agent nobody recorded", other)
	}
}

// A code that is not pending, or not a code, is one fixed 404; nobody but a signed-in person reads
// one (no credential: 401; an API key, which never holds device:read: 403; §11.1 row 42).
func TestAPI_DeviceRequest_Refusals(t *testing.T) {
	t.Parallel()

	h := newDeviceHarness(t)

	for _, code := range []string{"ZZZZZZZZ", "GONEGONE", "ABCD_EFGH", strings.Repeat("A", 33)} {
		res := h.as(t, ownerToken, http.MethodGet, "/v1/auth/device/"+code, "")
		if p := problem(t, res); res.status != http.StatusNotFound || deref(p.Code) != "device-code-not-found" || p.Detail != nil {
			t.Errorf("code %q = %d %+v; want the one fixed 404", code, res.status, p)
		}
	}

	if res := h.as(t, "", http.MethodGet, "/v1/auth/device/ABCD-EFGH", ""); res.status != http.StatusUnauthorized {
		t.Errorf("anonymous = %d; want 401", res.status)
	}

	key := "goal_sk_" + strings.Repeat("A", 43)
	h.identity.AddKey(keyHash(key), porttest.IdentityKey{
		ID: "019a4000-0000-7000-8000-0000000000c1", Org: testOrg, Name: "ci", Scopes: []domain.Permission{"authenticated", "org:read"},
		Ceiling: domain.FactoryViewer, ExpiresAt: t0.Add(24 * time.Hour),
	})

	res := h.request(t, http.MethodGet, "/v1/auth/device/ABCD-EFGH", "", http.Header{"Authorization": {"Bearer " + key}})
	if res.status != http.StatusForbidden {
		t.Errorf("an API key = %d %s; want 403", res.status, res.body)
	}
}

// keyHash is how an API key is looked up: base64url (unpadded) of its SHA-256.
func keyHash(key string) string {
	sum := sha256.Sum256([]byte(key))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Under GOAL_AUTH=none there is no CLI sign-in: every code is the fixed 404.
func TestAPI_DeviceRequest_NoneWithoutSignIn(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	res := h.request(t, http.MethodGet, "/v1/auth/device/ABCD-EFGH", "", nil)
	if p := problem(t, res); res.status != http.StatusNotFound || deref(p.Code) != "device-code-not-found" {
		t.Errorf("GET a code under none = %d %+v; want 404 device-code-not-found", res.status, p)
	}
}
