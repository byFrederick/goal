package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/domain"
)

// AuthPath is where goald serves the identity service's endpoints, on its own origin.
const AuthPath = "/api/auth"

// clientIPHeader tells the identity service the client's address, for its rate limits and
// session records. goald always writes it: a client's own value never passes.
const clientIPHeader = "X-Goal-Client-Ip"

// proxySecretHeader carries AuthProxyConfig.Secret to the identity service, which refuses
// requests without it. goald always writes it: a client's own value never passes.
const proxySecretHeader = "X-Goal-Proxy-Secret" //nolint:gosec // a header name, not a credential

// authTokenHeader is the bearer plugin's header carrying a new session token, which no browser
// response may expose to scripts.
const authTokenHeader = "Set-Auth-Token" //nolint:gosec // a header name, not a credential

// Bounds of a proxied request: its body, reaching the service, the service's response headers and
// the whole exchange; and of an answer the proxy reads (sessionAnswers).
const (
	maxAuthRequestBytes       = 64 << 10
	maxSessionAnswerBytes     = 256 << 10
	defaultAuthDialTimeout    = 2 * time.Second
	defaultAuthHeaderTimeout  = 10 * time.Second
	defaultAuthRequestTimeout = 15 * time.Second
)

// authRoutes are the only identity endpoints goald forwards, each path under AuthPath with its
// one method, compared exactly. The service's other endpoints (organization create, update and
// delete among them) are never reached through goald.
var authRoutes = map[string]string{
	"/get-session":                     http.MethodGet,
	"/sign-out":                        http.MethodPost,
	"/revoke-sessions":                 http.MethodPost,
	"/update-user":                     http.MethodPost,
	"/email-otp/send-verification-otp": http.MethodPost,
	"/sign-in/email-otp":               http.MethodPost,
	"/sign-in/social":                  http.MethodPost,
	"/callback/google":                 http.MethodGet,
	"/organization/invite-member":      http.MethodPost,
	"/organization/cancel-invitation":  http.MethodPost,
	"/organization/accept-invitation":  http.MethodPost,
	"/organization/reject-invitation":  http.MethodPost,
	"/organization/remove-member":      http.MethodPost,
	"/organization/update-member-role": http.MethodPost,
	"/organization/leave":              http.MethodPost,
	"/organization/get-invitation":     http.MethodGet,
	"/device/code":                     http.MethodPost,
	"/device/token":                    http.MethodPost,
	"/device":                          http.MethodGet,
	"/device/approve":                  http.MethodPost,
	"/device/deny":                     http.MethodPost,
}

// AuthProxyConfig configures AuthProxy.
type AuthProxyConfig struct {
	// Upstream is the identity service's origin (GOAL_AUTH_UPSTREAM, http on loopback).
	Upstream *url.URL
	// TrustedProxies are the proxies in front of goald whose X-Forwarded-For is believed
	// (GOAL_TRUSTED_PROXIES).
	TrustedProxies []netip.Prefix
	// Secret is sent as X-Goal-Proxy-Secret with every forwarded request (GOAL_AUTH_PROXY_SECRET),
	// so the service answers goald and nothing else on its loopback port.
	Secret string
	// DialTimeout, HeaderTimeout and RequestTimeout bound reaching the service, waiting for its
	// response headers and the whole request; zero is 2 s, 10 s and 15 s.
	DialTimeout, HeaderTimeout, RequestTimeout time.Duration
	// Logger may be nil.
	Logger *slog.Logger
}

