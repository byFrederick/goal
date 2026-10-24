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

	cc, _ := checkContainerOf(Config{}.withDefaults(), spec)

	want := "map[goal.check:make test goal.factory:f1 goal.item:i1 goal.org:o1 goal.run:r1 goal.station:build]"
	if got := fmt.Sprint(cc.Labels); got != want {
		t.Errorf("check container labels = %s; want %s", got, want)
	}
}
