// Package sandboxtest is the conformance suite for ports.Sandboxes. The in-memory fake in porttest
// and every adapter (docker, cloudflare) run it, so engine tests on the fake hold for real
// sandboxes: one sandbox per run, streamed output, exit codes and timeouts from the runtime, env
// per command, kill on cancel and on delete, idempotent delete, and a list for the reaper.
package sandboxtest

import (
	"bufio"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Harness is one Sandboxes under test. Other sandboxes may exist in it already (a Docker host
// shared with other tests); the suite never assumes it is empty.
type Harness struct {
	Sandboxes ports.Sandboxes
	// Image is the image the suite's sandboxes run: a POSIX sh with echo, printf, cat, pwd and
	// sleep, and curl for the network cases.
	Image string
	// Network runs the network cases when set; the in-memory fake has no network.
	Network *Network
}

// Network is what a sandbox must and must not reach.
type Network struct {
	// PublicURL is an https URL on the public internet that answers.
	PublicURL string
	// PrivateURLs must be unreachable from a sandbox: the host, the platform's own services,
	// private addresses and the metadata address.
	PrivateURLs []string
}

// Run runs the suite. Tests run in parallel and call newHarness once each.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"Create_SameRunIsTheSameSandbox", testCreateIdempotent},
		{"Exec_OutputAndExitCodes", testExitCodes},
		{"Exec_StderrTailIsTheLast8KiB", testStderrTail},
		{"Exec_StreamsOutputAndCancelKills", testStreamAndCancel},
		{"Exec_TimeoutKills", testTimeout},
		{"Exec_EnvPerCommand", testEnv},
		{"Exec_RunsInDir", testDir},
		{"Delete_EndsCommandsAndIsIdempotent", testDelete},
		{"List_EveryRunWithItsOrg", testList},
		{"Network_PublicHTTPSReachable", testPublic},
		{"Network_NoPrivateAddress", testPrivate},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.test(&fixture{Harness: newHarness(t), t: t})
		})
	}
}

// The suite's organizations, item and commands. Script answers exactly these commands.
const (
	acme    = domain.OrgID("019a4000-0000-7000-8000-000000000001")
	labs    = domain.OrgID("019a4000-0000-7000-8000-000000000002")
	factory = domain.FactoryID("019a4000-0000-7000-8000-000000000003")
	item    = domain.ItemID("019a4000-0000-7000-8000-000000000004")

	envBox  = "SANDBOXTEST_BOX"
	envExec = "SANDBOXTEST_EXEC"
	envOver = "SANDBOXTEST_OVER"

	scriptWrite  = "echo kept > /tmp/sandboxtest"
	scriptRead   = "cat /tmp/sandboxtest"
	scriptExit   = "echo out; echo oops >&2; exit 3"
	scriptOK     = "exit 0"
	scriptStderr = `i=0; while [ $i -lt 1024 ]; do echo 0123456789abcdef >&2; i=$((i+1)); done; echo end >&2`
	scriptLong   = "echo first; sleep 60; echo late"
	scriptEnv    = `printf '%s|%s|%s\n' "$SANDBOXTEST_BOX" "$SANDBOXTEST_EXEC" "$SANDBOXTEST_OVER"`
	scriptPwd    = "pwd"
)

// within is how long output may take to show and a killed command to end.
const within = 30 * time.Second

func sh(script string) []string { return []string{"sh", "-c", script} }

type fixture struct {
	Harness

	t *testing.T
}

// create starts a sandbox for a new run of org and deletes it when the test ends.
func (f *fixture) create(org domain.OrgID, env map[string]string) (ports.Sandbox, ports.SandboxSpec) {
	f.t.Helper()

	spec := ports.SandboxSpec{
		OrgID: org, FactoryID: factory, ItemID: item, RunID: domain.RunID(uuid.NewString()),
		Station: "build", Agent: "builder", Image: f.Image, Size: domain.SandboxSmall, Env: env,
		Deadline: time.Now().Add(time.Hour),
	}

	box, err := f.Sandboxes.Create(f.t.Context(), spec)
	if err != nil {
		f.t.Fatalf("Create: %v", err)
	}

	f.t.Cleanup(func() {
		if err := f.Sandboxes.Delete(context.WithoutCancel(f.t.Context()), spec.RunID); err != nil {
			f.t.Errorf("Delete at cleanup: %v", err)
		}
	})

	return box, spec
}

