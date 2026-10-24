package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"

	"github.com/byFrederick/goal/internal/domain"
)

// requestIDHeader carries a request's id both ways, so audit entries and logs trace back to it.
const requestIDHeader = "X-Request-Id"

// requestID is what goald accepts as a caller's request id; anything else is replaced.
var requestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// withRequestID gives every request an id: the caller's X-Request-Id when it is well formed,
// otherwise a new random one. It is echoed in the response and put in the context
// (domain.WithRequestID), where the audit log picks it up.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !requestID.MatchString(id) {
			id = newRequestID()
		}

		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(domain.WithRequestID(r.Context(), id)))
	})
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error

	return hex.EncodeToString(b)
}
