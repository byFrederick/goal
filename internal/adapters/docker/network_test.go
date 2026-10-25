package docker

import (
	"fmt"
	"testing"
)

// The proxy variables every networked container gets, as its sorted environment lists them: the
// upper-case names, then the lower-case ones.
const (
	noProxy        = "goal-egress,localhost,127.0.0.1,::1"
	upperProxyVars = "HTTPS_PROXY=http://goal-egress:3128 HTTP_PROXY=http://goal-egress:3128 NODE_USE_ENV_PROXY=1 NO_PROXY=" + noProxy
	lowerProxyVars = "http_proxy=http://goal-egress:3128 https_proxy=http://goal-egress:3128 no_proxy=" + noProxy
	proxyVars      = upperProxyVars + " " + lowerProxyVars
	// boxGateway is the gateway as a container is given it.
	boxGateway = "http://goal-egress:3128/mcp"
)

// unitRoute is the agents' network of the default configuration.
func unitRoute() route { return agentsRoute(Config{}.withDefaults().Network) }

// A container is given the egress proxy's address for goald's gateway, at the gateway's path,
// wherever goald listens: the proxy alone knows the way to goald.
func TestGatewayFor_IsTheProxyAtTheGatewaysPath(t *testing.T) {
	t.Parallel()

	for gateway, want := range map[string]string{
		"":                            "",
		"http://127.0.0.1:8080/mcp":   boxGateway,
		"http://172.17.0.1:8090/mcp":  boxGateway,
		"https://gw.example.com/mcp":  boxGateway,
		"http://goald:8090/mcp?x=%20": boxGateway,
	} {
		if got, err := gatewayFor(gateway); err != nil || got != want {
			t.Errorf("gatewayFor(%q) = %q, %v; want %q", gateway, got, err, want)
		}
	}

	for _, gateway := range []string{"goald:8080/mcp", "unix:///run/goald.sock", "http://goald:8080"} {
		if got, err := gatewayFor(gateway); err == nil {
			t.Errorf("gatewayFor(%q) = %q; want an error", gateway, got)
		}
	}
}

// A container's own proxy variables never win over the egress proxy's, and its loopback stays
// direct, so services a run starts in its box answer it.
func TestWithProxyEnv_TheProxyVariablesWin(t *testing.T) {
	t.Parallel()

	env := withProxyEnv(map[string]string{"HTTPS_PROXY": "http://attacker.example.com:3128", "no_proxy": "*", "GOAL_GATEWAY_URL": boxGateway})

	if got, want := fmt.Sprint(env), "[GOAL_GATEWAY_URL=http://goal-egress:3128/mcp "+proxyVars+"]"; got != want {
		t.Errorf("env = %s; want %s", got, want)
	}
}
