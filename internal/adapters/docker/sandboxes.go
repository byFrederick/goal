package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Sandboxes = (*Sandboxes)(nil)

// boxLimits are the CPU, memory and PID limits of each sandbox size (spec 4 §4.6); empty is
// medium.
var boxLimits = map[domain.SandboxSize]Config{
	domain.SandboxSmall:  {CPUs: 1, MemoryBytes: 2 << 30, PIDs: 1024},
	domain.SandboxMedium: {CPUs: 2, MemoryBytes: 4 << 30, PIDs: 2048},
	domain.SandboxLarge:  {CPUs: 4, MemoryBytes: 8 << 30, PIDs: 4096},
}

// Sandbox timings: how long a killed command gets for its output to end before the stream is
// cut, how often a command whose output ended is asked whether it exited, and how long the
// adapter's own calls get once the caller's context is gone.
const (
	killGrace = 10 * time.Second
	pollEvery = 50 * time.Millisecond
	callLimit = 30 * time.Second
	// foreverSeconds keeps a sandbox without a deadline up (sleep's argument): about 68 years.
	foreverSeconds = "2147483647"
)

// execMarker marks every process of one Exec, its children included (they inherit it), so the
// adapter can kill them: Docker has no call that ends an exec.
const execMarker = "GOAL_EXEC_ID"

// killScript kills every process whose environment carries execMarker=$1: it stops them all
// first, so none runs on while another is killed (a shell printing after its child died), then
// kills them, in up to three rounds for children started meanwhile. It needs only sh, tr, grep
// and kill, and /proc.
const killScript = `for round in 1 2 3; do
	pids=
	for p in /proc/[0-9]*; do
		if tr '\0' '\n' <"$p/environ" 2>/dev/null | grep -qxF "` + execMarker + `=$1"; then pids="$pids ${p#/proc/}"; fi
	done
	[ -n "$pids" ] || exit 0
	kill -STOP $pids 2>/dev/null
	kill -KILL $pids 2>/dev/null
done`

// Sandboxes runs one container per run on the agents' network (ports.Sandboxes): the run's dev
// box. A box keeps the hardening of agent tasks (uid 1000, every capability dropped, no new
// privileges, an init process, CPU, memory and PID limits of its size, a tmpfs /tmp) with a
// writable root filesystem and nothing mounted. It sleeps until its deadline, so a box no goald
// deletes ends by itself; commands run in it with docker exec, stdout and stderr attached. It is
// safe for concurrent use.
type Sandboxes struct {
	docker  *client.Client
	network string
	log     *slog.Logger
}

// NewSandboxes returns sandboxes on docker that run on network (GOAL_SANDBOX_NETWORK).
func NewSandboxes(docker *client.Client, network string, log *slog.Logger) *Sandboxes {
	if network == "" {
		network = DefaultNetwork
	}

	return &Sandboxes{docker: docker, network: network, log: log}
}

