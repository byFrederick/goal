package docker

import (
	"fmt"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// unitBoxImage is the image of the unit tests' boxes.
const unitBoxImage = "box:1"

// A box keeps the hardening of agent tasks, with the limits of its size, a writable root
// filesystem and nothing mounted, on the agents' network only; it sleeps until its deadline.
func TestBoxContainerOf_IsHardened(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	spec := ports.SandboxSpec{
		OrgID: "o1", FactoryID: "f1", ItemID: "i1", RunID: "r1", Station: "review", Agent: "builder", Image: unitBoxImage,
		Size: domain.SandboxSmall, Env: map[string]string{"GOAL_GATEWAY_URL": boxGateway}, Deadline: now.Add(90 * time.Minute),
	}

	limits, err := validateBox(spec)
	if err != nil {
		t.Fatal(err)
	}

	cc, hc := boxContainerOf(limits, DefaultNetwork, spec, now)

	got := hardeningOf(cc, hc)
	want := hardening{
		User: containerUser, CapDrop: "[ALL]", SecurityOpt: "[no-new-privileges:true]", Mounts: "[]",
		Labels: "map[goal.agent:builder goal.factory:f1 goal.item:i1 goal.org:o1 goal.run:r1 goal.station:review]",
		Env:    "[GOAL_GATEWAY_URL=http://goal-egress:3128/mcp " + proxyVars + "]",
		Tmpfs:  "map[/tmp:rw,nosuid,nodev,size=1g,mode=1777]",
		Init:   true, CPUs: 1e9, Memory: 2 << 30, Swap: 2 << 30, PIDs: 1024,
	}

	if got != want {
		t.Errorf("box hardening:\n got %+v\nwant %+v", got, want)
	}

	if hc.NetworkMode != DefaultNetwork || len(hc.ExtraHosts) != 0 || len(hc.Binds) != 0 {
		t.Errorf("network %q, extra hosts %v, binds %v; want the agents' network only", hc.NetworkMode, hc.ExtraHosts, hc.Binds)
	}

	if fmt.Sprint(cc.Entrypoint, cc.Cmd) != "[sleep] [5400]" {
		t.Errorf("box process %v %v; want sleep until the deadline", cc.Entrypoint, cc.Cmd)
	}
}

// Each size has its limits, no size is medium, and without a deadline a box sleeps for good; a
// deadline already past still starts the box, for a second.
func TestBoxContainerOf_SizesAndDeadlines(t *testing.T) {
	t.Parallel()

	now := time.Now()

	for size, want := range map[domain.SandboxSize]string{
		"": "2e+09 4294967296 2048", domain.SandboxMedium: "2e+09 4294967296 2048", domain.SandboxLarge: "4e+09 8589934592 4096",
	} {
		spec := ports.SandboxSpec{RunID: "r1", Image: unitBoxImage, Size: size}

		limits, err := validateBox(spec)
		if err != nil {
			t.Fatalf("size %q: %v", size, err)
		}

		cc, hc := boxContainerOf(limits, DefaultNetwork, spec, now)
		if got := fmt.Sprint(float64(hc.NanoCPUs), hc.Memory, *hc.PidsLimit); got != want {
			t.Errorf("size %q: limits %s; want %s", size, got, want)
		}

		if cc.Cmd[0] != foreverSeconds {
			t.Errorf("no deadline: sleep %v; want %s", cc.Cmd, foreverSeconds)
		}
	}

	if cc, _ := boxContainerOf(boxLimits[domain.SandboxSmall], DefaultNetwork, ports.SandboxSpec{Deadline: now.Add(-time.Minute)}, now); cc.Cmd[0] != "1" {
		t.Errorf("past deadline: sleep %v; want 1", cc.Cmd)
	}
}

func TestValidateBox_RefusesUnsafeSpecs(t *testing.T) {
	t.Parallel()

	for name, spec := range map[string]ports.SandboxSpec{
		"run id with a slash": {RunID: "../r1", Image: unitBoxImage},
		"no image":            {RunID: "r1"},
		"unknown size":        {RunID: "r1", Image: unitBoxImage, Size: "huge"},
	} {
		if _, err := validateBox(spec); err == nil {
			t.Errorf("%s: validateBox accepted %+v", name, spec)
		}
	}
}
