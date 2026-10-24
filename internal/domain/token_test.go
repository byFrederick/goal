package domain

import (
	"errors"
	"testing"
	"time"
)

func TestTaskToken_OnlyTheSigningKeyVerifiesUnexpiredClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	key := []byte("gateway-key")
	claims := TaskClaims{
		OrgID: "o1", FactoryID: "f1", ItemID: "i1", Station: "build", Agent: "backend", RunID: "r1",
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

			if got.OrgID != claims.OrgID || got.ItemID != claims.ItemID || got.Agent != claims.Agent || len(got.Connections) != 1 {
				t.Errorf("claims = %+v, want %+v", got, claims)
			}
		})
	}
}
