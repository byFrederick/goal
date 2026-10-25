//go:build integration

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	// testOrg labels the tests' containers; no store knows it.
	testOrg      = "org-reap-it"
	stubImage    = "goal-runner-stub:test"
	oauth        = "oauth-token-for-tests-only"
	gatewayToken = "gateway-token-for-tests-only"
	// stubGateway is the gateway stub tasks name; the shared test network's proxy maps it to its own address.
	stubGateway = "http://127.0.0.1:8081/mcp"
)

var (
	stubOnce     sync.Once
	runSeq       atomic.Int64
	errStubBuild error
)

// newTestRunner returns a runner on the local Docker with its own run folder, after building the
// stub worker image. It skips the test when Docker is not reachable.
func newTestRunner(t *testing.T) (*Runner, *client.Client) {
	t.Helper()

	docker, err := client.New(client.FromEnv)
	if err != nil {
		t.Skipf("docker client: %v", err)
	}

	t.Cleanup(func() { _ = docker.Close() })

	if _, err := docker.Ping(t.Context(), client.PingOptions{}); err != nil {
		t.Skipf("docker is not reachable: %v", err)
	}

	stubOnce.Do(func() { errStubBuild = buildStub(docker) })

	if errStubBuild != nil {
		t.Fatalf("build stub image: %v", errStubBuild)
	}

	return New(docker, promptFiles{}, Config{RunDir: t.TempDir(), StopGrace: time.Second, Network: testNet(t, docker)}, slog.New(slog.DiscardHandler)), docker
}

func buildStub(docker *client.Client) error {
	files := map[string][]byte{}

	for _, name := range []string{"Dockerfile", "stub.sh"} {
		body, err := os.ReadFile(filepath.Join("testdata", "stub", name)) //nolint:gosec // fixed test files
		if err != nil {
			return err
		}

		files[name] = body
	}

	return buildImage(docker, stubImage, files)
}

// buildImage builds tag from a build context of the given files.
func buildImage(docker *client.Client, tag string, files map[string][]byte) error {
	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			return err
		}

		if _, err := tw.Write(body); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}

	res, err := docker.ImageBuild(context.Background(), &buf, client.ImageBuildOptions{Tags: []string{tag}, Remove: true})
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	dec := json.NewDecoder(res.Body)

	for {
		var msg struct {
			Error string `json:"error"`
		}

		if err := dec.Decode(&msg); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		if msg.Error != "" {
			return errors.New(msg.Error)
		}
	}
}

// promptFiles writes a one-line prompt, as goald writes an agent's files.
type promptFiles struct{}

func (promptFiles) WriteAgentFiles(_ context.Context, t ports.TaskSpec, dir string) error {
	return os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("prompt-of-"+t.Agent), 0o644) //nolint:gosec // read by uid 1000
}

func stubSpec(t *testing.T, brief string) ports.TaskSpec {
	t.Helper()

	workspace := t.TempDir()
	// The container's uid 1000 must be able to write the workspace whoever owns it on the host.
	if err := os.Chmod(workspace, 0o777); err != nil { //nolint:gosec // test workspace, see above
		t.Fatal(err)
	}

	return ports.TaskSpec{
		OrgID: testOrg, FactoryID: "factory-1", ItemID: "item-1", RunID: domain.RunID(fmt.Sprintf("it-%s-%d-%d", brief, time.Now().UnixNano(), runSeq.Add(1))),
		Station: "build", Agent: "engineer", Image: stubImage, Brief: brief, WorkspaceDir: workspace,
		GatewayURL: stubGateway, GatewayToken: gatewayToken,
		Env: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": oauth, "ANTHROPIC_API_KEY": "sk-ant-must-not-reach-the-task"},
	}
}

