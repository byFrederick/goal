package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
)

// seen is what the stand-in identity service received.
type seen struct {
	method, path, rawPath, query, host string
	header                             http.Header
	body                               []byte
}

// identityService is an httptest stand-in for the auth service that records every request and
// answers with answer (200 and an empty JSON object when nil).
type identityService struct {
	*httptest.Server

	mu       sync.Mutex
	requests []seen
}

func newIdentityService(t *testing.T, answer http.HandlerFunc) *identityService {
	t.Helper()

	s := &identityService{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		s.mu.Lock()
		s.requests = append(s.requests, seen{
			method: r.Method, path: r.URL.Path, rawPath: r.URL.RawPath, query: r.URL.RawQuery, host: r.Host,
			header: r.Header.Clone(), body: body,
		})
		s.mu.Unlock()

		if answer != nil {
			answer(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *identityService) received() []seen {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]seen(nil), s.requests...)
}

func (s *identityService) url(t *testing.T) *url.URL {
	t.Helper()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

// spaStatus is what the handler after the proxy answers with, so a test can tell it was reached.
const spaStatus = http.StatusTeapot

func proxyOf(t *testing.T, c api.AuthProxyConfig) http.Handler {
	t.Helper()

	return api.AuthProxy(c, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(spaStatus) }))
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	return rec
}

func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var p gen.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body %q is not problem details: %v", rec.Body.String(), err)
	}

	if p.Status != rec.Code {
		t.Errorf("problem status %d, response status %d", p.Status, rec.Code)
	}

	if p.Code == nil {
		return ""
	}

	return *p.Code
}

// Exactly the allowlisted method and path reach the service, as spelled, with the query string.
func TestAuthProxy_ForwardsTheAllowlist(t *testing.T) {
	t.Parallel()

	allowed := []struct{ method, path string }{
		{http.MethodGet, "/get-session"},
		{http.MethodPost, "/sign-out"},
		{http.MethodPost, "/revoke-sessions"},
		{http.MethodPost, "/update-user"},
		{http.MethodPost, "/email-otp/send-verification-otp"},
		{http.MethodPost, "/sign-in/email-otp"},
		{http.MethodPost, "/sign-in/social"},
		{http.MethodGet, "/callback/google"},
		{http.MethodPost, "/organization/invite-member"},
		{http.MethodPost, "/organization/cancel-invitation"},
		{http.MethodPost, "/organization/accept-invitation"},
		{http.MethodPost, "/organization/reject-invitation"},
		{http.MethodPost, "/organization/remove-member"},
		{http.MethodPost, "/organization/update-member-role"},
		{http.MethodPost, "/organization/leave"},
		{http.MethodGet, "/organization/get-invitation"},
		{http.MethodPost, "/device/code"},
		{http.MethodPost, "/device/token"},
		{http.MethodGet, "/device"},
		{http.MethodPost, "/device/approve"},
		{http.MethodPost, "/device/deny"},
	}

	for _, tt := range allowed {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()

			svc := newIdentityService(t, nil)
			h := proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t)})

			rec := serve(h, httptest.NewRequestWithContext(t.Context(), tt.method, api.AuthPath+tt.path+"?state=s1", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (%s), want 200 from the service", rec.Code, rec.Body.String())
			}

			got := svc.received()
			if len(got) != 1 || got[0].method != tt.method || got[0].path != api.AuthPath+tt.path || got[0].query != "state=s1" {
				t.Errorf("the service received %+v, want one %s %s%s?state=s1", got, tt.method, api.AuthPath, tt.path)
			}
		})
	}
}

