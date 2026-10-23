# ADR 0006 — Run workspaces and isolated command checks

Status: accepted · 2026-10-23 · Builds on [ADR 0003](0003-hexagonal-architecture-and-deployment.md) and [ADR 0005](0005-factory-definitions-in-object-storage.md)

## Context

Phase 1 runs agents that change a repository and open pull requests. Each run needs a checkout to work in; the GitHub credential that makes the checkout and the pull request possible must never reach the agent's container; and a station's deterministic checks (`task check`, `task migrate-check`) must run against what the agent wrote before a reviewer sees it. That code is untrusted: an agent can edit the Taskfile, the tests or a migration, so running a check means running agent-written code.

`goald` is stateless (AGENTS.md), so nothing about a run's checkout may live only on one instance's disk between runs.

## Decision

1. **The repository is part of the factory definition.** `spec.runtime.repository` names an https clone URL and, for a private repository, a secret reference for its token. A factory without one gives its agents an empty workspace.
2. **Every run gets a fresh clone on the item's branch.** `ports.Workspaces` prepares `<GOAL_WORKSPACE_ROOT>/<run id>/repo` and removes it when the run ends. The branch is `goal/<number>-<title slug>` (`domain.Item.Branch`), a pure function of the item, so every run of the item (a retry, the reviewer, rework after a rejection) checks out the same branch: the remote branch when it exists, otherwise a new one from the default branch. **The remote branch is the durable state** between runs; no instance keeps a checkout.
3. **The clone credential stays in one git process.** `internal/adapters/gitworkspace` passes the token to `git clone` only, as an `http.<origin>/.extraHeader` in `GIT_CONFIG_*` environment variables (never argv, never a file), scoped to the repository's origin, with credential helpers, prompts and the user's global and system git configuration switched off. Nothing in the workspace holds it; a later `git fetch` there fails. Agents publish through the MCP gateway's GitHub tools, where the gateway adds the token on the host.
4. **Command checks run in their own hardened container.** `ports.Checker` runs each `kind: command` check of a station, in order, after the output satisfies the station's contract. The Docker adapter starts a container of the factory's image with the same hardening as an agent task (non-root, read-only root, no capabilities, no new privileges, resource limits, a throwaway home volume), the run's workspace at `/workspace`, `HOME` and `CI` as its only environment, and network access for module and provider downloads. It gets no secrets, no gateway token and no Docker socket. A failing check is retried once like a broken contract: the agent gets the command, its exit code and the tail of its output; a second failure blocks the item. A check is never run on the host.
5. **Toolchains ship in a dev variant of the agent image.** `worker/Dockerfile --target dev` (`task worker:image-dev`) adds what a factory building this repository needs: Go with a C compiler (`-race`), task, golangci-lint, pnpm, Terraform with the AWS provider mirrored for offline `terraform init`, and a PostgreSQL 18 server. Factories name it in `runtime.image`, so their agents and their checks use the same tools.
6. **`task migrate-check` needs no Docker.** Where `docker` is not on PATH (inside the check container), it applies the migrations to a throwaway PostgreSQL 18 cluster started in a temporary folder; with Docker it keeps using a throwaway `postgres:18` container. Neither touches goald's database.

## Rejected

- **Running checks on the host**: one edited Taskfile and the agent runs code as the user, with their credentials and Docker socket.
- **Giving the check container the Docker socket** (for `task migrate-check` as written): the socket is root on the host.
- **A throwaway database on the compose PostgreSQL**: the check would hold credentials for the server that also stores goald's state.
- **Keeping a checkout per item across runs**: it would make an instance stateful and break on another instance; the pushed branch already carries the work.
- **Cloning with the token in the URL or a credential helper file**: both leave the token in `.git/config` or on disk where the container can read it.

## Consequences

- A check gets network access; its blast radius is the container and the run's workspace. Egress control is a later hardening step.
- An agent that forgets to push loses its work at the end of the run; the next run starts from the remote branch. Prompts say how to publish.
- Every check run pays a container start and, without warm caches, downloads (Go modules come from a read-only proxy prewarmed in the dev image).
