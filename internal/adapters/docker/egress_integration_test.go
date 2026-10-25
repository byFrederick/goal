//go:build integration

package docker

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// egressTestImage is cmd/goal-egress built for the tests (testdata/egress).
const egressTestImage = "goal-egress:test"

var (
	egressImageOnce sync.Once
	errEgressImage  error

	// sharedEgress serves the tests that only need their containers' networks to exist; TestMain
	// removes it. Every test that checks reachability has an egress of its own.
	sharedEgressOnce sync.Once
	sharedEgress     *Egress
	errSharedEgress  error
)

func TestMain(m *testing.M) {
	code := m.Run()

	if sharedEgress != nil {
		if err := sharedEgress.Remove(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "remove the shared test egress:", err)
		}
	}

	os.Exit(code)
}

// uniqueEgressName names a test's egress proxy and networks apart from goald's and other runs'.
func uniqueEgressName() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)

	return "goal-egress-it-" + hex.EncodeToString(b)
}

// buildEgressImage cross-compiles cmd/goal-egress for the Docker host into an image from scratch.
func buildEgressImage(docker *client.Client) error {
	ctx := context.Background()

	v, err := docker.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "goal-egress-it-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	bin := filepath.Join(dir, "goal-egress")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", bin, "github.com/byFrederick/goal/cmd/goal-egress") //nolint:gosec // the test's own temp dir

	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+v.Arch)

	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}

	files := map[string][]byte{}

	for name, path := range map[string]string{"Dockerfile": filepath.Join("testdata", "egress", "Dockerfile"), "goal-egress": bin} {
		body, err := os.ReadFile(path) //nolint:gosec // fixed test files
		if err != nil {
			return err
		}

		files[name] = body
	}

	return buildImage(docker, egressTestImage, files)
}

// testEgress is the shared egress for the stub tasks' gateway (stubSpec).
func testEgress(t *testing.T, docker *client.Client) *Egress {
	t.Helper()

	sharedEgressOnce.Do(func() {
		if errSharedEgress = egressImage(docker); errSharedEgress != nil {
			return
		}

		sharedEgress, errSharedEgress = NewEgress(docker, EgressConfig{
			Name: uniqueEgressName(), Image: egressTestImage, Gateway: stubGateway,
		}, slog.New(slog.DiscardHandler))
	})

	if errSharedEgress != nil {
		t.Fatalf("shared test egress: %v", errSharedEgress)
	}

	return sharedEgress
}

func egressImage(docker *client.Client) error {
	egressImageOnce.Do(func() { errEgressImage = buildEgressImage(docker) })
	return errEgressImage
}

// newEgress returns an egress of the test's own, removed when the test ends (after the
// containers on its networks, which are cleaned up first).
func newEgress(t *testing.T, docker *client.Client, cfg EgressConfig) *Egress {
	t.Helper()

	if err := egressImage(docker); err != nil {
		t.Fatalf("build egress image: %v", err)
	}

	cfg.Name, cfg.Image = uniqueEgressName(), egressTestImage

	e, err := NewEgress(docker, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := e.Remove(context.Background()); err != nil {
			t.Errorf("remove egress %s: %v", cfg.Name, err)
		}
	})

	return e
}

// hostListener is an HTTP listener on the host where containers would find goald and its
// neighbours: loopback on macOS (Docker Desktop and OrbStack forward host.docker.internal to it),
// every address on Linux (host.docker.internal is the bridge's address there). It counts its
// requests.
type hostListener struct {
	*httptest.Server

	hits atomic.Int64
}

func newHostListener(t *testing.T, h http.Handler) *hostListener {
	t.Helper()

	addr := "0.0.0.0:0"
	if runtime.GOOS == "darwin" {
		addr = "127.0.0.1:0"
	}

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	l := &hostListener{}
	l.Server = &httptest.Server{Listener: ln, Config: &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.hits.Add(1)
		h.ServeHTTP(w, r)
	})}}
	l.Start()
	t.Cleanup(l.Close)

	return l
}