// Everything else under /api, however it is spelled, is 404 problem not-found and never reaches
// the service; nothing under /api reaches the SPA, while other paths do.
func TestAuthProxy_RefusesEverythingElseUnderAPI(t *testing.T) {
	t.Parallel()

	refused := []struct{ name, method, target string }{
		{"disabled endpoint", http.MethodPost, "/api/auth/organization/create"},
		{"organization update", http.MethodPost, "/api/auth/organization/update"},
		{"organization delete", http.MethodPost, "/api/auth/organization/delete"},
		{"set active organization", http.MethodPost, "/api/auth/organization/set-active"},
		{"delete user", http.MethodPost, "/api/auth/delete-user"},
		{"health is not proxied", http.MethodGet, "/api/auth/ok"},
		{"other method", http.MethodPost, "/api/auth/get-session"},
		{"head of a GET route", http.MethodHead, "/api/auth/get-session"},
		{"case", http.MethodGet, "/api/auth/Get-Session"},
		{"trailing slash", http.MethodGet, "/api/auth/get-session/"},
		{"double slash", http.MethodGet, "/api/auth//get-session"},
		{"leading double slash", http.MethodGet, "//api/auth/get-session"},
		{"dot segment", http.MethodGet, "/api/auth/./get-session"},
		{"dot-dot segment", http.MethodGet, "/api/auth/x/../get-session"},
		{"dot-dot to the API", http.MethodGet, "/api/auth/../../v1/orgs"},
		{"encoded slash", http.MethodPost, "/api/auth/device%2Fapprove"},
		{"encoded slash, lower case", http.MethodPost, "/api/auth/device%2fapprove"},
		{"encoded dots", http.MethodGet, "/api/auth/%2E%2E/get-session"},
		{"encoded lower-case dot", http.MethodGet, "/api/auth/%2e/get-session"},
		{"encoded backslash", http.MethodGet, "/api/auth/get-session%5C"},
		{"encoded backslash, lower case", http.MethodGet, "/api/auth/get-session%5c"},
		{"encoded letter", http.MethodGet, "/api/auth/get%2Dsession"},
		{"encoded prefix", http.MethodGet, "/api%2Fauth/get-session"},
		{"the base path", http.MethodGet, "/api/auth"},
		{"the base path with a slash", http.MethodGet, "/api/auth/"},
		{"an unknown API path", http.MethodGet, "/api/unknown"},
		{"the API root", http.MethodGet, "/api"},
	}

	svc := newIdentityService(t, nil)
	h := proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t)})

	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(h, httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s: status = %d, want 404", tt.method, tt.target, rec.Code)
			}

			if tt.method != http.MethodHead {
				if code := problemCode(t, rec); code != "not-found" {
					t.Errorf("code = %q, want not-found", code)
				}
			}
		})
	}

	t.Cleanup(func() {
		if got := svc.received(); len(got) != 0 {
			t.Errorf("the service received %+v, want nothing", got)
		}
	})

	for _, target := range []string{"/", "/o/acme/f/goal/board", "/apiary", "/API/auth/get-session"} {
		if rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)); rec.Code != spaStatus {
			t.Errorf("%s: status = %d, want the next handler's", target, rec.Code)
		}
	}
}

