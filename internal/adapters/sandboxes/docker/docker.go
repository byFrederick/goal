// Package docker implements ports.Sandboxes with local Docker containers (Docker Desktop,
// OrbStack or Docker Engine on Linux): one dev box per run (sandboxes.go), into which goald execs
// the agent image's commands; a run's stdio MCP servers run inside its box. Nothing is mounted
// from the host: a run's box gets everything it needs from the gateway's run bundle.
// apps/worker/README.md describes the contract from the box's side.
//
// Networking (network.go, ADR 0009 and its phase 4 amendment): boxes run on compose.yaml's agents
// network (GOAL_SANDBOX_NETWORK), which has no route to the host, to other networks or to the
// internet. The one other container there is the egress proxy (compose's goal-egress service,
// cmd/goal-egress), which every box's HTTP_PROXY and HTTPS_PROXY name: it reaches public hosts
// and, at its paths only, goald's gateway (the gateway URL a box is given is the proxy's,
// BoxGatewayURL). Nothing reaches a loopback, private or link-local address through it.
package docker

import (
	"context"
	"fmt"
	"log/slog"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// Config holds the limits of a box of one size.
type Config struct {
	CPUs        float64
	MemoryBytes int64
	PIDs        int64
}

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