func (l *hostListener) port() string {
	_, port, _ := net.SplitHostPort(l.Listener.Addr().String())
	return port
}

func (l *hostListener) loopbackURL(path string) string { return "http://127.0.0.1:" + l.port() + path }

func text(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
}

// hostAddresses are the addresses at which a container might find the host or a neighbour:
// the hosts' own as Docker Desktop and OrbStack give them, and the default bridge's gateway.
func hostAddresses(t *testing.T, docker *client.Client) []string {
	t.Helper()

	addrs := []string{"0.250.250.254", "198.19.248.254", "192.168.65.254", "172.17.0.1"}

	bridge, err := docker.NetworkInspect(t.Context(), egressExit, client.NetworkInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range bridge.Network.IPAM.Config {
		if gw := c.Gateway.String(); c.Gateway.IsValid() && !slices.Contains(addrs, gw) {
			addrs = append(addrs, gw)
		}
	}

	return addrs
}

// probes are the "probe <name>=<result>" messages of a netprobe task.
func probes(events []ports.RunEvent) map[string]string {
	out := map[string]string{}

	for _, ev := range events {
		if name, result, ok := strings.Cut(strings.TrimPrefix(fmt.Sprint(ev.Data["text"]), "probe "), "="); ok && ev.Kind == domain.EventAgentMessage {
			out[name] = result
		}
	}

	return out
}

// An agent reaches goald's gateway (and only its path) and the hosts its policy allows, through
// the egress proxy; it cannot reach a listener on the host, the host's or a bridge's address,
// or the metadata service, directly or through the proxy.
func TestEgress_AgentsReachOnlyTheGatewayAndTheirHosts(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)

	var gatewayAuth atomic.Value

	gateway := newHostListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayAuth.Store(r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization"))

		_, _ = io.WriteString(w, "gateway")
	}))
	external := newHostListener(t, text("external"))
	secret := newHostListener(t, text("secret"))

	externalDest := net.JoinHostPort(hostAlias, external.port())
	egress := newEgress(t, docker, EgressConfig{
		Gateway: gateway.loopbackURL("/mcp"), AgentHosts: []string{externalDest}, Trusted: []string{externalDest},
	})
	r.egress = egress

	spec := stubSpec(t, "netprobe")
	spec.GatewayURL = gateway.loopbackURL("/mcp")
	ips := hostAddresses(t, docker)
	spec.Env["PROBE_SECRET_PORT"], spec.Env["PROBE_EXTERNAL_PORT"], spec.Env["PROBE_HOST_IPS"] = secret.port(), external.port(), strings.Join(ips, " ")

	events, res := run(t.Context(), t, r, spec, nil)
	if res.Status != domain.RunSucceeded {
		t.Fatalf("netprobe = %+v", res)
	}

	got := probes(events)
	want := map[string]string{
		"direct-host-name": "fail", "direct-metadata": "fail",
		"proxy-host": "403", "proxy-metadata": "403",
		"proxy-external": "ok-external", "tunnel-external": "200,200,external", "tunnel-metadata": "403", "tunnel-host": "403",
		"gateway": "ok-gateway", "goald-api": "404",
	}

	for _, ip := range ips {
		want["direct-"+ip], want["proxy-"+ip] = "fail", "403"
	}

	for name, w := range want {
		if got[name] != w {
			t.Errorf("probe %s = %q, want %q", name, got[name], w)
		}
	}

	if t.Failed() {
		lines, _ := proxyLines(t.Context(), docker, egress.cfg.Name)
		t.Logf("proxy log: %v", lines)
	}

	if n := secret.hits.Load(); n != 0 {
		t.Errorf("the host listener the agent must not reach got %d requests", n)
	}

	if auth, _ := gatewayAuth.Load().(string); auth != "POST /mcp Bearer "+gatewayToken {
		t.Errorf("gateway saw %q; want the agent's POST /mcp with its task token", auth)
	}
}

