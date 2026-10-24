package docker

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Labels on the egress proxy and its networks: the egress name, and the configuration the
// proxy runs with, so a goald with another configuration replaces it.
const (
	LabelEgress       = "goal.egress"
	LabelEgressConfig = "goal.egress.config"
)

// Egress defaults and the proxy's contract (cmd/goal-egress).
const (
	DefaultEgressName  = "goal-egress"
	DefaultEgressImage = "ghcr.io/byfrederick/goal-egress:0.1.0"
	DefaultAgentHost   = "api.anthropic.com"
	egressPort         = "3128"
	egressBinary       = "/goal-egress"
	egressUser         = "65532:65532" // distroless's nonroot
	// egressExit is the network the proxy reaches the outside and goald's gateway on: Docker's
	// default bridge, which every engine has and whose containers reach host.docker.internal.
	egressExit = "bridge"
	// noBridgeAddress keeps the host off a network: its bridge gets no address, so a container on
	// it has no way to the host even where an internal network would leave one (Linux).
	noBridgeAddress = "com.docker.network.bridge.inhibit_ipv4"
	egressReady     = 30 * time.Second
)

// EgressConfig is the egress proxy and the networks agents, sidecars and checks run on (ADR
// 0009). The zero value of each field means its default.
type EgressConfig struct {
	// Name names the proxy's container and, with -agents and -tools, its two networks. goalds of
	// different configurations on one Docker host need different names (GOAL_EGRESS_NAME).
	// Default goal-egress.
	Name string
	// Image is the proxy's image, built by task egress:image. Default DefaultEgressImage.
	Image string
	// Gateway is goald's MCP gateway as tasks are given it (ports.TaskSpec.GatewayURL); a
	// loopback URL is goald on the host, which the proxy reaches as host.docker.internal.
	// Empty means agents have no gateway.
	Gateway string
	// AgentHosts are the hosts agents may reach besides the gateway, host or host:port (443 by
	// default). Default the model API, api.anthropic.com.
	AgentHosts []string
	// Trusted are destinations (host:port) the proxy dials without refusing non-public
	// addresses: a test's stub, a package mirror on a private network.
	Trusted []string
}

// Egress keeps the egress proxy and its networks: created when missing, replaced when they no
// longer match the configuration, checked before every container that needs them starts. It is
// safe for concurrent use.
type Egress struct {
	docker   *client.Client
	cfg      EgressConfig
	upstream string // the gateway as the proxy reaches it
	path     string // the gateway's path
	log      *slog.Logger

	mu   sync.Mutex
	want string // the configuration hash of the running proxy, once Ensure succeeded
}