// The service never sees a forwarding header a client sent: goald writes the client address it
// believes in X-Goal-Client-Ip, and the request's Host is the service's own, so its links come
// from its static base URL. Cookies, authorization and the request id pass.
func TestAuthProxy_ReplacesForwardingHeaders(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}

	tests := []struct {
		name    string
		trusted []netip.Prefix
		peer    string
		xff     []string
		want    string
	}{
		{name: "no trusted proxies", peer: "192.0.2.10:5100", xff: []string{clientAddr}, want: "192.0.2.10"},
		{name: "untrusted peer", trusted: trusted, peer: "192.0.2.10:5100", xff: []string{"10.0.0.9"}, want: "192.0.2.10"},
		{
			name: "trusted proxy", trusted: trusted, peer: proxyPeer,
			xff: []string{"198.51.100.7, 203.0.113.5"}, want: clientAddr,
		},
		{
			name: "two trusted proxies", trusted: trusted, peer: proxyPeer,
			xff: []string{"198.51.100.7, 203.0.113.9, 10.0.0.3"}, want: "203.0.113.9",
		},
		{
			name: "several header lines", trusted: trusted, peer: proxyPeer,
			xff: []string{"198.51.100.1", clientAddr}, want: clientAddr,
		},
		{name: "trusted proxy without the header", trusted: trusted, peer: proxyPeer, want: "10.0.0.2"},
		{name: "only trusted entries", trusted: trusted, peer: proxyPeer, xff: []string{"10.1.0.1, 10.0.0.3"}, want: "10.1.0.1"},
		{name: "malformed entry", trusted: trusted, peer: proxyPeer, xff: []string{"203.0.113.5, not-an-ip"}, want: "10.0.0.2"},
		{name: "entry with a port", trusted: trusted, peer: "[fd00::2]:5100", xff: []string{"[2001:db8::1]:4711"}, want: "2001:db8::1"},
		{name: "mapped IPv4 peer", peer: "[::ffff:192.0.2.10]:5100", want: "192.0.2.10"},
		// Not from a TCP listener: goald knows no address, and the client's own never passes.
		{name: "peer without an address", peer: "@", xff: []string{clientAddr}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newIdentityService(t, nil)
			h := proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t), TrustedProxies: tt.trusted})

			if rec := serve(h, forgedRequest(t, tt.peer, tt.xff)); rec.Code != http.StatusOK || rec.Header().Get("X-Request-Id") != callerID {
				t.Fatalf("status = %d, request id %q; want 200 and the caller's id", rec.Code, rec.Header().Get("X-Request-Id"))
			}

			got := svc.received()
			if len(got) != 1 {
				t.Fatalf("the service received %d requests, want 1", len(got))
			}

			checkForwarded(t, got[0], tt.want, svc.Listener.Addr().String())
		})
	}
}

// Addresses and the request id of TestAuthProxy_ReplacesForwardingHeaders.
const (
	clientAddr = "203.0.113.5"
	proxyPeer  = "10.0.0.2:5100"
	callerID   = "req-proxy-1"
)

// forgedRequest comes from peer with the X-Forwarded-For lines xff, every other forwarding
// header a client could forge, and credentials and a request id that must pass.
func forgedRequest(t *testing.T, peer string, xff []string) *http.Request {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/get-session", nil)
	r.Host, r.RemoteAddr = "goal.example.com", peer

	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}

	for name, value := range map[string]string{
		"X-Forwarded-Host": "evil.example", "X-Forwarded-Proto": "http", "X-Forwarded-Port": "80", "X-Forwarded-Prefix": "/x",
		"Forwarded": "for=198.51.100.66;host=evil.example", "X-Real-Ip": "198.51.100.66", "X-Goal-Client-Ip": "198.51.100.66",
		"Cookie": "goal.session_token=abc.def", "Authorization": "Bearer abc", "X-Request-Id": callerID,
	} {
		r.Header.Set(name, value)
	}

	return r
}

// checkForwarded asserts what the service received from forgedRequest: no forwarding header but
// X-Goal-Client-Ip with want (none when want is empty), its own address as Host, and the
// credentials and request id.
func checkForwarded(t *testing.T, in seen, want, serviceAddr string) {
	t.Helper()

	for name := range in.header {
		if strings.HasPrefix(name, "X-Forwarded-") || name == "Forwarded" || name == "X-Real-Ip" {
			t.Errorf("the service received %s: %q", name, in.header.Values(name))
		}
	}

	if ips := in.header.Values("X-Goal-Client-Ip"); strings.Join(ips, ",") != want {
		t.Errorf("X-Goal-Client-Ip = %q, want only %q", ips, want)
	}

	if in.host != serviceAddr {
		t.Errorf("Host = %s, want the service's own %s", in.host, serviceAddr)
	}

	if in.header.Get("Cookie") != "goal.session_token=abc.def" || in.header.Get("Authorization") != "Bearer abc" ||
		in.header.Get("X-Request-Id") != callerID {
		t.Errorf("cookie %q, authorization %q, request id %q; want them passed", in.header.Get("Cookie"),
			in.header.Get("Authorization"), in.header.Get("X-Request-Id"))
	}
}