// Create implements ports.Sandboxes: it creates and starts the run's container, or returns the
// one a first Create made.
func (s *Sandboxes) Create(ctx context.Context, spec ports.SandboxSpec) (ports.Sandbox, error) {
	limits, err := validateBox(spec)
	if err != nil {
		return nil, fmt.Errorf("sandbox spec: %w", err)
	}

	if err := checkNetwork(ctx, s.docker, s.network); err != nil {
		return nil, err
	}

	start := time.Now()
	name := boxName(spec.RunID)
	b := &box{s: s, run: spec.RunID, name: name}

	cc, hc := boxContainerOf(limits, s.network, spec, start)
	create := func() error {
		_, err := s.docker.ContainerCreate(ctx, client.ContainerCreateOptions{Config: cc, HostConfig: hc, Name: name})
		if cerrdefs.IsConflict(err) { // the run's box exists
			return nil
		}

		return err
	}

	err = create()
	if cerrdefs.IsNotFound(err) { // the image
		if err = ensureImage(ctx, s.docker, s.log, spec.Image); err == nil {
			err = create()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	if _, err := s.docker.ContainerStart(ctx, name, client.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("start sandbox: %w", err)
	}

	s.log.InfoContext(ctx, "sandbox created", slog.String("run", string(spec.RunID)), slog.String("size", string(sizeOf(spec))),
		slog.Float64("seconds", time.Since(start).Seconds()))

	return b, nil
}

// List implements ports.Sandboxes: every run with a container on this Docker host, whichever
// goald started it, with the organization it is labelled with.
func (s *Sandboxes) List(ctx context.Context) ([]ports.Leftover, error) {
	list, err := s.docker.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", LabelRun)})
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}

	out := make([]ports.Leftover, 0, len(list.Items))
	for _, c := range list.Items {
		l := ports.Leftover{Run: domain.RunID(c.Labels[LabelRun]), Org: domain.OrgID(c.Labels[LabelOrg])}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}

	return out, nil
}

// Delete implements ports.Sandboxes: it removes every container of the run, which kills the
// commands running in it.
func (s *Sandboxes) Delete(ctx context.Context, run domain.RunID) error {
	list, err := s.docker.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", LabelRun+"="+string(run))})
	if err != nil {
		return fmt.Errorf("list sandboxes of run %s: %w", run, err)
	}

	var errs []error

	for _, c := range list.Items {
		_, err := s.docker.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		if err != nil && !cerrdefs.IsNotFound(err) && !cerrdefs.IsConflict(err) { // conflict: a removal in progress
			errs = append(errs, fmt.Errorf("remove sandbox of run %s: %w", run, err))
		}
	}

	if len(list.Items) > 0 && len(errs) == 0 {
		s.log.InfoContext(ctx, "sandbox deleted", slog.String("run", string(run)))
	}

	return errors.Join(errs...)
}

func validateBox(spec ports.SandboxSpec) (Config, error) {
	var errs []error

	if !safeID.MatchString(string(spec.RunID)) {
		errs = append(errs, fmt.Errorf("run id %q is not usable as a container name", spec.RunID))
	}

	if spec.Image == "" {
		errs = append(errs, errors.New("image is required"))
	}

	limits, ok := boxLimits[sizeOf(spec)]
	if !ok {
		errs = append(errs, fmt.Errorf("size %q is not small, medium or large", spec.Size))
	}

	return limits, errors.Join(errs...)
}

func sizeOf(spec ports.SandboxSpec) domain.SandboxSize {
	if spec.Size == "" {
		return domain.SandboxMedium
	}

	return spec.Size
}

func boxName(run domain.RunID) string { return "goal-box-" + string(run) }

// boxContainerOf is a run's box: the image's own filesystem, writable, with nothing mounted; its
// process sleeps until the deadline (seen from now); the spec's env with the proxy variables on
// top; on network only.
func boxContainerOf(limits Config, network string, spec ports.SandboxSpec, now time.Time) (*container.Config, *container.HostConfig) {
	sleep := foreverSeconds
	if !spec.Deadline.IsZero() {
		sleep = strconv.FormatInt(max(1, int64(spec.Deadline.Sub(now).Seconds())), 10)
	}

	cc := &container.Config{
		Image:      spec.Image,
		User:       containerUser,
		Entrypoint: []string{"sleep"},
		Cmd:        []string{sleep},
		Env:        withProxyEnv(spec.Env),
		Labels: map[string]string{
			LabelOrg: string(spec.OrgID), LabelFactory: string(spec.FactoryID), LabelItem: string(spec.ItemID), LabelRun: string(spec.RunID),
			LabelStation: spec.Station, LabelAgent: spec.Agent,
		},
	}

	hc := isolated(limits)
	hc.ReadonlyRootfs = false
	hc.NetworkMode = container.NetworkMode(network)

	return cc, hc
}

// box is one run's container.
type box struct {
	s    *Sandboxes
	run  domain.RunID
	name string
}

// Delete implements ports.Sandbox.
func (b *box) Delete(ctx context.Context) error { return b.s.Delete(ctx, b.run) }

// Exec implements ports.Sandbox: docker exec with stdout streamed and stderr's tail kept.
// Cancelling ctx or passing e.Timeout kills the command and its children.
func (b *box) Exec(ctx context.Context, e ports.Exec) (ports.Process, error) {
	if len(e.Argv) == 0 {
		return nil, errors.New("exec without a command")
	}

	marker := make([]byte, 16)
	_, _ = rand.Read(marker)
	id := hex.EncodeToString(marker)

	env := maps.Clone(e.Env)
	if env == nil {
		env = map[string]string{}
	}

	env[execMarker] = id

	lines := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		lines = append(lines, k+"="+env[k])
	}

	created, err := b.s.docker.ExecCreate(ctx, b.name, client.ExecCreateOptions{
		AttachStdout: true, AttachStderr: true, Env: lines, WorkingDir: e.Dir, Cmd: e.Argv,
	})
	if err != nil {
		return nil, fmt.Errorf("exec in sandbox of run %s: %w", b.run, err)
	}

	attach, err := b.s.docker.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec in sandbox of run %s: %w", b.run, err)
	}

	stdoutR, stdoutW := io.Pipe()
	p := &process{b: b, execID: created.ID, marker: id, stdout: stdoutR, done: make(chan struct{})}

	go p.supervise(ctx, attach.HijackedResponse, stdoutW, e.Timeout)

	return p, nil
}

