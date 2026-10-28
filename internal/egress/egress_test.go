package egress

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The model API as the tests' DNS has it.
const (
	modelHost = "api.anthropic.com"
	modelDest = modelHost + ":443"
	modelAddr = "160.79.104.10:443"
)

// world is the DNS and the routes a test's proxy sees: names resolve to the addresses given, and
// dialing one of the routes' addresses connects to the local listener behind it. It records every
// address the proxy dials.
type world struct {
	names  map[string][]string
	routes map[string]string

	mu     sync.Mutex
	dialed []string
}

func (w *world) lookup(_ context.Context, host string) ([]netip.Addr, error) {
	raw, ok := w.names[host]
	if !ok {
		return nil, errors.New("no such host")
	}

	addrs := make([]netip.Addr, 0, len(raw))
	for _, a := range raw {
		addrs = append(addrs, netip.MustParseAddr(a))
	}

	return addrs, nil
}

func (w *world) dial(ctx context.Context, network, address string) (net.Conn, error) {
	w.mu.Lock()
	w.dialed = append(w.dialed, address)
	w.mu.Unlock()

	to, ok := w.routes[address]
	if !ok {
		return nil, fmt.Errorf("no route to %s", address)
	}

	var d net.Dialer

	return d.DialContext(ctx, network, to)
}

func (w *world) dials() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.dialed)
}

// echoServer answers each line it reads with "echo: <line>".
func echoServer(t *testing.T) string {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			go func() {
				defer func() { _ = c.Close() }()

				s := bufio.NewScanner(c)
				for s.Scan() {
					_, _ = fmt.Fprintf(c, "echo: %s\n", s.Text())
				}
			}()
		}
	}()

	return ln.Addr().String()
}

func startProxy(t *testing.T, cfg Config, w *world) *httptest.Server {
	t.Helper()

	cfg.Lookup, cfg.Dial = w.lookup, w.dial

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	return srv
}

// connect asks proxy for a tunnel to dest and, when it is granted, sends one line through it.
func connect(t *testing.T, proxy *httptest.Server, dest string) (status int, reply string) {
	t.Helper()

	c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	_ = c.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", dest, dest); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(c)

	res, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("CONNECT %s: %v", dest, err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return res.StatusCode, ""
	}

	if _, err := io.WriteString(c, "ping\n"); err != nil {
		t.Fatal(err)
	}

	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read through the tunnel: %v", err)
	}

	return res.StatusCode, strings.TrimSpace(line)
}

// Any client reaches any public host and port, by name (normalized) or by address, dialed at the
// address the proxy resolved.
func TestProxy_TunnelsToPublicHosts(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{
		names:  map[string][]string{modelHost: {"160.79.104.10"}, "registry.npmjs.org": {"104.16.1.34"}},
		routes: map[string]string{modelAddr: echo, "104.16.1.34:443": echo, "104.16.1.34:22": echo},
	}
	proxy := startProxy(t, Config{}, w)

	for _, dest := range []string{"API.Anthropic.com.:443", "registry.npmjs.org:443", "104.16.1.34:22"} {
		if status, reply := connect(t, proxy, dest); status != http.StatusOK || reply != "echo: ping" {
			t.Errorf("CONNECT %s: status %d, reply %q; want a working tunnel", dest, status, reply)
		}
	}

	if got := w.dials(); !slices.Equal(got, []string{modelAddr, "104.16.1.34:443", "104.16.1.34:22"}) {
		t.Errorf("dialed %v; want each destination at the address it resolved to", got)
	}
}

