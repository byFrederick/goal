# worker

The agent container image: TypeScript on Node 22 with the Claude Agent SDK (pinned exactly in `package.json`). goald's Docker runner (`internal/adapters/docker`) starts one hardened container per agent task; the worker runs the agent once and reports progress and its result as JSON lines on stdout.

```bash
task worker:check   # typecheck, Biome, Vitest (part of task check)
task worker:image       # docker build -f worker/Dockerfile --target runtime -t ghcr.io/byfrederick/goal-agent:0.1.0 .
task worker:image-dev   # the dev variant, ghcr.io/byfrederick/goal-agent-dev:0.1.0 (below)
```

## Dev variant

`--target dev` adds the toolchain a factory that builds this repository needs: Go (with a C compiler for `-race`), task, golangci-lint v2, pnpm, Terraform with the AWS provider mirrored (`TF_CLI_CONFIG_FILE`), and a PostgreSQL 18 server, which `task migrate-check` uses for a throwaway local cluster when there is no Docker. Go modules from `go.mod` are prewarmed as a read-only local `GOPROXY`; anything new still downloads in a check, and in an agent only from a host `GOAL_AGENT_EGRESS` adds (such as `proxy.golang.org` or `registry.npmjs.org`): agents reach only the model API by default. The goal factory names this image in `runtime.image`, so its agents and its station command checks run with it. Checks run in their own container of the same image (`docker.Checker`): same hardening, the run's workspace at `/workspace`, `HOME`, `CI` and the egress proxy's variables only in the environment, no secrets, no gateway token, no Docker socket, and the tools' network: public hosts through the proxy, never the host or a private address.

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

The runner writes `spec.json` into a per-run host folder (`$TMPDIR/goal-runs/<run id>/` by default) and has goald's `AgentFiles` write `agent/` next to it, then mounts the folder at `/goal`. The container runs as uid 1000 with every capability dropped, `no-new-privileges`, an init process, and CPU, memory and PID limits; it carries the labels `goal.org`, `goal.factory`, `goal.item`, `goal.run`, `goal.station` and `goal.agent`, and goald's reaper removes the containers and folders of runs that are over (neither running nor holding their lease, whichever goald on the host started them).

Environment: only the task's `Env` from goald (which carries `CLAUDE_CODE_OAUTH_TOKEN`), `GOAL_GATEWAY_TOKEN`, `HOME`, `CLAUDE_CONFIG_DIR=/home/node/.claude` and the egress proxy's variables (Networking, below; all set by the runner over anything in `Env`; the config dir set again by the worker, so Claude Code's config always lives in the container's throwaway home; no host `~/.claude` is ever mounted). `ANTHROPIC_API_KEY` is removed by the runner and again by the worker before the SDK starts; the gateway token is removed from the agent's environment too and reaches the gateway only as a header.

### spec.json

```json
{
  "factory_id": "…", "item_id": "…", "run_id": "…", "station": "build", "agent": "backend-engineer",
  "model": "claude-sonnet-5-5",
  "skills": ["goal-conventions"],
  "tools_allow": ["Read", "Edit", "Bash"], "tools_deny": ["Bash(git push*)"],
  "gateway_url": "http://goal-egress:3128/mcp",
  "purpose": "what the factory is for (omitted when it has none)",
  "brief": "title and body of the item",
  "inputs": { "spec": { "…": "outputs of earlier stations" } },
  "output": "change",
  "max_turns": 120, "max_usd": 6
}
```

## How the agent runs

