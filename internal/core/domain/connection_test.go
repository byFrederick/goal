package domain

import (
	"maps"
	"testing"
)

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

// The associated data binds organization, connection and key id, each to its own position.
func TestConnectionAAD(t *testing.T) {
	t.Parallel()

	base := ConnectionAAD("org-a", "conn-1", "k1")
	if string(base) != "goal/connection/v1|org-a|conn-1|k1" {
		t.Errorf("ConnectionAAD = %q", base)
	}

	others := map[string][]byte{
		"org": ConnectionAAD("org-b", "conn-1", "k1"), "connection": ConnectionAAD("org-a", "conn-2", "k1"),
		"key": ConnectionAAD("org-a", "conn-1", "k2"),
	}
	for what, aad := range maps.All(others) {
		if string(aad) == string(base) {
			t.Errorf("another %s gives the same associated data", what)
		}
	}
}
