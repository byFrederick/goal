//go:build integration

package docker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest/sandboxtest"
)

// boxImage is the suite's sandbox image: busybox's sh and curl.
const boxImage = "curlimages/curl:8.17.0@sha256:935d9100e9ba842cdb060de42472c7ca90cfe9a7c96e4dacb55e79e560b3ff40"

// publicURL answers on the public internet; the network cases need it.
const publicURL = "https://example.com/"

func newTestSandboxes(t *testing.T, gateway string) (*Sandboxes, *testNetwork, *client.Client) {
	t.Helper()

	docker := newTestDocker(t)
	n := ownNet(t, docker, gateway)

	return NewSandboxes(docker, n.name, slog.New(slog.DiscardHandler)), n, docker
}

// privateURLs are where a box must not get to: a listener on the host, the host by its other
// names, RustFS (its published port, and its container when compose runs it), private and
// metadata addresses. https, so curl fails when the proxy refuses the tunnel (plain http gets
// the proxy's 403: TestSandboxes_NoRouteButTheProxy).
func privateURLs(t *testing.T, docker *client.Client, host *hostListener) []string {
	t.Helper()

	urls := []string{
		"https://host.docker.internal:" + host.port() + "/", "https://host.docker.internal:9000/health",
		"https://169.254.169.254/latest/meta-data/", "https://10.0.0.1/",
	}

	for _, ip := range hostAddresses(t, docker) {
		urls = append(urls, "https://"+ip+":"+host.port()+"/")
	}

	list, err := docker.ContainerList(t.Context(), client.ContainerListOptions{Filters: make(client.Filters).Add("label", "com.docker.compose.service=rustfs")})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range list.Items {
		for _, ep := range c.NetworkSettings.Networks {
			if ep.IPAddress.IsValid() {
				urls = append(urls, "https://"+ep.IPAddress.String()+":9000/health")
			}
		}
	}

	return urls
}

func TestSandboxes_Conformance(t *testing.T) {
	t.Parallel()

	s, _, docker := newTestSandboxes(t, "")
	if err := ensureImage(t.Context(), docker, slog.New(slog.DiscardHandler), boxImage); err != nil {
		t.Fatal(err)
	}

	host := newHostListener(t, text("secret"))
	private := privateURLs(t, docker, host)

	sandboxtest.Run(t, func(*testing.T) sandboxtest.Harness {
		return sandboxtest.Harness{
			Sandboxes: s, Image: boxImage,
			Network: &sandboxtest.Network{PublicURL: publicURL, PrivateURLs: private},
		}
	})

	if n := host.hits.Load(); n != 0 {
		t.Errorf("the host listener got %d requests from boxes", n)
	}
}

