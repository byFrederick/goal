package docker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Checker = (*Checker)(nil)

// LabelCheck marks a check container (it also carries the run's labels, so Runner.Reap removes
// it with its run).
const LabelCheck = "goal.check"

// Check output limits: Docker keeps the last checkLogLines lines; at most checkLogBytes reach
// the result.
const (
	checkLogLines = "200"
	checkLogBytes = 32 << 10
	// exitTimeout is the exit code reported for a check stopped at its timeout, as timeout(1) does.
	exitTimeout = 124
)

// Checker runs station command checks: each in its own container of the factory's image, with
// the same hardening as an agent task, the run's workspace mounted read-write at /workspace and
// nothing else. A check gets no secrets, no gateway token and no Docker socket; it keeps
// network access, because toolchains fetch modules and providers.
type Checker struct {
	docker *client.Client
	cfg    Config
	log    *slog.Logger
}

// NewChecker returns a checker on docker with the runner's resource limits.
func NewChecker(docker *client.Client, cfg Config, log *slog.Logger) *Checker {
	return &Checker{docker: docker, cfg: cfg.withDefaults(), log: log}
}

// Check implements ports.Checker. A command that exits non-zero, or runs past c.Timeout, is a
// result; an error means the container could not be run.
func (k *Checker) Check(ctx context.Context, c ports.CheckSpec) (ports.CheckResult, error) {
	if err := validateCheck(c); err != nil {
		return ports.CheckResult{}, fmt.Errorf("check spec: %w", err)
	}

	if err := ensureImage(ctx, k.docker, k.log, c.Image); err != nil {
		return ports.CheckResult{}, err
	}

	cc, hc := checkContainerOf(k.cfg, c)

	created, err := k.docker.ContainerCreate(ctx, client.ContainerCreateOptions{Config: cc, HostConfig: hc, Name: checkName(c)})
	if err != nil {
		return ports.CheckResult{}, fmt.Errorf("create check container: %w", err)
	}

	defer k.remove(ctx, created.ID)

	wait := k.docker.ContainerWait(context.WithoutCancel(ctx), created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNextExit})
	start := time.Now()

	if _, err := k.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return ports.CheckResult{}, fmt.Errorf("start check container: %w", err)
	}

	code, timedOut, err := k.await(ctx, created.ID, wait, c.Timeout)
	if err != nil {
		return ports.CheckResult{}, err
	}

	res := ports.CheckResult{ExitCode: int(code), Duration: time.Since(start), Output: k.output(ctx, created.ID)}
	if timedOut {
		res.ExitCode = exitTimeout
		res.Output += fmt.Sprintf("\n[goal] check stopped after its timeout of %s", c.Timeout)
	}

	return res, nil
}

// await waits for the check to exit, stopping it when the timeout passes. Cancelling ctx stops
// the check and returns ctx's error.
func (k *Checker) await(ctx context.Context, id string, wait client.ContainerWaitResult, timeout time.Duration) (int64, bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	timedOut := false

	for {
		select {
		case res := <-wait.Result:
			if ctx.Err() != nil {
				return 0, false, ctx.Err()
			}

			return res.StatusCode, timedOut, nil
		case err := <-wait.Error:
			return 0, false, fmt.Errorf("wait for check container: %w", err)
		case <-ctx.Done():
			k.stop(ctx, id)
		case <-timer.C:
			timedOut = true

			k.stop(ctx, id)
		}
	}
}

func (k *Checker) stop(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	grace := int(k.cfg.StopGrace.Seconds())
	if _, err := k.docker.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &grace}); err != nil {
		k.log.WarnContext(ctx, "stop check container", slog.Any("error", err))
	}
}

// output returns the end of the check's combined stdout and stderr.
func (k *Checker) output(ctx context.Context, id string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	logs, err := k.docker.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: checkLogLines})
	if err != nil {
		return "[goal] check output unavailable: " + err.Error()
	}
	defer func() { _ = logs.Close() }()

	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &out, logs); err != nil && !errors.Is(err, io.EOF) {
		k.log.WarnContext(ctx, "read check output", slog.Any("error", err))
	}

	s := out.String()
	if len(s) > checkLogBytes {
		s = strings.ToValidUTF8(s[len(s)-checkLogBytes:], "")
	}

	return s
}

func (k *Checker) remove(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	_, err := k.docker.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		k.log.WarnContext(ctx, "remove check container; Reap will retry", slog.Any("error", err))
	}
}

func validateCheck(c ports.CheckSpec) error {
	var errs []error

	if !safeID.MatchString(string(c.RunID)) {
		errs = append(errs, fmt.Errorf("run id %q is not usable as a container name", c.RunID))
	}

	if c.Image == "" {
		errs = append(errs, errors.New("image is required"))
	}

	if strings.TrimSpace(c.Command) == "" {
		errs = append(errs, errors.New("command is required"))
	}

	if !filepath.IsAbs(c.WorkspaceDir) {
		errs = append(errs, fmt.Errorf("workspace dir %q must be an absolute host path", c.WorkspaceDir))
	}

	if c.Timeout <= 0 {
		errs = append(errs, errors.New("timeout is required"))
	}

	return errors.Join(errs...)
}

// checkContainerOf is one check's container: the command under sh -c in /workspace, with only
// HOME in its environment.
func checkContainerOf(cfg Config, c ports.CheckSpec) (*container.Config, *container.HostConfig) {
	cc := &container.Config{
		Image:      c.Image,
		User:       containerUser,
		Entrypoint: []string{"/bin/sh", "-c"},
		Cmd:        []string{c.Command},
		Env:        []string{envHome + "=" + containerHome, "CI=true"},
		WorkingDir: containerWorkspace,
		Labels: map[string]string{
			LabelFactory: string(c.FactoryID), LabelItem: string(c.ItemID), LabelRun: string(c.RunID),
			LabelStation: c.Station, LabelCheck: c.Command,
		},
	}

	return cc, hardened(cfg, mount.Mount{Type: mount.TypeBind, Source: c.WorkspaceDir, Target: containerWorkspace})
}

// checkName is unique per check, since a run can have several.
func checkName(c ports.CheckSpec) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)

	return "goal-check-" + string(c.RunID) + "-" + hex.EncodeToString(b)
}