- Working directory `/workspace`; Claude Code's system prompt with, appended, `## This factory` and the `purpose` (when set), then `prompt.md`; the brief, earlier outputs and the finishing instruction as the user prompt.
- Tools: the built-in tools named by `tools_allow` (Claude Code permission rules such as `Bash(npm test*)`) are the only ones available, and those rules are pre-approved. `tools_deny` rules are disallowed. Permission mode `dontAsk`: anything not pre-approved is denied, since nobody is there to ask. An agent that allows no tools gets `Read`, `Grep` and `Glob`.
- Skills: copied from `/goal/agent/skills` to `$CLAUDE_CONFIG_DIR/skills`; only the listed names are enabled (the SDK's `skills` option): they are the only skills listed to the model and the only ones the Skill tool accepts, and the only ones the `started` event reports. Claude Code would otherwise also discover the skills bundled with it (loop, schedule, claude-api, …), skills and plugins synced from the claude.ai account behind the token, and the checkout's `.claude/skills`; the worker turns the first two off (`settings`: `disableBundledSkills`, `syncClaudeAiSkills: false`, `syncClaudeAiPlugins: false`, `disableClaudeAiConnectors`) and the filter hides the rest. No plugins are loaded. Settings sources are `user` (those skills) and `project` (the checkout's `CLAUDE.md` and `.claude/`).
- MCP: exactly one server, `goal`, at `gateway_url` over streamable HTTP with `Authorization: Bearer $GOAL_GATEWAY_TOKEN` (`strictMcpConfig`, so `.mcp.json` and other configs are ignored). Its tools are pre-approved and always loaded (`alwaysLoad`): Claude Code would otherwise defer them behind tool search, and agents called them by name and failed. The gateway serves only the agent's allowed tools, which keeps the list short.
- Limits: `max_turns` and `max_usd` become the SDK's `maxTurns` and `maxBudgetUsd`; goald also kills the container when the task's timeout passes.
- Output: with `output.schema.json` the SDK's `json_schema` output format; without it the final reply is read as JSON (whole reply, a fenced block or the outermost object), or passed on as a JSON string. goald validates it against the contract either way.
- `SIGTERM` (docker stop) aborts the session.

## stdout protocol

One JSON object per line, each with `type` and `at` (RFC 3339). Logs go to stderr. goald decodes it with `domain.DecodeWorkerLine` (`internal/domain/worker.go`) and redacts secret values from every event.

| `type` | Fields | goald event kind |
| --- | --- | --- |
| `started` | `session_id`, `model`, `tools`, `skills` (the enabled ones Claude Code found), `mcp_servers` (`name`, `status`) | `agent_started` |
| `tool_use` | `id`, `tool`, `input` (JSON, cut to 500 chars) | `tool_call` |
| `tool_result` | `id`, `is_error`, `summary` (cut to 500 chars) | `tool_result` |
| `message` | `text` (assistant text, cut to 2000 chars) | `agent_message` |
| `usage` | `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`: one event per API call (one model turn), with that call's final counts, not a running total. They come from the stream's `message_delta` (`includePartialMessages`): the SDK's assistant messages carry only the opening counts (`output_tokens` 1). The run's totals are in `finished` | `usage` |
| `finished` | `status` (`succeeded` or `failed`), `subtype`, `output`, `error`, `cost_usd`, `turns`, `input_tokens`, `output_tokens`, `session_id`, `permission_denials` (`[{tool, id}]`, the calls the policy refused; absent when none) | the run's result; goald records each denial as a `tool_denied` event |

`finished` is always the last line. The exit code is 0 when the agent produced a result (even a failed one) and 1 when the worker crashed; a container that exits without a `finished` line is a failed run quoting the tail of stderr.

## Networking

The container runs on goald's agents' network (`<GOAL_EGRESS_NAME>-agents`, ADR 0009): an internal Docker network without a bridge address, so it has no route to the host, to other networks or to the internet. The only other container on it is goald's egress proxy (`cmd/goal-egress`), named like the network (`goal-egress` by default), and the runner sets `HTTP_PROXY`, `HTTPS_PROXY` (and the lower-case forms) to it, `NO_PROXY` to the proxy itself and `NODE_USE_ENV_PROXY=1`. Through it an agent reaches the model API (`GOAL_AGENT_EGRESS`, default `api.anthropic.com`) over `CONNECT`, and goald's gateway at `gateway_url`, which is the proxy's own address at the gateway's path (`http://goal-egress:3128/mcp`): the proxy forwards exactly that path to goald and nothing else of it. Every other destination, and any name that resolves to a loopback, private or link-local address, is refused. The worker turns off Claude Code's nonessential traffic (`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`: telemetry, error reports, the updater), which the proxy would refuse anyway. A tool that fetches from the web (`WebFetch`, `curl` in `Bash`) reaches only those hosts.