// exec starts a command.
func (f *fixture) exec(ctx context.Context, box ports.Sandbox, e ports.Exec) ports.Process {
	f.t.Helper()

	p, err := box.Exec(ctx, e)
	if err != nil {
		f.t.Fatalf("Exec %q: %v", e.Argv, err)
	}

	return p
}

// run runs a command to its end and returns its stdout and how it ended.
func (f *fixture) run(box ports.Sandbox, e ports.Exec) (string, ports.ExitStatus) {
	f.t.Helper()

	p := f.exec(f.t.Context(), box, e)

	out, ended := f.drain(lines(p))
	if !ended {
		f.t.Fatalf("Exec %q: stdout did not end", e.Argv)
	}

	st, err := f.wait(p)
	if err != nil {
		f.t.Fatalf("Exec %q: Wait: %v", e.Argv, err)
	}

	return strings.Join(out, "\n"), st
}

// lines streams p's stdout line by line until it ends.
func lines(p ports.Process) <-chan string {
	out := make(chan string)

	go func() {
		defer close(out)

		s := bufio.NewScanner(p.Stdout())
		for s.Scan() {
			out <- s.Text()
		}
	}()

	return out
}

// drain reads every line until the stream ends; ended is false when it has not after within.
func (f *fixture) drain(in <-chan string) (got []string, ended bool) {
	timer := time.NewTimer(within)
	defer timer.Stop()

	for {
		select {
		case line, ok := <-in:
			if !ok {
				return got, true
			}

			got = append(got, line)
		case <-timer.C:
			return got, false
		case <-f.t.Context().Done():
			return got, false
		}
	}
}

// next returns the next line, or false when none came within.
func (f *fixture) next(in <-chan string) (string, bool) {
	select {
	case line, ok := <-in:
		return line, ok
	case <-time.After(within):
		return "", false
	case <-f.t.Context().Done():
		return "", false
	}
}

// wait is p.Wait; the test fails when it has not returned within.
func (f *fixture) wait(p ports.Process) (ports.ExitStatus, error) {
	f.t.Helper()

	type result struct {
		st  ports.ExitStatus
		err error
	}

	done := make(chan result, 1)

	go func() {
		st, err := p.Wait()
		done <- result{st, err}
	}()

	select {
	case r := <-done:
		return r.st, r.err
	case <-time.After(within):
		f.t.Fatalf("Wait did not return within %s", within)

		return ports.ExitStatus{}, nil
	}
}

// A second Create for a run returns the sandbox the first made, env and all: what one handle
// writes, the other reads, and the run is listed once.
func testCreateIdempotent(f *fixture) {
	first, spec := f.create(acme, map[string]string{envBox: "box"})

	again := spec
	again.Env = map[string]string{envBox: "again"}

	second, err := f.Sandboxes.Create(f.t.Context(), again)
	if err != nil {
		f.t.Fatalf("second Create: %v", err)
	}

	if out, _ := f.run(second, ports.Exec{Argv: sh(scriptEnv)}); out != "box||" {
		f.t.Errorf("env after a second Create = %q; want box|| (the sandbox the first Create made)", out)
	}

	if _, st := f.run(first, ports.Exec{Argv: sh(scriptWrite)}); st.Code != 0 {
		f.t.Fatalf("writing a file = %+v; want exit 0 (the sandbox's filesystem is writable)", st)
	}

	if out, st := f.run(second, ports.Exec{Argv: sh(scriptRead)}); out != "kept" || st.Code != 0 {
		f.t.Errorf("reading through the second handle = %q, %+v; want what the first wrote", out, st)
	}

	left, err := f.Sandboxes.List(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}

	if n := countRun(left, spec.RunID); n != 1 {
		f.t.Errorf("List has run %s %d times; want once", spec.RunID, n)
	}
}

