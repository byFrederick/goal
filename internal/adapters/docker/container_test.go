package docker

import (
	"cmp"
	"fmt"

	"github.com/moby/moby/api/types/container"
)

// hardening is the security-relevant part of a container's configuration, comparable at once.
type hardening struct {
	User, CapDrop, SecurityOpt, Mounts, Labels, Env, Tmpfs string
	Privileged, ReadOnlyRoot, Init                         bool
	CPUs, Memory, Swap, PIDs                               int64
}

func hardeningOf(cc *container.Config, hc *container.HostConfig) hardening {
	mounts := make([]string, 0, len(hc.Mounts))
	for _, m := range hc.Mounts {
		mounts = append(mounts, fmt.Sprintf("%s %s:%s:ro=%v", m.Type, m.Source, m.Target, m.ReadOnly))
	}

	return hardening{
		User: cc.User, CapDrop: fmt.Sprint(hc.CapDrop), SecurityOpt: fmt.Sprint(hc.SecurityOpt), Mounts: fmt.Sprint(mounts),
		Labels: fmt.Sprint(cc.Labels), Env: fmt.Sprint(cc.Env), Tmpfs: fmt.Sprint(hc.Tmpfs),
		Privileged: hc.Privileged, ReadOnlyRoot: hc.ReadonlyRootfs, Init: hc.Init != nil && *hc.Init,
		CPUs: hc.NanoCPUs, Memory: hc.Memory, Swap: hc.MemorySwap, PIDs: *cmp.Or(hc.PidsLimit, new(int64)),
	}
}