// Sidecars and checks reach public hosts through the proxy (here a trusted stub stands in for
// one), never the host, a private address, the metadata service or goald's gateway.
func TestEgress_ToolsReachNeitherTheHostNorTheGateway(t *testing.T) {
	t.Parallel()

	_, docker := newTestRunner(t)
	external := newHostListener(t, text("external"))
	secret := newHostListener(t, text("secret"))
	gateway := newHostListener(t, text("gateway"))

	externalDest := net.JoinHostPort(hostAlias, external.port())
	egress := newEgress(t, docker, EgressConfig{Gateway: gateway.loopbackURL("/mcp"), Trusted: []string{externalDest}})
	k := NewChecker(docker, egress, Config{StopGrace: time.Second}, slog.New(slog.DiscardHandler))

	proxy := egress.cfg.Name + ":" + egressPort
	command := fmt.Sprintf(`try() { if out=$(wget -q -T 4 -O - "$@" 2>&1); then echo "ok-$out"; else echo "$out" | grep -o 'HTTP/1.1 [0-9]*' | cut -d' ' -f2 | grep . || echo fail; fi; }
echo "direct-host=$(try -Y off http://%[1]s:%[2]s/)"
echo "direct-metadata=$(try -Y off http://169.254.169.254/)"
echo "proxy-host=$(try http://%[1]s:%[2]s/)"
echo "proxy-metadata=$(try http://169.254.169.254/)"
echo "proxy-external=$(try http://%[1]s:%[3]s/)"
echo "gateway=$(try -Y off --post-data '{}' http://%[4]s/mcp)"`, hostAlias, secret.port(), external.port(), proxy)

	spec := checkSpec(t, command, time.Minute)

	res, err := k.Check(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"direct-host=fail", "direct-metadata=fail", "proxy-host=403", "proxy-metadata=403", "proxy-external=ok-external", "gateway=404",
	} {
		if !strings.Contains(res.Output, want+"\n") {
			t.Errorf("check output lacks %q:\n%s", want, res.Output)
		}
	}

	if n := secret.hits.Load() + gateway.hits.Load(); n != 0 {
		t.Errorf("the host listeners a check must not reach got %d requests", n)
	}
}

// A goald whose configuration differs replaces the proxy of the same name, keeping its
// networks; the same configuration keeps the running proxy.
func TestEgress_EnsureReplacesAStaleProxy(t *testing.T) {
	t.Parallel()

	_, docker := newTestRunner(t)
	first := newEgress(t, docker, EgressConfig{AgentHosts: []string{"api.anthropic.com"}})

	if err := first.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}

	id := func() string {
		c, err := docker.ContainerInspect(t.Context(), first.cfg.Name, client.ContainerInspectOptions{})
		if err != nil {
			t.Fatal(err)
		}

		return c.Container.ID
	}

	before := id()

	same, err := NewEgress(docker, first.cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	if err := same.Ensure(t.Context()); err != nil || id() != before {
		t.Errorf("the same configuration replaced the proxy (err %v)", err)
	}

	other := first.cfg
	other.AgentHosts = []string{"api.anthropic.com", "proxy.golang.org"}

	changed, err := NewEgress(docker, other, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	if err := changed.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}

	if id() == before {
		t.Error("a changed configuration kept the old proxy")
	}

	c, err := docker.ContainerInspect(t.Context(), first.cfg.Name, client.ContainerInspectOptions{})
	if err != nil || !slices.Contains(c.Container.Config.Cmd, "proxy.golang.org:443") {
		t.Errorf("the proxy runs with %v (err %v); want the new allowlist", c.Container.Config.Cmd, err)
	}
}

// workerImage is the real agent image (task worker:image).
const workerImage = "ghcr.io/byfrederick/goal-agent:0.1.0"