// newBox starts a sandbox of a new run, deleted when the test ends.
func newBox(t *testing.T, s *Sandboxes, deadline time.Time) ports.Sandbox {
	t.Helper()

	spec := ports.SandboxSpec{
		OrgID: testOrg, FactoryID: "factory-1", ItemID: "item-1", RunID: domain.RunID("it-box-" + uuid.NewString()),
		Station: "review", Agent: "builder", Image: boxImage, Size: domain.SandboxSmall, Deadline: deadline,
	}

	b, err := s.Create(t.Context(), spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Cleanup(func() { _ = s.Delete(context.Background(), spec.RunID) })

	return b
}

// output runs a command to its end and returns its stdout and how it ended.
func output(t *testing.T, b ports.Sandbox, argv ...string) (string, ports.ExitStatus) {
	t.Helper()

	p, err := b.Exec(t.Context(), ports.Exec{Argv: argv, Timeout: time.Minute})
	if err != nil {
		t.Fatalf("Exec %q: %v", argv, err)
	}

	out, _ := io.ReadAll(p.Stdout())

	st, err := p.Wait()
	if err != nil {
		t.Fatalf("Exec %q: Wait: %v", argv, err)
	}

	return strings.TrimSpace(string(out)), st
}

// processes lists the command lines of the box's processes.
func processes(t *testing.T, b ports.Sandbox) string {
	t.Helper()

	out, _ := output(t, b, "sh", "-c", `for p in /proc/[0-9]*; do tr '\0' ' ' <"$p/cmdline" 2>/dev/null; echo; done`)

	return out
}

// Docker cannot end an exec, so the adapter kills the command and the processes it started,
// whether its ctx ends or its timeout passes.
func TestSandboxes_CancelAndTimeoutKillTheCommandAndItsChildren(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestSandboxes(t, "")
	b := newBox(t, s, time.Now().Add(time.Hour))

	for name, sleep := range map[string]string{"cancel": "sleep 777", "timeout": "sleep 778"} {
		ctx, cancel := context.WithCancel(t.Context())

		e := ports.Exec{Argv: []string{"sh", "-c", sleep + " & echo started; wait"}}
		if name == "timeout" {
			e.Timeout = 2 * time.Second
		}

		p, err := b.Exec(ctx, e)
		if err != nil {
			t.Fatal(err)
		}

		line, _ := bufio.NewReader(p.Stdout()).ReadString('\n')
		if line != "started\n" {
			t.Fatalf("%s: first line %q", name, line)
		}

		if name == "cancel" {
			cancel()
		}

		_, _ = io.Copy(io.Discard, p.Stdout())
		st, err := p.Wait()

		cancel()

		switch {
		case name == "cancel" && !errors.Is(err, context.Canceled):
			t.Errorf("cancel: Wait = %+v, %v; want context.Canceled", st, err)
		case name == "timeout" && (err != nil || !st.TimedOut):
			t.Errorf("timeout: Wait = %+v, %v; want TimedOut", st, err)
		}

		if ps := processes(t, b); strings.Contains(ps, sleep) {
			t.Errorf("%s: %q still runs in the box:\n%s", name, sleep, ps)
		}
	}
}

// A cancelled command whose stdout nobody reads any more still ends: Wait returns.
func TestSandboxes_CancelEndsACommandNobodyReads(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestSandboxes(t, "")
	b := newBox(t, s, time.Now().Add(time.Hour))

	ctx, cancel := context.WithCancel(t.Context())

	p, err := b.Exec(ctx, ports.Exec{Argv: []string{"yes"}})
	if err != nil {
		t.Fatal(err)
	}

	cancel()

	done := make(chan error, 1)

	go func() {
		_, err := p.Wait()
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Wait = %v; want context.Canceled", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("Wait did not return for a cancelled command nobody reads")
	}

	if ps := processes(t, b); strings.Contains(ps, "yes") {
		t.Errorf("yes still runs in the box:\n%s", ps)
	}
}

// A box has no route but the egress proxy: nothing direct, even to the internet; through the
// proxy no private address over plain http either, and goald's gateway at its paths only.
func TestSandboxes_NoRouteButTheProxy(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		seen []string
	)

	gateway := newHostListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()

		_, _ = io.WriteString(w, "gateway")
	}))
	secret := newHostListener(t, text("secret"))

	s, _, _ := newTestSandboxes(t, gateway.hostURL())
	b := newBox(t, s, time.Now().Add(time.Hour))

	status := func(args ...string) string {
		out, _ := output(t, b, append([]string{"curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10"}, args...)...)
		return out
	}

	if _, st := output(t, b, "curl", "-sS", "--noproxy", "*", "--max-time", "10", publicURL); st.Code == 0 {
		t.Error("a box reached the internet without the proxy")
	}

	for _, url := range []string{
		"http://host.docker.internal:" + secret.port() + "/", "http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/",
	} {
		if got := status(url); got != "403" {
			t.Errorf("plain http to %s = %s; want the proxy's 403", url, got)
		}
	}

	proxy := "http://" + proxyHost + ":" + proxyPort
	for path, want := range map[string]string{"/mcp": "200", "/run": "200", "/v1/me": "404", "/api/auth/ok": "404"} {
		if got := status("-X", "POST", proxy+path); got != want {
			t.Errorf("POST %s through the proxy = %s; want %s", path, got, want)
		}
	}

	if n := secret.hits.Load(); n != 0 {
		t.Errorf("the host listener got %d requests from a box", n)
	}

	mu.Lock()
	defer mu.Unlock()

	if got := strings.Join(seen, ", "); !strings.Contains(got, "POST /mcp") || !strings.Contains(got, "POST /run") || len(seen) != 2 {
		t.Errorf("gateway saw %s; want POST /mcp and POST /run only", got)
	}
}

// A box no goald deletes ends at its deadline: its process exits, and its commands with it.
func TestSandboxes_BoxEndsAtItsDeadline(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestSandboxes(t, "")
	b := newBox(t, s, time.Now().Add(3*time.Second))

	if out, st := output(t, b, "echo", "up"); out != "up" || st.Code != 0 {
		t.Fatalf("before the deadline: %q, %+v", out, st)
	}

	time.Sleep(6 * time.Second)

	p, err := b.Exec(t.Context(), ports.Exec{Argv: []string{"echo", "up"}})
	if err == nil {
		_, _ = io.Copy(io.Discard, p.Stdout())
		if st, err := p.Wait(); err == nil {
			t.Errorf("after the deadline Exec ran: %+v; want an error", st)
		}
	}
}
