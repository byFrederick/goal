package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	shopDB = "shop-db"
	dbURL  = "postgres://reader:acme-db-s3cr3t-42@db.example.com/shop" //nolint:gosec // a fake credential
)

// fakeSidecars runs each sidecar's MCP server in process over a pipe, as the Docker adapter runs
// it in a container over the container's stdio, and records what it was asked to start.
type fakeSidecars struct {
	mu    sync.Mutex
	specs []ports.SidecarSpec
	live  int
	fail  error
}

func (f *fakeSidecars) Start(_ context.Context, spec ports.SidecarSpec) (ports.Sidecar, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.specs = append(f.specs, spec)
	if f.fail != nil {
		return nil, f.fail
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "acme-shop-db", Version: "1"}, nil)
	anyInput := json.RawMessage(`{"type":"object"}`)
	reply := func(text string) mcp.ToolHandler {
		return func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
		}
	}

	srv.AddTool(&mcp.Tool{Name: "query", InputSchema: anyInput}, reply("1 row"))
	// env answers with the credential the sidecar was started with, as a server echoing its
	// connection string in an error would.
	srv.AddTool(&mcp.Tool{Name: "env", InputSchema: anyInput}, reply("DATABASE_URL="+spec.Env["DATABASE_URL"]))
	srv.AddTool(&mcp.Tool{Name: "drop_table", InputSchema: anyInput}, reply("dropped"))

	client, server := net.Pipe()
	if _, err := srv.Connect(context.Background(), &mcp.IOTransport{Reader: server, Writer: server}, nil); err != nil {
		return nil, err
	}

	f.live++

	return &fakeSidecar{Conn: client, f: f}, nil
}

func (f *fakeSidecars) started() []ports.SidecarSpec {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.specs)
}

func (f *fakeSidecars) running() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.live
}

func (f *fakeSidecars) failWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fail = err
}

type fakeSidecar struct {
	net.Conn

	f    *fakeSidecars
	once sync.Once
}

func (s *fakeSidecar) Close() error {
	s.once.Do(func() {
		s.f.mu.Lock()
		s.f.live--
		s.f.mu.Unlock()

		_ = s.Conn.Close()
	})

	return nil
}

func callText(t *testing.T, cs *mcp.ClientSession, name string) (string, bool) {
	t.Helper()

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}

	raw, _ := json.Marshal(res.Content)

	return string(raw), res.IsError
}

// eventually polls cond until it holds, failing the test after a few seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()

	for range 200 {
		if cond() {
			return
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("timed out waiting until %s", what)
}

func TestGateway_SidecarStartsOnceForTheRunAndStopsWhenTheRunEnds(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, legacyProtocol)

	if _, err := cs.ListTools(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if text, isErr := callText(t, cs, "shop-db__query"); isErr || !strings.Contains(text, "1 row") {
			t.Fatalf("shop-db__query = %s (error %v), want the sidecar's answer", text, isErr)
		}
	}

	specs := h.sidecars.started()
	if len(specs) != 1 {
		t.Fatalf("sidecars started = %d, want one reused for the whole run", len(specs))
	}

	want := ports.SidecarSpec{
		FactoryID: factoryID, ItemID: itemID, RunID: "run-1", Connection: shopDB, Image: "ghcr.io/acme/shop-db-mcp:1.4.2",
		Command: []string{"--read-only", dbURL}, Env: map[string]string{"DATABASE_URL": dbURL, "LOG_LEVEL": "info"}, Network: true,
	}
	if got := specs[0]; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sidecar spec = %+v, want %+v (references resolved on the host)", got, want)
	}

	if e := h.events(); len(e) != 2 || e[0].Data["connection"] != shopDB || e[0].Data["outcome"] != outcomeOK {
		t.Fatalf("events = %+v, want two audited sidecar calls", e)
	}

	h.gw.EndRun(t.Context(), "other-run")

	if n := h.sidecars.running(); n != 1 {
		t.Fatalf("ending another run left %d sidecars, want 1", n)
	}

	h.gw.EndRun(t.Context(), "run-1")

	if n := h.sidecars.running(); n != 0 {
		t.Fatalf("sidecars running after the run ended = %d, want 0", n)
	}
}

func TestGateway_SidecarCredentialsNeverReachTheAgentLogsOrEvents(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, legacyProtocol)

	text, _ := callText(t, cs, "shop-db__env")

	if !strings.Contains(text, "DATABASE_URL="+redacted) {
		t.Errorf("result = %s, want the credential redacted", text)
	}

	h.sidecars.failWith(errors.New("pull ghcr.io/acme/shop-db-mcp: denied for " + dbURL))
	h.gw.EndRun(t.Context(), "run-1")

	failed, isErr := callText(t, cs, "shop-db__query")
	if !isErr {
		t.Fatalf("call with a failing sidecar = %s, want a tool error", failed)
	}

	for where, s := range map[string]string{
		"results": text + failed,
		"logs":    h.logs.String(),
		"events":  fmt.Sprintf("%+v", h.events()),
	} {
		if strings.Contains(s, "acme-db-s3cr3t-42") {
			t.Errorf("the sidecar's credential leaked into %s:\n%s", where, s)
		}
	}
}

func TestGateway_SidecarThatFailsToStartIsUnavailable(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	h.sidecars.failWith(errors.New("image not found"))
	cs := h.connect(t, legacyProtocol)

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list failed with the sidecar: %v", err)
	}

	status, _ := res.Meta[metaConnections].(map[string]any)
	if status[shopDB] != "unavailable: "+reasonUpstream || status[tracker] != "available" {
		t.Fatalf("connection status = %v, want shop-db unavailable and tracker served", status)
	}

	if names := toolNames(res); !slices.Contains(names, "tracker__echo") || slices.Contains(names, "shop-db__query") {
		t.Fatalf("tools = %v, want tracker's and none of shop-db's", names)
	}

	// The failure is not remembered: the next request starts the sidecar again.
	h.sidecars.failWith(nil)

	if text, isErr := callText(t, cs, "shop-db__query"); isErr {
		t.Fatalf("call after the sidecar recovered = %s, want it served", text)
	}
}

func TestGateway_IdleSidecarStops(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{SidecarIdle: 50 * time.Millisecond})
	cs := h.connect(t, legacyProtocol)

	callText(t, cs, "shop-db__query")
	eventually(t, "the idle sidecar stopped", func() bool { return h.sidecars.running() == 0 })

	callText(t, cs, "shop-db__query")

	if n := len(h.sidecars.started()); n != 2 {
		t.Fatalf("sidecars started = %d, want a new one after the idle stop", n)
	}
}

func TestGateway_SidecarsAreCappedPerInstance(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{MaxSidecars: 1})
	first := h.connect(t, legacyProtocol)

	callText(t, first, "shop-db__query")

	other := h.claims()
	other.RunID = "run-2"

	if text, isErr := callText(t, h.connectAs(t, legacyProtocol, other), "shop-db__query"); !isErr ||
		!strings.Contains(text, "as many MCP sidecars as it may") {
		t.Fatalf("second run's call = %s (error %v), want it refused at the cap", text, isErr)
	}

	h.gw.EndRun(t.Context(), domain.RunID("run-1"))

	if text, isErr := callText(t, h.connectAs(t, legacyProtocol, other), "shop-db__query"); isErr {
		t.Fatalf("second run's call after the first ended = %s, want it served", text)
	}
}