// NewEgress returns the egress of cfg on docker, refusing a configuration that does not hold.
func NewEgress(docker *client.Client, cfg EgressConfig, log *slog.Logger) (*Egress, error) {
	cfg.Name = cmp.Or(cfg.Name, DefaultEgressName)
	cfg.Image = cmp.Or(cfg.Image, DefaultEgressImage)

	if len(cfg.AgentHosts) == 0 {
		cfg.AgentHosts = []string{DefaultAgentHost}
	}

	e := &Egress{docker: docker, cfg: cfg, log: log}

	var errs []error

	if !safeID.MatchString(cfg.Name) {
		errs = append(errs, fmt.Errorf("egress name %q is not usable as a container name", cfg.Name))
	}

	hosts := make([]string, 0, len(cfg.AgentHosts))

	for _, h := range cfg.AgentHosts {
		hp, err := hostPort(h, "443")
		if err != nil {
			errs = append(errs, err)
		}

		hosts = append(hosts, hp)
	}

	e.cfg.AgentHosts = hosts

	for _, h := range cfg.Trusted {
		if _, _, err := net.SplitHostPort(h); err != nil {
			errs = append(errs, fmt.Errorf("trusted destination %q is not host:port", h))
		}
	}

	if cfg.Gateway != "" {
		upstream, err := containerURL(cfg.Gateway)
		if err != nil {
			errs = append(errs, fmt.Errorf("gateway url: %w", err))
		} else if u, _ := url.Parse(upstream); u.Path == "" || u.RawQuery != "" {
			errs = append(errs, fmt.Errorf("gateway url %q needs a path and no query", cfg.Gateway))
		} else {
			e.upstream, e.path = upstream, u.Path
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return e, nil
}

// hostPort is h as lower-case host:port, with port def when h has none.
func hostPort(h, def string) (string, error) {
	h = strings.ToLower(strings.TrimSpace(h))

	host, port, err := net.SplitHostPort(h)
	if err != nil {
		host, port = h, def
	}

	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || host == "" || strings.ContainsAny(host, "/@ ") {
		return "", fmt.Errorf("agent egress host %q is not a host or host:port", h)
	}

	return net.JoinHostPort(host, port), nil
}

// route is how a container reaches the outside: the network it runs on and the proxy variables
// that send its HTTP clients through the proxy.
type route struct {
	network string
	env     map[string]string
}

func (e *Egress) agentsNetwork() string { return e.cfg.Name + "-agents" }
func (e *Egress) toolsNetwork() string  { return e.cfg.Name + "-tools" }

func (e *Egress) proxyURL() string { return "http://" + net.JoinHostPort(e.cfg.Name, egressPort) }

// proxyEnv sends HTTP clients through the proxy (curl reads the lower-case names, most others
// either), Node's own fetch too, and keeps the proxy's own address (the gateway) direct.
func (e *Egress) proxyEnv() map[string]string {
	p := e.proxyURL()

	return map[string]string{
		"HTTP_PROXY": p, "HTTPS_PROXY": p, "http_proxy": p, "https_proxy": p,
		"NO_PROXY": e.cfg.Name, "no_proxy": e.cfg.Name, "NODE_USE_ENV_PROXY": "1",
	}
}

func (e *Egress) agents() route { return route{network: e.agentsNetwork(), env: e.proxyEnv()} }
func (e *Egress) tools() route  { return route{network: e.toolsNetwork(), env: e.proxyEnv()} }

// gatewayFor is the gateway URL an agent container is given for a task's gateway: the proxy's,
// at the gateway's path. A task names no gateway, or the one the proxy serves.
func (e *Egress) gatewayFor(taskGateway string) (string, error) {
	switch taskGateway {
	case "":
		return "", nil
	case e.cfg.Gateway:
		return e.proxyURL() + e.path, nil
	default:
		return "", fmt.Errorf("the task's gateway %q is not the one the egress proxy serves (%q)", taskGateway, e.cfg.Gateway)
	}
}

// CheckImage reports whether the proxy's image is on the Docker host, so goald can warn at start
// that no agent can run without it.
func (e *Egress) CheckImage(ctx context.Context) error {
	if _, err := e.docker.ImageInspect(ctx, e.cfg.Image); err != nil {
		return fmt.Errorf("egress proxy image %s: %w (build it with task egress:image)", e.cfg.Image, err)
	}

	return nil
}

// Ensure makes sure the proxy runs with this configuration on both networks, creating or
// replacing whatever is missing or stale, and waits until it accepts connections.
func (e *Egress) Ensure(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.want != "" {
		if c, err := e.docker.ContainerInspect(ctx, e.cfg.Name, client.ContainerInspectOptions{}); err == nil &&
			c.Container.State != nil && c.Container.State.Running && c.Container.Config.Labels[LabelEgressConfig] == e.want {
			return nil
		}
	}

	if err := e.ensure(ctx); err != nil {
		e.want = ""
		return fmt.Errorf("egress proxy: %w", err)
	}

	return nil
}

func (e *Egress) ensure(ctx context.Context) error {
	cc, hc, nc, err := e.desired(ctx)
	if err != nil {
		return err
	}

	if err := e.converge(ctx, cc, hc, nc); err != nil {
		return err
	}

	if err := e.waitHealthy(ctx); err != nil {
		return err
	}

	e.want = cc.Labels[LabelEgressConfig]

	return nil
}

// desired is the proxy's container as this configuration wants it, after making sure its
// networks and its image exist; its LabelEgressConfig label is the configuration's hash.
func (e *Egress) desired(ctx context.Context) (*container.Config, *container.HostConfig, *network.NetworkingConfig, error) {
	agents, err := e.network(ctx, e.agentsNetwork())
	if err != nil {
		return nil, nil, nil, err
	}

	tools, err := e.network(ctx, e.toolsNetwork())
	if err != nil {
		return nil, nil, nil, err
	}

	if err := ensureImage(ctx, e.docker, e.log, e.cfg.Image); err != nil {
		return nil, nil, nil, fmt.Errorf("%w (build it with task egress:image)", err)
	}

	img, err := e.docker.ImageInspect(ctx, e.cfg.Image)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("inspect image %s: %w", e.cfg.Image, err)
	}

	cc, hc, nc := e.proxyContainer(agents, tools)
	cc.Labels[LabelEgressConfig] = configHash(img.ID, cc.Cmd, hc.ExtraHosts)

	return cc, hc, nc, nil
}

// converge starts the proxy of the wanted configuration: the existing container when it has
// it, else a new one in place of whatever runs under the proxy's name.
func (e *Egress) converge(ctx context.Context, cc *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig) error {
	cur, err := e.docker.ContainerInspect(ctx, e.cfg.Name, client.ContainerInspectOptions{})

	switch {
	case cerrdefs.IsNotFound(err):
		return e.create(ctx, cc, hc, nc)
	case err != nil:
		return fmt.Errorf("inspect %s: %w", e.cfg.Name, err)
	case cur.Container.Config.Labels[LabelEgressConfig] != cc.Labels[LabelEgressConfig]:
		e.log.InfoContext(ctx, "replacing the egress proxy: its configuration changed", slog.String("container", e.cfg.Name))

		if _, err := e.docker.ContainerRemove(ctx, e.cfg.Name, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("remove stale %s: %w", e.cfg.Name, err)
		}

		return e.create(ctx, cc, hc, nc)
	case cur.Container.State == nil || !cur.Container.State.Running:
		if _, err := e.docker.ContainerStart(ctx, e.cfg.Name, client.ContainerStartOptions{}); err != nil {
			return fmt.Errorf("start %s: %w", e.cfg.Name, err)
		}
	}

	return nil
}

func (e *Egress) create(ctx context.Context, cc *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig) error {
	_, err := e.docker.ContainerCreate(ctx, client.ContainerCreateOptions{Config: cc, HostConfig: hc, NetworkingConfig: nc, Name: e.cfg.Name})
	if cerrdefs.IsConflict(err) {
		// Another goald of the same configuration created it first; waitHealthy checks it runs.
		return nil
	}

	if err != nil {
		return fmt.Errorf("create %s: %w", e.cfg.Name, err)
	}

	if _, err := e.docker.ContainerStart(ctx, e.cfg.Name, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start %s: %w", e.cfg.Name, err)
	}

	e.log.InfoContext(ctx, "egress proxy started", slog.String("container", e.cfg.Name),
		slog.String("agents", e.agentsNetwork()), slog.String("tools", e.toolsNetwork()))

	return nil
}

// network returns the subnet of the internal network name, creating it when missing. An existing
// network of that name that is not internal, or has a bridge address, is refused.
func (e *Egress) network(ctx context.Context, name string) (netip.Prefix, error) {
	res, err := e.docker.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		_, err = e.docker.NetworkCreate(ctx, name, client.NetworkCreateOptions{
			Driver: "bridge", Internal: true, Options: map[string]string{noBridgeAddress: "true"},
			Labels: map[string]string{LabelEgress: e.cfg.Name},
		})
		if err != nil && !cerrdefs.IsConflict(err) {
			return netip.Prefix{}, fmt.Errorf("create network %s: %w", name, err)
		}

		res, err = e.docker.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	}

	if err != nil {
		return netip.Prefix{}, fmt.Errorf("inspect network %s: %w", name, err)
	}

	n := res.Network
	if !n.Internal || n.Options[noBridgeAddress] != "true" {
		return netip.Prefix{}, fmt.Errorf("network %s exists but is not internal without a bridge address; remove it (docker network rm %s)", name, name)
	}

	for _, c := range n.IPAM.Config {
		if c.Subnet.IsValid() && c.Subnet.Addr().Is4() {
			return c.Subnet.Masked(), nil
		}
	}

	return netip.Prefix{}, fmt.Errorf("network %s has no IPv4 subnet", name)
}

