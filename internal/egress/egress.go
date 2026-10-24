// Package egress is the proxy that agent, sidecar and check containers reach everything outside
// their network through (ADR 0009). They run on Docker networks with no route out; the proxy is
// the one container on both those networks and one that has a route, and it decides by the
// client's source address what the client may reach:
//
//   - agents (Config.Agents): the hosts in Config.AgentHosts (the model API) over CONNECT, and
//     goald's MCP gateway, served as a reverse proxy at exactly the gateway's path, so the agent
//     reaches /mcp and nothing else of goald;
//   - tools (Config.Tools: sidecars with network access and command checks): any public host, over
//     CONNECT or as plain-http forward requests;
//   - anyone else: nothing.
//
// Every destination but the gateway and Config.Trusted is resolved by the proxy itself and dialed
// only at the addresses it vetted, all of which must be public (Public): a name that resolves, or
// rebinds, to a loopback, private, link-local (the cloud metadata service) or other non-public
// address is refused, whoever asks.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotPublic is the dial error for a destination that resolves to a non-public address.
var ErrNotPublic = errors.New("destination is not a public address")

// DialTimeout bounds resolving and connecting to one destination.
const DialTimeout = 10 * time.Second

// Config is the proxy's policy. Agents and Tools must not overlap.
type Config struct {
	// Gateway is goald's MCP gateway as the proxy reaches it; agents get it at Gateway.Path.
	// Nil serves no gateway.
	Gateway *url.URL
	// Agents and Tools are the client networks of the two policies.
	Agents []netip.Prefix
	Tools  []netip.Prefix
	// AgentHosts are the destinations agents may reach, as host:port.
	AgentHosts []string
	// Trusted are destinations (host:port) dialed by name without the public-address check, for
	// a test's stub or a package mirror on a private network. A trusted destination must still
	// be allowed by the client's policy.
	Trusted []string
	// Log receives one line per refusal and per tunnel or forward, never a path or a header.
	Log *slog.Logger
	// Lookup and Dial replace DNS resolution and connecting (tests); nil means the system's.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	Dial   func(ctx context.Context, network, address string) (net.Conn, error)
}

type role int

const (
	roleNone role = iota
	roleAgent
	roleTool
)

func (r role) String() string {
	switch r {
	case roleAgent:
		return "agent"
	case roleTool:
		return "tool"
	case roleNone:
	}

	return "unknown"
}

// Proxy is the egress proxy's HTTP handler. It is safe for concurrent use.
type Proxy struct {
	cfg     Config
	agents  []string // AgentHosts, normalized
	trusted []string // Trusted, normalized
	gateway *httputil.ReverseProxy
	forward *httputil.ReverseProxy
	lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
	dialFn  func(ctx context.Context, network, address string) (net.Conn, error)
}

// New returns the proxy for cfg, refusing a policy that does not hold together.
func New(cfg Config) (*Proxy, error) {
	p := &Proxy{cfg: cfg, lookup: cfg.Lookup, dialFn: cfg.Dial}
	if p.cfg.Log == nil {
		p.cfg.Log = slog.New(slog.DiscardHandler)
	}

	if p.lookup == nil {
		p.lookup = systemLookup
	}

	if p.dialFn == nil {
		p.dialFn = (&net.Dialer{Timeout: DialTimeout}).DialContext
	}

	agents, errAgents := destinations(cfg.AgentHosts)
	trusted, errTrusted := destinations(cfg.Trusted)
	p.agents, p.trusted = agents, trusted

	if err := errors.Join(overlaps(cfg.Agents, cfg.Tools), errAgents, errTrusted, gatewayError(cfg.Gateway)); err != nil {
		return nil, err
	}

	if cfg.Gateway != nil {
		p.gateway = p.gatewayProxy(*cfg.Gateway)
	}

	p.forward = p.forwardProxy()

	return p, nil
}

func overlaps(agents, tools []netip.Prefix) error {
	var errs []error

	for _, a := range agents {
		for _, t := range tools {
			if a.Overlaps(t) {
				errs = append(errs, fmt.Errorf("agent network %s overlaps tool network %s", a, t))
			}
		}
	}

	return errors.Join(errs...)
}

