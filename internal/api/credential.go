package api

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
)

// The credentials of a request (spec 3, §8.3): a bearer value, a Goal API key or a Better Auth
// session token (from goal login), or else Better Auth's session cookie. goald holds no Better
// Auth secret and does not check the cookie's signature: the token is the credential, looked up
// in the database per request (ports.Identity).
const (
	// sessionCookie is Better Auth's session cookie over http (a loopback GOAL_PUBLIC_URL);
	// secureSessionCookie over https. Only the one of the install's scheme counts.
	sessionCookie       = "goal.session_token"
	secureSessionCookie = "__Secure-goal.session_token"
	// apiKeyPrefix starts every API key: goal_sk_ and base64url of 32 random bytes.
	apiKeyPrefix = "goal_sk_"
)

var (
	sessionTokenForm = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)
	apiKeyForm       = regexp.MustCompile(`^` + apiKeyPrefix + `[A-Za-z0-9_-]{43}$`)
)

// credentialKind is what a request presents.
type credentialKind string

const (
	credentialNone    credentialKind = ""
	credentialSession credentialKind = "session"
	credentialAPIKey  credentialKind = "api_key"
)

// credential is a request's credential. secret is the session token or the API key; it is never
// logged and never part of an error.
type credential struct {
	kind   credentialKind
	secret string
}

// parseCredential reads the request's credential: an Authorization bearer value wins over
// cookies; else the session cookie of the install's scheme (secure: GOAL_PUBLIC_URL is https).
// A malformed one, an agent's task token included, wraps domain.ErrUnauthenticated; none at all
// is credentialNone, which only public operations accept.
func parseCredential(r *http.Request, secure bool) (credential, error) {
	if auth := r.Header.Values("Authorization"); len(auth) > 0 {
		return bearer(auth)
	}

	name := sessionCookie
	if secure {
		name = secureSessionCookie
	}

	c, err := r.Cookie(name)
	if errors.Is(err, http.ErrNoCookie) {
		return credential{}, nil
	}

	if err != nil {
		return credential{}, fmt.Errorf("session cookie: %w", domain.ErrUnauthenticated)
	}

	if c.Value == "" {
		return credential{}, nil
	}

	value, err := url.PathUnescape(c.Value)
	if err != nil {
		return credential{}, fmt.Errorf("session cookie: not URL-encoded: %w", domain.ErrUnauthenticated)
	}

	// Better Auth signs the cookie as <token>.<signature>; the token is the part before the dot.
	token, _, _ := strings.Cut(value, ".")
	if !sessionTokenForm.MatchString(token) {
		return credential{}, fmt.Errorf("session cookie: not a session token: %w", domain.ErrUnauthenticated)
	}

	return credential{kind: credentialSession, secret: token}, nil
}

// bearer reads the one Authorization header: Bearer and an API key or a session token.
func bearer(auth []string) (credential, error) {
	if len(auth) > 1 {
		return credential{}, fmt.Errorf("authorization: %d headers: %w", len(auth), domain.ErrUnauthenticated)
	}

	scheme, value, ok := strings.Cut(auth[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return credential{}, fmt.Errorf("authorization: not a bearer credential: %w", domain.ErrUnauthenticated)
	}

	switch {
	case apiKeyForm.MatchString(value):
		return credential{kind: credentialAPIKey, secret: value}, nil
	case sessionTokenForm.MatchString(value):
		return credential{kind: credentialSession, secret: value}, nil
	default:
		return credential{}, fmt.Errorf("authorization: neither an API key nor a session token: %w", domain.ErrUnauthenticated)
	}
}

// apiKeyHash is how an API key is stored and looked up: base64url (unpadded) of its SHA-256.
func apiKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// withLocalUser makes every request act as the local user: GOAL_AUTH=none, the only mode until
// goald authenticates requests (spec 3, PR 3-9), resolves no credential. It is the only place a
// request's principal and actor are made; handlers and the engine read domain.PrincipalFrom and
// domain.ActorFrom.
//
// A request carrying an Authorization header goald cannot use is 401 in every mode, never the
// local user: one that is not a bearer API key or session token. Agent task tokens work only at
// the gateway (/mcp), so an agent cannot present its own to the API.
func (s *server) withLocalUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Values("Authorization"); len(auth) > 0 {
			if _, err := bearer(auth); err != nil {
				s.problem(w, r, err)

				return
			}
		}

		ctx := domain.WithPrincipal(r.Context(), domain.LocalPrincipal)
		next.ServeHTTP(w, r.WithContext(domain.WithActor(ctx, domain.LocalPrincipal.Actor())))
	})
}
