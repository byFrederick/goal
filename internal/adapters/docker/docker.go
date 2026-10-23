// Package docker implements ports.Runner with local Docker containers (Docker Desktop,
// OrbStack or Docker Engine on Linux): one hardened container of the factory's worker image per
// agent task.
//
// Per run the runner creates <RunDir>/<run id>/ on the host, writes spec.json (the task without
// secrets) into it, has AgentFiles write the agent's prompt, skills and output schema into its
// agent/ folder, and mounts the folder read-only at /goal. The run's workspace (an already
// prepared git checkout) is mounted read-write at /workspace. The worker reports progress and
// its result as JSON lines on stdout (protocol.go). worker/README.md describes the whole
// contract from the container's side.
//
// Networking: the container runs on the default bridge network. A gateway URL on the host's
// loopback address is rewritten to host.docker.internal, which Docker Desktop and OrbStack
// resolve to the host; on Linux the host-gateway extra host provides the same name.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Runner = (*Runner)(nil)

// AgentFiles writes one task's agent files into dir, which the container sees read-only at
// /goal/agent:
//
//	prompt.md                  the agent's prompt (TaskSpec.PromptPath)
//	skills/<name>/SKILL.md     one folder per TaskSpec.Skills entry, with its other files
//	output.schema.json         the JSON Schema of TaskSpec.Output, when the station has a contract
//
// goald implements it from the factory definition in object storage. Files must be readable by
// uid 1000 (mode 0644 and folders 0755 are enough).
type AgentFiles interface {
	WriteAgentFiles(ctx context.Context, t ports.TaskSpec, dir string) error
}

// Config tunes the runner. The zero value of each field means its default.
type Config struct {
	// RunDir is the host folder for per-run files; Docker must be able to bind-mount it
	// (Docker Desktop shares /Users, /Volumes, /private, /tmp and /var/folders by default).
	// Default: $TMPDIR/goal-runs.
	RunDir string
	// CPUs, MemoryBytes and PIDs limit each container. Defaults: 2 CPUs, 4 GiB, 1024 PIDs.
	CPUs        float64
	MemoryBytes int64
	PIDs        int64
	// StopGrace is how long a stopped worker gets to exit before it is killed. Default 5s.
	StopGrace time.Duration
}

func (c Config) withDefaults() Config {
	if c.RunDir == "" {
		c.RunDir = filepath.Join(os.TempDir(), "goal-runs")
	}

	if c.CPUs <= 0 {
		c.CPUs = 2
	}

	if c.MemoryBytes <= 0 {
		c.MemoryBytes = 4 << 30
	}

	if c.PIDs <= 0 {
		c.PIDs = 1024
	}

	if c.StopGrace <= 0 {
		c.StopGrace = 5 * time.Second
	}

	return c
}

// Runner starts agent tasks as Docker containers. It is safe for concurrent use.
type Runner struct {
	docker *client.Client
	files  AgentFiles
	cfg    Config
	log    *slog.Logger
}

// New returns a runner that talks to docker and stages agent files with files.
func New(docker *client.Client, files AgentFiles, cfg Config, log *slog.Logger) *Runner {
	return &Runner{docker: docker, files: files, cfg: cfg.withDefaults(), log: log}
}

// cleanupTimeout bounds removing a container and its files once the task's context is gone.
const cleanupTimeout = 30 * time.Second

// Start implements ports.Runner. It pulls the image when it is missing, then creates, attaches
// to and starts the task's container.
func (r *Runner) Start(ctx context.Context, t ports.TaskSpec) (ports.Task, error) {
	if err := validate(t); err != nil {
		return nil, fmt.Errorf("task spec: %w", err)
	}

	runDir, err := r.prepare(ctx, t)
	if err != nil {
		return nil, err
	}

	tk, err := r.start(ctx, t, runDir)
	if err != nil {
		r.removeDir(ctx, runDir)
		return nil, err
	}

	return tk, nil
}

