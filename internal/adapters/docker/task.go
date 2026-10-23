package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// stderrLines is how much of the worker's stderr a failed result quotes.
const stderrLines = 20

// task is one running container.
type task struct {
	r      *Runner
	id     string
	runDir string
	log    *slog.Logger
	redact redactor

	events chan ports.RunEvent
	done   chan struct{}
	res    domain.RunResult
	err    error
}

func newTask(r *Runner, t ports.TaskSpec, id, runDir string) *task {
	secrets := slices.Collect(maps.Values(t.Env))

	return &task{
		r: r, id: id, runDir: runDir,
		log:    r.log.With(slog.String("run", string(t.RunID)), slog.String("container", id[:min(12, len(id))])),
		redact: newRedactor(append(secrets, t.GatewayToken)...),
		events: make(chan ports.RunEvent, 64),
		done:   make(chan struct{}),
	}
}

// Events implements ports.Task.
func (t *task) Events() <-chan ports.RunEvent { return t.events }

// Wait implements ports.Task. The container and the run's files are gone when it returns.
func (t *task) Wait() (domain.RunResult, error) {
	<-t.done
	return t.res, t.err
}

// supervise follows the container until it exits: it relays stdout events, keeps the stderr
// tail, stops the container when ctx ends or the timeout passes, then records the result and
// removes the container and its files.
func (t *task) supervise(ctx context.Context, attach client.HijackedResponse, wait client.ContainerWaitResult, timeout time.Duration) {
	defer close(t.done)
	defer attach.Close()

	var (
		streams sync.WaitGroup
		fin     *finished
		stderr  = &tail{max: stderrLines}
	)

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	streams.Go(func() {
		_, err := stdcopy.StdCopy(stdoutW, stderrW, attach.Reader)
		stdoutW.CloseWithError(err)
		stderrW.CloseWithError(err)
	})
	streams.Go(func() { fin = t.relay(ctx, stdoutR) })
	streams.Go(func() { t.keepStderr(ctx, stderrR, stderr) })

	exit, waitErr := t.await(ctx, wait, timeout)
	if waitErr != nil {
		attach.Close() // the stream may never end on its own
	}

	streams.Wait()
	close(t.events)
	t.remove(ctx)

	if waitErr != nil {
		t.err = fmt.Errorf("wait for container: %w", waitErr)
		return
	}

	t.res = resultOf(outcome{fin: fin, exitCode: exit.code, stopped: exit.stopped, stderr: t.redact.string(stderr.String())})
	t.res.Error = t.redact.string(t.res.Error)
}

// exit is how a container ended.
type exit struct {
	code int64
	// stopped is why the runner stopped the container; nil when it exited on its own.
	stopped error
}

// await blocks until the container exits, stopping it first when ctx ends or timeout passes.
func (t *task) await(ctx context.Context, wait client.ContainerWaitResult, timeout time.Duration) (exit, error) {
	var expired <-chan time.Time

	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		expired = timer.C
	}

	var stopped error

	done := ctx.Done()

	for {
		select {
		case res := <-wait.Result:
			return exit{code: res.StatusCode, stopped: stopped}, nil
		case err := <-wait.Error:
			return exit{stopped: stopped}, err
		case <-done:
			done = nil

			if stopped == nil {
				stopped = ctxStop(ctx)

				t.stop(ctx)
			}
		case <-expired:
			expired = nil

			if stopped == nil {
				stopped = fmt.Errorf("%w after %s", errTimeout, timeout)

				t.stop(ctx)
			}
		}
	}
}

// ctxStop says why ctx ended: a deadline is a timeout, anything else a cancellation.
func ctxStop(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", errTimeout, ctx.Err())
	}

	return ctx.Err()
}

// stop asks the worker to exit and kills it after the grace period.
func (t *task) stop(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	grace := int(t.r.cfg.StopGrace.Seconds())
	if _, err := t.r.docker.ContainerStop(ctx, t.id, client.ContainerStopOptions{Timeout: &grace}); err != nil {
		t.log.WarnContext(ctx, "stop task container", slog.Any("error", err))
	}
}

// remove deletes the container and the run's files.
func (t *task) remove(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	_, err := t.r.docker.ContainerRemove(ctx, t.id, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		t.log.WarnContext(ctx, "remove task container; Reap will retry", slog.Any("error", err))
	}

	t.r.removeDir(ctx, t.runDir)
}

// relay turns stdout lines into events until the stream ends and returns the finished record.
func (t *task) relay(ctx context.Context, r io.Reader) *finished {
	var fin *finished

	err := eachLine(r, maxLine, func(line []byte) {
		ev, f, err := parseLine(line, time.Now())

		switch {
		case err != nil:
			t.log.DebugContext(ctx, "ignored worker stdout line", slog.Any("error", err))
		case f != nil:
			fin = f
		default:
			t.redact.value(ev.Data)

			t.events <- *ev
		}
	})
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.log.WarnContext(ctx, "read worker stdout", slog.Any("error", err))
	}

	return fin
}

// keepStderr logs the worker's stderr at debug level and keeps its tail.
func (t *task) keepStderr(ctx context.Context, r io.Reader, keep *tail) {
	err := eachLine(r, maxLine, func(line []byte) {
		s := t.redact.string(string(line))
		keep.add(s)
		t.log.DebugContext(ctx, "worker", slog.String("stderr", s))
	})
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.log.WarnContext(ctx, "read worker stderr", slog.Any("error", err))
	}
}