// destinations normalizes host:port entries.
func destinations(in []string) ([]string, error) {
	var (
		out  []string
		errs []error
	)

	for _, hp := range in {
		host, port, err := destination(hp)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		out = append(out, net.JoinHostPort(host, port))
	}

	return out, errors.Join(errs...)
}

func gatewayError(g *url.URL) error {
	if g != nil && (g.Scheme != "http" && g.Scheme != "https" || g.Host == "" || !strings.HasPrefix(g.Path, "/")) {
		return fmt.Errorf("gateway %q is not an http(s) URL with a path", g.Redacted())
	}

	return nil
}

func systemLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// ServeHTTP implements http.Handler: CONNECT tunnels, absolute-form forward requests, and the
// gateway at its path for agents. Anything else is 404.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	who := p.roleOf(r.RemoteAddr)

	switch {
	case who == roleNone:
		p.refuse(w, r, who, r.Host, "unknown client network", http.StatusForbidden)
	case r.Method == http.MethodConnect:
		p.tunnel(w, r, who)
	case r.URL.IsAbs():
		p.forwardHTTP(w, r, who)
	case who == roleAgent && p.gateway != nil && r.URL.RawPath == "" && r.URL.Path == p.cfg.Gateway.Path:
		p.gateway.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

// roleOf is the policy of the client at addr (ip:port).
func (p *Proxy) roleOf(addr string) role {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return roleNone
	}

	ip := ap.Addr().Unmap()

	switch {
	case slices.ContainsFunc(p.cfg.Agents, func(n netip.Prefix) bool { return n.Contains(ip) }):
		return roleAgent
	case slices.ContainsFunc(p.cfg.Tools, func(n netip.Prefix) bool { return n.Contains(ip) }):
		return roleTool
	default:
		return roleNone
	}
}

// allowed reports whether who may reach host:port (normalized).
func (p *Proxy) allowed(who role, hostport string) bool {
	switch who {
	case roleAgent:
		return slices.Contains(p.agents, hostport)
	case roleTool:
		return true
	case roleNone:
	}

	return false
}

// tunnel serves CONNECT: the destination must be allowed and public, then bytes flow both ways
// until either side closes.
func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request, who role) {
	host, port, err := destination(r.Host)
	if err != nil {
		p.refuse(w, r, who, r.Host, "bad destination", http.StatusBadRequest)
		return
	}

	dest := net.JoinHostPort(host, port)
	if !p.allowed(who, dest) {
		p.refuse(w, r, who, dest, "not allowed", http.StatusForbidden)
		return
	}

	// The server cancels a CONNECT's context once the client sends past its headers (a TLS
	// hello, say), so the dial is bounded by DialTimeout alone.
	up, err := p.dial(context.WithoutCancel(r.Context()), dest)
	if err != nil {
		p.dialFailed(w, r, who, dest, err)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = up.Close()

		http.Error(w, "tunnels are not supported here", http.StatusInternalServerError)

		return
	}

	conn, brw, err := hj.Hijack()
	if err != nil {
		_ = up.Close()
		return
	}

	p.cfg.Log.Info("egress tunnel", slog.String("role", who.String()), slog.String("dest", dest))

	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = conn.Close()
		_ = up.Close()

		return
	}

	// Bytes the client sent after its CONNECT, such as a TLS hello, are already buffered.
	if n := brw.Reader.Buffered(); n > 0 {
		head, _ := brw.Peek(n)
		if _, err := up.Write(head); err != nil {
			_ = conn.Close()
			_ = up.Close()

			return
		}
	}

	splice(conn, up)
}

// forwardHTTP serves a plain-http request in absolute form; https goes through CONNECT.
func (p *Proxy) forwardHTTP(w http.ResponseWriter, r *http.Request, who role) {
	if r.URL.Scheme != "http" {
		p.refuse(w, r, who, r.URL.Host, "only http is forwarded; use CONNECT", http.StatusBadRequest)
		return
	}

	port := r.URL.Port()
	if port == "" {
		port = "80"
	}

	host, port, err := destination(net.JoinHostPort(r.URL.Hostname(), port))
	if err != nil {
		p.refuse(w, r, who, r.URL.Host, "bad destination", http.StatusBadRequest)
		return
	}

	dest := net.JoinHostPort(host, port)
	if !p.allowed(who, dest) {
		p.refuse(w, r, who, dest, "not allowed", http.StatusForbidden)
		return
	}

	p.cfg.Log.Info("egress forward", slog.String("role", who.String()), slog.String("dest", dest))
	p.forward.ServeHTTP(w, r)
}

