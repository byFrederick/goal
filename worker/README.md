# worker

`goal-agent`, the one image every run's sandbox starts from (spec 4 §4.1, ADR 0008), and the worker inside it: TypeScript on Node 22 with the Claude Agent SDK (pinned exactly in `package.json`), which prepares the sandbox from the gateway's run bundle and runs the agent once, reporting progress and its result as JSON lines on stdout.

```bash
task worker:check   # typecheck, Biome, Vitest (part of task check)
task worker:image   # docker build -f worker/Dockerfile --target runtime -t ghcr.io/byfrederick/goal-agent:0.2.0 ., then task worker:smoke
task worker:smoke   # the image's toolchains run as uid 1000 and goal-agent answers
```

Locally the image is built and never pushed; only the production deploy pushes it, to Cloudflare's registry (`deploy/cloudflare/scripts/deploy.sh`, reproducibly: unchanged inputs keep its digest). `task up` (and `task up:compose`, `task goald`) builds it first, without the smoke, so runs never start from an image older than the checkout's worker: an older worker silently drops what the run bundle gained since.

## Image

Debian 13 (trixie, slim) with, every version pinned in `worker/Dockerfile` and every download checked against its project's published checksums:

- the worker, `goal-agent` on `PATH` (Node from mise below, whatever Node a repository pins);
- git (identity `Goal agent <goal-agent@example.com>` in `/etc/gitconfig`, `GIT_TERMINAL_PROMPT=0`), gh, ripgrep, jq, curl, openssh-client, gcc, libc headers and make (cgo, `go test -race`, native modules);
- mise, with Go, Node and Python installed in `/opt/mise` (`MISE_DATA_DIR`, owned by uid 1000) from `/etc/mise/config.toml`, its shims first on `PATH`. A repository that pins the same versions installs nothing; others install at prepare. `MISE_TRUSTED_CONFIG_PATHS=/workspace` trusts the checkout's mise config, and `npm:` tools install with npm itself (`npm.package_manager = "npm"`: the automatic choice writes shims that start every bin with node, which breaks a native bin such as pnpm 12's). mise skips npm install scripts, so a tool whose postinstall puts its binary in place, as pnpm 12's does, needs `npm_args = "--ignore-scripts=false"` in the repository's mise config (this one's `.mise.toml` has it);
- PostgreSQL 18's server and client binaries (`/usr/lib/postgresql/18/bin` on `PATH`) and no cluster: a run that needs a database starts its own (`initdb`, `pg_ctl`).

It runs as uid 1000 (`node`, `HOME=/home/node`), works in `/workspace` and idles (`sleep infinity`) until goald execs into it. The filesystem is writable: an agent installs what it needs with mise, npm, pip or `go install`; the sandbox is deleted after the run.

On Cloudflare (`internal/adapters/cloudflare`, `deploy/cloudflare`) the same image runs with the `durable_object` scheduling policy, which needs it built for linux/amd64 (the deploy's buildx build does) and named by digest and takes about 1.5 GB of the instance's disk: `standard-2` for `small`, `standard-3` for `medium`, `standard-4` for `large`. The control Worker replaces its `CMD` with an entrypoint that records the image's environment and working directory, then sleeps until the run's deadline, and runs every command under GNU coreutils' `timeout`, which Debian's coreutils provides here: the runtime's exec inherits neither the image's environment nor the box's (only `PATH`), and its kill reaches only the process it started, so the Worker gives each command the image's environment, then the box's, then its own, and lets `timeout` end the whole process group. uid 1000 still owns what the agent creates, but under that policy every process holds root's capabilities: the microVM is the boundary (spec 4 §11.3).

## Sandbox contract

goald creates one sandbox per run (`ports.Sandboxes`) and execs, in order:

| Exec | Environment from goald | Does |
| --- | --- | --- |
| `goal-agent prepare` | `GOAL_GATEWAY_URL` (the sandbox's), `GOAL_GATEWAY_TOKEN`, the password variable of the repository connection's git login (such as `GH_TOKEN`) | builds `/goal` and `/workspace` from the run bundle (below); one `prepared` line on stdout; exit 0, or 1 with the reason on stderr |
| `goal-agent run` | `GOAL_GATEWAY_TOKEN`, the model connection's env, the env of the agent's own connections, `GOAL_FEEDBACK` when goald adds to the brief | runs the agent once (How the agent runs); the stdout protocol; exit 0 with a result, 1 when the worker crashed |
| a station check, `sh -c '<command>'` in `/workspace` | `HOME`, `CI=true` | the exit code is the runtime's |

`goal-agent` without a command prints its usage and exits 2.

| Path | Content |
| --- | --- |
| `/goal/spec.json` | the task, without secrets (below) |
| `/goal/agent/prompt.md` | the agent's prompt, appended to Claude Code's system prompt |
| `/goal/agent/skills/<name>/SKILL.md` | one folder per skill that ships in the factory, with its other files |
| `/goal/agent/output.schema.json` | the station's output contract (JSON Schema, without `$schema`), only when the station has one; for a part (ADR 0012) without the `plan` property and its `required` entry, since a part never plans an assembly |
| `/workspace` | the factory's repository on the item's branch, or empty; the agent's working directory |
| `/home/node` | `HOME`: Claude Code's config dir (`.claude`), mise's global config, git's credential helpers |

`GOAL_FEEDBACK` is what goald adds to the brief for one attempt: for a part, an `## Assembly` section naming its assembly (ref and title), its key, the other parts with their state, the parts it needs and that a part returns a spec, never a plan; the notes people left; on a retry in the same sandbox, why the previous attempt was rejected; and what the workspace holds. The worker puts it right after the brief. `GOAL_GATEWAY_TOKEN` is removed from the agent's own environment (the token reaches the gateway only as a header); everything else goald gave the exec is the agent's, its connections' credentials included (spec 4 §11.3).

### prepare and the run bundle

`goal-agent prepare` fetches `GET <origin of GOAL_GATEWAY_URL>/run` with `Authorization: Bearer $GOAL_GATEWAY_TOKEN` (`internal/gateway`, `ServeRun`; 401 for a bad token, 403 for a run the token's version or organization does not have). The bundle holds no secret: git credentials and MCP headers name the variables that hold the values.

```json
{
  "spec": { "…": "spec.json below, without gateway_url" },
  "files": { "agent/prompt.md": "<base64>", "agent/skills/acme-conventions/SKILL.md": "<base64>", "agent/output.schema.json": "<base64>" },
  "repository": { "url": "https://github.com/acme/api.git", "branch": "goal/7-add-a-health-endpoint", "fresh_base": "rebase" },
  "git_credentials": [{ "host": "github.com", "username": "x-access-token", "password_env": "GH_TOKEN" }],
  "tools": ["task@3.46.1", "aqua:cli/cli@2.83.0"]
}
```

`git_credentials` are the git logins of the repository's connection and of the agent's own connections (one per host, user and variable); `tools` are the definition's `runtime.tools`, then the CLI tools of the agent's connections. `internal/gateway/testdata/run-bundle.json` is a real one; the Go and the Vitest tests both read it. Then, in order:

1. Each file under `/goal` at its path (relative, no `.` or `..`).
2. Per git credential, a helper for `https://<host>` added to the global git config (`--add`: git asks each helper of a host in turn) that answers git's `get` with the user name and the value of `password_env`, read when git asks: the password is only ever in the environment of the exec that runs git, so prepare clones with the repository connection's and the agent pushes with one of its own.
3. With a repository: `git clone` into `/workspace` and the item's branch checked out, the remote one when earlier runs pushed it, otherwise new from the default branch. A branch that existed is then brought up to date with the default branch (`origin/HEAD`, which the clone sets) as `repository.fresh_base` says (the definition's `flow.assemblies.fresh_base`; ADR 0012): `rebase`, the default, runs `git rebase origin/<default>`; `merge` runs `git merge --no-edit origin/<default>`. The box's git identity (`Goal agent`, `/etc/gitconfig`) signs the rebased or merge commits. On a conflict prepare runs `git rebase --abort` or `git merge --abort`, so the branch stays as pushed with a clean tree, and reports the conflicting files (`git diff --name-only --diff-filter=U`, sorted) instead of failing; any other failure fails prepare. A new branch is neither rebased nor merged. Then `mise install` there for the repository's own tools (`mise.toml`, `.tool-versions`, …).
4. `spec.json` with `gateway_url` set to `GOAL_GATEWAY_URL` and, after a conflict in step 3, `base_conflicts` listing the files.
5. `mise use --global` for `tools`, so they are on `PATH` wherever the agent works.
6. One line on stdout: `{"type":"prepared","at":…,"seconds":5.4,"tools":1,"branch":"goal/7-…","branch_existed":true,"fresh_base":"rebase"}` (`branch` and `branch_existed` only with a repository, `fresh_base` only for a branch that existed, and `"conflicts":["go.mod",…]` only after a conflict). `tools` counts the bundle's `tools` (step 5), not the repository's own from step 3: a repository whose `.mise.toml` names six tools and a definition without `runtime.tools` prepare with `"tools":0`.

After a rebase the local branch has diverged from `origin/<branch>`, so the agent's plain `git push` is rejected: it pushes with `git push --force-with-lease`, which its `tools_allow` must allow and its `tools_deny` must not deny (a rule such as `Bash(git push --force*)` denies it). A definition that denies force pushes uses `fresh_base: merge`, whose merge commit pushes plainly.

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
  "max_turns": 120, "max_usd": 6,
  "connections": [
    { "name": "github", "kind": "github", "title": "GitHub", "env": ["GH_HOST", "GH_TOKEN"],
      "mcp": { "url": "https://api.githubcopilot.com/mcp/", "headers": { "Authorization": "Bearer ${GH_TOKEN}" } },
      "docs": "Use gh and git; GH_TOKEN is set." },
    { "name": "playwright", "kind": "mcp-server", "title": "MCP server", "env": [],
      "mcp": { "command": ["sh", "-c", "npx -y @playwright/mcp@0.0.41"] } }
  ],
  "knowledge_sources": ["handbook"],
  "base_conflicts": ["go.mod"]
}
```

`connections` are the agent's organization connections its token grants (the model and repository connections are not among them unless the agent names them): the names of the variables each sets in `goal-agent run`'s env, never a value, its MCP server and its kind's docs. `base_conflicts` (only after a conflict in prepare's step 3) names the files that conflict with the default branch.

## How the agent runs

- Working directory `/workspace`; Claude Code's system prompt with, appended, `## This factory` and the `purpose` (when set), then `prompt.md`, then `## Connections`: each connection's name and kind title, the names of its variables, its MCP server (`mcp__<name>__*`) and its kind's docs, never a value, then `## Knowledge` naming the `knowledge_sources` and the gateway's `knowledge__search` and `knowledge__read` (when the factory lists sources); the brief, earlier outputs and the finishing instruction as the user prompt. With `base_conflicts` the user prompt starts with a note (`## First: conflicts with the default branch`) listing the files and telling the agent to rebase or merge the default branch, resolve them and push before the brief.
- Tools: the built-in tools named by `tools_allow` (Claude Code permission rules such as `Bash(npm test*)`) are the only ones available, and those rules are pre-approved. `tools_deny` rules are disallowed. An agent that allows no tools gets `Read`, `Grep` and `Glob`. Permission mode `default` with a permission check (`canUseTool`) for every call nothing pre-approved: a tool of a connection's MCP server is allowed by the agent's `<connection>__<pattern>` rules (patterns as Go's `path.Match`; deny wins, a malformed deny pattern denies the connection's every tool, and a connection with allow rules allows only the tools they match), a `Write`, `Edit`, `MultiEdit` or `Bash` call Claude Code escalates (it asks about every write to `.claude/`, even one an allow rule covers) is allowed only when the agent's `tools_allow` grants it, no `tools_deny` rule matches it (Claude Code rule syntax: `Write`, `Write(/.claude/**)`, `Bash(cat*)`, `Bash(git:*)`) and it writes only the workspace's guidance files: `.claude/rules/`, `.claude/skills/`, `.claude/agents/`, `.claude/commands/` and the root `CLAUDE.md` and `AGENTS.md`. A file tool's `file_path` is resolved against `/workspace` and checked both as written and with its nearest existing ancestor's symlinks resolved; a Bash command, whose targets cannot be parsed reliably, must mention a guidance path and nothing protected (`settings*.json`, `.git`, any other `.claude` path, `..`, `~`, `$HOME`, `CLAUDE_CONFIG_DIR`, `/home/`). Writes to `.claude/settings*.json`, `.claude/hooks`, `.git/` and outside the workspace are always denied: they could change the agent's own permissions, hooks or history for later runs. Every other call is denied, since nobody is there to ask (`dontAsk` would deny those calls before the check sees them). Denied calls are the result's `permission_denials`.
- Skills: copied from `/goal/agent/skills` to `$CLAUDE_CONFIG_DIR/skills`; only the listed names are enabled (the SDK's `skills` option): they are the only skills listed to the model and the only ones the Skill tool accepts, and the only ones the `started` event reports. Claude Code would otherwise also discover the skills bundled with it (loop, schedule, claude-api, …), skills and plugins synced from the claude.ai account behind the token, and the checkout's `.claude/skills`; the worker turns the first two off (`settings`: `disableBundledSkills`, `syncClaudeAiSkills: false`, `syncClaudeAiPlugins: false`, `disableClaudeAiConnectors`) and the filter hides the rest. No plugins are loaded. Settings sources are `user` (those skills) and `project` (the checkout's `CLAUDE.md` and `.claude/`).
- MCP (`strictMcpConfig`, so `.mcp.json` and other configs are ignored): `goal`, goald's gateway, at `gateway_url` over streamable HTTP with `Authorization: Bearer $GOAL_GATEWAY_TOKEN`, its tools pre-approved (the gateway serves only the agent's allowed tools); and one server per connection with an MCP server, named after the connection and reached from the box like any API: a remote one at its `url` with its `headers`, whose `${NAME}` references Claude Code expands from its env (nothing on disk holds a value), or a stdio one started in the box by its `command`, which inherits the agent's env. A connection with both is reached at its URL; one named `goal` is refused. Every server's tools are always loaded (`alwaysLoad`): Claude Code would otherwise defer them behind tool search, and agents called them by name and failed.
- Limits: `max_turns` and `max_usd` become the SDK's `maxTurns` and `maxBudgetUsd`; goald also kills the container when the task's timeout passes.
- Output: with `output.schema.json` the SDK's `json_schema` output format; without it the final reply is read as JSON (whole reply, a fenced block or the outermost object), or passed on as a JSON string. goald validates it against the contract either way. The contract becomes the tool `input_schema` the model fills, and the model API refuses a top level that is not `"type": "object"` or that has `oneOf`, `anyOf` or `allOf`, so `config.Check` refuses such an output contract (`contract-not-tool-schema`) at validate and publish rather than letting every run fail.
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

`finished` is always the last line. The exit code is 0 when the agent produced a result (even a failed one) and 1 when the worker crashed; a container that exits without a `finished` line is a failed run quoting the tail of stderr. `goal-agent prepare` writes exactly one line, `prepared` (above), which `domain.DecodeWorkerLine` skips as not one of these.

## Networking

Locally the container runs on compose.yaml's agents network (`GOAL_SANDBOX_NETWORK`, default `goal_agents`; ADR 0009 and its phase 4 amendment): an internal Docker network without a bridge address, so it has no route to the host, to other networks or to the internet. The only other container on it is compose's `goal-egress` service (`cmd/goal-egress`), and the Docker adapter sets `HTTP_PROXY`, `HTTPS_PROXY` (and the lower-case forms) to it, `NO_PROXY` to the proxy and the container's loopback (services a run starts in its box) and `NODE_USE_ENV_PROXY=1`. Through it a run reaches any public host, by `CONNECT` or plain-http forwarding, and goald's gateway at `gateway_url`, the proxy's own address at the gateway's path (`http://goal-egress:3128/mcp`): the proxy forwards exactly `/mcp` and `/run` to goald and nothing else of it. Any name that resolves to a loopback, private, link-local (metadata) or other non-public address is refused. The worker turns off Claude Code's nonessential traffic (`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`: telemetry, error reports, the updater).

On Cloudflare there is no proxy and no proxy variable: the box has the internet and no route to a private, loopback, link-local or metadata address (Cloudflare's container networking, shown on 2026-10-25 by `sandboxtest`'s network cases, spec 4 §5.12 check 1), and it reaches goald's gateway at `http://gateway.internal/mcp`, which the box's outbound handler in the control Worker sends to goald's container. A smoke run went to done in such a box that day; a run that clones a repository and installs mise tools there is still to be seen (§5.12 check 2).

goald serves the run bundle at `/run` beside `/mcp`, on the same listener and under the same task token, so a sandbox reaches both at `GOAL_GATEWAY_URL`'s origin, through the egress proxy locally and through `gateway.internal` on Cloudflare.
