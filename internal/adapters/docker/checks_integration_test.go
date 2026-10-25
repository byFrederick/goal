//go:build integration

package docker

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/ports"
)

func newTestChecker(t *testing.T) (*Checker, *client.Client) {
	t.Helper()

	r, docker := newTestRunner(t) // builds the stub image, skips without Docker

	return NewChecker(docker, Config{RunDir: r.cfg.RunDir, StopGrace: time.Second, Network: r.cfg.Network}, slog.New(slog.DiscardHandler)), docker
}

func checkSpec(t *testing.T, command string, timeout time.Duration) ports.CheckSpec {
	t.Helper()

	spec := stubSpec(t, "check")

	return ports.CheckSpec{
		FactoryID: spec.FactoryID, ItemID: spec.ItemID, RunID: spec.RunID, Station: spec.Station,
		Image: stubImage, Command: command, WorkspaceDir: spec.WorkspaceDir, Timeout: timeout,
	}
}

func TestChecker_RunsTheCommandInTheWorkspaceWithoutSecrets(t *testing.T) {
	t.Parallel()

	k, docker := newTestChecker(t)
	spec := checkSpec(t, `echo built > /workspace/built.txt; echo "uid=$(id -u) vars=$(env | grep -ci -e token -e key)"; `+
		`touch /rootfs-probe 2>/dev/null && echo rootfs=writable || echo rootfs=readonly; echo failing >&2; exit 3`, time.Minute)

	res, err := k.Check(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	if res.ExitCode != 3 || res.Passed() {
		t.Errorf("exit code = %d, want the command's 3", res.ExitCode)
	}

	for _, want := range []string{"uid=1000 vars=0", "rootfs=readonly", "failing"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output %q lacks %q", res.Output, want)
		}
	}

	if body, err := os.ReadFile(filepath.Join(spec.WorkspaceDir, "built.txt")); err != nil || string(body) != "built\n" {
		t.Errorf("workspace file = %q, %v; want the check's write", body, err)
	}

	assertNoCheckContainers(t, docker, spec)
}

func TestChecker_StopsACheckAtItsTimeout(t *testing.T) {
	t.Parallel()

	k, docker := newTestChecker(t)
	spec := checkSpec(t, "echo started; exec sleep 600", 2*time.Second)

	start := time.Now()

	res, err := k.Check(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	if res.ExitCode != exitTimeout || !strings.Contains(res.Output, "started") || !strings.Contains(res.Output, "timeout") {
		t.Errorf("result = %+v; want exit %d with the output so far and a timeout note", res, exitTimeout)
	}

	if took := time.Since(start); took > 30*time.Second {
		t.Errorf("check took %s, want it stopped near its 2s timeout", took)
	}

	assertNoCheckContainers(t, docker, spec)
}

func assertNoCheckContainers(t *testing.T, docker *client.Client, spec ports.CheckSpec) {
	t.Helper()

	list, err := docker.ContainerList(t.Context(), client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", LabelRun+"="+string(spec.RunID)),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(list.Items) != 0 {
		t.Errorf("%d check containers left for run %s", len(list.Items), spec.RunID)
	}
}
