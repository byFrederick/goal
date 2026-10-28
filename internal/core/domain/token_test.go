package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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

func TestTaskToken_EvalNamesNoConnection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	key := []byte("gateway-key")
	claims := TaskClaims{
		Kind: TaskKindEval, OrgID: "o1", FactoryID: "f1", VersionID: "v1", Station: stationBuild, Agent: "backend", RunID: "r1",
		ExpiresAt: now.Add(time.Hour).Unix(),
	}

	token, err := SignTaskToken(key, claims)
	if err != nil {
		t.Fatal(err)
	}

	got, err := VerifyTaskToken(key, token, now)
	if err != nil {
		t.Fatal(err)
	}

	if got.Kind != TaskKindEval || len(got.Connections) != 0 {
		t.Errorf("claims = %+v, want an eval task with no connections", got)
	}

	withConnection := claims
	withConnection.Connections = []string{"acme-host"}

	if _, err := SignTaskToken(key, withConnection); !errors.Is(err, ErrBadTaskToken) {
		t.Errorf("signing an eval token with a connection: err = %v, want ErrBadTaskToken", err)
	}

	// A token signed by another issuer that names a connection is refused all the same.
	forged := withConnection
	forged.Audience = TaskAudience

	payload, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}

	enc := base64.RawURLEncoding
	if _, err := VerifyTaskToken(key, enc.EncodeToString(payload)+"."+enc.EncodeToString(mac(key, payload)), now); !errors.Is(err, ErrBadTaskToken) {
		t.Errorf("verifying an eval token with a connection: err = %v, want ErrBadTaskToken", err)
	}

	// A production task's token has no kind, and its encoding leaves the field out.
	plain, err := json.Marshal(TaskClaims{OrgID: "o1"})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(plain), `"k"`) {
		t.Errorf("production claims encode a kind: %s", plain)
	}
}

// A Jonah review's token names no connection and carries the review's window (and whether a
// person asked for it), which no other kind of token may carry. Signing refuses what verifying
// refuses, and a token another issuer signed with the same key is refused all the same.
func TestTaskToken_JonahNamesNoConnectionAndAWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	key := []byte("gateway-key")
	claims := TaskClaims{
		Kind: TaskKindJonah, OrgID: "o1", FactoryID: "f1", VersionID: "v1", Agent: "jonah", RunID: "r1",
		WindowStart: now.Add(-24 * time.Hour).Unix(), WindowEnd: now.Unix(), Manual: true, ExpiresAt: now.Add(time.Hour).Unix(),
	}

	token, err := SignTaskToken(key, claims)
	if err != nil {
		t.Fatal(err)
	}

	got, err := VerifyTaskToken(key, token, now)
	if err != nil {
		t.Fatal(err)
	}

	claims.Audience = TaskAudience
	if !reflect.DeepEqual(got, claims) {
		t.Errorf("claims = %+v, want %+v", got, claims)
	}

	tests := map[string]func(*TaskClaims){
		"a jonah token with a connection":      func(c *TaskClaims) { c.Connections = []string{"acme-host"} },
		"a jonah token without a window":       func(c *TaskClaims) { c.WindowStart, c.WindowEnd = 0, 0 },
		"a jonah token without a start":        func(c *TaskClaims) { c.WindowStart = 0 },
		"a jonah token ending at its start":    func(c *TaskClaims) { c.WindowEnd = c.WindowStart },
		"a jonah token ending before start":    func(c *TaskClaims) { c.WindowEnd = c.WindowStart - 1 },
		"a station token with a window":        func(c *TaskClaims) { c.Kind = "" },
		"an eval token with a window":          func(c *TaskClaims) { c.Kind = TaskKindEval },
		"a station token marked manual":        func(c *TaskClaims) { c.Kind, c.WindowStart, c.WindowEnd = "", 0, 0 },
		"an eval token with only a window end": func(c *TaskClaims) { c.Kind, c.WindowStart, c.Manual = TaskKindEval, 0, false },
	}

	enc := base64.RawURLEncoding

	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bad := claims
			change(&bad)

			if _, err := SignTaskToken(key, bad); !errors.Is(err, ErrBadTaskToken) {
				t.Errorf("SignTaskToken: err = %v, want ErrBadTaskToken", err)
			}

			payload, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := VerifyTaskToken(key, enc.EncodeToString(payload)+"."+enc.EncodeToString(mac(key, payload)), now); !errors.Is(err, ErrBadTaskToken) {
				t.Errorf("VerifyTaskToken: err = %v, want ErrBadTaskToken", err)
			}
		})
	}

	// Other tokens leave the window out of their encoding.
	plain, err := json.Marshal(TaskClaims{OrgID: "o1"})
	if err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{`"ws"`, `"we"`, `"jm"`} {
		if strings.Contains(string(plain), field) {
			t.Errorf("production claims encode %s: %s", field, plain)
		}
	}
}

