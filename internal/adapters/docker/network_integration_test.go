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

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
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

	// sharedNet serves the tests that only need their containers' network to exist; TestMain
	// removes it. Every test that checks reachability has a network of its own.
	sharedNetOnce sync.Once
	sharedNet     *testNetwork
	errSharedNet  error
)

func TestMain(m *testing.M) {
	code := m.Run()

	if sharedNet != nil {
		if err := sharedNet.remove(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "remove the shared test network:", err)
		}
	}

	os.Exit(code)
}

// testNetwork is compose.yaml's agents network and goal-egress service, made for a test: an
// internal network without a bridge address, and the proxy on it under the service's name and
// on the default bridge, its way out.
type testNetwork struct {
	docker *client.Client
	name   string // the network's
	proxy  string // the proxy container's
}

// newTestNetwork makes the network and starts its proxy, serving gateway (an origin; empty for
// none), and waits until the proxy is healthy.
func newTestNetwork(ctx context.Context, docker *client.Client, gateway string) (*testNetwork, error) {
	if err := egressImage(docker); err != nil {
		return nil, fmt.Errorf("build egress image: %w", err)
	}

	b := make([]byte, 4)
	_, _ = rand.Read(b)

	n := &testNetwork{docker: docker, name: "goal-it-agents-" + hex.EncodeToString(b), proxy: "goal-it-egress-" + hex.EncodeToString(b)}

	if _, err := docker.NetworkCreate(ctx, n.name, client.NetworkCreateOptions{
		Driver: "bridge", Internal: true, Options: map[string]string{noBridgeAddress: "true"},
	}); err != nil {
		return nil, fmt.Errorf("create network: %w", err)
	}

	useInit := true
	cc := &container.Config{
		Image: egressTestImage, User: "65532:65532", Cmd: []string{"-gateway", gateway},
		Healthcheck: &container.HealthConfig{
			Test: []string{"CMD", "/goal-egress", "-health"}, Interval: time.Second, Timeout: time.Second, StartInterval: 200 * time.Millisecond,
			StartPeriod: 10 * time.Second, Retries: 3,
		},
	}
	hc := &container.HostConfig{
		NetworkMode: "bridge", ReadonlyRootfs: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"}, Init: &useInit,
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}
	nc := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
		"bridge": {GwPriority: 1},
		n.name:   {Aliases: []string{proxyHost}},
	}}

	if _, err := docker.ContainerCreate(ctx, client.ContainerCreateOptions{Config: cc, HostConfig: hc, NetworkingConfig: nc, Name: n.proxy}); err != nil {
		_ = n.remove(ctx)
		return nil, fmt.Errorf("create proxy: %w", err)
	}

	if _, err := docker.ContainerStart(ctx, n.proxy, client.ContainerStartOptions{}); err != nil {
		_ = n.remove(ctx)
		return nil, fmt.Errorf("start proxy: %w", err)
	}

	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		c, err := docker.ContainerInspect(ctx, n.proxy, client.ContainerInspectOptions{})
		if err == nil && c.Container.State != nil && c.Container.State.Health != nil && c.Container.State.Health.Status == container.Healthy {
			return n, nil
		}

		if time.Now().After(deadline) {
			_ = n.remove(ctx)
			return nil, fmt.Errorf("proxy %s is not healthy (inspect error: %w)", n.proxy, err)
		}
	}
}

// remove removes the proxy and the network; a network a container still uses stays, with an
// error.
func (n *testNetwork) remove(ctx context.Context) error {
	if _, err := n.docker.ContainerRemove(ctx, n.proxy, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return err
	}

	if _, err := n.docker.NetworkRemove(ctx, n.name, client.NetworkRemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
		return err
	}

	return nil
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

func egressImage(docker *client.Client) error {
	egressImageOnce.Do(func() { errEgressImage = buildEgressImage(docker) })
	return errEgressImage
}

// testNet is the shared network, for tests whose containers need one and reach nothing.
func testNet(t *testing.T, docker *client.Client) string {
	t.Helper()

	sharedNetOnce.Do(func() { sharedNet, errSharedNet = newTestNetwork(context.Background(), docker, "") })

	if errSharedNet != nil {
		t.Fatalf("shared test network: %v", errSharedNet)
	}

	return sharedNet.name
}

// ownNet is a network of the test's own whose proxy serves gateway, removed when the test ends
// (after the containers on it, which are cleaned up first).
func ownNet(t *testing.T, docker *client.Client, gateway string) *testNetwork {
	t.Helper()

	n, err := newTestNetwork(t.Context(), docker, gateway)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := n.remove(context.Background()); err != nil {
			t.Errorf("remove test network %s: %v", n.name, err)
		}
	})

	return n
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

