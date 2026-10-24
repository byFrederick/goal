package api

import (
	"net/http"

	"github.com/byFrederick/goal/internal/domain"
)

// legacyOrg runs every request in one organization, the install's default, while the routes
// are still unscoped. Phase 2.2 A5 moves them under /v1/orgs/{org}, whose org middleware takes
// the organization from the path, and deletes this file.
func legacyOrg(org domain.OrgID, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(domain.WithOrg(r.Context(), org)))
	})
}