func (r *Runner) start(ctx context.Context, t ports.TaskSpec, runDir string) (*task, error) {
	if err := r.ensureImage(ctx, t.Image); err != nil {
		return nil, err
	}

	cc, hc := containerOf(r.cfg, t, runDir)

	created, err := r.docker.ContainerCreate(ctx, client.ContainerCreateOptions{Config: cc, HostConfig: hc, Name: containerName(t.RunID)})
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	tk := newTask(r, t, created.ID, runDir)

	attach, err := r.docker.ContainerAttach(ctx, created.ID, client.ContainerAttachOptions{Stream: true, Stdout: true, Stderr: true})
	if err != nil {
		tk.remove(ctx)
		return nil, fmt.Errorf("attach to container: %w", err)
	}

	// Waiting starts before the container does, so a fast exit is not missed; it outlives ctx
	// because a cancelled task is stopped, not abandoned.
	wait := r.docker.ContainerWait(context.WithoutCancel(ctx), created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNextExit})

	if _, err := r.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		attach.Close()
		tk.remove(ctx)

		return nil, fmt.Errorf("start container: %w", err)
	}

	go tk.supervise(ctx, attach.HijackedResponse, wait, t.Timeout)

	return tk, nil
}

// prepare creates the run's folder with spec.json and the agent's files.
func (r *Runner) prepare(ctx context.Context, t ports.TaskSpec) (string, error) {
	spec, err := workerSpecOf(t)
	if err != nil {
		return "", fmt.Errorf("task spec: %w", err)
	}

	body, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encode worker spec: %w", err)
	}

	// The container's user (uid 1000) may not own these files on Linux, and they hold no
	// secrets, so they are world-readable.
	if err := os.MkdirAll(r.cfg.RunDir, 0o755); err != nil { //nolint:gosec // see above
		return "", fmt.Errorf("create run root: %w", err)
	}

	runDir := r.runDir(t.RunID)
	if err := os.Mkdir(runDir, 0o755); err != nil { //nolint:gosec // see above
		return "", fmt.Errorf("create run dir: %w", err)
	}

	err = os.WriteFile(filepath.Join(runDir, specFile), body, 0o644) //nolint:gosec // see above
	if err == nil {
		err = os.Mkdir(filepath.Join(runDir, agentDir), 0o755) //nolint:gosec // see above
	}

	if err == nil {
		err = r.files.WriteAgentFiles(ctx, t, filepath.Join(runDir, agentDir))
	}

	if err != nil {
		r.removeDir(ctx, runDir)
		return "", fmt.Errorf("stage run files: %w", err)
	}

	return runDir, nil
}

func (r *Runner) runDir(run domain.RunID) string { return filepath.Join(r.cfg.RunDir, string(run)) }

func (r *Runner) removeDir(ctx context.Context, dir string) {
	if err := os.RemoveAll(dir); err != nil {
		r.log.WarnContext(ctx, "remove run dir", slog.String("dir", dir), slog.Any("error", err))
	}
}

func (r *Runner) ensureImage(ctx context.Context, ref string) error {
	_, err := r.docker.ImageInspect(ctx, ref)
	if err == nil {
		return nil
	}

	if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspect image %s: %w", ref, err)
	}

	r.log.InfoContext(ctx, "pulling agent image", slog.String("image", ref))

	pull, err := r.docker.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}

	if err := pull.Wait(ctx); err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}

	return nil
}

// Reap removes task containers and run folders whose run is no longer live, for example after
// goald crashed mid-run. live reports whether a run is still running; goald answers it from the
// store. It returns how many runs it cleaned up.
func (r *Runner) Reap(ctx context.Context, live func(domain.RunID) bool) (int, error) {
	list, err := r.docker.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", LabelRun)})
	if err != nil {
		return 0, fmt.Errorf("list task containers: %w", err)
	}

	reaped := map[domain.RunID]bool{}

	var errs []error

	for _, c := range list.Items {
		run := domain.RunID(c.Labels[LabelRun])
		if live(run) {
			continue
		}

		if _, err := r.docker.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("remove container of run %s: %w", run, err))
			continue
		}

		reaped[run] = true
	}

	entries, err := os.ReadDir(r.cfg.RunDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("list run dirs: %w", err))
	}

	for _, e := range entries {
		run := domain.RunID(e.Name())
		if !e.IsDir() || live(run) {
			continue
		}

		if err := os.RemoveAll(r.runDir(run)); err != nil {
			errs = append(errs, fmt.Errorf("remove run dir %s: %w", run, err))
			continue
		}

		reaped[run] = true
	}

	return len(reaped), errors.Join(errs...)
}