// The real worker on the agents' network: Claude Code connects to the gateway through the proxy
// and sends its model traffic to the proxy, which refuses it here (the model API is not
// allowlisted, so the test reaches no real service), and nothing else.
func TestRunner_RealWorkerGoesOutOnlyThroughTheProxy(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)
	if _, err := docker.ImageInspect(t.Context(), workerImage); err != nil {
		t.Skipf("%s is not built (task worker:image): %v", workerImage, err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "stub-gateway", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "ping", Description: "Answers pong."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, struct{}{}, nil
	})

	var (
		authorized atomic.Int64
		calls      syncBuffer
	)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, DisableLocalhostProtection: true})
	gateway := newHostListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gatewayToken {
			http.Error(w, "no task token", http.StatusUnauthorized)
			return
		}

		authorized.Add(1)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mcpHandler.ServeHTTP(rec, r)
		_, _ = fmt.Fprintf(&calls, "%s %d; ", r.Method, rec.status)
	}))
	secret := newHostListener(t, text("secret"))

	egress := newEgress(t, docker, EgressConfig{Gateway: gateway.loopbackURL("/mcp"), AgentHosts: []string{"proxy.golang.org"}})
	r.egress = egress

	spec := stubSpec(t, "real")
	spec.Image, spec.GatewayURL, spec.Brief, spec.Timeout = workerImage, gateway.loopbackURL("/mcp"), "Reply with {}.", 3*time.Minute

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var servers string

	_, res := run(ctx, t, r, spec, func(ev ports.RunEvent) {
		servers = fmt.Sprint(ev.Data["mcp_servers"])

		// The model call follows the start; once the proxy has seen it, the task has shown what
		// it reaches.
		if !waitForLog(ctx, t, docker, egress.cfg.Name, "api.anthropic.com:443", time.Minute) {
			t.Error("the proxy never saw the model API: Claude Code does not use HTTPS_PROXY")
		}

		cancel()
	})

	if res.Status != domain.RunCancelled && res.Status != domain.RunFailed {
		t.Logf("result: %+v", res)
	}

	if !strings.Contains(servers, "goal") || !strings.Contains(servers, "connected") {
		t.Errorf("started with MCP servers %s; want goal connected through the proxy (gateway calls: %s)", servers, calls.String())
	}

	if authorized.Load() == 0 {
		t.Error("the gateway got no request with the task's token")
	}

	if n := secret.hits.Load(); n != 0 {
		t.Errorf("a host listener got %d requests from the worker", n)
	}

	dests := proxyDestinations(t, docker, egress.cfg.Name)
	t.Logf("destinations the worker asked the proxy for: %v", dests)

	for _, d := range dests {
		if d != "api.anthropic.com:443" {
			t.Errorf("the worker asked the proxy for %s: nonessential traffic should be off", d)
		}
	}
}

// statusRecorder keeps the status a handler answered with.
type statusRecorder struct {
	http.ResponseWriter

	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// proxyLines are the egress proxy's log lines with a destination.
func proxyLines(ctx context.Context, docker *client.Client, name string) ([]map[string]any, error) {
	logs, err := docker.ContainerLogs(ctx, name, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = logs.Close() }()

	pr, pw := io.Pipe()

	go func() { _, err := stdcopy.StdCopy(pw, pw, logs); pw.CloseWithError(err) }()

	var out []map[string]any

	s := bufio.NewScanner(pr)
	for s.Scan() {
		var line map[string]any
		if json.Unmarshal(s.Bytes(), &line) == nil && line["dest"] != nil {
			out = append(out, line)
		}
	}

	return out, nil
}

func waitForLog(ctx context.Context, t *testing.T, docker *client.Client, name, dest string, within time.Duration) bool {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		lines, err := proxyLines(ctx, docker, name)
		if err == nil && slices.ContainsFunc(lines, func(l map[string]any) bool { return l["dest"] == dest }) {
			return true
		}

		time.Sleep(500 * time.Millisecond)
	}

	return false
}

func proxyDestinations(t *testing.T, docker *client.Client, name string) []string {
	t.Helper()

	lines, err := proxyLines(t.Context(), docker, name)
	if err != nil {
		t.Fatal(err)
	}

	var dests []string

	for _, l := range lines {
		if d := fmt.Sprint(l["dest"]); !slices.Contains(dests, d) {
			dests = append(dests, d)
		}
	}

	return dests
}