// run starts a task and collects its events until Wait returns.
func run(ctx context.Context, t *testing.T, r *Runner, spec ports.TaskSpec, onStart func(ports.RunEvent)) ([]ports.RunEvent, domain.RunResult) {
	t.Helper()

	task, err := r.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var (
		events []ports.RunEvent
		drain  sync.WaitGroup
	)

	drain.Go(func() {
		for ev := range task.Events() {
			if ev.Kind == domain.EventAgentStarted && onStart != nil {
				onStart(ev)
			}

			events = append(events, ev)
		}
	})

	res, err := task.Wait()
	drain.Wait()

	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	return events, res
}

func assertCleanedUp(t *testing.T, docker *client.Client, r *Runner, spec ports.TaskSpec) {
	t.Helper()

	if _, err := docker.ContainerInspect(t.Context(), containerName(spec.RunID), client.ContainerInspectOptions{}); !cerrdefs.IsNotFound(err) {
		t.Errorf("container still exists after Wait (inspect error %v)", err)
	}

	if _, err := os.Stat(r.runDir(spec.RunID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run dir still exists after Wait (stat error %v)", err)
	}
}

func TestRunner_StreamsEventsAndReturnsResult(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)
	spec := stubSpec(t, "succeed")

	events, res := run(t.Context(), t, r, spec, nil)

	if got := fmt.Sprintf("%s %s %d %.2f %s", res.Status, res.Output, res.Turns, res.CostUSD, res.SessionID); got != `succeeded {"ok":true} 2 0.25 stub` {
		t.Fatalf("result = %s", got)
	}

	var kinds []domain.EventKind
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}

	if want := []domain.EventKind{domain.EventAgentStarted, domain.EventToolCall, domain.EventToolResult, domain.EventAgentMessage, domain.EventUsage}; !slices.Equal(kinds, want) {
		t.Fatalf("event kinds = %v; want %v", kinds, want)
	}

	probe := fmt.Sprint(events[2].Data["summary"])
	for _, want := range []string{"api_key=absent", "uid=1000", "rootfs=readonly", "workspace=writable", "goal=readonly", "home=/home/node", "prompt=prompt-of-engineer"} {
		if !strings.Contains(probe, want) {
			t.Errorf("container probe %q lacks %q", probe, want)
		}
	}

	if msg := fmt.Sprint(events[3].Data["text"]); strings.Contains(msg, oauth) || strings.Contains(msg, gatewayToken) || !strings.Contains(msg, redacted) {
		t.Errorf("secrets reached an event: %q", msg)
	}

	if body, err := os.ReadFile(filepath.Join(spec.WorkspaceDir, "stub.txt")); err != nil || string(body) != "done\n" {
		t.Errorf("workspace file = %q, %v; want the task's write", body, err)
	}

	assertCleanedUp(t, docker, r, spec)
}

func TestRunner_TimeoutKillsTheTask(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)
	spec := stubSpec(t, "sleep")
	spec.Timeout = 2 * time.Second

	start := time.Now()
	_, res := run(t.Context(), t, r, spec, nil)

	if res.Status != domain.RunFailed || !strings.Contains(res.Error, "timed out after 2s") {
		t.Fatalf("result = %+v; want failed with a timeout", res)
	}

	if took := time.Since(start); took > 30*time.Second {
		t.Errorf("the task took %s to end after a 2s timeout", took)
	}

	assertCleanedUp(t, docker, r, spec)
}

func TestRunner_CancelStopsHardenedTask(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)
	spec := stubSpec(t, "sleep")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, res := run(ctx, t, r, spec, func(ports.RunEvent) {
		assertHardened(t, docker, r, spec)
		cancel()
	})

	if res.Status != domain.RunCancelled {
		t.Fatalf("result = %+v; want cancelled", res)
	}

	assertCleanedUp(t, docker, r, spec)
}

