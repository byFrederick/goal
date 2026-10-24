# ADR 0009 — Agent network isolation and the egress proxy

Status: accepted · 2026-10-24 · Builds on [ADR 0003](0003-hexagonal-architecture-and-deployment.md), [ADR 0006](0006-workspaces-and-command-checks.md) and [ADR 0007](0007-identity-better-auth.md). (0008 is reserved for phase 4's connectors, [spec 4](../specs/4-credentials-and-connectors.md) §15.)

## Context

Agent containers ran on Docker's default bridge with `host.docker.internal:host-gateway`, and networked sidecars and command checks ran there too. Under Docker Desktop and OrbStack that name, and the host's own address on the bridge, forward to the host's loopback, so a prompt-injected agent reached every service a laptop keeps there: goald's `/v1` and `/api/auth` (with a forged `Host`), the auth service on 8081, Mailpit's API on 8025 (read the owner's sign-in code, take over the account) and PostgreSQL on 5432 with the published dev passwords (review of phase 3, H1). A check runs agent-written code, so it had the same reach. Agents also had the whole internet, so a stolen model token or workspace could leave from any host.

What each kind of container needs: an agent, goald's MCP gateway (`/mcp`) and the model API (`api.anthropic.com` over https; Claude Code's other traffic, telemetry, error reports and the updater, is optional and turned off); a networked sidecar or a check, the public hosts its server or toolchain talks to. None needs the host, a private network or a cloud metadata service. The fix must work the same under OrbStack, Docker Desktop and Docker Engine on Linux, keep goald stateless, and map onto ECS.

## Decision

1. **Two internal networks with no way out.** The Docker adapter keeps `<name>-agents` and `<name>-tools` (`GOAL_EGRESS_NAME`, default `goal-egress`): `internal: true` bridge networks with no address on the host's side (`com.docker.network.bridge.inhibit_ipv4`), so a container on them has no route to the host, to other networks or to the internet, and Docker's DNS answers only container names. Agent tasks run on the agents' network; sidecars with `network: true` and command checks on the tools' one; sidecars without network keep `none`. No container gets `host.docker.internal` any more.
2. **One egress proxy is the only way out.** `cmd/goal-egress` (package `internal/egress`, standard library only, image built by `task egress:image` from distroless, never pushed) runs as a container named `<name>` on both internal networks and on the default bridge, hardened like every goal container (nonroot, read-only root, no capabilities, 256 MiB). Every networked container gets `HTTP_PROXY`/`HTTPS_PROXY` (both cases) pointing at it, `NO_PROXY` for the proxy itself and `NODE_USE_ENV_PROXY=1`. The proxy decides by the client's source network:
   - agents: `CONNECT` (and plain-http forwarding) to the hosts of `GOAL_AGENT_EGRESS` only, default `api.anthropic.com:443`;
   - tools: any destination whose address is public;
   - anyone else: nothing.
3. **Nothing private, whatever the name.** The proxy resolves every destination itself and dials only the addresses it checked: a name with any loopback, RFC 1918, unique-local, link-local (169.254.0.0/16, the cloud metadata services, `fd00:ec2::254`), CGNAT, reserved, documentation, "this network" (OrbStack's host is in 0.0.0.0/8) or NAT64/6to4 address is refused (`egress.Public`). The check and the dial use the same answer, so DNS rebinding cannot slip a private address in between. A refused request is a 403 and a log line (role, client, destination; never a path or a header).
4. **The gateway through the proxy, at its path only.** An agent's `gateway_url` is the proxy's own address at the gateway's path (`http://goal-egress:3128/mcp`). The proxy forwards exactly that path, from the agents' network only, to goald's gateway as the proxy reaches it (a loopback `GOAL_GATEWAY_URL` becomes `host.docker.internal`, which only the proxy resolves); `/v1`, `/api/auth`, the SPA and every other path are 404, encoded paths included. The task token still authorizes each call. We chose this over a goald listener on the agents' network because goald runs on the macOS host under Docker Desktop and OrbStack and cannot join a Docker network; the same container works on all three engines. On Linux goald must listen where the bridge reaches it, as before (`GOAL_GATEWAY_LISTEN=172.17.0.1:8090`).
5. **Owned by the Docker adapter, recreated by any goald.** `docker.Egress` creates the networks and the proxy when missing and checks the proxy before every agent task, networked sidecar and check starts (one inspect). The proxy carries a hash of its configuration (image, gateway, allowlists, the networks' subnets); a goald whose configuration differs replaces it under the same name, so stale proxies do not pile up, and the networks are kept. The proxy keeps no state and outlives goald like compose's services; run reaping is unchanged, since the proxy and its networks belong to no run. goalds of different configurations on one Docker host set different `GOAL_EGRESS_NAME`s.

## ECS

The same split maps onto `awsvpc` without the proxy knowing ECS: agent tasks and tool tasks run in private subnets of their own with no NAT route and security groups whose only egress is the proxy's port (plus the VPC endpoints ECS itself needs: ECR, CloudWatch Logs); the proxy runs as a service of this image in a subnet with NAT, given the two subnets' CIDRs as `-agents` and `-tools`, and forwards `/mcp` to the gateway's own internal listener (spec 3 §5.5). Agent tasks get no task role, and the proxy refuses 169.254.170.2 like any link-local address. AWS Network Firewall with domain lists is the managed alternative when the allowlists grow.

## Rejected

- **Firewall rules on the host** (iptables, pf): different on every engine, invisible under Docker Desktop and OrbStack, and not ours to change on a laptop.
- **A second goald listener for the gateway**: Docker Desktop and OrbStack forward every loopback port, so it would not keep agents from `/v1`; it is not reachable from an internal network anyway.
- **Squid, Smokescreen or another proxy image**: each needs a configuration language and has no reverse path for exactly `/mcp`; the policy here is small enough to own, test and read in one file.
- **A network per run**: true isolation between runs, but Docker's default address pools hold about thirty networks, and connecting the proxy to each adds state the adapter would have to track. Containers on one network can reach each other's listening ports; see consequences.
- **Allowing agents the internet**: an agent holds the model token and the workspace; its egress is now as narrow as its job.

## Consequences

- An agent reaches the model API and the gateway only. A factory whose agents fetch packages themselves (`pnpm install`, `go mod download` beyond the prewarmed cache, `WebFetch`) needs those hosts in `GOAL_AGENT_EGRESS`; checks still reach any public host. Tools that ignore proxy variables (raw sockets, `git` over ssh) have no network at all.
- `task egress:image` must have run once (`task up` does it); without the image goald starts but logs that no agent, networked sidecar or check can start.
- Containers on the same internal network can reach each other's listening ports. Agents and checks open none by default; per-run isolation (a network per run, or security groups per task on ECS) is a later step.
- Phase 4's egress filtering for sidecars starts from here: the tools' policy is "public hosts"; a connection's own host allowlist becomes another policy of the same proxy.
