package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// accessRecord is what one request's access log line says beyond its method, status and request
// id: scope fills it in once the operation is routed and the principal made. It never holds a
// secret: the principal and the credential are named by their ids.
type accessRecord struct {
	operation string
	principal domain.Principal
	org       domain.OrgID
}

type accessKey struct{}

// noteOperation records the request's operation for its access log line.
func noteOperation(ctx context.Context, op string) {
	if rec, ok := ctx.Value(accessKey{}).(*accessRecord); ok {
		rec.operation = op
	}
}

// notePrincipal records who the request acts for, and in which organization, for its access log
// line.
func notePrincipal(ctx context.Context) {
	rec, ok := ctx.Value(accessKey{}).(*accessRecord)
	if !ok {
		return
	}

	rec.principal, _ = domain.PrincipalFrom(ctx)
	rec.org = orgOf(ctx).ID
}

// accessLog logs every request once it is answered (spec 3 §12): its method, operationId (never
// its path, which may name an invitation), status and request id, its principal (user:<id>,
// service:<key id> or none) and credential (session, key:<id>, pat:<id>), its organization's id
// and how long it took. Never a cookie, an Authorization value, a token or an email. A refusal
// before routing (cross-origin, a malformed credential) has no operation; one before the
// principal is made (401) has principal none.
func (s *server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &accessRecord{}
		sw := &statusWriter{ResponseWriter: w}

		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), accessKey{}, rec)))

		attrs := []slog.Attr{
			slog.String("method", r.Method), slog.String("operation", rec.operation), slog.Int("status", sw.code()),
			slog.String("request_id", w.Header().Get(requestIDHeader)), slog.String("principal", principalName(rec.principal)),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		}
		if c := credentialLabel(rec.principal); c != "" {
			attrs = append(attrs, slog.String("credential", c))
		}

		if rec.org != "" {
			attrs = append(attrs, slog.String("org_id", string(rec.org)))
		}

		s.log.LogAttrs(r.Context(), slog.LevelInfo, "api request", attrs...)
	})
}

// principalName names a principal in logs: user:<id> (user:local under GOAL_AUTH=none),
// service:<key id>, or none for nobody.
func principalName(p domain.Principal) string {
	switch p.Kind {
	case domain.PrincipalUser:
		return "user:" + string(p.User)
	case domain.PrincipalService:
		return "service:" + string(p.Key)
	case domain.PrincipalAgent, domain.PrincipalSystem:
		return string(p.Kind) + ":" + p.Name
	default:
		return "none"
	}
}

// credentialLabel names the credential a principal came with: pat:<id> for a personal token,
// key:<id> for an API key, session for a session (the browser's cookie or goal login's bearer);
// "" for none (GOAL_AUTH=none, a public operation, a refusal).
func credentialLabel(p domain.Principal) string {
	switch {
	case p.Token != nil:
		return "pat:" + string(p.Token.ID)
	case p.Kind == domain.PrincipalService:
		return "key:" + string(p.Key)
	case p.Session != nil:
		return "session"
	default:
		return ""
	}
}