// Review 3, L4: the identity service answers only goald, which sends it the shared secret with
// every forwarded request; a client's own X-Goal-Proxy-Secret never passes, configured or not.
func TestAuthProxy_SendsTheSharedSecret(t *testing.T) {
	t.Parallel()

	for _, secret := range []string{"proxy-secret-for-tests-only-0123456789", ""} {
		svc := newIdentityService(t, nil)
		h := proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t), Secret: secret})

		r := forgedRequest(t, "192.0.2.10:5100", nil)
		r.Header.Set("X-Goal-Proxy-Secret", "a-client-guess")

		if rec := serve(h, r); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		got := svc.received()
		if len(got) != 1 {
			t.Fatalf("the service received %d requests, want 1", len(got))
		}

		if values := got[0].header.Values("X-Goal-Proxy-Secret"); strings.Join(values, ",") != secret {
			t.Errorf("secret %q: the service received %q; want only goald's", secret, values)
		}
	}
}

// The bearer plugin's Set-Auth-Token never reaches a browser; the session cookie does.
func TestAuthProxy_DropsSetAuthToken(t *testing.T) {
	t.Parallel()

	svc := newIdentityService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Auth-Token", "token-value")
		w.Header().Set("Set-Cookie", "goal.session_token=abc.def; HttpOnly; SameSite=Lax; Path=/")
		w.WriteHeader(http.StatusOK)
	})

	rec := serve(proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t)}),
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/sign-in/email-otp", strings.NewReader("{}")))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if v := rec.Header().Values("Set-Auth-Token"); len(v) != 0 {
		t.Errorf("Set-Auth-Token = %q, want none", v)
	}

	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("the session cookie was dropped too")
	}
}

// Spec 3 §11.1 row 5 (review M2): no response a page script reads carries the session token. The
// auth service strips it already; the proxy strips it again from whatever answers a session
// (get-session, the sign-ins) and refuses one it cannot read. goal login's access token passes.
func TestAuthProxy_AnswersNoSessionToken(t *testing.T) {
	t.Parallel()

	const (
		token      = "LeakLeakLeakLeakLeakLeakLeakLe01" //nolint:gosec // a fabricated session token
		getSession = "/api/auth/get-session"
	)

	session := `{"session":{"id":"s1","token":"` + token + `","userId":"u1"},"user":{"id":"u1","email":"ada@example.com"},` +
		`"token":"` + token + `","redirect":false}`

	for _, c := range []struct {
		name, method, path, answer, encoding string
		status                               int
		keep                                 []string
		gone                                 bool
	}{
		{"get-session", http.MethodGet, getSession, session, "", http.StatusOK, []string{`"userId":"u1"`, `"email":"ada@example.com"`}, true},
		{"no session", http.MethodGet, getSession, "null", "", http.StatusOK, []string{"null"}, true},
		{"a code sign-in", http.MethodPost, "/api/auth/sign-in/email-otp", session, "", http.StatusOK, []string{`"user":`}, true},
		{"a Google sign-in", http.MethodPost, "/api/auth/sign-in/social", session, "", http.StatusOK, []string{`"redirect":false`}, true},
		{"a compressed session", http.MethodGet, getSession, session, "gzip", http.StatusServiceUnavailable, nil, true},
		{"goal login's token", http.MethodPost, "/api/auth/device/token", `{"access_token":"` + token + `"}`, "", http.StatusOK, []string{token}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			svc := newIdentityService(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				if c.encoding != "" {
					w.Header().Set("Content-Encoding", c.encoding)
				}

				_, _ = w.Write([]byte(c.answer))
			})

			rec := serve(proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t)}),
				httptest.NewRequestWithContext(t.Context(), c.method, c.path, strings.NewReader("{}")))

			body := rec.Body.String()
			if rec.Code != c.status || (c.gone && strings.Contains(body, token)) {
				t.Fatalf("%s = %d %s; want %d without the session token", c.path, rec.Code, body, c.status)
			}

			for _, k := range c.keep {
				if !strings.Contains(body, k) {
					t.Errorf("%s = %s; want %s kept", c.path, body, k)
				}
			}

			if n := rec.Header().Get("Content-Length"); n != "" && n != strconv.Itoa(len(body)) {
				t.Errorf("Content-Length = %s for a body of %d bytes", n, len(body))
			}
		})
	}
}