// AuthProxy serves every request whose path, cleaned, is /api or under it with a reverse proxy to
// the identity service, and every other request with next. The proxy forwards only authRoutes,
// spelled exactly (no percent-encoding, empty or dot segment, trailing slash, other case or other
// method), and answers everything else under /api with 404 problem not-found; nothing under /api
// is redirected to a cleaner path or reaches the SPA. A forwarded request:
//
//   - passes cross-origin protection (unsafe methods only from goald's own origin) and gets a
//     request id (X-Request-Id, the caller's when well formed), which the service receives too;
//   - has a body of at most 64 KiB (413 problem too-large), read before the service is reached;
//   - loses every X-Forwarded-* header, Forwarded, X-Real-Ip and X-Goal-Client-Ip, and gets
//     X-Goal-Client-Ip: the right-most X-Forwarded-For entry outside TrustedProxies when the peer
//     is one of them, else the peer's address;
//   - carries Secret in X-Goal-Proxy-Secret, whatever the client sent there;
//   - names the upstream as its Host, so the service builds its links from its own static base URL;
//   - is answered without Set-Auth-Token, and with 503 problem auth-unavailable when the service is
//     down or slower than the timeouts.
//
// Each request is logged with its method, path, status, duration and request id: never cookies,
// authorization, query strings or bodies.
func AuthProxy(c AuthProxyConfig, next http.Handler) http.Handler {
	p := newAuthProxy(c)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if underAPI(r.URL.Path) {
			p.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// underAPI reports whether p, as sent or cleaned as http.ServeMux would clean it, is /api or
// under it.
func underAPI(p string) bool {
	for _, s := range []string{p, path.Clean("/" + p)} {
		if s == "/api" || strings.HasPrefix(s, "/api/") {
			return true
		}
	}

	return false
}

type authProxy struct {
	proxy   *httputil.ReverseProxy
	trusted []netip.Prefix
	secret  string
	timeout time.Duration
	log     *slog.Logger
	handler http.Handler
}

func newAuthProxy(c AuthProxyConfig) *authProxy {
	log := c.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	p := &authProxy{trusted: c.TrustedProxies, secret: c.Secret, timeout: orDefault(c.RequestTimeout, defaultAuthRequestTimeout), log: log}
	upstream := *c.Upstream

	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) { p.rewrite(pr, &upstream) },
		Transport: &http.Transport{
			// Never an environment proxy: the service is on this machine.
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: orDefault(c.DialTimeout, defaultAuthDialTimeout)}).DialContext,
			ResponseHeaderTimeout: orDefault(c.HeaderTimeout, defaultAuthHeaderTimeout),
			MaxIdleConns:          16,
			IdleConnTimeout:       90 * time.Second,
			// Bodies pass as they are, compressed or not.
			DisableCompression: true,
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del(authTokenHeader)

			return withoutSessionToken(resp)
		},
		ErrorHandler: p.unavailable,
		ErrorLog:     slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		detail := errCrossOrigin.Error()
		writeProblem(w, gen.Problem{
			Title: http.StatusText(http.StatusForbidden), Status: http.StatusForbidden, Detail: &detail,
			Code: optional("forbidden"),
		})
	}))
	p.handler = withRequestID(cop.Handler(http.HandlerFunc(p.forward)))

	return p
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}

	return def
}

func (p *authProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w}

	// Deferred, so a response the proxy aborts midway (http.ErrAbortHandler) is logged too.
	defer func() {
		p.log.InfoContext(r.Context(), "auth request", slog.String("method", r.Method), slog.String("path", r.URL.Path),
			slog.Int("status", sw.code()), slog.Duration("duration", time.Since(start)),
			slog.String("request_id", w.Header().Get(requestIDHeader)))
	}()

	p.handler.ServeHTTP(sw, r)
}

