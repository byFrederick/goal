package docker

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/byFrederick/goal/internal/domain"
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
		FactoryID: "f1", ItemID: "i1", RunID: "r1", Station: "build", Agent: "engineer", Image: "img:1",
		WorkspaceDir: "/work/r1", GatewayToken: "gateway-token-123",
		Env: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "oauth-token-123", "ANTHROPIC_API_KEY": "sk-ant-not-passed"}, //nolint:gosec // fake values
	}

	got := hardeningOf(containerOf(Config{}.withDefaults(), spec, "/runs/r1"))
	want := hardening{
		User: "1000:1000", CapDrop: "[ALL]", SecurityOpt: "[no-new-privileges:true]",
		Mounts:     "[bind /runs/r1:/goal:ro=true bind /work/r1:/workspace:ro=false volume :/home/node:ro=false]",
		Labels:     "map[goal.agent:engineer goal.factory:f1 goal.item:i1 goal.run:r1 goal.station:build]",
		Env:        "[CLAUDE_CODE_OAUTH_TOKEN=oauth-token-123 GOAL_GATEWAY_TOKEN=gateway-token-123 HOME=/home/node]",
		Tmpfs:      "map[/tmp:rw,nosuid,nodev,size=1g,mode=1777]",
		Privileged: false, ReadOnlyRoot: true, Init: true,
		CPUs: 2e9, Memory: 4 << 30, Swap: 4 << 30, PIDs: 1024,
	}

	if got != want {
		t.Errorf("container hardening:\n got %+v\nwant %+v", got, want)
	}
}

func TestWorkerSpecOf_KeepsSecretsOutAndRewritesLoopbackGateway(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"http://127.0.0.1:8081/mcp":  "http://host.docker.internal:8081/mcp",
		"http://localhost/mcp":       "http://host.docker.internal/mcp",
		"http://[::1]:9/mcp":         "http://host.docker.internal:9/mcp",
		"https://gw.example.com/mcp": "https://gw.example.com/mcp",
		"":                           "",
	}

	for in, want := range tests {
		s, err := workerSpecOf(ports.TaskSpec{
			GatewayURL: in, GatewayToken: "gateway-token-123", Env: map[string]string{"X": "secret-value-123"},
			Skills: []domain.SkillRef{{Name: "acme-conventions", Source: "./skills/acme-conventions"}},
		})
		if err != nil {
			t.Fatalf("workerSpecOf(%q): %v", in, err)
		}

		if s.GatewayURL != want || !slices.Equal(s.Skills, []string{"acme-conventions"}) {
			t.Errorf("workerSpecOf(%q) = gateway %q skills %v; want %q", in, s.GatewayURL, s.Skills, want)
		}
	}

	if _, err := workerSpecOf(ports.TaskSpec{GatewayURL: "unix:///tmp/gw.sock"}); err == nil {
		t.Error("a non-HTTP gateway URL must be rejected")
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