// Spec 3 §11.1 row 45: an API key or a personal token is Goal's own credential: the proxy answers
// 401 and the identity service never sees it. A session token still passes (goal login's).
func TestAuthProxy_NeverForwardsGoalCredentials(t *testing.T) {
	t.Parallel()

	svc := newIdentityService(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	proxy := proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t)})

	for _, c := range []struct {
		name, authorization string
		want                int
	}{
		{"an API key", "Bearer goal_sk_" + strings.Repeat("a", 43), http.StatusUnauthorized},
		{"a personal token", "Bearer goal_pat_" + strings.Repeat("b", 43), http.StatusUnauthorized},
		{"a personal token, scheme in lower case", "bearer goal_pat_" + strings.Repeat("c", 43), http.StatusUnauthorized},
		{"a malformed personal token", "Bearer goal_pat_short", http.StatusUnauthorized},
		{"goal login's session token", "Bearer " + strings.Repeat("S", 32), http.StatusOK},
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/get-session", nil)
		r.Header.Set("Authorization", c.authorization)

		rec := serve(proxy, r)
		if rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}

		if c.want == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: no WWW-Authenticate", c.name)
		}
	}

	for _, in := range svc.received() {
		if strings.Contains(in.header.Get("Authorization"), "goal_") {
			t.Errorf("the service received %q", in.header.Get("Authorization"))
		}
	}

	if n := len(svc.received()); n != 1 {
		t.Errorf("the service received %d requests, want the session's alone", n)
	}
}

// A body of up to 64 KiB is forwarded whole; a larger one is 413 and never reaches the service.
func TestAuthProxy_LimitsBodies(t *testing.T) {
	t.Parallel()

	const limit = 64 << 10

	svc := newIdentityService(t, nil)
	h := proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t)})

	full := bytes.Repeat([]byte("a"), limit)
	if rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/update-user", bytes.NewReader(full))); rec.Code != http.StatusOK {
		t.Fatalf("64 KiB: status = %d, want 200", rec.Code)
	}

	if got := svc.received(); len(got) != 1 || !bytes.Equal(got[0].body, full) || got[0].header.Get("Content-Length") != "65536" {
		t.Fatalf("the service received %d requests, want the 64 KiB body whole", len(got))
	}

	rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/update-user",
		bytes.NewReader(append(full, 'b'))))
	if rec.Code != http.StatusRequestEntityTooLarge || problemCode(t, rec) != "too-large" {
		t.Fatalf("64 KiB + 1: status = %d (%s), want 413 too-large", rec.Code, rec.Body.String())
	}

	if got := svc.received(); len(got) != 1 {
		t.Errorf("the service received %d requests, want the oversized one kept from it", len(got))
	}
}

