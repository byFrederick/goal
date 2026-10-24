package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// Bearer values goald can use (phase 3 §8.3): a Goal API key and a Better Auth session token.
// Anything else, an agent's task token above all, is refused.
var (
	apiKeyBearer  = regexp.MustCompile(`^goal_sk_[A-Za-z0-9_-]{43}$`)
	sessionBearer = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)
)

// withActor sets the actor of every request. It is the only place a request's actor is made:
// phase 2.2 has no identity, so it is always domain.LocalUser. Phase 3 replaces its source (a
// session or a token) and nothing else; handlers and the engine read domain.ActorFrom.
//
// A request carrying a credential goald cannot use is 401 in every mode, never the local user:
// an Authorization header that is not a bearer API key or session token. Agent task tokens work
// only at the gateway (/mcp), so an agent cannot present its own to the API.
func withActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("Authorization"); v != "" && !usableBearer(v) {
			unauthenticated(w)

			return
		}

		next.ServeHTTP(w, r.WithContext(domain.WithActor(r.Context(), domain.LocalUser)))
	})
}

// usableBearer reports whether an Authorization header is a bearer API key or session token.
func usableBearer(header string) bool {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}

	value = strings.TrimSpace(value)

	return apiKeyBearer.MatchString(value) || sessionBearer.MatchString(value)
}

// unauthenticated answers a credential goald cannot use: 401 unauthenticated, one body without
// detail (phase 3 §8.5).
func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="goal"`)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)

	// The client is gone or the connection broke; there is nothing left to tell it.
	_ = json.NewEncoder(w).Encode(gen.Problem{
		Title: http.StatusText(http.StatusUnauthorized), Status: http.StatusUnauthorized, Code: optional("unauthenticated"),
	})
}
