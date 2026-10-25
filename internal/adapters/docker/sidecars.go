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
	"strings"
	"sync"
	"sync/atomic"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Sidecars = (*Sidecars)(nil)

// LabelConnection marks a sidecar container with its connection. Sidecars also carry their
// run's labels, so Runner.Reap removes them with their run.
const LabelConnection = "goal.connection"

// sidecarUser runs a sidecar whose image would run as root: nobody.
const sidecarUser = "65534:65534"

// sidecarHome is HOME in a sidecar: the root filesystem is read-only, and servers write caches
// and browser profiles under their home.
const sidecarHome = "/tmp"

// Sidecar defaults: a stdio MCP server needs less than an agent; a headless browser is the
// largest expected.
const (
	sidecarCPUs   = 1
	sidecarMemory = 2 << 30
	sidecarPIDs   = 512
)

// sidecarStderrLines is how much of a sidecar's stderr is logged when it exits on its own.
const sidecarStderrLines = 20

// Sidecars runs stdio MCP servers as containers with the hardening of agent tasks: read-only
// root filesystem with a tmpfs /tmp (also HOME), every capability dropped, no privilege
// escalation, an init process and CPU, memory and PID limits. A sidecar runs as its image's user
// unless that is root, in which case it runs as nobody. It has no network unless its spec asks
// for one; then it is on the agents' network, where it reaches public hosts through the
// egress proxy and nothing on the host or a private network. Nothing is mounted: a sidecar sees
// neither the run's workspace nor its agent files.
type Sidecars struct {
	docker *client.Client
	cfg    Config
	log    *slog.Logger
}

// NewSidecars returns a sidecar manager on docker whose networked sidecars run on the agents
// network of cfg. Zero CPUs, MemoryBytes and PIDs in cfg take the sidecar defaults (1 CPU, 2 GiB, 512
// PIDs); RunDir is not used.
func NewSidecars(docker *client.Client, cfg Config, log *slog.Logger) *Sidecars {
	if cfg.CPUs <= 0 {
		cfg.CPUs = sidecarCPUs
	}

	if cfg.MemoryBytes <= 0 {
		cfg.MemoryBytes = sidecarMemory
	}

	if cfg.PIDs <= 0 {
		cfg.PIDs = sidecarPIDs
	}

	return &Sidecars{docker: docker, cfg: cfg.withDefaults(), log: log}
}

// Start implements ports.Sidecars: it creates the container, attaches to its stdio and starts it.
func (s *Sidecars) Start(ctx context.Context, spec ports.SidecarSpec) (ports.Sidecar, error) {
	if err := validateSidecar(spec); err != nil {
		return nil, fmt.Errorf("sidecar spec: %w", err)
	}

	if spec.Network {
		if err := checkNetwork(ctx, s.docker, s.cfg.Network); err != nil {
			return nil, err
		}
	}

	if err := ensureImage(ctx, s.docker, s.log, spec.Image); err != nil {
		return nil, err
	}

	img, err := s.docker.ImageInspect(ctx, spec.Image)
	if err != nil {
		return nil, fmt.Errorf("inspect image %s: %w", spec.Image, err)
	}

	imageUser := ""
	if img.Config != nil {
		imageUser = img.Config.User
	}

	cc, hc := sidecarContainerOf(s.cfg, agentsRoute(s.cfg.Network), spec, imageUser)

	created, err := s.docker.ContainerCreate(ctx, client.ContainerCreateOptions{Config: cc, HostConfig: hc, Name: sidecarName(spec)})
	if err != nil {
		return nil, fmt.Errorf("create sidecar container: %w", err)
	}

	sc := &sidecar{
		docker: s.docker, id: created.ID,
		log: s.log.With(slog.String("run", string(spec.RunID)), slog.String("connection", spec.Connection),
			slog.String("container", created.ID[:min(12, len(created.ID))])),
		redact: newRedactor(append(slices.Collect(maps.Values(spec.Env)), spec.Command...)...),
		done:   make(chan struct{}),
	}

	attach, err := s.docker.ContainerAttach(ctx, created.ID, client.ContainerAttachOptions{Stream: true, Stdin: true, Stdout: true, Stderr: true})
	if err != nil {
		sc.remove()
		return nil, fmt.Errorf("attach to sidecar: %w", err)
	}

	sc.attach = attach.HijackedResponse

	if _, err := s.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		attach.Close()
		sc.remove()

		return nil, fmt.Errorf("start sidecar: %w", err)
	}

	sc.follow()

	return sc, nil
}