func countRun(left []ports.Leftover, run domain.RunID) int {
	n := 0

	for _, l := range left {
		if l.Run == run {
			n++
		}
	}

	return n
}

// Stdout and the exit code are the command's own; stderr's tail comes with the status.
func testExitCodes(f *fixture) {
	box, _ := f.create(acme, nil)

	out, st := f.run(box, ports.Exec{Argv: sh(scriptExit)})
	if out != "out" || st.Code != 3 || st.TimedOut || !strings.Contains(st.StderrTail, "oops") {
		f.t.Errorf("failing command = %q, %+v; want stdout out, exit 3 and oops in the stderr tail", out, st)
	}

	if out, st := f.run(box, ports.Exec{Argv: sh(scriptOK)}); out != "" || st.Code != 0 || st.TimedOut {
		f.t.Errorf("passing command = %q, %+v; want no output and exit 0", out, st)
	}
}

// StderrTail keeps the end of a long stderr, at most MaxStderrTail bytes.
func testStderrTail(f *fixture) {
	box, _ := f.create(acme, nil)

	_, st := f.run(box, ports.Exec{Argv: sh(scriptStderr)})
	if len(st.StderrTail) > ports.MaxStderrTail || !strings.HasSuffix(strings.TrimSpace(st.StderrTail), "end") {
		f.t.Errorf("stderr tail of %d bytes ending %q; want at most %d ending with end", len(st.StderrTail),
			st.StderrTail[max(0, len(st.StderrTail)-20):], ports.MaxStderrTail)
	}

	if len(st.StderrTail) < ports.MaxStderrTail/2 {
		f.t.Errorf("stderr tail of %d bytes from 17 KiB written; want close to %d", len(st.StderrTail), ports.MaxStderrTail)
	}
}

// Output arrives while the command runs; cancelling its ctx kills it, and Wait says so.
func testStreamAndCancel(f *fixture) {
	box, _ := f.create(acme, nil)

	ctx, cancel := context.WithCancel(f.t.Context())
	defer cancel()

	p := f.exec(ctx, box, ports.Exec{Argv: sh(scriptLong)})
	out := lines(p)

	if line, ok := f.next(out); !ok || line != "first" {
		f.t.Errorf("first line of a running command = %q, %v; want first, streamed before the command ends", line, ok)
	}

	cancel()

	rest, ended := f.drain(out)
	if !ended || slices.Contains(rest, "late") {
		f.t.Fatalf("stdout after cancel = %q, ended %v; want it to end without the command finishing", rest, ended)
	}

	if _, err := f.wait(p); !errors.Is(err, context.Canceled) {
		f.t.Errorf("Wait after cancel = %v; want context.Canceled", err)
	}
}

// A command past its timeout is killed and reported as timed out, with the output it wrote.
func testTimeout(f *fixture) {
	box, _ := f.create(acme, nil)

	start := time.Now()
	out, st := f.run(box, ports.Exec{Argv: sh(scriptLong), Timeout: time.Second})

	if !st.TimedOut || out != "first" {
		f.t.Errorf("command past its timeout = %q, %+v; want first and TimedOut", out, st)
	}

	if took := time.Since(start); took > within {
		f.t.Errorf("a 1s timeout took %s", took)
	}
}

// A command sees the sandbox's env with its own on top; the next command does not see it.
func testEnv(f *fixture) {
	box, _ := f.create(acme, map[string]string{envBox: "box", envOver: "box"})

	out, _ := f.run(box, ports.Exec{Argv: sh(scriptEnv), Env: map[string]string{envExec: "one", envOver: "exec"}})
	if out != "box|one|exec" {
		f.t.Errorf("env of a command with its own = %q; want box|one|exec", out)
	}

	if out, _ := f.run(box, ports.Exec{Argv: sh(scriptEnv)}); out != "box||box" {
		f.t.Errorf("env of the next command = %q; want box||box (a command's env is its own)", out)
	}
}