// assertHardened checks that the daemon runs the task's container as the runner configured it.
func assertHardened(t *testing.T, docker *client.Client, r *Runner, spec ports.TaskSpec) {
	t.Helper()

	got, err := docker.ContainerInspect(t.Context(), containerName(spec.RunID), client.ContainerInspectOptions{})
	if err != nil {
		t.Errorf("inspect running task: %v", err)
		return
	}

	running := hardeningOf(got.Container.Config, got.Container.HostConfig)
	want := hardeningOf(containerOf(r.cfg, agentsRoute(r.cfg.Network), spec, r.runDir(spec.RunID)))
	running.Env, want.Env = "", "" // the image adds its own variables, such as PATH

	if running != want {
		t.Errorf("running container:\n got %+v\nwant %+v", running, want)
	}

	for _, kv := range got.Container.Config.Env {
		if strings.HasPrefix(kv, envAPIKey+"=") {
			t.Errorf("container env has %s", envAPIKey)
		}
	}
}

func TestRunner_CrashReportsRedactedStderr(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)
	spec := stubSpec(t, "crash")

	_, res := run(t.Context(), t, r, spec, nil)

	if res.Status != domain.RunFailed || !strings.Contains(res.Error, "code 3") || !strings.Contains(res.Error, "boom: token "+redacted) {
		t.Fatalf("result = %+v; want failed with the redacted stderr tail", res)
	}

	assertCleanedUp(t, docker, r, spec)
}

// Several goalds may share one Docker host, so Leftovers lists every run's containers, labelled
// with the run's organization, and its run folder, while Reap removes only the runs it is given:
// here this test's orphan, never another test's or goald's containers.
// createOrphan creates spec's task container, unstarted, as a goald that died would leave it;
// it goes when the test ends should Reap not remove it.
func createOrphan(t *testing.T, r *Runner, docker *client.Client, spec ports.TaskSpec, runDir string) {
	t.Helper()

	cc, hc := containerOf(r.cfg, agentsRoute(r.cfg.Network), spec, runDir)
	if _, err := docker.ContainerCreate(t.Context(), client.ContainerCreateOptions{Config: cc, HostConfig: hc, Name: containerName(spec.RunID)}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = docker.ContainerRemove(context.Background(), containerName(spec.RunID), client.ContainerRemoveOptions{Force: true})
	})
}

func TestRunner_ReapRemovesOnlyTheGivenRuns(t *testing.T) {
	t.Parallel()

	r, docker := newTestRunner(t)
	orphan := stubSpec(t, "sleep")
	orphanDir := r.runDir(orphan.RunID)

	if err := os.MkdirAll(orphanDir, 0o755); err != nil { //nolint:gosec // test run dir
		t.Fatal(err)
	}

	createOrphan(t, r, docker, orphan, orphanDir)

	live := stubSpec(t, "sleep")
	ctx, cancel := context.WithCancel(t.Context())

	task, err := r.Start(ctx, live)
	if err != nil {
		t.Fatal(err)
	}

	left, err := r.Leftovers(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []ports.Leftover{{Run: orphan.RunID, Org: testOrg}, {Run: orphan.RunID}, {Run: live.RunID, Org: testOrg}, {Run: live.RunID}} {
		if !slices.Contains(left, want) {
			t.Errorf("Leftovers lacks %+v (container labelled with its organization, or run folder)", want)
		}
	}

	if err := r.Reap(t.Context(), []domain.RunID{orphan.RunID}); err != nil {
		t.Errorf("Reap: %v", err)
	}

	assertCleanedUp(t, docker, r, orphan)

	if _, err := docker.ContainerInspect(t.Context(), containerName(live.RunID), client.ContainerInspectOptions{}); err != nil {
		t.Errorf("Reap removed another run's container: %v", err)
	}

	if _, err := os.Stat(r.runDir(live.RunID)); err != nil {
		t.Errorf("Reap removed another run's folder: %v", err)
	}

	cancel()

	for range task.Events() {
	}

	if res, err := task.Wait(); err != nil || res.Status != domain.RunCancelled {
		t.Errorf("live task = %+v, %v", res, err)
	}
}