// proxyContainer is the proxy's container: hardened like every goal container, as distroless's
// nonroot user, on the default bridge (its way out, preferred for its default route) and on both
// internal networks under its own name, restarted with Docker.
func (e *Egress) proxyContainer(agents, tools netip.Prefix) (*container.Config, *container.HostConfig, *network.NetworkingConfig) {
	args := []string{"-listen", ":" + egressPort, "-agents", agents.String(), "-tools", tools.String()}
	if e.upstream != "" {
		args = append(args, "-gateway", e.upstream)
	}

	for _, h := range e.cfg.AgentHosts {
		args = append(args, "-agent-host", h)
	}

	for _, h := range e.cfg.Trusted {
		args = append(args, "-trust", h)
	}

	cc := &container.Config{
		Image: e.cfg.Image, User: egressUser, Cmd: args,
		Labels: map[string]string{LabelEgress: e.cfg.Name},
		Healthcheck: &container.HealthConfig{
			Test:     []string{"CMD", egressBinary, "-health"},
			Interval: 30 * time.Second, Timeout: 3 * time.Second, StartPeriod: 10 * time.Second, StartInterval: 250 * time.Millisecond, Retries: 3,
		},
	}

	hc := isolated(Config{CPUs: 1, MemoryBytes: 256 << 20, PIDs: 256})
	hc.NetworkMode = egressExit
	hc.ExtraHosts = []string{hostAlias + ":host-gateway"}
	hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}

	nc := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
		egressExit:        {GwPriority: 1},
		e.agentsNetwork(): {Aliases: []string{e.cfg.Name}},
		e.toolsNetwork():  {Aliases: []string{e.cfg.Name}},
	}}

	return cc, hc, nc
}

