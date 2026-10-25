package docker

import (
	"fmt"
	"log/slog"
	"strings"
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
	tools := unitRoute()

	cc, hc := sidecarContainerOf(cfg, tools, spec, "")
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
	spec.Env["HTTPS_PROXY"] = "http://attacker.example.com:3128"

	cc, hc = sidecarContainerOf(cfg, tools, spec, "")
	if hc.NetworkMode != DefaultNetwork || len(hc.ExtraHosts) != 0 {
		t.Errorf("with network: mode %q, extra hosts %v; want the agents' network and no host alias", hc.NetworkMode, hc.ExtraHosts)
	}

	if got := fmt.Sprint(cc.Env); got != "[HOME=/tmp "+upperProxyVars+" SHOP_TOKEN=token-123456 "+lowerProxyVars+"]" {
		t.Errorf("with network: env %s; want the egress proxy's variables over the spec's", got)
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

func TestRedactor_HidesSecretsInNestedEventData(t *testing.T) {
	t.Parallel()

	r := newRedactor("sk-oauth-0123456789", "gateway-token-xyz", "true")
	data := map[string]any{
		"summary": "CLAUDE_CODE_OAUTH_TOKEN=sk-oauth-0123456789\n",
		"input":   map[string]any{"headers": []any{"Bearer gateway-token-xyz", 3.0}},
		"flag":    "true",
	}

	r.value(data)

	got := fmt.Sprint(data)
	if strings.Contains(got, "sk-oauth") || strings.Contains(got, "gateway-token") {
		t.Errorf("secrets leaked: %s", got)
	}

	if data["flag"] != "true" {
		t.Errorf("short values must not be redacted: %v", data["flag"])
	}
}

func TestEachLine_DropsOverlongLinesAndKeepsReading(t *testing.T) {
	t.Parallel()

	in := "first\n" + strings.Repeat("x", 200_000) + "\n\n  second  \nlast-without-newline"

	var got []string

	err := eachLine(strings.NewReader(in), 100_000, func(b []byte) { got = append(got, string(b)) })
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"first", "second", "last-without-newline"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("lines = %q; want %q", got, want)
	}
}
