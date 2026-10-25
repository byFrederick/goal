// Package docker implements ports.Sandboxes and ports.Sidecars with local Docker containers
// (Docker Desktop, OrbStack or Docker Engine on Linux): one dev box per run (sandboxes.go), into
// which goald execs the agent image's commands, and, until runs start their stdio MCP servers in
// their own box (spec 4 PR 4-11), one hardened container per stdio MCP server a run uses
// (sidecars.go). Nothing is mounted from the host: a run's box gets everything it needs from the
// gateway's run bundle. worker/README.md describes the contract from the box's side.
//
// Networking (network.go, ADR 0009 and its phase 4 amendment): boxes and networked sidecars run
// on compose.yaml's agents network (GOAL_SANDBOX_NETWORK), which has no route to the host, to
// other networks or to the internet. The one other container there is the egress proxy
// (compose's goal-egress service, cmd/goal-egress), which every container's HTTP_PROXY and
// HTTPS_PROXY name: it reaches public hosts and, at its paths only, goald's gateway (the gateway
// URL a box is given is the proxy's, BoxGatewayURL). Nothing reaches a loopback, private or
// link-local address through it.
package docker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// Config holds the limits and the network of the containers of one kind. The zero value of each
// field means its default.
type Config struct {
	// CPUs, MemoryBytes and PIDs limit each container. Defaults: 2 CPUs, 4 GiB, 1024 PIDs.
	CPUs        float64
	MemoryBytes int64
	PIDs        int64
	// Network is the agents' network containers run on (GOAL_SANDBOX_NETWORK). Default
	// DefaultNetwork.
	Network string
}

func (c Config) withDefaults() Config {
	if c.Network == "" {
		c.Network = DefaultNetwork
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

	return c
}

// cleanupTimeout bounds removing a container once its caller's context is gone.
const cleanupTimeout = 30 * time.Second

// ensureImage pulls ref unless the local Docker already has it.
func ensureImage(ctx context.Context, docker *client.Client, log *slog.Logger, ref string) error {
	_, err := docker.ImageInspect(ctx, ref)
	if err == nil {
		return nil
	}

	if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspect image %s: %w", ref, err)
	}

	log.InfoContext(ctx, "pulling image", slog.String("image", ref))

	pull, err := docker.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}

	if err := pull.Wait(ctx); err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}

	return nil
}
