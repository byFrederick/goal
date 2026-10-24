package egress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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

// loopback is where the tests' clients come from: httptest serves on 127.0.0.1.
var loopback = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}

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

func TestProxy_AgentsReachOnlyTheirHosts(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{
		names:  map[string][]string{modelHost: {"160.79.104.10"}, "example.com": {"93.184.215.14"}},
		routes: map[string]string{modelAddr: echo, "93.184.215.14:443": echo},
	}
	proxy := startProxy(t, Config{Agents: loopback, AgentHosts: []string{"API.Anthropic.com.:443"}}, w)

	if status, reply := connect(t, proxy, modelDest); status != http.StatusOK || reply != "echo: ping" {
		t.Errorf("allowed host: status %d, reply %q; want a working tunnel", status, reply)
	}

	for _, dest := range []string{"example.com:443", "api.anthropic.com:80", "160.79.104.10:443"} {
		if status, _ := connect(t, proxy, dest); status != http.StatusForbidden {
			t.Errorf("CONNECT %s = %d, want 403", dest, status)
		}
	}

	if got := w.dials(); !slices.Equal(got, []string{modelAddr}) {
		t.Errorf("dialed %v; want only the allowed host, at the address it resolved to", got)
	}
}

// A name that resolves (or rebinds) to a non-public address is never dialed, even when the
// policy allows it, and no IP literal of the host, its bridge or the metadata service is either.
func TestProxy_RefusesNonPublicDestinations(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{
		names: map[string][]string{
			modelHost:              {"10.0.0.7"},
			"mixed.example.com":    {"93.184.215.14", "127.0.0.1"},
			"host.docker.internal": {"0.250.250.254"},
			"metadata.example.com": {"169.254.169.254"},
			"v6.example.com":       {"::1"},
			"mapped.example.com":   {"::ffff:192.168.1.10"},
			"localhost":            {"127.0.0.1", "::1"},
		},
		routes: map[string]string{"10.0.0.7:443": echo, "127.0.0.1:443": echo, "93.184.215.14:443": echo},
	}
	agents := startProxy(t, Config{Agents: loopback, AgentHosts: []string{modelDest}}, w)
	tools := startProxy(t, Config{Tools: loopback}, w)

	if status, _ := connect(t, agents, modelDest); status != http.StatusForbidden {
		t.Errorf("allowed host rebound to 10.0.0.7: status %d, want 403", status)
	}

	for _, dest := range []string{
		"mixed.example.com:443", "host.docker.internal:8080", "metadata.example.com:80", "v6.example.com:443", "mapped.example.com:443",
		"127.0.0.1:5432", "[::1]:8080", "169.254.169.254:80", "172.17.0.1:8090", "192.168.65.254:8025", "localhost:8081",
	} {
		if status, _ := connect(t, tools, dest); status != http.StatusForbidden {
			t.Errorf("tool CONNECT %s = %d, want 403", dest, status)
		}
	}

	if got := w.dials(); len(got) != 0 {
		t.Errorf("dialed %v; want nothing", got)
	}
}

func TestProxy_ToolsReachPublicHostsAndTrustedOnes(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{
		names:  map[string][]string{"registry.npmjs.org": {"104.16.1.34"}, "mirror.acme.internal": {"10.20.30.40"}},
		routes: map[string]string{"104.16.1.34:443": echo, "mirror.acme.internal:8443": echo},
	}
	proxy := startProxy(t, Config{Tools: loopback, Trusted: []string{"mirror.acme.internal:8443"}}, w)

	for _, dest := range []string{"registry.npmjs.org:443", "mirror.acme.internal:8443"} {
		if status, reply := connect(t, proxy, dest); status != http.StatusOK || reply != "echo: ping" {
			t.Errorf("CONNECT %s: status %d, reply %q; want a working tunnel", dest, status, reply)
		}
	}

	// Trusted is one host:port, not the host.
	if status, _ := connect(t, proxy, "mirror.acme.internal:22"); status != http.StatusForbidden {
		t.Errorf("CONNECT to another port of a trusted host = %d, want 403", status)
	}
}

