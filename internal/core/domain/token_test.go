package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestTaskToken_OnlyTheSigningKeyVerifiesUnexpiredClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	key := []byte("gateway-key")
	claims := TaskClaims{
		OrgID: "o1", FactoryID: "f1", VersionID: "v1", ItemID: "i1", Station: "build", Agent: "backend", RunID: "r1",
		Connections: []string{"github"}, ExpiresAt: now.Add(time.Hour).Unix(),
	}

	token, err := SignTaskToken(key, claims)
	if err != nil {
		t.Fatal(err)
	}

	noOrg := claims
	noOrg.OrgID = ""

	unscoped, err := SignTaskToken(key, noOrg)
	if err != nil {
		t.Fatal(err)
	}

	noVersion := claims
	noVersion.VersionID = ""

	unversioned, err := SignTaskToken(key, noVersion)
	if err != nil {
		t.Fatal(err)
	}

	// SignTaskToken always names the gateway, whatever the claims say.
	other := claims
	other.Audience = "goal-api"

	renamed, err := SignTaskToken(key, other)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		key   []byte
		token string
		now   time.Time
		ok    bool
	}{
		{name: "same key before expiry", key: key, token: token, now: now, ok: true},
		{name: "other key", key: []byte("other"), token: token, now: now},
		{name: "after expiry", key: key, token: token, now: now.Add(2 * time.Hour)},
		{name: "tampered payload", key: key, token: "x" + token, now: now},
		{name: "no signature", key: key, token: "abc", now: now},
		{name: "no organization", key: key, token: unscoped, now: now},
		{name: "no definition version", key: key, token: unversioned, now: now},
		{name: "signed for the gateway whatever the claims say", key: key, token: renamed, now: now, ok: true},
		{name: "no audience", key: key, token: signedAs(t, key, claims, ""), now: now},
		{name: "another audience", key: key, token: signedAs(t, key, claims, "goal-api"), now: now},
		{name: "the audience in another case", key: key, token: signedAs(t, key, claims, "Goal-Gateway"), now: now},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := VerifyTaskToken(tt.key, tt.token, tt.now)
			if !tt.ok {
				if !errors.Is(err, ErrBadTaskToken) {
					t.Fatalf("err = %v, want ErrBadTaskToken", err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			want := claims
			want.Audience = TaskAudience

			if !reflect.DeepEqual(got, want) {
				t.Errorf("claims = %+v, want %+v", got, want)
			}
		})
	}
}

// signedAs signs claims with key under audience aud, as a token issuer other than SignTaskToken
// (or one from before audiences, when aud is "") would.
func signedAs(t *testing.T, key []byte, c TaskClaims, aud string) string {
	t.Helper()

	c.Audience = aud

	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}

	enc := base64.RawURLEncoding

	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(key, payload))
}