// A name that resolves (or rebinds) to a non-public address is never dialed, and no IP literal
// of the host, its bridge or the metadata service is either.
func TestProxy_RefusesNonPublicDestinations(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{
		names: map[string][]string{
			modelHost:              {"10.0.0.7"},
			"mixed.example.com":    {"93.184.215.14", "127.0.0.2"},
			"host.docker.internal": {"0.250.250.254"},
			"rustfs":               {"172.18.0.3"},
			"metadata.example.com": {"169.254.169.254"},
			"v6.example.com":       {"::1"},
			"mapped.example.com":   {"::ffff:192.168.1.10"},
			"localhost":            {"127.0.0.1", "::1"},
		},
		routes: map[string]string{"10.0.0.7:443": echo, "127.0.0.1:443": echo, "93.184.215.14:443": echo},
	}
	proxy := startProxy(t, Config{}, w)

	for _, dest := range []string{
		modelDest, "mixed.example.com:443", "host.docker.internal:8080", "rustfs:9000", "metadata.example.com:80", "v6.example.com:443",
		"mapped.example.com:443", "127.0.0.1:5432", "[::1]:8080", "169.254.169.254:80", "172.17.0.1:8090", "192.168.65.254:8025",
		"localhost:8081", "100.100.100.200:80", "[fd00:ec2::254]:80",
	} {
		if status, _ := connect(t, proxy, dest); status != http.StatusForbidden {
			t.Errorf("CONNECT %s = %d, want 403", dest, status)
		}
	}

	if got := w.dials(); len(got) != 0 {
		t.Errorf("dialed %v; want nothing", got)
	}
}

// A client may send its first bytes (a TLS hello) right behind the CONNECT, before the answer.
func TestProxy_TunnelsBytesSentWithTheConnect(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{names: map[string][]string{modelHost: {"160.79.104.10"}}, routes: map[string]string{modelAddr: echo}}
	proxy := startProxy(t, Config{}, w)

	c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	_ = c.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := io.WriteString(c, "CONNECT api.anthropic.com:443 HTTP/1.1\r\nHost: api.anthropic.com:443\r\n\r\nhello\n"); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(c)

	res, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT = %d; want 200", res.StatusCode)
	}

	if line, err := br.ReadString('\n'); err != nil || line != "echo: hello\n" {
		t.Errorf("through the tunnel: %q, %v; want the bytes sent with the CONNECT echoed", line, err)
	}
}

// gatewayStub records the requests goald's gateway would receive.
type gatewayStub struct {
	mu   sync.Mutex
	seen []string
}

func (g *gatewayStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.seen = append(g.seen, r.Method+" "+r.Host+r.URL.RequestURI()+" auth="+r.Header.Get("Authorization"))
	g.mu.Unlock()

	_, _ = io.WriteString(w, "gateway")
}

func (g *gatewayStub) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.seen)
}

// A box reaches goald at exactly the gateway's paths, with its task token and the query it sent;
// nothing else of goald.
func TestProxy_ServesTheGatewayAtItsPathsOnly(t *testing.T) {
	t.Parallel()

	stub := &gatewayStub{}
	upstream := httptest.NewServer(stub)
	t.Cleanup(upstream.Close)

	gw, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	w := &world{routes: map[string]string{upstream.Listener.Addr().String(): upstream.Listener.Addr().String()}}
	proxy := startProxy(t, Config{Gateway: gw}, w)

	call := func(method, path string) int {
		req, err := http.NewRequestWithContext(t.Context(), method, proxy.URL+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}

		req.Header.Set("Authorization", "Bearer task-token")

		res, err := proxy.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/mcp"}, {http.MethodGet, "/run?part=bundle"}, {http.MethodPost, "/run/tool-approval"},
	} {
		if status := call(req.method, req.path); status != http.StatusOK {
			t.Errorf("%s %s = %d, want 200", req.method, req.path, status)
		}
	}

	for _, path := range []string{"/v1/me", "/api/auth/email-otp/send-verification-otp", "/mcp/", "/run/x", "/run/tool-approval/", "/run/tool-approval/x", "/run/%74ool-approval", "/%6Dcp", "/%72un", "/"} {
		if status := call(http.MethodPost, path); status != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, status)
		}
	}

	host := upstream.Listener.Addr().String()

	got := stub.requests()
	slices.Sort(got)

	if want := []string{
		"GET " + host + "/run?part=bundle auth=Bearer task-token", "POST " + host + "/mcp auth=Bearer task-token",
		"POST " + host + "/run/tool-approval auth=Bearer task-token",
	}; !slices.Equal(got, want) {
		t.Errorf("gateway saw %v; want %v", got, want)
	}
}