// A client may send its first bytes (a TLS hello) right behind the CONNECT, before the answer.
func TestProxy_TunnelsBytesSentWithTheConnect(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{names: map[string][]string{modelHost: {"160.79.104.10"}}, routes: map[string]string{modelAddr: echo}}
	proxy := startProxy(t, Config{Agents: loopback, AgentHosts: []string{modelDest}}, w)

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

func TestProxy_RefusesClientsOfNoNetwork(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{names: map[string][]string{"example.com": {"93.184.215.14"}}, routes: map[string]string{"93.184.215.14:443": echo}}
	proxy := startProxy(t, Config{Agents: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}, w)

	if status, _ := connect(t, proxy, "example.com:443"); status != http.StatusForbidden {
		t.Errorf("client outside every network: status %d, want 403", status)
	}
}

// gatewayStub records the requests goald's gateway would receive.
type gatewayStub struct {
	mu   sync.Mutex
	seen []string
}

func (g *gatewayStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.seen = append(g.seen, r.Method+" "+r.URL.RequestURI()+" auth="+r.Header.Get("Authorization"))
	g.mu.Unlock()

	_, _ = io.WriteString(w, "gateway")
}

func (g *gatewayStub) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.seen)
}

// Agents reach goald at exactly the gateway's path, with their task token; nothing else of
// goald, and tools not even that.
func TestProxy_ServesTheGatewayToAgentsOnly(t *testing.T) {
	t.Parallel()

	stub := &gatewayStub{}
	upstream := httptest.NewServer(stub)
	t.Cleanup(upstream.Close)

	gw, err := url.Parse(upstream.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}

	w := &world{routes: map[string]string{upstream.Listener.Addr().String(): upstream.Listener.Addr().String()}}
	agents := startProxy(t, Config{Agents: loopback, Gateway: gw}, w)
	tools := startProxy(t, Config{Tools: loopback, Gateway: gw}, w)

	call := func(proxy *httptest.Server, method, path string) int {
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

	if status := call(agents, http.MethodPost, "/mcp"); status != http.StatusOK {
		t.Errorf("agent POST /mcp = %d, want 200", status)
	}

	for _, path := range []string{"/v1/me", "/api/auth/email-otp/send-verification-otp", "/mcp/", "/%6Dcp", "/"} {
		if status := call(agents, http.MethodPost, path); status != http.StatusNotFound {
			t.Errorf("agent POST %s = %d, want 404", path, status)
		}
	}

	if status := call(tools, http.MethodPost, "/mcp"); status != http.StatusNotFound {
		t.Errorf("tool POST /mcp = %d, want 404", status)
	}

	if got := stub.requests(); !slices.Equal(got, []string{"POST /mcp auth=Bearer task-token"}) {
		t.Errorf("gateway saw %v; want the agent's one call with its token", got)
	}
}

func TestProxy_ForwardsPlainHTTPForToolsOnly(t *testing.T) {
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
	tools := startProxy(t, Config{Tools: loopback}, w)
	agents := startProxy(t, Config{Agents: loopback, AgentHosts: []string{modelDest}}, w)

	get := func(proxy *httptest.Server, target string) int {
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

	if status := get(tools, "http://deb.debian.org/debian/dists/bookworm/Release"); status != http.StatusOK {
		t.Errorf("tool GET = %d, want 200", status)
	}

	for name, target := range map[string]string{
		"private name": "http://internal.example.com/", "metadata": "http://169.254.169.254/latest/meta-data/", "loopback": "http://127.0.0.1:8080/v1/me",
	} {
		if status := get(tools, target); status != http.StatusForbidden {
			t.Errorf("tool GET %s = %d, want 403", name, status)
		}
	}

	if status := get(agents, "http://deb.debian.org/"); status != http.StatusForbidden {
		t.Errorf("agent GET of a host it may not reach = %d, want 403", status)
	}

	mu.Lock()
	defer mu.Unlock()

	if !slices.Equal(got, []string{"deb.debian.org/debian/dists/bookworm/Release proxy-auth="}) {
		t.Errorf("upstream saw %v; want the one allowed request, without the proxy's headers", got)
	}
}

func TestNew_RefusesAPolicyThatDoesNotHoldTogether(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]Config{
		"overlapping networks": {Agents: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Tools: []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")}},
		"host without port":    {AgentHosts: []string{"api.anthropic.com"}},
		"bad port":             {Trusted: []string{"mirror.example.com:99999"}},
		"gateway without path": {Gateway: &url.URL{Scheme: "http", Host: "host.docker.internal:8080"}},
		"gateway not http":     {Gateway: &url.URL{Scheme: "unix", Host: "x", Path: "/mcp"}},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: New accepted %+v", name, cfg)
		}
	}
}
