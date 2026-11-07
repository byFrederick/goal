package domain

import "testing"

// Hosts the domain's tests repeat.
const (
	hostGitHub = "github.com"
	hostGHE    = "ghe.example.com"
)

func TestReservedRunEnv(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"GOAL_GATEWAY_KEY": true, "goal_x": true, "HOME": true, "Path": true, "http_proxy": true, "NO_PROXY": true,
		"NODE_USE_ENV_PROXY": true, "CLAUDE_CONFIG_DIR": true, "LD_PRELOAD": true, "MISE_DATA_DIR": true,
		"GH_TOKEN": false, "ANTHROPIC_API_KEY": false, "GOALS": false, "PATHS": false,
	} {
		if got := ReservedRunEnv(name); got != want {
			t.Errorf("ReservedRunEnv(%q) = %v; want %v", name, got, want)
		}
	}
}
