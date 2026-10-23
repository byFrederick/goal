# worker

The agent container image: TypeScript on Node 22 with the Claude Agent SDK (pinned exactly in `package.json`). goald's Docker runner (`internal/adapters/docker`) starts one hardened container per agent task; the worker runs the agent once and reports progress and its result as JSON lines on stdout.

```bash
task worker:check   # typecheck, Biome, Vitest (part of task check)
task worker:image       # docker build -f worker/Dockerfile --target runtime -t ghcr.io/byfrederick/goal-agent:0.1.0 .
task worker:image-dev   # the dev variant, ghcr.io/byfrederick/goal-agent-dev:0.1.0 (below)
```

## Dev variant

`--target dev` adds the toolchain a factory that builds this repository needs: Go (with a C compiler for `-race`), task, golangci-lint v2, pnpm, Terraform with the AWS provider mirrored (`TF_CLI_CONFIG_FILE`), and a PostgreSQL 18 server, which `task migrate-check` uses for a throwaway local cluster when there is no Docker. Go modules from `go.mod` are prewarmed as a read-only local `GOPROXY`; anything new still downloads. The goal factory names this image in `runtime.image`, so its agents and its station command checks run with it. Checks run in their own container of the same image (`docker.Checker`): same hardening, the run's workspace at `/workspace`, `HOME` and `CI` only in the environment, no secrets, no gateway token, no Docker socket.

The image is built locally and never pushed from this repository.

## Container contract

| Path | Mode | Content |
| --- | --- | --- |
| `/goal/spec.json` | read-only | the task, without secrets (below) |
| `/goal/agent/prompt.md` | read-only | the agent's prompt, appended to Claude Code's system prompt |
| `/goal/agent/skills/<name>/SKILL.md` | read-only | one folder per skill the agent loads, with its other files |
| `/goal/agent/output.schema.json` | read-only | the station's output contract (JSON Schema), only when the station has one |
| `/workspace` | read-write | the run's git checkout, prepared by goald; the agent's working directory. No git or GitHub credentials are mounted or passed. |
| `/tmp` | tmpfs (noexec) | scratch space |
| `/home/node` | anonymous volume | `HOME`, disk-backed so toolchain caches fit; removed with the container. Everything else is a read-only root filesystem |

The runner writes `spec.json` into a per-run host folder (`$TMPDIR/goal-runs/<run id>/` by default) and has goald's `AgentFiles` write `agent/` next to it, then mounts the folder at `/goal`. The container runs as uid 1000 with every capability dropped, `no-new-privileges`, an init process, and CPU, memory and PID limits; it carries the labels `goal.factory`, `goal.item`, `goal.run`, `goal.station` and `goal.agent`, and `Runner.Reap` removes containers and folders of runs that are no longer live.

Environment: only the task's `Env` from goald (which carries `CLAUDE_CODE_OAUTH_TOKEN`), `GOAL_GATEWAY_TOKEN` and `HOME`. `ANTHROPIC_API_KEY` is removed by the runner and again by the worker before the SDK starts; the gateway token is removed from the agent's environment too and reaches the gateway only as a header.

### spec.json

```json
{
  "factory_id": "…", "item_id": "…", "run_id": "…", "station": "build", "agent": "backend-engineer",
  "model": "claude-sonnet-5-5",
  "skills": ["goal-conventions"],
  "tools_allow": ["Read", "Edit", "Bash"], "tools_deny": ["Bash(git push*)"],
  "gateway_url": "http://host.docker.internal:8081/mcp",
  "brief": "title and body of the item",
  "inputs": { "spec": { "…": "outputs of earlier stations" } },
  "output": "change",
  "max_turns": 120, "max_usd": 6
}
```

## How the agent runs

- Working directory `/workspace`; Claude Code's system prompt with `prompt.md` appended; the brief, earlier outputs and the finishing instruction as the user prompt.
- Tools: the built-in tools named by `tools_allow` (Claude Code permission rules such as `Bash(npm test*)`) are the only ones available, and those rules are pre-approved. `tools_deny` rules are disallowed. Permission mode `dontAsk`: anything not pre-approved is denied, since nobody is there to ask. An agent that allows no tools gets `Read`, `Grep` and `Glob`.
- Skills: copied from `/goal/agent/skills` to `~/.claude/skills`; only the listed names are enabled. Settings sources are `user` (those skills) and `project` (the checkout's `CLAUDE.md` and `.claude/`).
- MCP: exactly one server, `goal`, at `gateway_url` over streamable HTTP with `Authorization: Bearer $GOAL_GATEWAY_TOKEN` (`strictMcpConfig`, so `.mcp.json` and other configs are ignored). Its tools are pre-approved and always loaded (`alwaysLoad`): Claude Code would otherwise defer them behind tool search, and agents called them by name and failed. The gateway serves only the agent's allowed tools, which keeps the list short.
- Limits: `max_turns` and `max_usd` become the SDK's `maxTurns` and `maxBudgetUsd`; goald also kills the container when the task's timeout passes.
- Output: with `output.schema.json` the SDK's `json_schema` output format; without it the final reply is read as JSON (whole reply, a fenced block or the outermost object), or passed on as a JSON string. goald validates it against the contract either way.
- `SIGTERM` (docker stop) aborts the session.

## stdout protocol

One JSON object per line, each with `type` and `at` (RFC 3339). Logs go to stderr. goald parses it in `internal/adapters/docker/protocol.go` and redacts secret values from every event.

| `type` | Fields | goald event kind |
| --- | --- | --- |
| `started` | `session_id`, `model`, `tools`, `skills`, `mcp_servers` (`name`, `status`) | `agent_started` |
| `tool_use` | `id`, `tool`, `input` (JSON, cut to 500 chars) | `tool_call` |
| `tool_result` | `id`, `is_error`, `summary` (cut to 500 chars) | `tool_result` |
| `message` | `text` (assistant text, cut to 2000 chars) | `agent_message` |
| `usage` | `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens` (per API message) | `usage` |
| `finished` | `status` (`succeeded` or `failed`), `subtype`, `output`, `error`, `cost_usd`, `turns`, `input_tokens`, `output_tokens`, `session_id` | the run's result |

`finished` is always the last line. The exit code is 0 when the agent produced a result (even a failed one) and 1 when the worker crashed; a container that exits without a `finished` line is a failed run quoting the tail of stderr.

## Networking

The container runs on Docker's default bridge network. goald's gateway URL on the host's loopback (`127.0.0.1`, `localhost`, `::1`) is rewritten to `host.docker.internal`, which Docker Desktop and OrbStack on macOS resolve to the host; the runner also adds `host.docker.internal:host-gateway` so the same name works on Linux. On macOS a gateway listening on the host's `127.0.0.1` is reachable that way; on Linux it must listen on an address the bridge reaches (for example the `docker0` address), not only on loopback.
