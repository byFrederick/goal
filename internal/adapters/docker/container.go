package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Paths inside the container and the image contract: the worker image runs as uid 1000 with
// its home at /home/node (the node base image's user).
const (
	containerWorkspace = "/workspace"
	containerGoal      = "/goal"
	containerUID       = "1000"
	containerUser      = containerUID + ":" + containerUID
	containerHome      = "/home/node"
	specFile           = "spec.json"
	agentDir           = "agent"
)

// Labels on every task container, so orphans can be found and reaped.
const (
	LabelFactory = "goal.factory"
	LabelItem    = "goal.item"
	LabelRun     = "goal.run"
	LabelStation = "goal.station"
	LabelAgent   = "goal.agent"
)

// Environment variables the runner sets or keeps out.
const (
	envGatewayToken = "GOAL_GATEWAY_TOKEN"
	envHome         = "HOME"
	// envAPIKey never reaches a task: model auth on a laptop is the subscription token.
	envAPIKey = "ANTHROPIC_API_KEY" //nolint:gosec // a variable name, not a credential
)

// hostAlias is how a container reaches the host; Docker Desktop and OrbStack resolve it, and
// the host-gateway extra host makes it resolve on Linux too.
const hostAlias = "host.docker.internal"

// safeID matches run ids usable as a container name and a directory name.
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// workerSpec is /goal/spec.json, the task as the worker reads it. It holds no secrets: the
// gateway token and the model token arrive as environment variables.
type workerSpec struct {
	FactoryID  domain.FactoryID           `json:"factory_id"`
	ItemID     domain.ItemID              `json:"item_id"`
	RunID      domain.RunID               `json:"run_id"`
	Station    string                     `json:"station"`
	Agent      string                     `json:"agent"`
	Model      string                     `json:"model,omitempty"`
	Skills     []string                   `json:"skills"`
	ToolsAllow []string                   `json:"tools_allow"`
	ToolsDeny  []string                   `json:"tools_deny"`
	GatewayURL string                     `json:"gateway_url,omitempty"`
	Brief      string                     `json:"brief"`
	Inputs     map[string]json.RawMessage `json:"inputs,omitempty"`
	Output     string                     `json:"output,omitempty"`
	MaxTurns   int                        `json:"max_turns,omitempty"`
	MaxUSD     float64                    `json:"max_usd,omitempty"`
}

func workerSpecOf(t ports.TaskSpec) (workerSpec, error) {
	gateway, err := containerURL(t.GatewayURL)
	if err != nil {
		return workerSpec{}, fmt.Errorf("gateway url: %w", err)
	}

	s := workerSpec{
		FactoryID: t.FactoryID, ItemID: t.ItemID, RunID: t.RunID, Station: t.Station, Agent: t.Agent, Model: t.Model,
		Skills: make([]string, 0, len(t.Skills)), ToolsAllow: nonNil(t.ToolsAllow), ToolsDeny: nonNil(t.ToolsDeny),
		GatewayURL: gateway, Brief: t.Brief, Inputs: t.Inputs, Output: t.Output,
		MaxTurns: t.Budget.MaxTurns, MaxUSD: t.Budget.MaxUSD,
	}
	for _, sk := range t.Skills {
		s.Skills = append(s.Skills, sk.Name)
	}

	return s, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// containerURL rewrites a loopback URL (goald's own address on the host) to one a container
// can reach.
func containerURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%q is not an http(s) URL", raw)
	}

	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		if port := u.Port(); port != "" {
			u.Host = net.JoinHostPort(hostAlias, port)
		} else {
			u.Host = hostAlias
		}
	}

	return u.String(), nil
}

// taskEnv is the container's whole environment: the spec's Env (which carries
// CLAUDE_CODE_OAUTH_TOKEN), the gateway token and HOME. ANTHROPIC_API_KEY is always removed.
func taskEnv(t ports.TaskSpec) []string {
	env := maps.Clone(t.Env)
	if env == nil {
		env = map[string]string{}
	}

	delete(env, envAPIKey)

	env[envHome] = containerHome
	if t.GatewayToken != "" {
		env[envGatewayToken] = t.GatewayToken
	} else {
		delete(env, envGatewayToken)
	}

	out := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		out = append(out, k+"="+env[k])
	}

	return out
}

func validate(t ports.TaskSpec) error {
	var errs []error
	if !safeID.MatchString(string(t.RunID)) {
		errs = append(errs, fmt.Errorf("run id %q is not usable as a container name", t.RunID))
	}

	if t.Image == "" {
		errs = append(errs, errors.New("image is required"))
	}

	if !filepath.IsAbs(t.WorkspaceDir) {
		errs = append(errs, fmt.Errorf("workspace dir %q must be an absolute host path", t.WorkspaceDir))
	}

	return errors.Join(errs...)
}

func containerName(run domain.RunID) string { return "goal-run-" + string(run) }

// containerOf is the hardened container for one task: non-root, read-only root filesystem with
// a tmpfs /tmp and a throwaway home volume, every capability dropped, no privilege escalation,
// CPU, memory and PID limits. The run directory is mounted read-only at /goal and the workspace
// read-write at /workspace.
func containerOf(cfg Config, t ports.TaskSpec, runDir string) (*container.Config, *container.HostConfig) {
	cc := &container.Config{
		Image:      t.Image,
		User:       containerUser,
		Env:        taskEnv(t),
		WorkingDir: containerWorkspace,
		Labels: map[string]string{
			LabelFactory: string(t.FactoryID), LabelItem: string(t.ItemID), LabelRun: string(t.RunID),
			LabelStation: t.Station, LabelAgent: t.Agent,
		},
	}

	hc := hardened(cfg,
		mount.Mount{Type: mount.TypeBind, Source: runDir, Target: containerGoal, ReadOnly: true},
		mount.Mount{Type: mount.TypeBind, Source: t.WorkspaceDir, Target: containerWorkspace},
	)
	hc.ExtraHosts = []string{hostAlias + ":host-gateway"}

	return cc, hc
}

// hardened is the host configuration of agent tasks and checks: isolated, plus an anonymous
// volume as home (disk-backed, so toolchain caches fit; removed with the container) and the
// given mounts.
func hardened(cfg Config, mounts ...mount.Mount) *container.HostConfig {
	hc := isolated(cfg)
	hc.Mounts = append(hc.Mounts, mounts...)
	hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeVolume, Target: containerHome})

	return hc
}

// isolated is the host configuration every goal container shares: read-only root filesystem,
// tmpfs /tmp, every capability dropped, no privilege escalation, an init process and CPU,
// memory and PID limits. The user (non-root) is set on the container.
func isolated(cfg Config) *container.HostConfig {
	useInit := true // reaps the container's child processes
	pids := cfg.PIDs
	memory := cfg.MemoryBytes

	return &container.HostConfig{
		Privileged:     false,
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		Init:           &useInit,
		Tmpfs:          map[string]string{"/tmp": "rw,nosuid,nodev,size=1g,mode=1777"},
		Resources: container.Resources{
			NanoCPUs:   int64(cfg.CPUs * 1e9),
			Memory:     memory,
			MemorySwap: memory, // no swap beyond the memory limit
			PidsLimit:  &pids,
		},
	}
}