// forward sends an allowed request, its body read first, to the service within the timeout.
func (p *authProxy) forward(w http.ResponseWriter, r *http.Request) {
	if !authRoute(r) {
		writeProblem(w, gen.Problem{Title: http.StatusText(http.StatusNotFound), Status: http.StatusNotFound, Code: optional("not-found")})
		return
	}

	// An API key or a personal token is Goal's own credential, for /v1 only: the identity
	// service never sees one (spec 3 §11.1 row 45).
	if goalBearer(r.Header.Values("Authorization")) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="goal"`)
		writeProblem(w, problemOf(domain.ErrUnauthenticated))

		return
	}

	deadline := time.Now().Add(p.timeout)
	// The deadline bounds a slow body too. A ResponseWriter without read deadlines (a test
	// recorder) leaves that to the server's ReadTimeout.
	_ = http.NewResponseController(w).SetReadDeadline(deadline)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAuthRequestBytes))
	if err != nil {
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			writeProblem(w, gen.Problem{
				Title: http.StatusText(http.StatusRequestEntityTooLarge), Status: http.StatusRequestEntityTooLarge,
				Code: optional(authoring.CodeTooLarge),
			})

			return
		}

		writeProblem(w, gen.Problem{Title: http.StatusText(http.StatusBadRequest), Status: http.StatusBadRequest})

		return
	}

	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()

	out := r.WithContext(ctx)
	out.Body, out.ContentLength, out.TransferEncoding = http.NoBody, 0, nil

	if len(body) > 0 {
		out.Body, out.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
	}

	p.proxy.ServeHTTP(w, out)
}

// sessionAnswers are the identity endpoints whose answer to the page holds a session: Better Auth
// puts its token there (get-session's session.token, a sign-in's token), where any script on the
// page could read what the httpOnly cookie hides. The auth service takes it out; the proxy does
// again (spec 3 §11.1 row 5, review M2). /device/token keeps its access_token: goal login's
// credential, read by the CLI, not a page.
var sessionAnswers = map[string]bool{"/get-session": true, "/sign-in/email-otp": true, "/sign-in/social": true}

// errUnreadableSessionAnswer is a session answer the proxy cannot check for a token (compressed,
// too large or not JSON): it is refused rather than passed on. It names no part of the body.
var errUnreadableSessionAnswer = errors.New("the identity service's session answer cannot be checked")

// withoutSessionToken removes the top-level token and session.token from the JSON answer of a
// sessionAnswers endpoint, keeping everything else; other answers pass untouched.
func withoutSessionToken(resp *http.Response) error {
	if resp.Request == nil || !sessionAnswers[strings.TrimPrefix(resp.Request.URL.Path, AuthPath)] {
		return nil
	}

	if enc := resp.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return errUnreadableSessionAnswer
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSessionAnswerBytes+1))
	_ = resp.Body.Close()

	if err != nil || len(raw) > maxSessionAnswerBytes {
		return errUnreadableSessionAnswer
	}

	out, err := stripSessionToken(raw)
	if err != nil {
		return err
	}

	resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(out)), int64(len(out))
	resp.Header.Set("Content-Length", strconv.Itoa(len(out)))

	return nil
}

// stripSessionToken is raw without its top-level "token" and session's "token". An empty body,
// null or any JSON value but an object passes as it is; anything that is not JSON is refused.
func stripSessionToken(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, nil
	}

	if !json.Valid(trimmed) {
		return nil, errUnreadableSessionAnswer
	}

	if trimmed[0] != '{' {
		return raw, nil // null, a list or a scalar: no token field to hold
	}

	var answer map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &answer); err != nil {
		return nil, errors.Join(errUnreadableSessionAnswer, err)
	}

	delete(answer, "token")

	var session map[string]json.RawMessage
	if s, ok := answer["session"]; ok && json.Unmarshal(s, &session) == nil && session != nil {
		delete(session, "token")

		kept, err := json.Marshal(session)
		if err != nil {
			return nil, fmt.Errorf("session answer: %w", err)
		}

		answer["session"] = kept
	}

	out, err := json.Marshal(answer)
	if err != nil {
		return nil, fmt.Errorf("session answer: %w", err)
	}

	return out, nil
}

// goalBearer reports whether an Authorization header carries a Goal API key or personal token
// (Bearer goal_sk_… or goal_pat_…, the scheme's case ignored), whatever else it holds.
func goalBearer(values []string) bool {
	for _, v := range values {
		scheme, value, _ := strings.Cut(strings.TrimSpace(v), " ")
		value = strings.TrimSpace(value)

		if strings.EqualFold(scheme, "Bearer") && (strings.HasPrefix(value, apiKeyPrefix) || strings.HasPrefix(value, personalTokenPrefix)) {
			return true
		}
	}

	return false
}

// authRoute reports whether r is one of authRoutes, spelled exactly. Any percent-encoding sets
// RawPath (the allowed paths hold no character that needs one), and an empty or dot segment, a
// trailing slash or another case is a path that is not in the table.
func authRoute(r *http.Request) bool {
	if r.URL.RawPath != "" {
		return false
	}

	route, ok := strings.CutPrefix(r.URL.Path, AuthPath)

	return ok && authRoutes[route] == r.Method
}

// rewrite addresses the request to upstream and replaces every forwarding header with goald's
// own: the client address and the request id.
func (p *authProxy) rewrite(pr *httputil.ProxyRequest, upstream *url.URL) {
	pr.SetURL(upstream)
	pr.Out.URL.Path, pr.Out.URL.RawPath = pr.In.URL.Path, ""

	for name := range pr.Out.Header {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), "X-Forwarded-") {
			delete(pr.Out.Header, name)
		}
	}

	for _, name := range []string{"Forwarded", "X-Real-Ip", clientIPHeader, proxySecretHeader} {
		pr.Out.Header.Del(name)
	}

	if p.secret != "" {
		pr.Out.Header.Set(proxySecretHeader, p.secret)
	}

	if ip := clientIP(pr.In.RemoteAddr, pr.In.Header.Values("X-Forwarded-For"), p.trusted); ip.IsValid() {
		pr.Out.Header.Set(clientIPHeader, ip.String())
	}

	pr.Out.Header.Set(requestIDHeader, domain.RequestIDFrom(pr.In.Context()))
}

// clientIP is the address of the client behind remoteAddr (the peer, host:port): the peer itself
// unless it is a trusted proxy, else the right-most X-Forwarded-For entry that is not one (every
// entry a trusted proxy appended stands for the hop before it; the entries left of the first
// untrusted one are the client's to forge). When every entry is trusted the left-most is the
// client; a malformed entry stops the walk at the last trusted address. It is invalid only when
// remoteAddr is not an address, which a TCP listener never gives.
func clientIP(remoteAddr string, forwardedFor []string, trusted []netip.Prefix) netip.Addr {
	peer, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return netip.Addr{}
	}

	addr := peer.Addr().Unmap().WithZone("")
	if !isTrusted(addr, trusted) {
		return addr
	}

	var entries []string
	for _, v := range forwardedFor {
		entries = append(entries, strings.Split(v, ",")...)
	}

	for _, entry := range slices.Backward(entries) {
		hop, ok := forwardedAddr(strings.TrimSpace(entry))
		if !ok {
			return addr
		}

		addr = hop
		if !isTrusted(hop, trusted) {
			return hop
		}
	}

	return addr
}

// forwardedAddr parses one X-Forwarded-For entry: an address, or an address with a port.
func forwardedAddr(s string) (netip.Addr, bool) {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap().WithZone(""), true
	}

	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap().WithZone(""), true
	}

	return netip.Addr{}, false
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}

	return false
}

// unavailable answers a request the service did not answer in time, or at all.
func (p *authProxy) unavailable(w http.ResponseWriter, r *http.Request, err error) {
	// The transport's errors name the service's address, never the request's URL or headers.
	p.log.WarnContext(r.Context(), "auth service unavailable", slog.String("method", r.Method), slog.String("path", r.URL.Path),
		slog.String("request_id", domain.RequestIDFrom(r.Context())), slog.Any("error", err))

	detail := "the identity service cannot be reached now; see goald's log"
	writeProblem(w, gen.Problem{
		Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable,
		Detail: &detail, Code: optional("auth-unavailable"),
	})
}

// writeProblem writes p as problem details.
func writeProblem(w http.ResponseWriter, p gen.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)

	// The client is gone or the connection broke; there is nothing left to tell it.
	_ = json.NewEncoder(w).Encode(p)
}

// statusWriter records the status of a response for the log. Unwrap keeps the underlying
// writer's flushing and deadlines reachable (http.ResponseController).
type statusWriter struct {
	http.ResponseWriter

	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 && code >= http.StatusOK {
		w.status = code
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}

	return w.status
}
