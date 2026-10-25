package docker

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// The agents' network and its egress proxy (ADR 0009 and its phase 4 amendment): compose.yaml's
// agents network, internal with no address on the host's side, so a container on it has no route
// to the host, to other networks or to the internet; and its goal-egress service, the one other
// container there, which every container's proxy variables name. It reaches public hosts only,
// and goald's gateway at its paths.
const (
	// DefaultNetwork is compose.yaml's agents network in the compose project goal
	// (GOAL_SANDBOX_NETWORK).
	DefaultNetwork = "goal_agents"
	// proxyHost is the egress proxy's name on the network (its compose service), proxyPort its port.
	proxyHost = "goal-egress"
	proxyPort = "3128"
	// noBridgeAddress keeps the host off a network: its bridge gets no address, so a container on
	// it has no way to the host even where an internal network would leave one (Linux).
	noBridgeAddress = "com.docker.network.bridge.inhibit_ipv4"
)

// proxyURL is the egress proxy as containers on the network reach it.
func proxyURL() string { return "http://" + net.JoinHostPort(proxyHost, proxyPort) }

// proxyEnv sends HTTP clients through the proxy (curl reads the lower-case names, most others
// either), Node's own fetch too, and keeps the proxy's own address (the gateway) and the
// container's loopback (services a run starts in its box) direct.
func proxyEnv() map[string]string {
	p := proxyURL()
	direct := proxyHost + ",localhost,127.0.0.1,::1"

	return map[string]string{
		"HTTP_PROXY": p, "HTTPS_PROXY": p, "http_proxy": p, "https_proxy": p,
		"NO_PROXY": direct, "no_proxy": direct, "NODE_USE_ENV_PROXY": "1",
	}
}

// withProxyEnv is env with the proxy variables on top, as KEY=value lines in name order.
func withProxyEnv(env map[string]string) []string {
	all := maps.Clone(env)
	if all == nil {
		all = map[string]string{}
	}

	maps.Copy(all, proxyEnv())

	out := make([]string, 0, len(all))
	for _, k := range slices.Sorted(maps.Keys(all)) {
		out = append(out, k+"="+all[k])
	}

	return out
}

// route is how a container reaches the outside: the network it runs on and the proxy variables
// that send its HTTP clients through the proxy.
type route struct {
	network string
	env     map[string]string
}

func agentsRoute(network string) route { return route{network: network, env: proxyEnv()} }

// gatewayFor is the gateway URL a container is given for goald's gateway (a task's GatewayURL):
// the egress proxy's, at the gateway's path, which the proxy forwards to goald.
func gatewayFor(gateway string) (string, error) {
	if gateway == "" {
		return "", nil
	}

	u, err := url.Parse(gateway)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Path == "" {
		return "", fmt.Errorf("gateway url %q is not an http(s) URL with a path", gateway)
	}

	return proxyURL() + u.EscapedPath(), nil
}

// checkNetwork refuses to start containers on a network that is missing or has a route out:
// compose.yaml creates it with its egress proxy, which goal up starts.
func checkNetwork(ctx context.Context, docker *client.Client, name string) error {
	res, err := docker.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return fmt.Errorf("the agents' network %s does not exist: start the egress proxy, which creates it "+
			"(goal up does; docker compose up -d --wait goal-egress), or set GOAL_SANDBOX_NETWORK to its name", name)
	}

	if err != nil {
		return fmt.Errorf("inspect network %s: %w", name, err)
	}

	if n := res.Network; !n.Internal || n.Options[noBridgeAddress] != "true" {
		return fmt.Errorf("network %s is not internal without a bridge address, so containers on it could reach the host; "+
			"use compose.yaml's agents network", name)
	}

	return nil
}
