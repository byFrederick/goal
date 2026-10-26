package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/netip"
	"regexp"

	"github.com/byFrederick/goal/internal/core/domain"
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

type clientAddrKey struct{}

// withClientAddr puts the request's client address in its context (clientAddrFrom): the peer, or
// behind trusted proxies the address they name (clientIP, as the /api/auth proxy tells the
// identity service), so goald and the identity service see the same client.
func withClientAddr(trusted []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr := clientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), trusted)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientAddrKey{}, addr)))
	})
}

// clientAddrFrom is the request's client address; invalid when unknown.
func clientAddrFrom(ctx context.Context) netip.Addr {
	a, _ := ctx.Value(clientAddrKey{}).(netip.Addr)

	return a
}