// hostURL is the listener as the proxy reaches it: goald's gateway on the host is found so.
func (l *hostListener) hostURL() string { return "http://host.docker.internal:" + l.port() }

func text(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
}

// hostAddresses are the addresses at which a container might find the host or a neighbour:
// the hosts' own as Docker Desktop and OrbStack give them, and the default bridge's gateway.
func hostAddresses(t *testing.T, docker *client.Client) []string {
	t.Helper()

	addrs := []string{"0.250.250.254", "198.19.248.254", "192.168.65.254", "172.17.0.1"}

	bridge, err := docker.NetworkInspect(t.Context(), "bridge", client.NetworkInspectOptions{})
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

// An agent task reaches goald's gateway (and only its paths) through the egress proxy; it cannot
// reach a listener on the host, the host's or a bridge's address, or the metadata service,
// directly or through the proxy.
func TestRunner_AgentReachesTheGatewayAndNoPrivateAddress(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)

	var (
		mu      sync.Mutex
		gateway []string
	)

	gw := newHostListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		gateway = append(gateway, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()

		_, _ = io.WriteString(w, "gateway")
	}))
	external := newHostListener(t, text("external"))
	secret := newHostListener(t, text("secret"))

	n := ownNet(t, docker, gw.hostURL())
	r.cfg.Network = n.name

	spec := stubSpec(t, "netprobe")
	ips := hostAddresses(t, docker)
	spec.Env["PROBE_SECRET_PORT"], spec.Env["PROBE_EXTERNAL_PORT"], spec.Env["PROBE_HOST_IPS"] = secret.port(), external.port(), strings.Join(ips, " ")

	events, res := run(t.Context(), t, r, spec, nil)
	if res.Status != domain.RunSucceeded {
		t.Fatalf("netprobe = %+v", res)
	}

	got := probes(events)
	want := map[string]string{
		"direct-host-name": "fail", "direct-metadata": "fail",
		"proxy-host": "403", "proxy-metadata": "403", "proxy-external": "403",
		"tunnel-external": "403", "tunnel-metadata": "403", "tunnel-host": "403",
		"gateway": "ok-gateway", "gateway-run": "ok-gateway", "goald-api": "404",
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
		lines, _ := proxyLines(t.Context(), docker, n.proxy)
		t.Logf("proxy log: %v", lines)
	}

	if hits := secret.hits.Load() + external.hits.Load(); hits != 0 {
		t.Errorf("host listeners the agent must not reach got %d requests", hits)
	}

	mu.Lock()
	defer mu.Unlock()

	if w := []string{"POST /mcp Bearer " + gatewayToken, "GET /run Bearer " + gatewayToken}; !slices.Equal(gateway, w) {
		t.Errorf("gateway saw %q; want %q: the agent's calls with its task token", gateway, w)
	}
}

// workerImage is the real agent image (task worker:image).
const workerImage = "ghcr.io/byfrederick/goal-agent:0.1.0"

// The real worker on the agents' network: Claude Code connects to the gateway through the proxy
// and sends its model traffic through the proxy too (the test's token is refused there), and
// nothing else.
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

	n := ownNet(t, docker, gateway.hostURL())
	r.cfg.Network = n.name

	spec := stubSpec(t, "real")
	spec.Image, spec.Brief, spec.Timeout = workerImage, "Reply with {}.", 3*time.Minute

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var servers string

	_, res := run(ctx, t, r, spec, func(ev ports.RunEvent) {
		servers = fmt.Sprint(ev.Data["mcp_servers"])

		// The model call follows the start; once the proxy has seen it, the task has shown what
		// it reaches.
		if !waitForLog(ctx, t, docker, n.proxy, "api.anthropic.com:443", time.Minute) {
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

	dests := proxyDestinations(t, docker, n.proxy)
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
