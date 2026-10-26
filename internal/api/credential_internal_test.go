package api

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Fabricated credentials of the right shapes.
const (
	testToken  = "AbCdEfGhIjKlMnOpQrStUvWxYz012345" //nolint:gosec // a fabricated session token
	testAPIKey = apiKeyPrefix + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJ-_01234"
	testPAT    = personalTokenPrefix + "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij-_56789"
	authHeader = "Authorization"
)

func TestParseCredential_ReadsBearerValuesAndTheSessionCookie(t *testing.T) {
	t.Parallel()

	signed := testToken + ".c2lnbmF0dXJl%2Bd2l0aA%3D%3D"                    // Better Auth's <token>.<signature>, URL-encoded
	taskToken := "eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJnb2FsLWdhdGV3YXkifQ.c2ln" //nolint:gosec // a fabricated task token

	tests := []struct {
		name    string
		headers map[string][]string
		cookies map[string]string
		secure  bool
		want    credential
		refused bool
	}{
		{name: "nothing", want: credential{}},
		{name: "api key", headers: map[string][]string{authHeader: {"Bearer " + testAPIKey}}, want: credential{kind: credentialAPIKey, secret: testAPIKey}},
		{name: "personal token", headers: map[string][]string{authHeader: {"Bearer " + testPAT}}, want: credential{kind: credentialPersonalToken, secret: testPAT}},
		{name: "session token from goal login", headers: map[string][]string{authHeader: {"Bearer " + testToken}}, want: credential{kind: credentialSession, secret: testToken}},
		{name: "scheme in any case", headers: map[string][]string{authHeader: {"bearer " + testToken}}, want: credential{kind: credentialSession, secret: testToken}},
		{name: "agent task token", headers: map[string][]string{authHeader: {"Bearer " + taskToken}}, refused: true},
		{name: "api key one character short", headers: map[string][]string{authHeader: {"Bearer " + testAPIKey[:len(testAPIKey)-1]}}, refused: true},
		{name: "personal token one character short", headers: map[string][]string{authHeader: {"Bearer " + testPAT[:len(testPAT)-1]}}, refused: true},
		{name: "personal token one character long", headers: map[string][]string{authHeader: {"Bearer " + testPAT + "x"}}, refused: true},
		{name: "personal token with a symbol", headers: map[string][]string{authHeader: {"Bearer " + testPAT[:len(testPAT)-1] + "!"}}, refused: true},
		{name: "token with a symbol", headers: map[string][]string{authHeader: {"Bearer " + testToken[:31] + "!"}}, refused: true},
		{name: "basic credentials", headers: map[string][]string{authHeader: {"Basic dXNlcjpwdw=="}}, refused: true},
		{name: "two authorization headers", headers: map[string][]string{authHeader: {"Bearer " + testToken, "Bearer " + testAPIKey}}, refused: true},
		{name: "empty bearer", headers: map[string][]string{authHeader: {"Bearer "}}, refused: true},
		{name: "signed cookie over http", cookies: map[string]string{sessionCookie: signed}, want: credential{kind: credentialSession, secret: testToken}},
		{name: "signed cookie over https", cookies: map[string]string{secureSessionCookie: signed}, secure: true, want: credential{kind: credentialSession, secret: testToken}},
		{name: "the http cookie is ignored on https", cookies: map[string]string{sessionCookie: signed}, secure: true, want: credential{}},
		{name: "the https cookie is ignored on http", cookies: map[string]string{secureSessionCookie: signed}, want: credential{}},
		{name: "cookie that is not a token", cookies: map[string]string{sessionCookie: "short.sig"}, refused: true},
		{name: "cookie badly encoded", cookies: map[string]string{sessionCookie: testToken + ".%zz"}, refused: true},
		{
			name:    "bearer wins over the cookie",
			headers: map[string][]string{authHeader: {"Bearer " + testAPIKey}}, cookies: map[string]string{sessionCookie: signed},
			want: credential{kind: credentialAPIKey, secret: testAPIKey},
		},
		{
			name:    "a malformed bearer is not saved by a good cookie",
			headers: map[string][]string{authHeader: {"Bearer nope"}}, cookies: map[string]string{sessionCookie: signed},
			refused: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/orgs", nil)
			maps.Copy(r.Header, tt.headers)

			for name, v := range tt.cookies {
				r.AddCookie(&http.Cookie{Name: name, Value: v}) //nolint:gosec // a request's cookie, as a browser sends it, carries no attributes
			}

			got, err := parseCredential(r, tt.secure)

			switch {
			case tt.refused && !errors.Is(err, domain.ErrUnauthenticated):
				t.Errorf("parseCredential = %+v, %v; want ErrUnauthenticated", got, err)
			case !tt.refused && (err != nil || got != tt.want):
				t.Errorf("parseCredential = %+v, %v; want %+v", got, err, tt.want)
			}

			// A refusal never carries the credential: errors reach logs.
			if err != nil && (strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), testAPIKey) || strings.Contains(err.Error(), testPAT)) {
				t.Errorf("error %q names the credential", err)
			}
		})
	}
}

func TestAPIKeyHash_IsUnpaddedBase64URLOfSHA256(t *testing.T) {
	t.Parallel()

	// Python: base64.urlsafe_b64encode(hashlib.sha256(key).digest()).rstrip(b"=")
	const want = "OTxc-XvYmVUgOV34MNmVkeebM13UFVlUHS4Ok3sfhPA"
	if got := apiKeyHash(testAPIKey); got != want {
		t.Errorf("apiKeyHash = %s, want %s", got, want)
	}
}
