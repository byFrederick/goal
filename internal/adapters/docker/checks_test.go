package docker

import (
	"fmt"
	"testing"

	"github.com/byFrederick/goal/internal/ports"
)

// A check container carries its run's labels, organization included, so a reaper finds it with
// the run's other containers and knows which organization's store to ask.
func TestCheckContainerOf_CarriesTheRunsLabels(t *testing.T) {
	t.Parallel()

	spec := ports.CheckSpec{
		OrgID: "o1", FactoryID: "f1", ItemID: "i1", RunID: "r1", Station: "build", Image: "img:1", Command: "make test",
		WorkspaceDir: "/work/r1",
	}

	cc, _ := checkContainerOf(Config{}.withDefaults(), unitEgress(t).tools(), spec)

	want := "map[goal.check:make test goal.factory:f1 goal.item:i1 goal.org:o1 goal.run:r1 goal.station:build]"
	if got := fmt.Sprint(cc.Labels); got != want {
		t.Errorf("check container labels = %s; want %s", got, want)
	}
}

// A check keeps network access for its toolchain, but only on the tools' network, through the
// egress proxy.
func TestCheckContainerOf_GoesOutThroughTheProxy(t *testing.T) {
	t.Parallel()

	spec := ports.CheckSpec{RunID: "r2", Image: "ghcr.io/acme/toolchain:2", Command: "go test ./...", WorkspaceDir: "/work/r2"}

	cc, hc := checkContainerOf(Config{}.withDefaults(), unitEgress(t).tools(), spec)

	if got := fmt.Sprint(cc.Env); got != "[HOME=/home/node CI=true "+proxyVars+"]" {
		t.Errorf("check env = %s", got)
	}

	if hc.NetworkMode != "goal-egress-tools" || len(hc.ExtraHosts) != 0 {
		t.Errorf("network %q, extra hosts %v; want the tools' network and no host alias", hc.NetworkMode, hc.ExtraHosts)
	}
}
