package api

import (
	"context"
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

// The credentials of a request (spec 3, §8.3): a bearer value, a Goal API key, a personal token
// or a Better Auth session token (from goal login), or else Better Auth's session cookie. goald
// holds no Better Auth secret and does not check the cookie's signature: the token is the
// credential, looked up in the database per request (ports.Identity).
const (
	// sessionCookie is Better Auth's session cookie over http (a loopback GOAL_PUBLIC_URL);
	// secureSessionCookie over https. Only the one of the install's scheme counts.
	sessionCookie       = "goal.session_token"
	secureSessionCookie = "__Secure-goal.session_token"
	// apiKeyPrefix starts every API key: goal_sk_ and base64url of 32 random bytes.
	apiKeyPrefix = "goal_sk_"
	// personalTokenPrefix starts every personal token: goal_pat_ and base64url of 32 random bytes.
	personalTokenPrefix = "goal_pat_"
)

var (
	sessionTokenForm  = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)
	apiKeyForm        = regexp.MustCompile(`^` + apiKeyPrefix + `[A-Za-z0-9_-]{43}$`)
	personalTokenForm = regexp.MustCompile(`^` + personalTokenPrefix + `[A-Za-z0-9_-]{43}$`)
)

// credentialKind is what a request presents.
type credentialKind string

const (
	credentialNone          credentialKind = ""
	credentialSession       credentialKind = "session"
	credentialAPIKey        credentialKind = "api_key"
	credentialPersonalToken credentialKind = "personal_token"
)

// credential is a request's credential. secret is the session token, the API key or the personal
// token; it is never logged and never part of an error. bearer: it came as an Authorization
// bearer value, not the browser's cookie.
type credential struct {
	kind   credentialKind
	secret string
	bearer bool
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

// bearer reads the one Authorization header: Bearer and an API key, a personal token or a session
// token.
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
		return credential{kind: credentialAPIKey, secret: value, bearer: true}, nil
	case personalTokenForm.MatchString(value):
		return credential{kind: credentialPersonalToken, secret: value, bearer: true}, nil
	case sessionTokenForm.MatchString(value):
		return credential{kind: credentialSession, secret: value, bearer: true}, nil
	default:
		return credential{}, fmt.Errorf("authorization: neither an API key, a personal token nor a session token: %w", domain.ErrUnauthenticated)
	}
}

// apiKeyHash is how an API key or a personal token is stored and looked up: base64url (unpadded)
// of its SHA-256.
func apiKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// parsed is a request's credential as withCredential read it, for scope to resolve.
type parsed struct {
	c   credential
	err error
}

type credentialKey struct{}

// withCredential reads every /v1 request's credential (spec 3 §8.3), by mode:
//   - GOAL_AUTH=none: every request acts as the local user (domain.LocalPrincipal), and the
//     principal and actor are set here; cookies are ignored;
//   - GOAL_AUTH=session: the credential is kept for scope, which resolves it per operation (a
//     public operation reads none at all, so a stale or broken cookie never keeps anyone from the
//     health check or the sign-in page's configuration).
//
// In both modes a request carrying an Authorization header goald cannot use is 401 at once, never
// the local user: one that is not a bearer API key, personal token or session token. Agent task
// tokens work only at the gateway (/mcp), so an agent cannot present its own to the API.
func (s *server) withCredential(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Values("Authorization"); len(auth) > 0 {
			if _, err := bearer(auth); err != nil {
				s.problem(w, r, err)

				return
			}
		}

		ctx := r.Context()

		if s.auth.Mode == AuthNone {
			ctx = domain.WithActor(domain.WithPrincipal(ctx, domain.LocalPrincipal), domain.LocalPrincipal.Actor())
		} else {
			c, err := parseCredential(r, s.auth.SecureCookies)
			ctx = context.WithValue(ctx, credentialKey{}, parsed{c: c, err: err})
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// credentialFrom is the request's credential under GOAL_AUTH=session: none, malformed or absent
// wraps domain.ErrUnauthenticated.
func credentialFrom(ctx context.Context) (credential, error) {
	p, _ := ctx.Value(credentialKey{}).(parsed)

	switch {
	case p.err != nil:
		return credential{}, p.err
	case p.c.kind == credentialNone:
		return credential{}, fmt.Errorf("no credential: %w", domain.ErrUnauthenticated)
	default:
		return p.c, nil
	}
}