func validateSidecar(s ports.SidecarSpec) error {
	var errs []error

	if !safeID.MatchString(string(s.RunID)) {
		errs = append(errs, fmt.Errorf("run id %q is not usable in a container name", s.RunID))
	}

	if !safeID.MatchString(s.Connection) {
		errs = append(errs, fmt.Errorf("connection %q is not usable in a container name", s.Connection))
	}

	if s.Image == "" {
		errs = append(errs, errors.New("image is required"))
	}

	return errors.Join(errs...)
}

// sidecarContainerOf is one sidecar's container: the image's entrypoint with the spec's command,
// stdin open for the gateway, the spec's environment with HOME on the tmpfs, and no network
// unless the spec asks for it; then the agents' network and its proxy variables (r).
func sidecarContainerOf(cfg Config, r route, s ports.SidecarSpec, imageUser string) (*container.Config, *container.HostConfig) {
	env := maps.Clone(s.Env)
	if env == nil {
		env = map[string]string{}
	}

	if _, ok := env[envHome]; !ok {
		env[envHome] = sidecarHome
	}

	if s.Network {
		maps.Copy(env, r.env)
	}

	vars := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		vars = append(vars, k+"="+env[k])
	}

	cc := &container.Config{
		Image:        s.Image,
		User:         sidecarUserOf(imageUser),
		Env:          vars,
		Cmd:          s.Command,
		OpenStdin:    true,
		StdinOnce:    true, // the server sees EOF when the gateway lets go
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Labels: map[string]string{
			LabelOrg: string(s.OrgID), LabelFactory: string(s.FactoryID), LabelItem: string(s.ItemID), LabelRun: string(s.RunID),
			LabelConnection: s.Connection,
		},
	}

	hc := isolated(cfg)
	if s.Network {
		hc.NetworkMode = container.NetworkMode(r.network)
	} else {
		hc.NetworkMode = "none"
	}

	return cc, hc
}

// sidecarUserOf keeps an image's own non-root user and replaces root (or no user) with nobody.
func sidecarUserOf(imageUser string) string {
	name, _, _ := strings.Cut(imageUser, ":")
	if name == "" || name == "root" || name == "0" {
		return sidecarUser
	}

	return imageUser
}

// sidecarName is unique per sidecar: a run's connection can be started again after it idled out.
func sidecarName(s ports.SidecarSpec) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)

	return "goal-mcp-" + string(s.RunID) + "-" + s.Connection + "-" + hex.EncodeToString(b)
}

// sidecar is a running sidecar container and its attached stdio.
type sidecar struct {
	docker *client.Client
	id     string
	log    *slog.Logger
	redact redactor
	attach client.HijackedResponse
	stdout *io.PipeReader

	closed atomic.Bool
	once   sync.Once
	// done is closed when the container's output streams have ended.
	done chan struct{}
}

// follow splits the attached stream into stdout, which Read returns, and stderr, which is
// logged at debug level and, when the server exits on its own, as a warning with its tail.
func (c *sidecar) follow() {
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	c.stdout = stdoutR

	go func() {
		defer close(c.done)

		var (
			wg     sync.WaitGroup
			stderr = &tail{max: sidecarStderrLines}
		)

		wg.Go(func() { c.keepStderr(stderrR, stderr) })

		_, err := stdcopy.StdCopy(stdoutW, stderrW, c.attach.Reader)
		stdoutW.CloseWithError(err)
		stderrW.CloseWithError(err)
		wg.Wait()

		if !c.closed.Load() {
			c.log.Warn("MCP sidecar exited", slog.String("stderr", stderr.String()))
		}
	}()
}

func (c *sidecar) keepStderr(r io.Reader, keep *tail) {
	err := eachLine(r, maxLine, func(line []byte) {
		s := c.redact.string(string(line))
		keep.add(s)
		c.log.Debug("MCP sidecar", slog.String("stderr", s))
	})
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		c.log.Warn("read sidecar stderr", slog.Any("error", err))
	}
}

// Read implements io.Reader with the server's stdout.
func (c *sidecar) Read(p []byte) (int, error) { return c.stdout.Read(p) }

// Write implements io.Writer to the server's stdin.
func (c *sidecar) Write(p []byte) (int, error) { return c.attach.Conn.Write(p) }

// Close closes the server's stdin, removes its container and waits for its streams to end.
func (c *sidecar) Close() error {
	c.once.Do(func() {
		c.closed.Store(true)
		_ = c.attach.CloseWrite()

		c.remove()
		c.attach.Close()
		// Unblocks the stream copy when nothing reads stdout any more.
		_ = c.stdout.CloseWithError(io.ErrClosedPipe)
		<-c.done
	})

	return nil
}

// remove force-removes the container; a stdio server keeps nothing worth a graceful stop.
func (c *sidecar) remove() {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	_, err := c.docker.ContainerRemove(ctx, c.id, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		c.log.Warn("remove sidecar container; Reap will retry", slog.Any("error", err))
	}
}
