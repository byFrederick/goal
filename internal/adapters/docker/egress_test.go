package docker

import (
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// The proxy variables every networked container gets from unitEgress, as its sorted environment
// lists them: the upper-case names, then the lower-case ones.
const (
	upperProxyVars = "HTTPS_PROXY=http://goal-egress:3128 HTTP_PROXY=http://goal-egress:3128 NODE_USE_ENV_PROXY=1 NO_PROXY=goal-egress"
	lowerProxyVars = "http_proxy=http://goal-egress:3128 https_proxy=http://goal-egress:3128 no_proxy=goal-egress"
	proxyVars      = upperProxyVars + " " + lowerProxyVars
	agentGateway   = "http://goal-egress:3128/mcp"
)

// unitEgress is the default egress for goald's default gateway, without Docker.
func unitEgress(t *testing.T) *Egress {
	t.Helper()

	e, err := NewEgress(nil, EgressConfig{Gateway: "http://127.0.0.1:8080/mcp"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	return e
}

// An agent is given the proxy's address for the gateway, at the gateway's path, while the proxy
// reaches goald on the host as host.docker.internal.
func TestNewEgress_ServesAgentsTheGatewayThroughTheProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		gateway, upstream, agent string
	}{
		{"http://127.0.0.1:8080/mcp", "http://host.docker.internal:8080/mcp", agentGateway},
		{"http://localhost/mcp", "http://host.docker.internal/mcp", agentGateway},
		{"http://[::1]:9/mcp", "http://host.docker.internal:9/mcp", agentGateway},
		{"http://172.17.0.1:8090/mcp", "http://172.17.0.1:8090/mcp", agentGateway}, // a gateway listener on the Linux bridge
		{"https://gw.example.com/goal/mcp", "https://gw.example.com/goal/mcp", "http://goal-egress:3128/goal/mcp"},
	}

	for _, tt := range tests {
		t.Run(tt.gateway, func(t *testing.T) {
			t.Parallel()

			e, err := NewEgress(nil, EgressConfig{Gateway: tt.gateway}, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}

			cc, _, _ := e.proxyContainer(netip.MustParsePrefix("192.168.10.0/24"), netip.MustParsePrefix("192.168.11.0/24"))
			if i := slices.Index(cc.Cmd, "-gateway"); i < 0 || cc.Cmd[i+1] != tt.upstream {
				t.Errorf("proxy args %v; want -gateway %s", cc.Cmd, tt.upstream)
			}

			if got, err := e.gatewayFor(tt.gateway); err != nil || got != tt.agent {
				t.Errorf("gatewayFor = %q, %v; want %q", got, err, tt.agent)
			}
		})
	}
}

func TestEgress_GatewayForRefusesAnotherGateway(t *testing.T) {
	t.Parallel()

	e := unitEgress(t)

	if got, err := e.gatewayFor(""); err != nil || got != "" {
		t.Errorf("no gateway: %q, %v; want none", got, err)
	}

	if _, err := e.gatewayFor("http://127.0.0.1:9999/mcp"); err == nil {
		t.Error("a task naming a gateway the proxy does not serve was accepted")
	}
}

// The proxy runs hardened like every goal container, on the default bridge (its way out) and on
// both internal networks under its own name, with the agents' allowlist and both client networks.
func TestEgress_ProxyContainer(t *testing.T) {
	t.Parallel()

	e, err := NewEgress(nil, EgressConfig{
		Gateway: "http://127.0.0.1:8080/mcp", AgentHosts: []string{"API.anthropic.com", "proxy.golang.org:443", "mirror.example.com:8443"},
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	cc, hc, nc := e.proxyContainer(netip.MustParsePrefix("192.168.10.0/24"), netip.MustParsePrefix("192.168.11.0/24"))

	wantArgs := "-listen :3128 -agents 192.168.10.0/24 -tools 192.168.11.0/24 -gateway http://host.docker.internal:8080/mcp " +
		"-agent-host api.anthropic.com:443 -agent-host proxy.golang.org:443 -agent-host mirror.example.com:8443"
	if got := strings.Join(cc.Cmd, " "); got != wantArgs {
		t.Errorf("args:\n got %s\nwant %s", got, wantArgs)
	}

	if cc.User != "65532:65532" || !hc.ReadonlyRootfs || fmt.Sprint(hc.CapDrop) != "[ALL]" || hc.Privileged ||
		fmt.Sprint(hc.SecurityOpt) != "[no-new-privileges:true]" || hc.Memory != 256<<20 {
		t.Errorf("proxy not hardened: user %q, config %+v", cc.User, hc)
	}

	var nets []string

	for name, ep := range nc.EndpointsConfig {
		nets = append(nets, fmt.Sprintf("%s%v", name, ep.Aliases))
	}

	slices.Sort(nets)

	if got := fmt.Sprint(nets); hc.NetworkMode != "bridge" || got != "[bridge[] goal-egress-agents[goal-egress] goal-egress-tools[goal-egress]]" {
		t.Errorf("networks %s (mode %q); want the default bridge and both internal networks with the proxy's name", got, hc.NetworkMode)
	}
}

func TestNewEgress_RefusesABadConfiguration(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]EgressConfig{
		"name":                 {Name: "../goal"},
		"agent host":           {AgentHosts: []string{"https://api.anthropic.com/v1"}},
		"trusted without port": {Trusted: []string{"mirror.example.com"}},
		"gateway not http":     {Gateway: "unix:///tmp/gw.sock"},
		"gateway without path": {Gateway: "http://127.0.0.1:8080"},
	} {
		if _, err := NewEgress(nil, cfg, slog.New(slog.DiscardHandler)); err == nil {
			t.Errorf("%s: NewEgress accepted %+v", name, cfg)
		}
	}
}