// A service that is down, or slower than the header or the whole-request timeout, is 503 problem
// auth-unavailable.
func TestAuthProxy_UnavailableService(t *testing.T) {
	t.Parallel()

	down := newIdentityService(t, nil)
	downURL := down.url(t)
	down.Close()

	stalled := func(t *testing.T) *url.URL {
		t.Helper()

		release := make(chan struct{})
		svc := newIdentityService(t, func(http.ResponseWriter, *http.Request) { <-release })
		// Cleanups run last first: the stalled handlers return before the server closes.
		t.Cleanup(func() { close(release) })

		return svc.url(t)
	}

	const short = 50 * time.Millisecond

	tests := []struct {
		name   string
		config func(t *testing.T) api.AuthProxyConfig
	}{
		{"down", func(*testing.T) api.AuthProxyConfig { return api.AuthProxyConfig{Upstream: downURL} }},
		{"no response headers in time", func(t *testing.T) api.AuthProxyConfig {
			t.Helper()
			return api.AuthProxyConfig{Upstream: stalled(t), HeaderTimeout: short}
		}},
		{"over the request timeout", func(t *testing.T) api.AuthProxyConfig {
			t.Helper()
			return api.AuthProxyConfig{Upstream: stalled(t), RequestTimeout: short}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(proxyOf(t, tt.config(t)), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/get-session", nil))
			if rec.Code != http.StatusServiceUnavailable || problemCode(t, rec) != "auth-unavailable" {
				t.Errorf("status = %d (%s), want 503 auth-unavailable", rec.Code, rec.Body.String())
			}
		})
	}
}

// A browser page of another origin cannot use the proxied endpoints that change things.
func TestAuthProxy_RefusesCrossOriginWrites(t *testing.T) {
	t.Parallel()

	svc := newIdentityService(t, nil)
	h := proxyOf(t, api.AuthProxyConfig{Upstream: svc.url(t)})

	post := func(site, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/sign-out", strings.NewReader("{}"))
		r.Host = "goal.example.com"
		r.Header.Set("Sec-Fetch-Site", site)
		r.Header.Set("Origin", origin)

		return serve(h, r)
	}

	if rec := post("cross-site", "https://evil.example"); rec.Code != http.StatusForbidden || problemCode(t, rec) != "forbidden" {
		t.Fatalf("cross-site: status = %d (%s), want 403 forbidden", rec.Code, rec.Body.String())
	}

	if got := svc.received(); len(got) != 0 {
		t.Fatalf("the service received the cross-site request")
	}

	if rec := post("same-origin", "https://goal.example.com"); rec.Code != http.StatusOK {
		t.Fatalf("same-origin: status = %d, want 200", rec.Code)
	}
}

// goald logs each proxied request's method, path, status and request id, and none of its
// credentials, query string or body, nor the service's cookies, whether the service answers or not.
func TestAuthProxy_LogsNoSecrets(t *testing.T) {
	t.Parallel()

	secrets := []string{"cookie-secret", "bearer-secret", "query-secret", "body-secret", "set-cookie-secret", "auth-token-secret", "proxy-shared-secret"}

	svc := newIdentityService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "goal.session_token=set-cookie-secret")
		w.Header().Set("Set-Auth-Token", "auth-token-secret")
		_, _ = w.Write([]byte("{}"))
	})
	down := newIdentityService(t, nil)
	downURL := down.url(t)
	down.Close()

	for _, upstream := range []*url.URL{svc.url(t), downURL} {
		var buf bytes.Buffer

		h := proxyOf(t, api.AuthProxyConfig{Upstream: upstream, Secret: "proxy-shared-secret", Logger: slog.New(slog.NewJSONHandler(&buf, nil))})

		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/sign-in/email-otp?code=query-secret",
			strings.NewReader(`{"otp":"body-secret"}`))
		r.Header.Set("Cookie", "goal.session_token=cookie-secret")
		r.Header.Set("Authorization", "Bearer bearer-secret")
		r.Header.Set("X-Request-Id", "req-7")

		rec := serve(h, r)

		log := buf.String()
		for _, s := range secrets {
			if strings.Contains(log, s) {
				t.Errorf("the log holds %q:\n%s", s, log)
			}
		}

		for _, want := range []string{
			`"method":"POST"`, `"path":"/api/auth/sign-in/email-otp"`, `"request_id":"req-7"`, `"duration"`,
			`"status":` + strconv.Itoa(rec.Code),
		} {
			if !strings.Contains(log, want) {
				t.Errorf("the log lacks %s:\n%s", want, log)
			}
		}
	}
}
