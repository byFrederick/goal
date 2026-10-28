// Package egress is the proxy every sandbox reaches the outside through (ADR 0009 and its phase 4
// amendment). Sandboxes run on a Docker network with no route out; the proxy is the one
// container on that network and on one that has a route, and every client gets one policy:
//
//   - any destination whose resolved addresses are all public, over CONNECT or as plain-http
//     forward requests;
//   - goald's gateway, served as a reverse proxy at exactly its paths (GatewayPaths: the MCP
//     gateway, the run's bundle and its tool approvals), so a box reaches those and nothing else of goald.
//
// Every destination but the gateway is resolved by the proxy itself and dialed only at the
// addresses it vetted, all of which must be public (Public): a name that resolves, or rebinds, to
// a loopback, private, link-local (the cloud metadata service) or other non-public address is
// refused.
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

// gatewayPaths are goald's paths a box reaches through the proxy, exactly: the MCP gateway, the
// run's bundle and its tool approvals. Any other path, an encoded one included, is 404.
var gatewayPaths = []string{"/mcp", "/run", "/run/tool-approval"}

// GatewayPaths are the paths the proxy forwards to goald's gateway (goald's api.GatewayPath,
// api.RunPath and api.ToolApprovalPath, which cmd/goald's test holds it to).
func GatewayPaths() []string { return slices.Clone(gatewayPaths) }

// Config is the proxy's setup.
type Config struct {
	// Gateway is goald's gateway listener as the proxy reaches it, an http(s) origin; requests at
	// GatewayPaths go there as they came. Nil serves no gateway.
	Gateway *url.URL
	// Log receives one line per refusal and per tunnel or forward, never a path or a header.
	Log *slog.Logger
	// Lookup and Dial replace DNS resolution and connecting (tests); nil means the system's.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	Dial   func(ctx context.Context, network, address string) (net.Conn, error)
}

// Proxy is the egress proxy's HTTP handler. It is safe for concurrent use.
type Proxy struct {
	cfg     Config
	gateway *httputil.ReverseProxy
	forward *httputil.ReverseProxy
	lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
	dialFn  func(ctx context.Context, network, address string) (net.Conn, error)
}

// New returns the proxy for cfg, refusing a gateway that is not an http(s) origin.
func New(cfg Config) (*Proxy, error) {
	if err := gatewayError(cfg.Gateway); err != nil {
		return nil, err
	}

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

	if cfg.Gateway != nil {
		p.gateway = p.gatewayProxy(*cfg.Gateway)
	}

	p.forward = p.forwardProxy()

	return p, nil
}

func gatewayError(g *url.URL) error {
	if g != nil && (g.Scheme != "http" && g.Scheme != "https" || g.Host == "" || strings.Trim(g.Path, "/") != "" ||
		g.RawQuery != "" || g.User != nil) {
		return fmt.Errorf("gateway %q is not an http(s) origin, such as http://goald:8080", g.Redacted())
	}

	return nil
}

func systemLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// ServeHTTP implements http.Handler: CONNECT tunnels, absolute-form forward requests, and the
// gateway at its paths. Anything else is 404.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodConnect:
		p.tunnel(w, r)
	case r.URL.IsAbs():
		p.forwardHTTP(w, r)
	case p.gateway != nil && r.URL.RawPath == "" && slices.Contains(gatewayPaths, r.URL.Path):
		p.gateway.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

// tunnel serves CONNECT: the destination must be public, then bytes flow both ways until either
// side closes.
func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	host, port, err := destination(r.Host)
	if err != nil {
		p.refuse(w, r, r.Host, "bad destination", http.StatusBadRequest)
		return
	}

	dest := net.JoinHostPort(host, port)

	// The server cancels a CONNECT's context once the client sends past its headers (a TLS
	// hello, say), so the dial is bounded by DialTimeout alone.
	up, err := p.dial(context.WithoutCancel(r.Context()), dest)
	if err != nil {
		p.dialFailed(w, r, dest, err)
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

	p.cfg.Log.Info("egress tunnel", slog.String("client", clientIP(r.RemoteAddr)), slog.String("dest", dest))

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
func (p *Proxy) forwardHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme != "http" {
		p.refuse(w, r, r.URL.Host, "only http is forwarded; use CONNECT", http.StatusBadRequest)
		return
	}

	port := r.URL.Port()
	if port == "" {
		port = "80"
	}

	host, port, err := destination(net.JoinHostPort(r.URL.Hostname(), port))
	if err != nil {
		p.refuse(w, r, r.URL.Host, "bad destination", http.StatusBadRequest)
		return
	}

	dest := net.JoinHostPort(host, port)

	// Logged once the destination answered (forwardProxy), since the dial may still refuse it.
	p.forward.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), forwardKey{}, forwarded{client: clientIP(r.RemoteAddr), dest: dest})))
}

// forwardKey is the context key of a plain-http request's forwarded: its client and its
// normalized destination, for forwardProxy's log lines.
type forwardKey struct{}

type forwarded struct {
	client string
	dest   string
}

// dial connects to dest (host:port, normalized) at the addresses its name resolves to, all of
// which must be public (PublicDialer).
func (p *Proxy) dial(ctx context.Context, dest string) (net.Conn, error) {
	return PublicDialer{Lookup: p.lookup, Dial: p.dialFn}.DialContext(ctx, "tcp", dest)
}

func (p *Proxy) dialFailed(w http.ResponseWriter, r *http.Request, dest string, err error) {
	if errors.Is(err, ErrNotPublic) {
		p.refuse(w, r, dest, "not a public address", http.StatusForbidden)
		return
	}

	p.cfg.Log.Info("egress unreachable", slog.String("client", clientIP(r.RemoteAddr)), slog.String("dest", dest), slog.Any("error", err))
	w.Header().Set("Connection", "close")
	http.Error(w, "destination unreachable", http.StatusBadGateway)
}

// refuse answers status and closes the connection, so nothing the client sent after a refused
// CONNECT is read as a request of its own.
func (p *Proxy) refuse(w http.ResponseWriter, r *http.Request, dest, reason string, status int) {
	p.cfg.Log.Info("egress refused", slog.String("client", clientIP(r.RemoteAddr)), slog.String("dest", dest), slog.String("reason", reason))
	w.Header().Set("Connection", "close")
	http.Error(w, "refused by goal's egress proxy: "+reason, status)
}

func clientIP(addr string) string {
	if ap, err := netip.ParseAddrPort(addr); err == nil {
		return ap.Addr().Unmap().String()
	}

	return ""
}

// gatewayProxy forwards a request at one of the gateway's paths to the gateway, as it came but
// for the host, streaming responses (server-sent events) as they arrive.
func (p *Proxy) gatewayProxy(gw url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.URL.RawPath = gw.Scheme, gw.Host, ""
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
		ModifyResponse: func(res *http.Response) error {
			f, _ := res.Request.Context().Value(forwardKey{}).(forwarded)
			p.cfg.Log.Info("egress forward", slog.String("client", f.client), slog.String("dest", f.dest))

			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			f, _ := r.Context().Value(forwardKey{}).(forwarded)
			p.dialFailed(w, r, f.dest, err)
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