// process is one command running in a box.
type process struct {
	b      *box
	execID string
	marker string
	stdout *io.PipeReader
	stderr tailWriter

	done   chan struct{}
	status ports.ExitStatus
	err    error
}

// Stdout implements ports.Process.
func (p *process) Stdout() io.Reader { return p.stdout }

// Wait implements ports.Process.
func (p *process) Wait() (ports.ExitStatus, error) {
	<-p.done
	return p.status, p.err
}

// supervise copies the command's output until it ends, kills the command when ctx ends or the
// timeout passes, then records how it ended.
func (p *process) supervise(ctx context.Context, attach client.HijackedResponse, stdout *io.PipeWriter, timeout time.Duration) {
	defer close(p.done)
	defer attach.Close()

	copied := make(chan struct{})

	go func() {
		defer close(copied)

		_, err := stdcopy.StdCopy(stdout, &p.stderr, attach.Reader)
		stdout.CloseWithError(err)
	}()

	var expired <-chan time.Time

	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		expired = timer.C
	}

	// poll asks whether the command exited once its output ended (ended is nil then).
	ended := (<-chan struct{})(copied)
	tick := time.NewTicker(pollEvery)
	tick.Stop()

	defer tick.Stop()

	var poll <-chan time.Time

	for {
		select {
		case <-ended:
			ended, poll = nil, tick.C
			tick.Reset(pollEvery)
		case <-poll:
		case <-ctx.Done():
			p.stop(ctx, attach, stdout, copied)
			p.err = fmt.Errorf("exec in sandbox of run %s: %w", p.b.run, ctx.Err())

			return
		case <-expired:
			p.stop(ctx, attach, stdout, copied)
			p.status = ports.ExitStatus{Code: -1, TimedOut: true, StderrTail: p.stderr.String()}

			return
		}

		if ended != nil {
			continue
		}

		// The output ended; the command has exited, or is about to.
		res, err := p.inspect(ctx)
		if err != nil {
			p.err = fmt.Errorf("exec in sandbox of run %s: %w", p.b.run, err)
			return
		}

		if !res.Running {
			p.status = ports.ExitStatus{Code: res.ExitCode, StderrTail: p.stderr.String()}
			return
		}
	}
}

// stop kills the command and its children and waits for its output to end, cutting the stream
// (and the caller's stdout) when it has not after killGrace.
func (p *process) stop(ctx context.Context, attach client.HijackedResponse, stdout *io.PipeWriter, copied <-chan struct{}) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callLimit)
	defer cancel()

	if err := p.b.kill(ctx, p.marker); err != nil {
		p.b.s.log.WarnContext(ctx, "kill a sandbox command", slog.String("run", string(p.b.run)), slog.Any("error", err))
	}

	select {
	case <-copied:
		return
	case <-time.After(killGrace):
	}

	// Unblocks the copy too when nothing reads stdout any more.
	attach.Close()
	stdout.CloseWithError(io.ErrClosedPipe)
	<-copied
}

// inspect asks Docker how the command is, within callLimit whatever ctx says.
func (p *process) inspect(ctx context.Context) (client.ExecInspectResult, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callLimit)
	defer cancel()

	return p.b.s.docker.ExecInspect(ctx, p.execID, client.ExecInspectOptions{})
}

// kill ends every process of the Exec marked marker.
func (b *box) kill(ctx context.Context, marker string) error {
	created, err := b.s.docker.ExecCreate(ctx, b.name, client.ExecCreateOptions{
		AttachStdout: true, AttachStderr: true, Cmd: []string{"sh", "-c", killScript, "goal-kill", marker},
	})
	if cerrdefs.IsNotFound(err) || cerrdefs.IsConflict(err) { // the box is gone or stopped, and its commands with it
		return nil
	}

	if err != nil {
		return err
	}

	attach, err := b.s.docker.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attach.Close()

	_, err = io.Copy(io.Discard, attach.Reader)

	return err
}

// tailWriter keeps the last ports.MaxStderrTail bytes written to it.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *tailWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, b...)
	if over := len(w.buf) - ports.MaxStderrTail; over > 0 {
		w.buf = slices.Delete(w.buf, 0, over)
	}

	return len(b), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return string(w.buf)
}
