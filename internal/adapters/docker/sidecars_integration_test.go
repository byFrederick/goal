//go:build integration

package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	mcpStubImage = "goal-mcp-stub:test"
	stubSecret   = "postgres://reader:sidecar-s3cr3t-for-tests@db.example.com/shop" //nolint:gosec // a fake credential
)

var (
	mcpStubOnce     sync.Once
	errMCPStubBuild error
)

// newTestSidecars returns a sidecar manager on the local Docker that logs into logs, after
// building the stub MCP server image. It skips the test when Docker is not reachable.
func newTestSidecars(t *testing.T, logs *syncBuffer) (*Sidecars, *client.Client) {
	t.Helper()

	docker := newTestDocker(t)

	mcpStubOnce.Do(func() { errMCPStubBuild = buildMCPStub(t.Context(), t.TempDir(), docker) })

	if errMCPStubBuild != nil {
		t.Fatalf("build MCP stub image: %v", errMCPStubBuild)
	}

	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	return NewSidecars(docker, Config{Network: testNet(t, docker)}, log), docker
}

// buildMCPStub cross-compiles testdata/mcpstub for the Docker host and builds it into an image
// from scratch, so the build pulls nothing.
func buildMCPStub(ctx context.Context, dir string, docker *client.Client) error {
	v, err := docker.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return err
	}

	bin := filepath.Join(dir, "mcpstub")

	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", bin, "./testdata/mcpstub") //nolint:gosec // the test's own temp dir

	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+v.Arch)

	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}

	files := map[string][]byte{}

	for name, path := range map[string]string{"Dockerfile": filepath.Join("testdata", "mcpstub", "Dockerfile"), "mcpstub": bin} {
		body, err := os.ReadFile(path) //nolint:gosec // fixed test files
		if err != nil {
			return err
		}

		files[name] = body
	}

	return buildImage(docker, mcpStubImage, files)
}

func sidecarSpec(network bool) ports.SidecarSpec {
	return ports.SidecarSpec{
		OrgID: testOrg, FactoryID: "factory-1", ItemID: "item-1", RunID: domain.RunID(fmt.Sprintf("it-mcp-%d-%d", time.Now().UnixNano(), runSeq.Add(1))),
		Connection: "shop-db", Image: mcpStubImage, Command: []string{"--read-only", "--schema=public"},
		Env: map[string]string{"STUB_SECRET": stubSecret}, Network: network,
	}
}

// connectSidecar opens an MCP session over a started sidecar's stdio, as the gateway does.
func connectSidecar(t *testing.T, sc ports.Sidecar) *mcp.ClientSession {
	t.Helper()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "goal-gateway-test", Version: "1"}, nil).
		Connect(t.Context(), &mcp.IOTransport{Reader: sc, Writer: sc}, nil)
	if err != nil {
		t.Fatalf("MCP session over the sidecar: %v", err)
	}

	return cs
}

func probeSidecar(t *testing.T, cs *mcp.ClientSession) string {
	t.Helper()

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "probe", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call probe: %v", err)
	}

	text, ok := res.Content[0].(*mcp.TextContent)
	if res.IsError || !ok {
		t.Fatalf("probe result = %+v", res)
	}

	return text.Text
}

func sidecarContainers(t *testing.T, docker *client.Client, run domain.RunID) []string {
	t.Helper()

	list, err := docker.ContainerList(t.Context(), client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", LabelRun+"="+string(run)),
	})
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 0, len(list.Items))
	for _, c := range list.Items {
		ids = append(ids, c.ID)
	}

	return ids
}

func TestSidecars_ServeMCPOverStdioInAHardenedContainer(t *testing.T) {
	t.Parallel()

	for _, network := range []bool{false, true} {
		t.Run(fmt.Sprintf("network %v", network), func(t *testing.T) {
			t.Parallel()

			logs := &syncBuffer{}
			s, docker := newTestSidecars(t, logs)
			spec := sidecarSpec(network)

			sc, err := s.Start(t.Context(), spec)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}

			cs := connectSidecar(t, sc)

			tools, err := cs.ListTools(t.Context(), nil)
			if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "probe" {
				t.Fatalf("tools = %+v, %v; want the stub's probe", tools, err)
			}

			wantNet := map[bool]string{false: "network=none", true: "network=attached"}[network]
			probe := probeSidecar(t, cs)

			for _, want := range []string{
				"uid=65534", "rootfs=readonly", "tmp=writable", "home=/tmp", "secret=" + stubSecret,
				"args=--read-only,--schema=public", wantNet,
			} {
				if !strings.Contains(probe, want) {
					t.Errorf("sidecar probe %q lacks %q", probe, want)
				}
			}

			assertSidecarHardened(t, docker, s, spec)

			if err := cs.Close(); err != nil {
				t.Logf("close session: %v", err)
			}

			if err := sc.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			if left := sidecarContainers(t, docker, spec.RunID); len(left) != 0 {
				t.Errorf("containers left after Close: %v", left)
			}

			if l := logs.String(); strings.Contains(l, "sidecar-s3cr3t-for-tests") || !strings.Contains(l, "STUB_SECRET="+redacted) {
				t.Errorf("sidecar stderr was not logged redacted:\n%s", l)
			}
		})
	}
}