// dial connects to dest (host:port, normalized): a trusted destination by name; any other at the
// addresses its name resolves to, all of which must be public.
func (p *Proxy) dial(ctx context.Context, dest string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()

	if slices.Contains(p.trusted, dest) {
		return p.dialFn(ctx, "tcp", dest)
	}

	host, port, err := net.SplitHostPort(dest)
	if err != nil {
		return nil, err
	}

	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else if addrs, err = p.lookup(ctx, host); err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}

	// One non-public answer refuses the name: it is how a rebinding attack looks.
	for _, a := range addrs {
		if !Public(a) {
			return nil, fmt.Errorf("%w: %s is %s", ErrNotPublic, host, a.Unmap())
		}
	}

	var errs []error

	for _, a := range addrs {
		conn, err := p.dialFn(ctx, "tcp", net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}

		errs = append(errs, err)
	}

	return nil, errors.Join(errs...)
}

func (p *Proxy) dialFailed(w http.ResponseWriter, r *http.Request, who role, dest string, err error) {
	if errors.Is(err, ErrNotPublic) {
		p.refuse(w, r, who, dest, "not a public address", http.StatusForbidden)
		return
	}

	p.cfg.Log.Info("egress unreachable", slog.String("role", who.String()), slog.String("dest", dest), slog.Any("error", err))
	w.Header().Set("Connection", "close")
	http.Error(w, "destination unreachable", http.StatusBadGateway)
}

// refuse answers status and closes the connection, so nothing the client sent after a refused
// CONNECT is read as a request of its own.
func (p *Proxy) refuse(w http.ResponseWriter, r *http.Request, who role, dest, reason string, status int) {
	p.cfg.Log.Info("egress refused", slog.String("role", who.String()), slog.String("client", clientIP(r.RemoteAddr)),
		slog.String("dest", dest), slog.String("reason", reason))
	w.Header().Set("Connection", "close")
	http.Error(w, "refused by goal's egress proxy: "+reason, status)
}

func clientIP(addr string) string {
	if ap, err := netip.ParseAddrPort(addr); err == nil {
		return ap.Addr().Unmap().String()
	}

	return ""
}

// gatewayProxy forwards a request at the gateway's path to the gateway, as it came but for the
// host, streaming responses (server-sent events) as they arrive.
func (p *Proxy) gatewayProxy(gw url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.URL.Path, pr.Out.URL.RawPath = gw.Scheme, gw.Host, gw.Path, ""
			pr.Out.Host = gw.Host
		},
		Transport: &http.Transport{
			Proxy: nil, DialContext: p.dialFn, ForceAttemptHTTP2: true,
			MaxIdleConns: 32, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			p.cfg.Log.Warn("egress gateway unreachable", slog.Any("error", err))
			http.Error(w, "gateway unreachable", http.StatusBadGateway)
		},
	}
}

// forwardProxy sends a plain-http request on to its own URL. ReverseProxy drops hop-by-hop
// headers (Proxy-Authorization and Proxy-Connection among them) and follows no redirects.
func (p *Proxy) forwardProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) { pr.Out.Host = pr.In.URL.Host },
		Transport: &http.Transport{
			Proxy: nil, DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				host, port, err := destination(addr)
				if err != nil {
					return nil, err
				}

				return p.dial(ctx, net.JoinHostPort(host, port))
			},
			MaxIdleConns: 32, IdleConnTimeout: 90 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			who := p.roleOf(r.RemoteAddr)
			p.dialFailed(w, r, who, r.URL.Host, err)
		},
	}
}

// destination parses host:port into a lower-case host without a trailing dot and a port in
// 1–65535.
func destination(hostport string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(hostport)
	if err != nil {
		return "", "", fmt.Errorf("destination %q: %w", hostport, err)
	}

	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || host == "" {
		return "", "", fmt.Errorf("destination %q is not host:port", hostport)
	}

	return host, port, nil
}

// splice copies both ways until each side is done, then closes both.
func splice(a, b net.Conn) {
	var wg sync.WaitGroup

	half := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)

		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}

	wg.Go(func() { half(a, b) })
	wg.Go(func() { half(b, a) })
	wg.Wait()

	_ = a.Close()
	_ = b.Close()
}
