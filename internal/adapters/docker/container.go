package docker

import (
	"regexp"

	"github.com/moby/moby/api/types/container"
)

// The image contract: the agent image runs as uid 1000 (its node user, home /home/node).
const (
	containerUID  = "1000"
	containerUser = containerUID + ":" + containerUID
)

// Labels on every container goald starts for a run (its box and its sidecars), so Sandboxes.List
// finds them and Sandboxes.Delete removes them with their run; LabelOrg names the organization
// whose store knows the run (ports.Leftover.Org).
const (
	LabelOrg     = "goal.org"
	LabelFactory = "goal.factory"
	LabelItem    = "goal.item"
	LabelRun     = "goal.run"
	LabelStation = "goal.station"
	LabelAgent   = "goal.agent"
)

// envHome is the variable a sidecar's home is set by.
const envHome = "HOME"

// safeID matches run ids usable in a container name.
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

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