func testDir(f *fixture) {
	box, _ := f.create(acme, nil)

	if out, _ := f.run(box, ports.Exec{Argv: sh(scriptPwd), Dir: "/tmp"}); out != "/tmp" {
		f.t.Errorf("pwd in /tmp = %q", out)
	}
}

// Deleting a sandbox ends its running commands and removes it from the list; deleting it again,
// through either handle, or deleting an unknown run is not an error; it runs nothing more.
func testDelete(f *fixture) {
	box, spec := f.create(acme, nil)

	p := f.exec(f.t.Context(), box, ports.Exec{Argv: sh(scriptLong)})
	out := lines(p)

	if line, ok := f.next(out); !ok || line != "first" {
		f.t.Fatalf("first line = %q, %v", line, ok)
	}

	if err := f.Sandboxes.Delete(f.t.Context(), spec.RunID); err != nil {
		f.t.Fatalf("Delete: %v", err)
	}

	if rest, ended := f.drain(out); !ended || slices.Contains(rest, "late") {
		f.t.Errorf("stdout of a command in a deleted sandbox = %q, ended %v; want it to end", rest, ended)
	}

	_, _ = f.wait(p) // any outcome, as long as it returns

	left, err := f.Sandboxes.List(f.t.Context())
	if err != nil || countRun(left, spec.RunID) != 0 {
		f.t.Errorf("List after Delete = %v, %v; want run %s gone", left, err, spec.RunID)
	}

	for what, err := range map[string]error{
		"Delete again":             f.Sandboxes.Delete(f.t.Context(), spec.RunID),
		"Sandbox.Delete":           box.Delete(f.t.Context()),
		"Delete of an unknown run": f.Sandboxes.Delete(f.t.Context(), domain.RunID(uuid.NewString())),
	} {
		if err != nil {
			f.t.Errorf("%s: %v; want nil", what, err)
		}
	}

	if p, err := box.Exec(f.t.Context(), ports.Exec{Argv: sh(scriptOK)}); err == nil {
		_, _ = f.drain(lines(p))
		if _, err := f.wait(p); err == nil {
			f.t.Error("Exec in a deleted sandbox succeeded; want an error")
		}
	}
}

// List has every sandbox with its run and organization.
func testList(f *fixture) {
	_, a := f.create(acme, nil)
	_, b := f.create(labs, nil)

	left, err := f.Sandboxes.List(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}

	for _, want := range []ports.Leftover{{Run: a.RunID, Org: acme}, {Run: b.RunID, Org: labs}} {
		if !slices.Contains(left, want) {
			f.t.Errorf("List lacks %+v", want)
		}
	}
}

func testPublic(f *fixture) {
	if f.Network == nil {
		f.t.Skip("the harness has no network")
	}

	box, _ := f.create(acme, nil)

	if _, st := f.run(box, curl(f.Network.PublicURL)); st.Code != 0 || st.TimedOut {
		f.t.Errorf("curl %s = %+v; want the public internet reachable", f.Network.PublicURL, st)
	}
}

func testPrivate(f *fixture) {
	if f.Network == nil {
		f.t.Skip("the harness has no network")
	}

	box, _ := f.create(acme, nil)

	for _, url := range f.Network.PrivateURLs {
		if _, st := f.run(box, curl(url)); st.Code == 0 {
			f.t.Errorf("curl %s succeeded; want private addresses unreachable", url)
		}
	}
}

func curl(url string) ports.Exec {
	return ports.Exec{Argv: []string{"curl", "-sS", "-o", "/dev/null", "--max-time", "10", url}, Timeout: within}
}
