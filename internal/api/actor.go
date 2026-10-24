package api

import (
	"net/http"

	"github.com/byFrederick/goal/internal/domain"
)

// withActor sets the actor of every request. It is the only place a request's actor is made:
// phase 2.2 has no identity, so it is always domain.LocalUser. Phase 4 replaces its source (a
// session or a token) and nothing else; handlers and the engine read domain.ActorFrom.
func withActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(domain.WithActor(r.Context(), domain.LocalUser)))
	})
}