func TestTaskToken_CarriesTheRunsBranchAndBase(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	key := []byte("gateway-key")
	claims := TaskClaims{
		OrgID: "o1", FactoryID: "f1", VersionID: "v1", ItemID: "i1", Station: stationBuild, Agent: testAgent, RunID: "r1",
		Branch: "goal/2-store", Base: "goal/a1-big-request", ExpiresAt: now.Add(time.Hour).Unix(),
	}

	token, err := SignTaskToken(key, claims)
	if err != nil {
		t.Fatal(err)
	}

	got, err := VerifyTaskToken(key, token, now)
	if err != nil || got.Branch != claims.Branch || got.Base != claims.Base {
		t.Errorf("VerifyTaskToken = %+v, %v; want branch %q and base %q", got, err, claims.Branch, claims.Base)
	}
}

func TestTaskToken_DraftRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	key := []byte("gateway-key")
	claims := TaskClaims{
		Kind: TaskKindDraftRequest, OrgID: "o1", FactoryID: "f1", VersionID: "v1", Agent: "reviewer", RunID: "d1",
		DraftRequest: "d1", DraftRevision: 7, ExpiresAt: now.Add(time.Hour).Unix(),
	}

	token, err := SignTaskToken(key, claims)
	if err != nil {
		t.Fatal(err)
	}

	got, err := VerifyTaskToken(key, token, now)
	if err != nil {
		t.Fatal(err)
	}

	if got.Kind != TaskKindDraftRequest || got.DraftRequest != "d1" || got.DraftRevision != 7 {
		t.Errorf("claims = %+v, want draft request d1 at revision 7", got)
	}

	sign := func(c TaskClaims) string {
		c.Audience = TaskAudience

		payload, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}

		enc := base64.RawURLEncoding

		return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(key, payload))
	}

	bad := map[string]func(c *TaskClaims){
		"an item":             func(c *TaskClaims) { c.ItemID = "i1" },
		"a station":           func(c *TaskClaims) { c.Station = stationBuild },
		"connections":         func(c *TaskClaims) { c.Connections = []string{"acme-host"} },
		"no request":          func(c *TaskClaims) { c.DraftRequest = "" },
		"a negative revision": func(c *TaskClaims) { c.DraftRevision = -1 },
	}

	for name, change := range bad {
		c := claims
		change(&c)

		if _, err := SignTaskToken(key, c); !errors.Is(err, ErrBadTaskToken) {
			t.Errorf("signing a draft request token with %s: err = %v, want ErrBadTaskToken", name, err)
		}

		// A token signed by another issuer is refused all the same.
		if _, err := VerifyTaskToken(key, sign(c), now); !errors.Is(err, ErrBadTaskToken) {
			t.Errorf("verifying a draft request token with %s: err = %v, want ErrBadTaskToken", name, err)
		}
	}

	// Only a draft request's token names a draft request.
	other := TaskClaims{
		OrgID: "o1", FactoryID: "f1", VersionID: "v1", ItemID: "i1", Station: stationBuild, Agent: testAgent, RunID: "r1",
		DraftRequest: "d1", ExpiresAt: now.Add(time.Hour).Unix(),
	}
	if _, err := VerifyTaskToken(key, sign(other), now); !errors.Is(err, ErrBadTaskToken) {
		t.Errorf("verifying a production token that names a draft request: err = %v, want ErrBadTaskToken", err)
	}
}
