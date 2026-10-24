package docker

import (
	"log/slog"
	"testing"

	"github.com/byFrederick/goal/internal/ports"
)

func TestSidecarContainerOf_IsHardened(t *testing.T) {
	t.Parallel()

	spec := ports.SidecarSpec{
		OrgID: "o1", FactoryID: "f1", ItemID: "i1", RunID: "r1", Connection: "shop-db", Image: "ghcr.io/acme/shop-db-mcp:1.4.2",
		Command: []string{"--read-only"}, Env: map[string]string{"SHOP_TOKEN": "token-123456"},
	}
	cfg := NewSidecars(nil, Config{}, slog.New(slog.DiscardHandler)).cfg

	cc, hc := sidecarContainerOf(cfg, spec, "")
	got := hardeningOf(cc, hc)
	want := hardening{
		User: "65534:65534", CapDrop: "[ALL]", SecurityOpt: "[no-new-privileges:true]", Mounts: "[]",
		Labels:     "map[goal.connection:shop-db goal.factory:f1 goal.item:i1 goal.org:o1 goal.run:r1]",
		Env:        "[HOME=/tmp SHOP_TOKEN=token-123456]",
		Tmpfs:      "map[/tmp:rw,nosuid,nodev,size=1g,mode=1777]",
		Privileged: false, ReadOnlyRoot: true, Init: true,
		CPUs: 1e9, Memory: 2 << 30, Swap: 2 << 30, PIDs: 512,
	}

	if got != want {
		t.Errorf("sidecar hardening:\n got %+v\nwant %+v", got, want)
	}

	if hc.NetworkMode != "none" || len(hc.ExtraHosts) != 0 || !cc.OpenStdin || !cc.StdinOnce {
		t.Errorf("network %q, extra hosts %v, stdin open %v once %v; want no network and stdin for the gateway",
			hc.NetworkMode, hc.ExtraHosts, cc.OpenStdin, cc.StdinOnce)
	}

	spec.Network = true
	if _, hc := sidecarContainerOf(cfg, spec, ""); hc.NetworkMode != "" || len(hc.ExtraHosts) != 1 {
		t.Errorf("with network: mode %q, extra hosts %v; want the default bridge with the host alias", hc.NetworkMode, hc.ExtraHosts)
	}
}

func TestSidecarUserOf_NeverRoot(t *testing.T) {
	t.Parallel()

	for image, want := range map[string]string{
		"":          sidecarUser,
		"root":      sidecarUser,
		"0":         sidecarUser,
		"0:0":       sidecarUser,
		"root:app":  sidecarUser,
		"node":      "node",
		"1000:1000": "1000:1000",
		"app":       "app",
	} {
		if got := sidecarUserOf(image); got != want {
			t.Errorf("sidecarUserOf(%q) = %q, want %q", image, got, want)
		}
	}
}