func configHash(imageID string, args, hosts []string) string {
	h := sha256.New()
	for _, part := range slices.Concat([]string{imageID}, args, hosts) {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// waitHealthy waits until the proxy's health check passes.
func (e *Egress) waitHealthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, egressReady)
	defer cancel()

	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()

	for {
		c, err := e.docker.ContainerInspect(ctx, e.cfg.Name, client.ContainerInspectOptions{})
		if err == nil && c.Container.State != nil {
			s := c.Container.State
			if s.Running && s.Health != nil && s.Health.Status == container.Healthy {
				return nil
			}

			if !s.Running && !s.Restarting && s.Status != container.StateCreated {
				return fmt.Errorf("%s exited (code %d); see docker logs %s", e.cfg.Name, s.ExitCode, e.cfg.Name)
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s is not healthy after %s; see docker logs %s", e.cfg.Name, egressReady, e.cfg.Name)
		case <-tick.C:
		}
	}
}

// Remove removes the proxy and its networks; a network a container still uses stays, with an
// error. goald never calls it: the proxy outlives goald, like compose's services.
func (e *Egress) Remove(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.want = ""

	var errs []error

	if _, err := e.docker.ContainerRemove(ctx, e.cfg.Name, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		errs = append(errs, err)
	}

	for _, n := range []string{e.agentsNetwork(), e.toolsNetwork()} {
		if _, err := e.docker.NetworkRemove(ctx, n, client.NetworkRemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
