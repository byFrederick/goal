package docker

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"

	"github.com/byFrederick/goal/internal/ports"
)

// hardening is the security-relevant part of a container's configuration, comparable at once.
type hardening struct {
	User, CapDrop, SecurityOpt, Mounts, Labels, Env, Tmpfs string
	Privileged, ReadOnlyRoot, Init                         bool
	CPUs, Memory, Swap, PIDs                               int64
}

func hardeningOf(cc *container.Config, hc *container.HostConfig) hardening {
	mounts := make([]string, 0, len(hc.Mounts))
	for _, m := range hc.Mounts {
		mounts = append(mounts, fmt.Sprintf("%s %s:%s:ro=%v", m.Type, m.Source, m.Target, m.ReadOnly))
	}

	return hardening{
		User: cc.User, CapDrop: fmt.Sprint(hc.CapDrop), SecurityOpt: fmt.Sprint(hc.SecurityOpt), Mounts: fmt.Sprint(mounts),
		Labels: fmt.Sprint(cc.Labels), Env: fmt.Sprint(cc.Env), Tmpfs: fmt.Sprint(hc.Tmpfs),
		Privileged: hc.Privileged, ReadOnlyRoot: hc.ReadonlyRootfs, Init: hc.Init != nil && *hc.Init,
		CPUs: hc.NanoCPUs, Memory: hc.Memory, Swap: hc.MemorySwap, PIDs: *cmp.Or(hc.PidsLimit, new(int64)),
	}
}

func TestContainerOf_IsHardened(t *testing.T) {
	t.Parallel()

	spec := ports.TaskSpec{
		OrgID: "o1", FactoryID: "f1", ItemID: "i1", RunID: "r1", Station: "build", Agent: "engineer", Image: "img:1",
		WorkspaceDir: "/work/r1", GatewayToken: "gateway-token-123",
		Env: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "oauth-token-123", "ANTHROPIC_API_KEY": "sk-ant-not-passed"}, //nolint:gosec // fake values
	}

	cc, hc := containerOf(Config{}.withDefaults(), unitEgress(t).agents(), spec, "/runs/r1")
	got := hardeningOf(cc, hc)
	want := hardening{
		User: "1000:1000", CapDrop: "[ALL]", SecurityOpt: "[no-new-privileges:true]",
		Mounts: "[bind /runs/r1:/goal:ro=true bind /work/r1:/workspace:ro=false volume :/home/node:ro=false]",
		Labels: "map[goal.agent:engineer goal.factory:f1 goal.item:i1 goal.org:o1 goal.run:r1 goal.station:build]",
		Env: "[CLAUDE_CODE_OAUTH_TOKEN=oauth-token-123 CLAUDE_CONFIG_DIR=/home/node/.claude GOAL_GATEWAY_TOKEN=gateway-token-123 HOME=/home/node " +
			proxyVars + "]",
		Tmpfs:      "map[/tmp:rw,nosuid,nodev,size=1g,mode=1777]",
		Privileged: false, ReadOnlyRoot: true, Init: true,
		CPUs: 2e9, Memory: 4 << 30, Swap: 4 << 30, PIDs: 1024,
	}

	if got != want {
		t.Errorf("container hardening:\n got %+v\nwant %+v", got, want)
	}

	// The agents' network reaches nothing but the egress proxy; no name leads to the host.
	if hc.NetworkMode != "goal-egress-agents" || len(hc.ExtraHosts) != 0 {
		t.Errorf("network %q, extra hosts %v; want the agents' network and no host alias", hc.NetworkMode, hc.ExtraHosts)
	}
}

// A task's environment cannot point its HTTP clients past the egress proxy.
func TestTaskEnv_TheProxyVariablesWin(t *testing.T) {
	t.Parallel()

	spec := ports.TaskSpec{Env: map[string]string{"HTTPS_PROXY": "http://attacker.example.com:3128", "NO_PROXY": "*"}}

	env := taskEnv(spec, unitEgress(t).agents())
	if !slices.Contains(env, "HTTPS_PROXY=http://goal-egress:3128") || !slices.Contains(env, "NO_PROXY=goal-egress") {
		t.Errorf("env %v: want the egress proxy's variables", env)
	}
}

// An agent gets only its definition's skills: no host Claude Code config (settings, skills,
// plugins, credentials) is mounted, and a task environment that names a config dir cannot point
// Claude Code away from the container's throwaway home.
func TestContainerOf_KeepsHostClaudeConfigOut(t *testing.T) {
	t.Parallel()

	spec := ports.TaskSpec{
		RunID: "r1", Image: "img:1", WorkspaceDir: "/work/r1",
		Env: map[string]string{"CLAUDE_CONFIG_DIR": "/Users/acme/.claude", "CLAUDE_CODE_OAUTH_TOKEN": "oauth-token-123"}, //nolint:gosec // fake values
	}

	cc, hc := containerOf(Config{}.withDefaults(), unitEgress(t).agents(), spec, "/runs/r1")

	for _, m := range hc.Mounts {
		if m.Type == mount.TypeBind && m.Target != containerGoal && m.Target != containerWorkspace {
			t.Errorf("unexpected bind mount %s:%s", m.Source, m.Target)
		}

		if strings.Contains(m.Source, ".claude") || strings.Contains(m.Target, ".claude") {
			t.Errorf("mount %s:%s carries Claude Code config", m.Source, m.Target)
		}
	}

	if !slices.Contains(cc.Env, "CLAUDE_CONFIG_DIR=/home/node/.claude") || slices.Contains(cc.Env, "CLAUDE_CONFIG_DIR=/Users/acme/.claude") {
		t.Errorf("env %v: want CLAUDE_CONFIG_DIR in the container's home only", cc.Env)
	}
}

// spec.json carries the factory's purpose for the worker's system prompt, and leaves it out
// when the factory has none.
func TestWorkerSpecOf_CarriesThePurpose(t *testing.T) {
	t.Parallel()

	for _, purpose := range []string{"Answers acme's support questions.", ""} {
		s := workerSpecOf(ports.TaskSpec{Purpose: purpose}, "")

		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}

		if got := strings.Contains(string(b), `"purpose"`); s.Purpose != purpose || got != (purpose != "") {
			t.Errorf("purpose %q: spec.json %s", purpose, b)
		}
	}
}

func TestValidate_RejectsUnsafeSpecs(t *testing.T) {
	t.Parallel()

	good := ports.TaskSpec{RunID: "0192c8a0-run", Image: "img", WorkspaceDir: "/abs"}
	if err := validate(good); err != nil {
		t.Fatalf("validate(good) = %v", err)
	}

	for name, mutate := range map[string]func(*ports.TaskSpec){
		"path in run id":     func(s *ports.TaskSpec) { s.RunID = "../etc" },
		"no image":           func(s *ports.TaskSpec) { s.Image = "" },
		"relative workspace": func(s *ports.TaskSpec) { s.WorkspaceDir = "work" },
	} {
		s := good
		mutate(&s)

		if err := validate(s); err == nil {
			t.Errorf("%s: validate accepted %+v", name, s)
		}
	}
}