// assertSidecarHardened checks that the daemon runs the sidecar as the adapter configured it.
func assertSidecarHardened(t *testing.T, docker *client.Client, s *Sidecars, spec ports.SidecarSpec) {
	t.Helper()

	ids := sidecarContainers(t, docker, spec.RunID)
	if len(ids) != 1 {
		t.Fatalf("sidecar containers of the run = %v, want one", ids)
	}

	got, err := docker.ContainerInspect(t.Context(), ids[0], client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect sidecar: %v", err)
	}

	running := hardeningOf(got.Container.Config, got.Container.HostConfig)
	want := hardeningOf(sidecarContainerOf(s.cfg, agentsRoute(s.cfg.Network), spec, ""))
	running.Env, want.Env = "", "" // the image may add its own variables

	if running != want {
		t.Errorf("running sidecar:\n got %+v\nwant %+v", running, want)
	}

	hc := got.Container.HostConfig
	if wantMode := map[bool]string{false: "none", true: s.cfg.Network}[spec.Network]; string(hc.NetworkMode) != wantMode {
		t.Errorf("network mode = %q, want %q", hc.NetworkMode, wantMode)
	}

	if len(hc.Mounts) != 0 || len(hc.Binds) != 0 {
		t.Errorf("sidecar has mounts %v and binds %v, want none", hc.Mounts, hc.Binds)
	}

	if !slices.Contains(got.Container.Config.Env, "STUB_SECRET="+stubSecret) {
		t.Error("the sidecar's environment lacks its resolved credential")
	}
}

// Factories pin images as name:tag@digest; the adapter must inspect, pull and run that form.
func TestSidecars_RunTagAndDigestPinnedImages(t *testing.T) {
	t.Parallel()

	s, _ := newTestSidecars(t, &syncBuffer{})
	spec := sidecarSpec(false)
	spec.Image = "busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e" // the runner stub's base
	spec.Command = []string{"cat"}

	sc, err := s.Start(t.Context(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sc.Close() }()

	if _, err := sc.Write([]byte("{\"jsonrpc\":\"2.0\"}\n")); err != nil {
		t.Fatal(err)
	}

	got := make([]byte, 18)
	if _, err := io.ReadFull(sc, got); err != nil || string(got) != "{\"jsonrpc\":\"2.0\"}\n" {
		t.Fatalf("read %q, %v; want the line echoed through stdin and stdout", got, err)
	}
}

func TestSidecars_StartFailsCleanlyForAMissingImage(t *testing.T) {
	t.Parallel()

	s, docker := newTestSidecars(t, &syncBuffer{})
	spec := sidecarSpec(false)
	spec.Image = "goal-mcp-stub-does-not-exist:0.0.0"

	if _, err := s.Start(t.Context(), spec); err == nil {
		t.Fatal("Start succeeded with an image that does not exist")
	}

	if left := sidecarContainers(t, docker, spec.RunID); len(left) != 0 {
		t.Errorf("containers left after a failed start: %v", left)
	}
}

// A run's sidecars carry its labels, so the sandboxes list them with the run and delete them
// with its sandbox: the reaper needs no other port.
func TestSandboxes_DeleteRemovesTheRunsSidecars(t *testing.T) {
	t.Parallel()

	s, docker := newTestSidecars(t, &syncBuffer{})
	r := NewSandboxes(docker, s.cfg.Network, slog.New(slog.DiscardHandler))
	orphan, live := sidecarSpec(false), sidecarSpec(false)

	started := make([]ports.Sidecar, 0, 2)

	for _, spec := range []ports.SidecarSpec{orphan, live} {
		sc, err := s.Start(t.Context(), spec)
		if err != nil {
			t.Fatal(err)
		}

		started = append(started, sc)
	}

	left, err := r.List(t.Context())
	if err != nil || !slices.Contains(left, ports.Leftover{Run: orphan.RunID, Org: testOrg}) {
		t.Errorf("List = %v, %v; want the orphan's sidecar labelled with its organization", left, err)
	}

	// Other tests' and goald's containers carry the run label too; only the orphan's go.
	if err := r.Delete(t.Context(), orphan.RunID); err != nil {
		t.Errorf("Delete: %v", err)
	}

	if left := sidecarContainers(t, docker, orphan.RunID); len(left) != 0 {
		t.Errorf("orphaned sidecar still exists: %v", left)
	}

	if left := sidecarContainers(t, docker, live.RunID); len(left) != 1 {
		t.Errorf("live run's sidecars = %v, want its one sidecar kept", left)
	}

	// Closing a deleted sidecar is not an error: its container is already gone.
	for _, sc := range started {
		if err := sc.Close(); err != nil {
			t.Errorf("Close after Delete: %v", err)
		}
	}

	if left := sidecarContainers(t, docker, live.RunID); len(left) != 0 {
		t.Errorf("live run's sidecar still exists after Close: %v", left)
	}
}

// syncBuffer collects logs written from several goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