// Plain http goes to public hosts only, without the client's proxy headers.
func TestProxy_ForwardsPlainHTTPToPublicHostsOnly(t *testing.T) {
	t.Parallel()

	var got []string

	var mu sync.Mutex

	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()

		got = append(got, r.Host+r.URL.RequestURI()+" proxy-auth="+r.Header.Get("Proxy-Authorization"))
		mu.Unlock()
	}))
	t.Cleanup(upstream.Close)

	w := &world{
		names:  map[string][]string{"deb.debian.org": {"151.101.2.132"}, "internal.example.com": {"192.168.1.20"}},
		routes: map[string]string{"151.101.2.132:80": upstream.Listener.Addr().String(), "192.168.1.20:80": upstream.Listener.Addr().String()},
	}
	proxy := startProxy(t, Config{}, w)

	get := func(target string) int {
		proxyURL, _ := url.Parse(proxy.URL)
		client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}

		req.Header.Set("Proxy-Authorization", "Basic c2VjcmV0")

		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	if status := get("http://deb.debian.org/debian/dists/bookworm/Release"); status != http.StatusOK {
		t.Errorf("GET of a public host = %d, want 200", status)
	}

	for name, target := range map[string]string{
		"private name": "http://internal.example.com/", "metadata": "http://169.254.169.254/latest/meta-data/", "loopback": "http://127.0.0.1:8080/v1/me",
	} {
		if status := get(target); status != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403", name, status)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if !slices.Equal(got, []string{"deb.debian.org/debian/dists/bookworm/Release proxy-auth="}) {
		t.Errorf("upstream saw %v; want the one public request, without the proxy's headers", got)
	}
}

// lockedBuffer is a log's output, written by the proxy's goroutines and read by the test.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

// byMessage is the log's destinations by message.
func (b *lockedBuffer) byMessage(t *testing.T) map[string][]string {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	got := map[string][]string{}

	for line := range strings.Lines(b.buf.String()) {
		var rec struct{ Msg, Dest, Client string }
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}

		if rec.Client != "127.0.0.1" {
			t.Errorf("log line %q names client %q; want the box's address", line, rec.Client)
		}

		got[rec.Msg] = append(got[rec.Msg], rec.Dest)
	}

	return got
}

// The log is the proxy's record of where traffic went: a plain-http request refused at the dial
// (its name or address is not public) is one refusal, never also a forward.
func TestProxy_LogsAForwardOnlyWhenItWasForwarded(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(upstream.Close)

	w := &world{
		names:  map[string][]string{"deb.debian.org": {"151.101.2.132"}, "internal.example.com": {"192.168.1.20"}},
		routes: map[string]string{"151.101.2.132:80": upstream.Listener.Addr().String()},
	}

	var out lockedBuffer

	proxy := startProxy(t, Config{Log: slog.New(slog.NewJSONHandler(&out, nil))}, w)
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	for target, want := range map[string]int{
		"http://deb.debian.org/debian/dists/bookworm/Release": http.StatusOK,
		"http://169.254.169.254/latest/meta-data/":            http.StatusForbidden,
		"http://internal.example.com/":                        http.StatusForbidden,
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		if res.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", target, res.StatusCode, want)
		}
	}

	got := out.byMessage(t)
	if !slices.Equal(got["egress forward"], []string{"deb.debian.org:80"}) {
		t.Errorf("forwards logged: %v; want only deb.debian.org:80", got["egress forward"])
	}

	refused := got["egress refused"]
	slices.Sort(refused)

	if !slices.Equal(refused, []string{"169.254.169.254:80", "internal.example.com:80"}) {
		t.Errorf("refusals logged: %v; want the metadata address and the private name, once each", refused)
	}
}

func TestNew_RefusesAGatewayThatIsNotAnOrigin(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"a path":     "http://host.docker.internal:8080/mcp",
		"a query":    "http://goald:8080?x=1",
		"user info":  "http://user@goald:8080",
		"not http":   "unix://goald",
		"no host":    "http:///",
		"plain name": "goald:8080",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}

		if _, err := New(Config{Gateway: u}); err == nil {
			t.Errorf("%s: New accepted gateway %s", name, raw)
		}
	}

	for _, raw := range []string{"http://goald:8080", "http://host.docker.internal:8080/", "https://gateway.example.com"} {
		u, _ := url.Parse(raw)
		if _, err := New(Config{Gateway: u}); err != nil {
			t.Errorf("New refused gateway %s: %v", raw, err)
		}
	}
}
