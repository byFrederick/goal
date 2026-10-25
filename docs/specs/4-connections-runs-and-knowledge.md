# Phase 4 — Connections, runs and knowledge

Status: owner-approved design (2026-10-24), not built · follows phase 3 · migrations `00050–00059` (`00050–00054` built) · OpenAPI tags `connections` and `knowledge` (touch metrics under `work`) · PRs `4-1…4-25` · decision records [ADR 0008](../adr/0008-portable-runs-connections-and-knowledge.md), [ADR 0009](../adr/0009-agent-network-isolation.md) (amended), [ADR 0005](../adr/0005-factory-definitions-in-object-storage.md) (amended)

(phase 4 redesign) This file was `4-credentials-and-connectors.md` (parts 4.1 vault and credentials, 4.2 connectors and MCP gateway, PRs `4.1-n`/`4.2-n`). The design pass replaced it. Kept, simplified: credentials encrypted in PostgreSQL, write-only values, RLS, audit, AWS STS, the merge station. Cut: remote MCP through the gateway (connections go direct from the box), the Credentials page, credential grants, the connector catalog and `kind: Connector` files, scopes, CLI exec sidecars, stdio sidecar containers, `ModelAuth`, `CredentialProbe`, per-org data keys, vault settings, the MCP Registry import, multi-instance forwarding. Deferred: approval-gated tool calls, OAuth connections, the GitHub App. References to "4.1 §x" or "4.2 §x" in older text point at retired parts.

Security here is proportionate to a personal platform that is multi-organization by design: give agents the freedom of a developer on their own laptop, keep organizations apart, keep secrets out of logs and responses, and write the accepted risks down (§11.3).

## 1. Summary

An organization's admins add **connections** on one Connections page: a GitHub, GitLab, AWS, Jira, Datadog or PagerDuty account, a model API key or anything an org-defined **connection kind** describes. Each connection carries its credential, encrypted in PostgreSQL and never readable back, and a factory definition names the connections it uses. Every agent run is a **disposable dev box**: one container per run, started through a new `ports.Sandboxes` port (Docker locally, Cloudflare Containers in the cloud, the same engine code), with public internet, the run's connections as environment variables, tools installed with mise and the factory's repository pre-cloned; the agent works there freely, and the box is deleted when the run ends. Each organization also gets one **knowledge base** of uploaded and synced sources, indexed in PostgreSQL with pgvector, which agents search through the gateway's `knowledge__search` tool. Goal can then run unchanged on a laptop with Docker Compose and on Cloudflare, with Terraform only for the platform itself. Because a factory's end goal is to take an item from start to finish with as little human help as possible, **human touches per item** becomes a headline metric, measured per definition version, with a reason on each touch; [phase 5](5-continuous-learning.md) (Jonah) uses it to turn that guidance into knowledge, standard work and evals.

## 2. Scope

**In**
- Connection kinds as data (platform kinds shipped with goald, org kinds through the API), connections with write-only credentials sealed under one platform master key (`ports.MasterKey`), audit of create, update, delete, test and use.
- Runs that receive their connections as environment variables and use them directly from the box: API calls, CLIs, stdio MCP servers and remote MCP servers (a header rendered from those variables); AWS credentials minted per run with STS (`ports.Minter`).
- The gateway as Goal's MCP server for runs: Goal's own tools and the run bundle (`GET /run`), authorized by the task token. It proxies no connection.
- `ports.Sandboxes` replacing `Runner`, `Workspaces`, `Checker`, `Sidecars` and `Reaper`; adapters `docker` and `cloudflare`; one base image `goal-agent`; tools from the repository's `mise.toml`/`.tool-versions` and `runtime.tools`; `runtime.image` removed.
- Agents with public internet; private, loopback and metadata addresses refused (ADR 0009 amendment); `GOAL_AGENT_EGRESS` removed.
- Definition schema: `spec.connections` as org connection names, `runtime.modelConnection`, `runtime.repository: {url, connection}`, `runtime.tools`, `runtime.size`, `spec.knowledge.sources`; removal of `runtime.image`, `runtime.runner`, `runtime.auth`, `spec.secrets`, `spec.knowledgeBase` and inline `type:` connections, with quick fixes.
- `CodeHost` generalized to `github` and `gitlab`, chosen by the connection's kind; the merge station merges every pull request an item's outputs name.
- The knowledge base: sources (uploads, synced connections: git docs and Confluence; live connections), one ingestion loop in goald with leases, `ports.Knowledge` (pgvector + keyword, hybrid), `ports.Embedder` (OpenAI-compatible HTTP), gateway tools `knowledge__search` and `knowledge__read`.
- `ObjectStore` without object versions, so RustFS and R2 behave the same; deletion of the legacy import (`internal/factorystore`).
- Local Docker Compose with goald, the auth service, the egress proxy and agents in containers; the Cloudflare target: the `cloudflare` adapter, a small control Worker under `deploy/cloudflare/`, and Terraform for the platform's own infrastructure (wrangler where Terraform cannot).
- Web: Connections (list, new, detail), Knowledge (sources, new source, documents, search), the builder's Basics runtime and Connections sections; their phone layouts.
- **Human touches per item** (§4.9), measurement only: derived from existing item events; touches per finished item, touchless rate, by station, kind, reason and definition version, planned gates apart from unplanned interventions; a reason field on each touch (§4.10); on the Floor, the Board header, each Item, the API and `goal status`.

**Out**
- Approval-gated tool calls, OAuth connections (MCP authorization to outside servers), the GitHub App: deferred, unscheduled. Their designs in the retired parts remain valid inputs; OAuth connections and approval-gated calls may route through the gateway when they arrive.
- Per-factory grants of connections and knowledge sources, repository allow-lists, a git proxy, branch rules, gateway-only egress, per-run egress allow-lists, a shared tool cache, placeholder credentials: rejected as disproportionate (ADR 0008).
- TCP sidecars (a shared PostgreSQL next to the box): a run starts its own services inside its box.
- Per-organization quotas on runs, storage or embeddings: unscheduled (§17).
- PDF and Office extraction for uploads (text, Markdown, HTML, JSON and YAML only in phase 4).
- The learning loop (Learned knowledge, retros, standard-work proposals, evals, repeat-guidance flags, the knowledge-first prompt): [phase 5, continuous learning](5-continuous-learning.md).
- Goal's own MCP server for outside clients: [phase 6](6-goal-mcp-server.md).

## 3. Canvas boards covered

A designer updates the boards from the same decisions in parallel; names may be adjusted by that work, and the board wins on layout.

| Board | Element | Delivered by |
| --- | --- | --- |
| `Connections.dc.html` (org page, was per factory) | the organization's connections: name, kind, status (OK, failing test, never tested, missing secret), used by (factories, knowledge sources), last used, last test; filter by kind and factory; "New connection" for admins | 4-15 |
| `Connection-New.dc.html` (new) | choose a kind, then its fields (secret ones as password inputs), description, Test, Create; "Define a kind" for org kinds | 4-15 |
| `Connection.dc.html` (new, detail) | fields with secrets shown as "set", Replace secret, Test, used by, recent use from the audit log, Delete with what uses it; the kind's docs and env names | 4-15 |
| `Knowledge.dc.html` (org page, was per factory) | sources with kind, status, document count, last sync, next sync, used by; Sync now; a source's documents; "Try a search" showing what an agent gets | 4-20 |
| `Knowledge-Source-New.dc.html` (new) | upload (drop files) or synced (pick a connection, then repository and paths, or Confluence space) or live (pick a connection) | 4-20 |
| `Define-Basics.dc.html` | runtime: model, model connection, repository (URL and connection), size, tools; no image, runner or auth | 4-15 |
| `Define-Connections.dc.html`, `New-Connections.dc.html` | pick org connections for the factory and per agent; the kind's mode badges (env, MCP, CLI); knowledge sources the factory may search; no catalog, no credential picker, no approval rules | 4-15, 4-20 |
| `Mobile-Connections.dc.html` (new), `Mobile-Knowledge.dc.html` | phone layouts of the two org pages (read, test, sync, replace secret) | 4-15, 4-20 |
| `Main.dc.html` (Floor overview), `Factories.dc.html`, `Mobile-Floor.dc.html`, `Mobile-Factories.dc.html` | headline "Human touches per item" with trend and touchless rate per factory; on the Floor a Touches panel: by station, by kind, by definition version, planned gates against unplanned interventions, by reason | 4-24 |
| `Board.dc.html`, `Mobile-Board.dc.html` | the header's touches per finished item and touchless rate over the board's window; a card's touch count | 4-24 |
| `Item.dc.html`, `Mobile-Item.dc.html` | the item's touch count and list (who, what, station, when, planned or not, reason with an edit select) | 4-24 |

Retired: `Credentials.dc.html`, `Credential-New.dc.html`, `Catalog.dc.html` (the Connections page owns credentials; kinds replace the catalog), `Approval.dc.html` (approvals deferred). Not delivered: the factory Connections board's per-tool usage panel (connections' tool calls go direct from the box, so goald sees them only in the run's tool events), and Knowledge's "Recently written" (agents do not write to the knowledge base in phase 4).

## 4. Domain and engine changes

### 4.1 A run is a disposable dev box

One container per run is the isolation boundary. Inside it the agent is free: it chooses branch names, clones any repository its token allows, opens as many pull requests as it needs (in several repositories for one item), installs packages and starts services such as PostgreSQL for its tests. The filesystem is writable; nothing is mounted from the host. The box is deleted when the run ends, so nothing carries over between runs except what the agent pushed or the engine recorded. Protecting a default branch or CI secrets is the code host's job (branch protection, environments), not Goal's.

### 4.2 Connection kinds are data

A **connection kind** is a small template. The platform ships a few as YAML files embedded in goald (`internal/connkind/kinds/`), upserted at startup; an organization's admins add their own through the API. Both use one JSON Schema, `schema/connection-kind.schema.json`.

```yaml
apiVersion: goal/v1
kind: ConnectionKind
metadata:
  name: github                     # slug; unique among the platform's kinds and the org's
  title: GitHub
  description: A fine-grained personal access token for the repositories agents may use.
spec:
  fields:                          # what an admin fills in; secret fields are write-only
    - {name: token, title: Token, secret: true, required: true}
    - {name: host, title: Host, default: github.com}
  env:                             # what a run's box gets: API and CLI use, MCP servers
    GH_TOKEN: "{{token}}"
    GH_HOST: "{{host}}"
  git:                             # optional: git over https to this host, user and password from env
    host: "{{host}}"
    username: x-access-token
    passwordEnv: GH_TOKEN
  mcp:                             # optional: a remote MCP server, reached from the box
    url: https://api.githubcopilot.com/mcp/
    headers: {Authorization: "Bearer ${GH_TOKEN}"}   # expanded by Claude Code from the box's env
  codeHost: github                 # optional: the CodeHost adapter that reads and merges its pull requests
  test: {method: GET, url: "https://api.{{host}}/user", headers: {Authorization: "Bearer {{token}}"}, expect: 200}
  docs: Use gh and git; GH_TOKEN is set. Open a pull request for every change.
```

- **Template grammar** (closed): `{{field}}` and, for minted kinds, `{{minted.<name>}}`. Nothing else is evaluated. `mcp.headers` values may also hold `${NAME}`, a variable of the kind's own `env`, which goald leaves as written for Claude Code to expand in the box, so no value is in the bundle or on disk. A kind that names an unknown field or variable is invalid. Env names and header names may be templates too (`{{prefix}}_URL`). A value that names an optional field with no value and no default is left out, with the env variable or header it belongs to (an MCP header whose `${NAME}` is left out goes too); a git login, MCP url or MCP command with such a value is left out whole (4-8). Secret fields may appear only in env values, a test's headers and its `basic` password, never in an MCP header (use `${NAME}`), a URL, a command, a git login or an env name (4-8). A test may authenticate with `basic: {username, password}` (Atlassian API tokens) and expects 200 unless it says otherwise.
- **Modes.** `env` serves API calls and CLIs (`cli: {tool: "<mise tool spec>"}` installs one at prepare); `mcp.url` + `headers` is a remote MCP server the worker configures in Claude Code, called directly from the box like any API; `mcp.command` (an argv, for example `[npx, -y, "@acme/jira-mcp@2.1.0"]`) is a stdio MCP server the worker starts inside the box with the kind's `env`. A kind may combine them.
- **Model access.** `model: true` marks a kind that gives Claude Code its model (`anthropic`: `ANTHROPIC_API_KEY`; `claude-subscription`: `CLAUDE_CODE_OAUTH_TOKEN`, for one person's own use; `bedrock`: `CLAUDE_CODE_USE_BEDROCK=1` and minted AWS credentials). Only such a kind may be `runtime.modelConnection`.
- **Adapter hooks**, the only provider code: `codeHost` (`github`, `gitlab`), `mint` (`aws-sts`), `sync` (`gitdocs`, `confluence`). Absent means the kind needs no Go code at all.
- **Reserved names.** A kind's `env` may not set `GOAL_*`, `HOME`, `PATH`, `USER`, `SHELL`, `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` (any case), `NODE_USE_ENV_PROXY`, `CLAUDE_CONFIG_DIR`, `LD_*` or `MISE_*` (`domain.ReservedRunEnv`).
- **Platform kinds in 4-8:** `github`, `gitlab`, `aws`, `anthropic`, `claude-subscription`, `bedrock`, `jira`, `confluence`, `datadog`, `pagerduty`, `http-api-key` (generic: `base_url`, `header`, `key`; env `<PREFIX>_URL`, `<PREFIX>_KEY` from a `prefix` field), `mcp-server` (generic: no secret, a stdio command or a remote URL, such as Playwright or a public documentation server; the command runs as `sh -c "<command>"`). As built (4-8), `aws` and `bedrock` render `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN` from `{{minted.access_key_id}}`, `{{minted.secret_access_key}}` and `{{minted.session_token}}` (the names 4-12's minter returns), with the fields `access_key_id`, `secret_access_key` (secret), `role_arn`, `external_id` and `region`; `jira` and `confluence` take `site`, `email` and a secret `token` and test with basic authentication; `gitlab` installs `glab`; `aws` installs `aws-cli`.
- **Changes.** A kind has no versions: editing an org kind changes every connection of it from the next run on. A platform kind changes with a goald release. An org kind may not take a platform kind's name (409 `connection-kind-name-taken`); if a later release adds a platform kind with an org kind's name, the org's own kind wins inside that org.

### 4.3 Connections and their credentials

A **connection** is one org row: a name (slug), a kind, a description, the kind's non-secret field values and its secret field values sealed as one JSON object. Org owners and admins create, update, test and delete connections; members see names, kinds and status (to pick them in the builder), never values. Secret values are **write-only**: the API accepts them on create and on replace and never returns them, not even masked; the detail shows which secret fields are set. Every create, update, secret replacement, delete and test is audited (`connection.created`, `connection.updated`, `connection.secret_replaced`, `connection.deleted`, `connection.tested`), and each run that receives a connection writes `connection.used` with the run id and updates `last_used_at`.

Sealing (§5.2): AES-256-GCM under the platform master key with its key id, a fresh 12-byte nonce per write, and associated data `goal/connection/v1|<org id>|<connection id>|<key id>`, so a ciphertext copied to another row or organization does not open.

### 4.4 Definition changes

```yaml
spec:
  runtime:
    model: claude-sonnet-5-5
    modelConnection: claude            # an org connection whose kind has model: true
    size: large                        # small | medium (default) | large
    tools: ["go@1.26.10", "task@3.46.1"]   # mise tool specs, installed after the repository's own
    repository:                        # optional: cloned into /workspace before the agent starts
      url: https://github.com/acme/goal
      connection: github               # optional for a public repository; a kind with a git block for this host
    budget: {weeklyUsd: 150, perItemUsd: 6}
  connections: [github, datadog, playwright]   # org connections the factory uses (by name)
  knowledge:
    sources: [handbook, goal-docs]     # org knowledge sources the factory may search
  agents:
    - name: builder
      connections: [github, playwright]   # what this agent's runs get; tools.allow/deny unchanged
```

- `runtime.modelConnection` and `runtime.repository.connection` are part of every run of the factory; they need not be repeated in `spec.connections` or an agent's list. An agent's `connections` names entries of `spec.connections`.
- **Removed**, each with a validation finding and a quick fix in the builder and `goal factory validate`: `runtime.image` and `runtime.runner` (`field-removed`: one base image, the platform chooses the sandbox backend), `runtime.auth` (`field-removed`: set `runtime.modelConnection`), `runtime.repository.token` and every `env:` reference (`env-reference-removed`: use a connection), `spec.secrets`, `spec.knowledgeBase` (`field-removed`: use `spec.knowledge.sources`), inline connections with `type: builtin|mcp|aws` and the connector form (`connection-inline-removed`: create an org connection, for example of kind `mcp-server`).
- **New checks** (authoring's credentials check becomes the connections check, through B's `authoring.Credentials` extension point): `connection-unknown` (no such org connection), `connection-not-model`, `repository-host-mismatch` (the connection's `git.host` is not the URL's host), `connection-env-conflict` (two connections of one agent set the same variable), `knowledge-source-unknown`, `tool-spec-invalid` (not a mise tool spec). All are errors at publish.
- `domain.Runtime` loses `Image`; `domain.Repository` becomes `{URL, Connection}`; it gains `ModelConnection`, `Size` (`domain.SandboxSize`: small, medium, large) and `Tools`. `domain.Line` gains `Connections` and `KnowledgeSources`.
- As built (4-10): the removed forms are warnings until the cutover (4-14) makes goald refuse to run a version that has them, so versions published before keep loading and running; each has its quick fix (remove the key or the token; an inline connection becomes the organization's connection of its name, which agents keep naming). `config.Runtime` has no `Image` (4-7's engine ignores it; `field-removed` names it) and `domain.Repository.AuthRef` (the token) stays until 4-14 for those versions' runs; `Runtime.Size` comes from `runtime.size`, empty being medium; `Line.Connections` leaves inline connections out. In the Visual model, Basics holds `model_connection`, `size`, `tools`, `knowledge_sources` and, while a definition has them, the removed `runner`, `image`, `auth`, `secrets` and `knowledge_base` (so their fixes can remove them); Connections holds `{name}` for each organization connection (written as the bare name). Config's checks `tools` (`tool-spec-invalid`) and `removed` (the three removal codes) run offline in `goal validate`; the rest is authoring's connections check (`authoring.ConnectionsCheck`, check id `connections`, which replaces `credentials` and runs the interim env: checks until 4-14), reading names, kinds and non-secret fields through `ports.Connections` (`domain.ConnectionKind.Outline` gives the env names and git host). Its knowledge sources come from `authoring.KnowledgeSources`, which 4-16 wires; until then no source exists, so every listed one is `knowledge-source-unknown`. Fixes: `connection-unknown` (remove an unused `spec.connections` entry; for the model and repository connections, use each one that fits), `connection-not-model` ("Use <model connection>"), `repository-host-mismatch` ("Use <connection for the host>", "Clone without a connection"), `connection-env-conflict` ("Keep <one>", taking the other out of the agent's list), `knowledge-source-unknown` and `tool-spec-invalid` (remove it). `goal factory validate` prints each fix under its problem. The blank skeleton is `runtime: {size: medium}` and `GOAL_DEFAULT_AGENT_IMAGE` went with it; the catalog's `template-credential` went with the `credential:` form.

### 4.5 Run lifecycle (engine)

```
claim lease (unchanged: slot of the station, WIP, budget, andon)        → StartRun
Connections.Resolve(model, repository, agent's connections; run)         → values; audit connection.used
mint for kinds with `mint` (Minter, lifetime = task timeout + 15 min, capped by the provider)
render each kind: env, git credentials, remote MCP, stdio MCP command, CLI tool, docs  (domain.ConnectionKind.Render)
Sandboxes.Create(run, image GOAL_AGENT_IMAGE, size, env {GOAL_GATEWAY_URL}, deadline = now + 2 × task timeout + lease TTL)
exec prepare: goal-agent prepare   (env: task token, git credentials' env)
    GET <gateway>/run   → spec.json, prompt, skills, output schema, MCP config, tools, repository  → /goal
    git credential helper per git block (reads the password from the named env var; nothing on disk)
    clone runtime.repository into /workspace; check out goal/<number>-<slug> (remote branch if it exists, else new)
    mise install: the repository's mise.toml / .tool-versions, then runtime.tools; CLI tools of the connections
exec agent: goal-agent run   (env: the agent's connection env, task token)  → NDJSON events → relay (protocol unchanged)
Contracts.Validate; on failure: exec goal-agent run again in the same box with GOAL_FEEDBACK (once, as today)
each station check: exec sh -c '<command>' in /workspace (env: HOME, CI=true; no connection env, no token)
    on failure: one more agent exec with the failing output, then the checks again (as today)
Sandbox.Delete; FinishRun; Transition
```

- **Exit codes come from the runtime** (Docker exec, the container's exec on Cloudflare), never from agent-reachable code, so an agent cannot report "checks passed".
- **Agent files** no longer go through a host folder and a bind mount: the worker fetches its bundle from the gateway's `/run` (task token; served from the definitions cache by version id). `docker.AgentFiles` and `ports.Task`'s staging go away. The bundle's brief is the item's title and body (`domain.Item.Work`) and its inputs the item's outputs, read when prepare asks; what the engine adds for one attempt (people's notes, a rejected attempt's reason) reaches `goal-agent run` as `GOAL_FEEDBACK`, which the worker puts right after the brief. Git credentials in the bundle name the variable that holds their password (`GOAL_REPOSITORY_TOKEN` for the repository until connections, 4-11); the engine puts it in the env of the execs that need git (worker/README.md, "Sandbox contract").
- **Branches.** Prepare checks out the item's branch (`domain.Item.Branch`) so the next run continues the work; the agent may create others. A retry or the next station gets a new box and starts from what was pushed.
- **Prompt.** The worker appends a "Connections" section to the system prompt: each connection's name, kind title, environment variable names (never values), MCP server name and the kind's `docs`.
- **Scrubbing.** Every resolved secret value is removed from relayed events, errors and logs (best effort, exact strings, as the gateway does today).
- **Recovery** is unchanged: the run goroutine holds the exec stream; a goald that dies leaves leases that expire and are requeued, and its boxes are reaped (below).
- **Reaping** keeps today's rule with one source: `engine.ReapOrphans` lists boxes (`Sandboxes.List`, run and org from labels), asks `Store.RunsOver` and deletes those runs' boxes. The deadline is a backstop: the Cloudflare run Durable Object destroys its container at the deadline even if no goald is left; locally any goald on the Docker host reaps.

### 4.6 Autoscaling from zero to the agent count

There is no new scheduler. A work center's agent count (`agents`, capped by `wip`) is already its number of lease slots, and every live lease is one box, so a line with nothing to claim runs zero containers and a busy one runs at most its slots. `GOAL_MAX_RUNS` caps one goald (0: none). On Cloudflare the container classes' `max_instances` (wrangler) must cover the sum of the slots; a start refused for capacity fails the claim's run as `failed` with `sandbox-capacity` and the item returns to its queue. Sizes map per adapter: Docker small 1 CPU / 2 GiB, medium 2 CPU / 4 GiB, large 4 CPU / 8 GiB; Cloudflare `standard-1`, `standard-3`, `standard-4`, one container class per size of the same image.

### 4.7 Merge station across repositories

`engine.Merge` takes the item's pull requests from its outputs: the `pull_request` string and the `pull_requests` array of every station's latest output, deduplicated, in output order. For each URL it picks the factory's connection whose kind has a `codeHost` and whose rendered `git.host` is the URL's host (none: 409 `no-code-host-connection`) and, through that `CodeHost`, reads it, merges it at the head SHA just read (squash by default, never forced) and deletes its head branch when it lives in the same repository and is not the base. Already merged ones are skipped, so a retry after a partial failure is safe; the first refusal stops with 409 `cannot-merge` naming it. When every pull request is merged the item is approved with one `merged` event per pull request. Which repositories can be merged is the token's own scope. As built (4-13): the factory's connections come from `engine.Deps.MergeConnections` (a function of the line, for the organization in `ctx`), the first connection serving a host wins, and a URL on a host none serves refuses the whole merge before anything is merged. Until the cutover that source is goald's `repositoryConnections`, the `runtime.repository.token` reference (refused when reserved) as a connection of the platform `github` kind on `GOAL_GITHUB_WEB_URL`'s host; 4-14 replaces only that function with one that resolves the factory's connections.

### 4.8 Knowledge base

One knowledge base per organization, made of **sources**:

| Source kind | What | Indexed |
| --- | --- | --- |
| `upload` | files an admin uploads (text, Markdown, HTML, JSON, YAML; 25 MiB each by default) | yes; originals in the bucket |
| `synced` | a connection whose kind has `sync`: `gitdocs` (a repository, a branch, path globs such as `docs/**/*.md`) or `confluence` (a space) | yes: text chunks, embeddings and a link back; originals stay at the source |
| `live` | a connection whose kind serves search itself (a Slack MCP server) | no: a factory listing it gets that connection's tools |

- A factory lists the sources it may search (`spec.knowledge.sources`); `knowledge__search` filters by that list inside the one query, from the run's definition version, whatever the agent asks.
- Admins choose what is indexed. A synced source is read with the connection's own access, so every factory that lists it sees everything that access sees: per-user permissions at the source are flattened, by design (§11.3).
- **Ingestion** is one goald background loop (`internal/knowledge.Ingestor`, `GOAL_KB_WORKERS` workers per goald, default 2), not an agent run. Each tick it goes through the active organizations and claims a due source with a lease: `UPDATE knowledge_sources SET lease_until = now() + interval '10 minutes', status = 'syncing' WHERE id = (SELECT id FROM knowledge_sources WHERE kind <> 'live' AND next_sync_at <= now() AND (lease_until IS NULL OR lease_until < now()) ORDER BY next_sync_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *`, renewed while it works, so any goald may run any source and a dead one's lease expires. (As built in 4-16: the claim takes `now` and the lease's end from goald's clock, as item claims do, and also claims a source whose chunks are of another model unless its last sync failed; a sync cut short, by goald stopping or its lease lost, records nothing, so the next claim after the lease takes the source over; documents listed unchanged are marked seen.)
- **Pipeline**: `SourceReader.List` (every document with a version: git blob id, page version, upload sha256) → for each new or changed version `Fetch` → extract (text, Markdown, HTML to text with headings; JSON and YAML as text) → `knowledge.Chunk` (heading-aware, about 512 tokens with 64 overlap, pure; as built about 2,048 characters a piece, the pieces joining back into the document, the overlap only in the text the embedding reads) → `Embedder.Embed` in batches of at most 64 (as built also at most about 40,000 tokens padded to the longest text: Workers AI refuses a request over 60,000) → replace that document's chunks in one transaction. Documents not listed any more are deleted (`seen_at` before the pass). Unchanged versions cost nothing but the listing; that is the incremental sync.
- **Schedule**: `sync_every` (default 1 hour, at least 15 minutes); "Sync now" sets `next_sync_at = now()`; an upload marks its document `pending` and makes its source due.
- **Search**: one SQL statement with two candidate lists, the 50 nearest chunks by cosine distance (`SET LOCAL hnsw.iterative_scan = relaxed_order`) and the 50 best by `ts_rank` on a `simple` `tsvector` (as built: any of the query's words, normalization 1, the rank over 1 + the log of the chunk's length, which the recall check below found worth 0.73 MRR against 0.57 without), fused by reciprocal rank (k = 60), both filtered by `source_id = ANY($sources)` under RLS. Without an embedder (`GOAL_EMBED_URL` unset) it is keyword search only. Results: snippet, title, source, score and `url` (synced) or the document id (uploads, which `knowledge__read` returns whole, at most 200 KiB).
- **Embedding space**: one model per install (`GOAL_EMBED_MODEL`, default Workers AI's `@cf/baai/bge-m3`, §17 q2), 1024 dimensions stored as `halfvec(1024)`; chunks record their model, search uses only chunks of the current model, and changing the model makes every source due for a full re-embed.

### 4.9 Human touches per item

The end goal of every factory is to finish an item from start to finish with as little human interaction as possible, so the number of times a person had to act on an item is a headline metric next to lead time and throughput. It is derived from the item events already recorded; nothing new is stored to count it.

- **A touch** is one action by a person on an item: the item events of one item with the same person as `data.actor` (`user:<id>`, `user:local` included) and the same `at` (one transition) count once. API keys (`service:…`), agents and the engine never touch. `created` (the submission is the input) and `lease_taken` (it accompanies the person's own event) never count. The touch is named by its most specific event: `merged` > `rejected` > `gate_rejected` > `gate_approved` > `approved` > `moved` > `paused` > `retried` > `note` > `released` > any other kind. An answer to an agent's question is a note or a decision's reason today (a dedicated "ask a person" tool is deferred with approvals).
- **Planned and unplanned.** A *planned* touch is a decision a line requires: an approval, gate approval or merge at a human station. Everything else is *unplanned*: rejections and send-backs, moves, pauses, retries, notes, approvals of an escalation (a gate after too many reworks). From 4-23 on decision events carry `data.planned`; older approvals, gate approvals and merges count as planned. The headline counts both; both stay visible.
- **Metrics** over a window (UTC days, as E's metrics; one work type optional), for the items **finished** (done) in it: touches per finished item (mean and median), the **touchless rate** (finished with zero touches), planned and unplanned per item, touches by station (the event's station), by kind, by reason (§4.10) and **by definition version** (an item counts for the version of its last agent run, `agent_runs.definition_version_id`, so a publish shows whether it changed touches), a weekly trend (E's weekly buckets) and, beside them, the line stops people made (andon pulls in the window, which belong to the line, not an item). Organization level sums the factories the principal may read; factory level is the line.
- **Where it shows**: the Floor and factory overview (headline and trend), the Board header (over the board's window), each item card (its count) and the Item (count and list), the API (§8.2) and `goal status` (§9). Permissions are the existing ones: `factory:read` for a factory and an item, `org:read` with per-factory visibility for the organization view; RLS as for every item read.
- `FlowStats` computes it in SQL over `item_events` with a partial index on person events (`00052`): the view `item_touches` groups and names them, read by item (one item, the cards' items, or the items finished in a window), and `domain.TallyTouches` sums those items in Go for both stores; `porttest.Store` keeps the same rule in memory (`domain.TouchesOf`) and `storetest` checks both agree. (4-23: the index is on `(item_id, at)`, since every read starts from items, not on `(factory_id, at)`.)

### 4.10 Touch reasons

Each touch may carry a reason, so the metric says why people had to act: `missing_knowledge`, `wrong_decision`, `required_gate` (planned touches start as this) or `other`. Anyone with `item:operate` sets or changes it on the Item (audited as `touch.labelled`, never an item event, so it is not a touch itself); the touch metrics count by reason. Stored in one small table, `item_touch_labels` (a row per person event, added by a trigger), which [phase 5](5-continuous-learning.md) extends with agent-suggested reasons and repeat-guidance flags. Nothing else of the learning loop is in phase 4.

## 5. Ports and adapters

### 5.1 `ports.Sandboxes` (new; replaces `Runner`, `Task`, `Workspaces`, `Checker`, `Sidecars`, `Reaper`)

```go
// Sandboxes runs one container per agent run: Docker locally, Cloudflare Containers in the
// cloud. A sandbox has public internet and no route to private, loopback or metadata addresses.
type Sandboxes interface {
	// Create starts the sandbox of s.RunID; a second call for the same run returns the same one.
	Create(ctx context.Context, s SandboxSpec) (Sandbox, error)
	// List returns every sandbox this backend holds, live ones included, with run and org.
	List(ctx context.Context) ([]Leftover, error)
	// Delete removes a run's sandbox, ending its running commands; one that is already gone is
	// not an error.
	Delete(ctx context.Context, run domain.RunID) error
}

type SandboxSpec struct {
	OrgID     domain.OrgID
	FactoryID domain.FactoryID
	ItemID    domain.ItemID
	RunID     domain.RunID
	Station   string
	Agent     string
	Image     string
	Size      domain.SandboxSize
	// Env holds settings, never credentials: those go to the Exec that needs them.
	Env      map[string]string
	Deadline time.Time
}

type Sandbox interface {
	// Exec runs one command; cancelling ctx kills it. Env is added to the sandbox's.
	Exec(ctx context.Context, e Exec) (Process, error)
	Delete(ctx context.Context) error
}

type Exec struct {
	Argv    []string
	Env     map[string]string // adapters never log it
	Dir     string            // empty: the image's
	Timeout time.Duration     // 0: none
}

type Process interface {
	Stdout() io.Reader // the caller drains it to its end, then calls Wait
	// Wait returns how the command ended; an error means the outcome is unknown. A command
	// killed because its ctx ended returns that ctx's error.
	Wait() (ExitStatus, error)
}

type ExitStatus struct {
	Code       int // meaningless when TimedOut
	TimedOut   bool
	StderrTail string // last MaxStderrTail (8 KiB) bytes
}
```

`Leftover` keeps its shape. The worker's stdout protocol decoder moves from `adapters/docker/protocol.go` to `internal/domain/worker.go` (`domain.DecodeWorkerLine(line, now) (*domain.Event, *domain.WorkerFinished, error)`, `domain.ErrNotWorkerLine` for other lines, `WorkerFinished.Result()` and `.Denials(now)`; event kinds `domain.EventAgentStarted|ToolCall|ToolResult|AgentMessage`), so the engine relays events with no adapter-specific code. `porttest.Sandboxes` is the in-memory fake (`porttest.NewSandboxes(ExecFunc)`: a scripted function plays each command and sees the merged env; the fake enforces timeouts, kills on cancel and delete, and keeps the stderr tail) and `porttest/sandboxtest` the conformance suite every adapter passes (`sandboxtest.Run(t, func(*testing.T) sandboxtest.Harness)`; `sandboxtest.Script()` answers its commands on the fake): idempotent create, exec output and exit codes, stderr tail, streaming and kill on cancel, timeout, env per exec, working directory, delete ends commands and is idempotent, list by run and org, and, when the harness gives a `Network`, no private address reachable and public HTTPS reachable.

**`internal/adapters/docker`** keeps today's hardening (non-root uid 1000, every capability dropped, no new privileges, init, CPU, memory and PID limits per size, labels `goal.org/factory/item/run/station/agent`) and drops the read-only root (the box's filesystem is writable; `/tmp` stays tmpfs). No mounts. It attaches the container to the agents' network (`GOAL_SANDBOX_NETWORK`, default `goal_agents`, created by compose) and sets the proxy variables to `goal-egress`. Exec is `docker exec` with stdout and stderr attached; `Delete` is `rm -f`.

**`internal/adapters/cloudflare`** is an HTTP client of the control Worker's sandbox API (`api/sandbox.yaml`, bearer `GOAL_SANDBOX_KEY`, base `GOAL_SANDBOX_URL`):

```
PUT    /v1/sandboxes/{run}       {org, factory, item, station, agent, image, size, env, deadline} → 201 | 200 (same run)
POST   /v1/sandboxes/{run}/exec  {argv, env, dir, timeout_s} → 200 application/x-ndjson: {"o":"…"} {"e":"…"} … {"exit":0,"timed_out":false}
DELETE /v1/sandboxes/{run}       → 204 (idempotent)
GET    /v1/sandboxes             → {sandboxes: [{run, org, deadline}]}
```

The Worker keeps one Durable Object per run (`idFromName(run)`), starts the size's container class with `enableInternet: true` and exactly the `env` goald sent (never its own bindings), streams `exec` output, and sets an alarm at `deadline` that destroys the container. It holds no other state.

### 5.2 `ports.MasterKey` (new; the registry's `KeyWrapper`, renamed: there are no data keys to wrap)

```go
// MasterKey seals and opens small secrets with the platform's master key (AES-256-GCM).
type MasterKey interface {
	// Seal encrypts with the active key and returns its id.
	Seal(plaintext, aad []byte) (keyID string, nonce, ciphertext []byte, err error)
	// Open decrypts with the named key; an unknown key id or a wrong aad is an error.
	Open(keyID string, nonce, ciphertext, aad []byte) ([]byte, error)
	Active() string
}
```

`internal/adapters/masterkey` reads `GOAL_MASTER_KEYS` (`<id>:<base64 32 bytes>[,<id>:…]`, the first active) or `GOAL_MASTER_KEY_FILE` (the same, one per line; mode 0600, not a symlink, refused otherwise). Locally it lives in the git-ignored `.env` (`.env.example` shows how to make one; generating it from `goal up` on first start is not built yet: as built in 4-8, goald without a key starts, warns, and serves everything but connections with secrets, which fail with `ports.ErrNoMasterKey`); on Cloudflare it is a Secrets Store secret the control Worker passes to the goald container as env. goald refuses to start when a connection is sealed under a key id it does not have. Rotation: add a key in front, restart, run `goal db rekey` (reseals every connection under the active key, in batches), then drop the old key. A KMS adapter can implement the same port later.

### 5.3 `ports.Connections` (new)

```go
type Connections interface {
	Kinds(ctx context.Context) ([]domain.ConnectionKind, error) // the platform's and the org's
	Kind(ctx context.Context, name string) (domain.ConnectionKind, error)
	PutKind(ctx context.Context, k domain.ConnectionKind, by domain.Actor) error // org kinds
	DeleteKind(ctx context.Context, name string, by domain.Actor) error          // ErrConflict while used
	SyncPlatformKinds(ctx context.Context, kinds []domain.ConnectionKind) error  // goald's start

	List(ctx context.Context) ([]domain.Connection, error)
	Get(ctx context.Context, name string) (domain.Connection, error)
	Create(ctx context.Context, c domain.NewConnection, by domain.Actor) (domain.Connection, error)
	// Update changes metadata and fields; Secrets replaces the named secret fields only.
	Update(ctx context.Context, name string, p domain.ConnectionPatch, by domain.Actor) (domain.Connection, error)
	Delete(ctx context.Context, name string, by domain.Actor) error
	RecordTest(ctx context.Context, name string, r domain.ConnectionTest, by domain.Actor) error

	// Resolve opens the named connections for one run, audits connection.used and sets
	// last_used_at. It and Open are the only methods that return secret values.
	Resolve(ctx context.Context, names []string, run domain.RunID) ([]domain.ResolvedConnection, error)
	// Open opens one connection for its test (as built, 4-9): no use recorded; RecordTest records the outcome.
	Open(ctx context.Context, name string) (domain.ResolvedConnection, error)
}
```

As built (4-9): `domain.ConnectionPatch` has `Name` (a rename; the sealed values stay, bound to the connection's id; `ErrNameTaken` for a taken name), and every change and test notifies the stream event `connection` in its transaction (§8.3).

`postgres.NewConnections(store, masterKey)` implements it; errors `ErrNotFound`, `ErrNameTaken`, `ErrConflict`, `ErrNoMasterKey`. `porttest.Connections` (with `porttest.MasterKeys`, a fake key that seals without cryptography) and `porttest/connectiontest` (fake and PostgreSQL). `postgres.Rekey(ctx, ownerStore, masterKey)` is `goal db rekey`; `postgres.ConnectionKeyIDs` feeds goald's startup check. Kind types and `Render(values, minted) (domain.RunConnection, error)` live in `internal/domain/connection.go` (standard library only); `internal/connkind` parses and validates kind YAML against the schema and embeds the platform kinds.

### 5.4 `ports.Minter` (new; AWS only)

```go
type Minter interface {
	// Mint returns short-lived values for one run from the connection's stored ones.
	Mint(ctx context.Context, values map[string]string, run domain.RunID, ttl time.Duration) (minted map[string]string, expires time.Time, err error)
}
```

`internal/adapters/awssts`: with a `role_arn` field, `AssumeRole` (`RoleSessionName` `goal-<run>`, duration = ttl capped by the role's maximum); without one, `GetSessionToken`. It returns `access_key_id`, `secret_access_key`, `session_token`, `expiration`. The `aws` kind renders them into `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN` and `AWS_REGION`; the stored keys never reach a box. `engine.Deps.Minters` maps the kind's `mint` name to an adapter.

### 5.5 `ports.CodeHost` (changed)

```go
type CodeHost interface {
	PullRequest(ctx context.Context, c domain.ResolvedConnection, url string) (PullRequest, error)
	Merge(ctx context.Context, c domain.ResolvedConnection, url string, m Merge) (string, error)
	DeleteBranch(ctx context.Context, c domain.ResolvedConnection, url, branch string) error
}
```

The credential comes from the connection instead of `Secrets` (the host and the password of its rendered git login, `domain.ResolvedConnection.GitLogin`), and the repository from the URL instead of `runtime.repository`. `github` (REST; API base `https://api.<host>` for github.com and GitHub Enterprise Cloud's `*.ghe.com`, `https://<host>/api/v3` for any other host, a GitHub Enterprise Server) and `gitlab` (REST v4, merge requests; squash or the project's merge method, no rebase) implement it, keep today's rules (only to the connection's host, no redirects, token scrubbed from errors) and refuse a URL on another host. `engine.Deps.CodeHosts` maps the kind's `codeHost` name to an adapter. `PullRequest.SameRepository` keeps its meaning.

### 5.6 `ports.Knowledge`, `ports.Embedder`, `ports.SourceReader` (new)

As built in 4-16 (`internal/ports/knowledge.go` is the source of truth):

```go
type Knowledge interface {
	Sources(ctx context.Context) ([]domain.KnowledgeSource, error)
	Source(ctx context.Context, name string) (domain.KnowledgeSource, error)
	CreateSource(ctx context.Context, s domain.NewKnowledgeSource, by domain.Actor) (domain.KnowledgeSource, error)
	UpdateSource(ctx context.Context, name string, p domain.KnowledgeSourcePatch, by domain.Actor) (domain.KnowledgeSource, error)
	DeleteSource(ctx context.Context, name string, by domain.Actor) (orphans []domain.BlobHash, err error) // documents, chunks
	SyncNow(ctx context.Context, name string) error

	// now and until from goald's clock, as item claims; model "" without an embedder
	ClaimSource(ctx context.Context, now, until time.Time, model string) (*domain.KnowledgeSource, error) // nil when none is due
	RenewSource(ctx context.Context, id domain.KnowledgeSourceID, now, until time.Time) error            // ErrLeaseLost
	FinishSync(ctx context.Context, id domain.KnowledgeSourceID, r domain.SyncResult) error             // r.Due: Sync now since the claim stands

	Versions(ctx context.Context, source domain.KnowledgeSourceID) (map[string]string, error) // external id → version, pending excluded
	PutDocument(ctx context.Context, d domain.KnowledgeDocument, chunks []domain.Chunk) error  // replaces its chunks
	MarkSeen(ctx context.Context, source domain.KnowledgeSourceID, externalIDs []string, at time.Time) error
	DeleteUnseen(ctx context.Context, source domain.KnowledgeSourceID, before time.Time) (int, error)
	AddUpload(ctx context.Context, source string, d domain.KnowledgeDocument, by domain.Actor) (domain.KnowledgeDocument, error) // d.Blob is id and version
	DeleteDocument(ctx context.Context, id domain.KnowledgeDocumentID, by domain.Actor) (orphan domain.BlobHash, err error)
	Documents(ctx context.Context, source string, after domain.KnowledgeDocumentID, limit int) ([]domain.KnowledgeDocument, error)
	Document(ctx context.Context, id domain.KnowledgeDocumentID) (domain.KnowledgeDocument, string, error) // with its text

	// Search ranks chunks of q.Sources (names) only; there is no unfiltered variant.
	Search(ctx context.Context, q domain.KnowledgeQuery) ([]domain.KnowledgeHit, error)
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error) // 1024 dimensions each
	Model() string
}

type SourceReader interface {
	List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] // id, version, title, url, mime
	Fetch(ctx context.Context, s domain.SyncSpec, id string) (domain.SourceDoc, error)
}
```

Refusals: `ErrNameTaken`, `ErrNotFound`, `domain.ErrInvalid`, `domain.ErrConnectionCannotSync` (400 `connection-cannot-sync`), `domain.ErrNotUploadSource` (409 `not-an-upload-source`), `domain.ErrUnsupportedDocument` (415) and `ports.ErrTooLarge` (413) from `knowledge.Uploads`; a connection a source reads is not deleted (`Connections.Delete`: `ErrConflict`). `internal/knowledge` gives 4-17 `Uploads` (`Upload`, `DeleteDocument`, `DeleteSource`, which also delete the objects no document holds) and `Searcher` (`Search(ctx, text, sources, limit)`: the query embedded, keywords alone without a model or when it fails); a synced source's connection is opened with `Connections.Resolve` and the run id `""` (the audit entry `connection.used` has no run).

Adapters: `postgres` (Knowledge, pgvector), `openaiembed` (`POST <GOAL_EMBED_URL>/embeddings` with `model`, `input`, `dimensions: 1024`; bearer `GOAL_EMBED_KEY`; Workers AI's OpenAI-compatible endpoint on Cloudflare, Ollama or any compatible server locally), `gitdocs` (go-git, pure Go: a depth-1 fetch of the branch into a temporary directory removed after the pass, blob ids as versions; https with the connection's git credentials; any git host), `confluence` (REST v2: pages of a space with their version numbers, body as storage format → text). `domain.SyncSpec` carries the source's config and the resolved connection. Chunking, extraction, fusion and the ingestion loop are in `internal/knowledge` (domain, ports, standard library, `golang.org/x/net/html`).

**Touches** (4-23) add, without a new port:

```go
// FlowStats
Touches(ctx context.Context, q FlowQuery) (domain.TouchStats, error)        // §4.9, items finished in the window
OrgTouches(ctx context.Context, factories []domain.FactoryID, q FlowQuery) (domain.TouchStats, []domain.FactoryTouchStats, error)
TouchesByBucket(ctx context.Context, q FlowQuery, d time.Duration, n int) ([]domain.TouchStats, error) // the weekly trend, as DoneByBucket
ItemTouches(ctx context.Context, item domain.ItemID) ([]domain.Touch, error) // with planned and reason
TouchCounts(ctx context.Context, items []domain.ItemID) (map[domain.ItemID]int, error) // the cards' touch_count
// Store
LabelTouch(ctx context.Context, event int64, reason domain.TouchReason, by domain.Actor) error // a person's choice (audited)
```

`FlowQuery` gains an optional `VersionID`; `TouchStats` carries the by-version breakdown.

### 5.7 `ports.ObjectStore` (changed: no object versions)

R2 implements neither bucket versioning nor `ListObjectVersions`, so the port stops relying on them: `Get(ctx, key)`, `Put`, `PutIfAbsent` (unchanged, `If-None-Match: *`), `List(prefix)` over `ListObjectsV2`, `Delete(key)` (gone for good on an unversioned bucket), `DeletePrefix(prefix)` (list and batch delete). `ObjectInfo` is `{Key, ETag, Size}`. `EnsureBucket` creates the bucket and calls no versioning API. The builder's skills index is cached by ETag instead of version id. Definitions already use immutable content-addressed blobs, so nothing else changes. `internal/factorystore`, the legacy import (`internal/definitions/legacy.go`) and `GOAL_LEGACY_IMPORT` are deleted (they read object versions); `internal/retention` purges with `ObjectStore.DeletePrefix("orgs/<org-id>/")` instead of `factorystore.DeleteOrg`; goald refuses to start while an organization still has a pending legacy import ("start the previous release once").

### 5.8 `ports.Secrets` (narrowed)

Platform settings only (`env:` and literals), as today's `envsecrets`; factory definitions never name one after the cutover (4-14). On Cloudflare the control Worker passes Secrets Store values to the goald and auth containers as env, so the same adapter serves both targets.

### 5.9 Gateway (`internal/gateway`)

The gateway becomes **Goal's MCP server for runs**: Goal's own tools and the run bundle, authorized by the run's task token. It proxies no connection and adds no header: remote MCP servers, like stdio ones, API calls and CLIs, are used directly from the box with the env the run received.

- **Connections' MCP servers are the worker's.** The `/run` bundle's MCP config lists each of the agent's connections whose kind has `mcp.url` (URL, headers as `${NAME}` references) or `mcp.command`; Claude Code connects to them from the box. The worker applies `tools.allow/deny` (`<connection>__<pattern>`, deny wins) in Claude Code's permission check, and their calls show in the run's tool events, not `gateway_tool_call`. The sidecars (`sidecars.go`, `ports.Sidecars`, `MaxSidecars`, `SidecarIdle`, `RunEnded` for sidecars, the docker sidecar code) are deleted in 4-11, and with them the gateway's only in-memory state; upstream forwarding, header resolution on the host and the per-connection host checks (`upstream.go`, `policy.go`) serve only legacy inline connections until the cutover deletes them (4-14).
- **Goal's tools**, served to every run whose factory lists knowledge sources: `knowledge__search {query, limit?: 1–20 (8), source?}` and `knowledge__read {document}`, filtered by the run's version's sources; item tools join them later. The names keep the `<server>__<tool>` form (Claude's tool names allow no dots); decisions call it `knowledge.search`. Every call appends `gateway_tool_call` as today.
- `GET /run`: the task's bundle (spec, prompt, skills, output schema, MCP config, tools, repository, connection docs) for the run in the token, from the definitions cache.
- OAuth connections and approval-gated calls (deferred, §2) may route through the gateway when they arrive.
- Every request goald makes for a connection (tests, sync, STS, embeddings) dials public addresses only, through one shared dialer from `internal/egress` (`egress.PublicDialer`); `GOAL_ALLOW_PRIVATE_UPSTREAMS=true` lifts it for a laptop's own test servers and is refused when goald listens off loopback.
- `domain.SecretPolicy` and the `env:` host bindings are deleted with the `env:` references (4-14).

### 5.10 Egress (`internal/egress`, `cmd/goal-egress`)

One policy (ADR 0009 amendment): any client on the agents' network reaches any destination whose resolved address is public, by `CONNECT` or plain-http forwarding, and goald's gateway paths `/mcp` and `/run` by the reverse path; loopback, private, link-local and metadata, CGNAT and reserved addresses are refused as today (`egress.Public`, rebinding-safe). The agent and tool roles, `GOAL_AGENT_EGRESS`, `GOAL_EGRESS_NAME`, `GOAL_EGRESS_IMAGE` and `docker.Egress` (goald creating and replacing the proxy) go: `goal-egress` is a compose service with its upstream in `GOAL_EGRESS_GATEWAY` (an origin; as built in 4-6 the proxy keeps no client-network check: only containers on compose's two networks reach its port, so the one policy is every client's). On Cloudflare there is no proxy: containers have the internet and no route to private addresses, which prototype 4-1 verifies.

### 5.11 Packages and depguard

| Package | May import | Rule |
| --- | --- | --- |
| `internal/connkind` | `domain`, `schema`, yaml, jsonschema | `connkind`: no adapters, engine, `api` |
| `internal/knowledge` | `domain`, `ports`, `golang.org/x/net/html` | `knowledge`: no adapters, engine, `api` |
| `internal/adapters/masterkey`, `awssts`, `openaiembed`, `gitdocs`, `confluence`, `gitlab`, `cloudflare` | own deps | adapters rule |

Deleted: `internal/factorystore`, `internal/adapters/gitworkspace`, the docker adapter's `task.go`, `checks.go`, `sidecars.go`, `egress.go` and `protocol.go`, `internal/gateway/sidecars.go`, `upstream.go` and `policy.go`. The `engine` rule becomes the allow-list `domain`, `ports` (README recommendation). The `api` rule denies `connkind` and `knowledge`; handlers reach them through interfaces declared in `internal/api`.

### 5.12 Topology

**Local (Docker Compose).** `compose.yaml` gains `goald` (image built by `task goald:image`, the Docker socket mounted read-write: acceptable on one's own machine, §11.3), `auth` (from `auth/Dockerfile`), `goal-egress` and the network `agents` (`internal: true`, no host-side address, ADR 0009). goald, the auth service and the data services are on the default network; `goal-egress` joins both networks; boxes are only on `agents`. `postgres` moves to a pinned pgvector image of PostgreSQL 18 (as built in 4-16: `pgvector/pgvector:0.8.7-pg18-trixie`, the official image's PostgreSQL 18.6 on Debian trixie with pgvector 0.8.7, so an existing `postgres:18` data directory, its glibc 2.41 collations included, starts unchanged; PlanetScale has pgvector 0.8.5, the same halfvec, HNSW and iterative scan). Every service keeps a healthcheck and `depends_on: condition: service_healthy`; ports stay on 127.0.0.1. `goal up --compose` (`task up:compose`) brings the whole stack up (as built in 4-6: goald and auth are in the `stack` profile, goald reads `.env` with compose's addresses on top and the auth service's secrets blanked, the auth service shares goald's network namespace so both keep their loopback rules and mails compose's Mailpit, which has no TLS, with `GOAL_SMTP_PLAINTEXT=on`, refused for any host but loopback or a name without dots, GOAL_AUTH must be session); plain `goal up` (`task up`) keeps running goald as a host binary, as installs do today: the same adapter talks to the same Docker socket, and `goal up` starts `goal-egress` with `GOAL_EGRESS_GATEWAY` derived from goald's gateway listener (`host.docker.internal`). The agents network's name is `GOAL_SANDBOX_NETWORK` (default `goal_agents`) for compose and goald alike. As built in 4-7, nothing is mounted into goald's container but the Docker socket: runs get everything from the gateway's `/run`.

**Cloudflare.** One control Worker (TypeScript, `deploy/cloudflare/`, the only Cloudflare-specific code besides `internal/adapters/cloudflare`):

```
public https ─▶ Worker ─▶ goald container (Durable Object, standard-1, kept warm)      every public path
goald ─▶ auth.internal     (goald container's outbound handler) ─▶ auth container      proxy secret as today
goald ─▶ sandbox.internal  (goald container's outbound handler) ─▶ run DO ─▶ box       bearer GOAL_SANDBOX_KEY
box   ─▶ gateway.internal  (box's outbound handler) ─▶ goald container  /mcp, /run      task token
goald ─▶ PostgreSQL (direct TLS, verify-full), R2 (S3 API through s3store), the internet
```

R2 through `s3store` (endpoint and keys in settings), managed PostgreSQL over a direct connection (no Hyperdrive: goald needs `LISTEN`, advisory locks and session settings), the provider chosen by prototype 4-2. Secrets Store holds platform secrets only (master key, gateway key, sandbox key, auth proxy secret, Better Auth secret, database passwords, embedding key); the Worker passes each container exactly its own. Snapshots stay off for goald. A goald redeploy ends its runs in flight; their leases expire and the items are claimed again.

**Terraform** (`terraform/`, platform infrastructure only, applied by a person): the R2 bucket, DNS records, Secrets Store secrets, the Workers route and Access for the staging host. wrangler deploys the Worker, its Durable Object classes and the container images, which Terraform cannot. Creating an organization, a connection, a knowledge source or a factory never needs Terraform or a deploy. Today's AWS Terraform (a Bedrock knowledge base per factory) is deleted.

### 5.13 Settings

New: `GOAL_SANDBOX` (`docker` | `cloudflare`, default `docker`), `GOAL_AGENT_IMAGE` (replaces `GOAL_DEFAULT_AGENT_IMAGE`), `GOAL_SANDBOX_NETWORK`, `GOAL_SANDBOX_URL`, `GOAL_SANDBOX_KEY`, `GOAL_MASTER_KEYS` / `GOAL_MASTER_KEY_FILE`, `GOAL_EMBED_URL`, `GOAL_EMBED_MODEL`, `GOAL_EMBED_KEY`, `GOAL_KB_WORKERS`, `GOAL_UPLOAD_MAX_BYTES` (25 MiB), `GOAL_ALLOW_PRIVATE_UPSTREAMS`; `GOAL_EGRESS_GATEWAY` for `goal-egress`. Removed: `GOAL_TASK_ENV`, `GOAL_GITHUB_TOKEN`, `GOAL_GITHUB_API_URL`, `GOAL_GITHUB_WEB_URL`, `GOAL_AGENT_EGRESS`, `GOAL_EGRESS_NAME`, `GOAL_EGRESS_IMAGE`, `GOAL_WORKSPACE_ROOT`, `GOAL_RUN_DIR`, `GOAL_LEGACY_IMPORT`; goald warns when one is still set and ignores it. Every secret setting joins goald's reserved list.

## 6. Data model

Every org table: `org_id uuid NOT NULL DEFAULT goal_org()` referencing `organizations(id) ON DELETE CASCADE`, composite foreign keys `(org_id, …)`, the `org_isolation` policy, explicit grants to `goal_app`, an index that starts with `org_id`.

| Migration | PR | Content |
| --- | --- | --- |
| `00050_drop_legacy_import.sql` | 4-3 | drops 00039's `legacy_import_pending` (no SQL function used it; its queries `LegacyImportPending`, `EndLegacyImport` and `HasVersions` go with the import). `postgres.Migrate` refuses, before applying it, a database where a live organization's import is still pending or that is from before 00039 (`ErrLegacyImportPending`) |
| `00051_connections.sql` | 4-8 | `connection_kinds`, `connections`, `goal_sync_platform_kinds(jsonb)` |
| `00052_touches.sql` | 4-23 | the partial index on person events; `item_touch_labels`, the trigger that adds a row per person event, `goal_touch_planned` and the view `item_touches` (4-23 landed before 4-16, so it took `00052`) |
| `00053_connection_rename.sql` | 4-9 | `GRANT UPDATE (name) ON connections TO goal_app`: a rename (§8.2), which 00051's column grants left out (4-9 landed after 4-23, so it took `00053`) |
| `00054_knowledge.sql` | 4-16 | `knowledge_sources`, `knowledge_documents`, `knowledge_chunks`; requires the `vector` extension |

```sql
-- 00051_connections.sql
CREATE TABLE connection_kinds (
  id          uuid PRIMARY KEY,
  org_id      uuid DEFAULT goal_org() REFERENCES organizations(id) ON DELETE CASCADE,  -- NULL: shipped by the platform
  name        text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  spec        jsonb NOT NULL CHECK (jsonb_typeof(spec) = 'object' AND octet_length(spec::text) <= 65536),
  spec_sha256 text NOT NULL,
  created_by  text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE NULLS NOT DISTINCT (org_id, name)
);
-- RLS: SELECT where org_id IS NULL OR org_id = (SELECT goal_org()); INSERT/UPDATE/DELETE only
-- org_id = goal_org() (as built: org_isolation plus a SELECT-only platform_kinds policy that also needs an organization bound). Platform
-- rows only through goal_sync_platform_kinds (SECURITY DEFINER, search_path pinned), called at
-- goald's start; goal_connection_key_ids() (definer) lists the key ids in use for the startup check.

CREATE TABLE connections (
  id              uuid PRIMARY KEY,
  org_id          uuid NOT NULL DEFAULT goal_org() REFERENCES organizations(id) ON DELETE CASCADE,
  name            text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  kind_id         uuid NOT NULL REFERENCES connection_kinds(id),   -- NO ACTION (as built): checked at the statement's end, so an org's purge cascades in any order
  -- as built: a trigger (connections_kind_of_org) refuses a kind of another organization
  description     text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  fields          jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(fields) = 'object' AND octet_length(fields::text) <= 8192),
  secret_fields   text[] NOT NULL DEFAULT '{}',          -- the names that are set, for display
  key_id          text,
  nonce           bytea CHECK (octet_length(nonce) = 12),
  ciphertext      bytea CHECK (octet_length(ciphertext) <= 65536),
  CHECK ((key_id IS NULL) = (nonce IS NULL) AND (nonce IS NULL) = (ciphertext IS NULL)),
  CHECK ((ciphertext IS NULL) = (cardinality(secret_fields) = 0)),
  created_by      text NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      text NOT NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  last_used_at    timestamptz,
  last_test_at    timestamptz,
  last_test_ok    boolean,
  last_test_error text CHECK (length(last_test_error) <= 500),
  UNIQUE (org_id, name),
  UNIQUE (org_id, id)
);
CREATE INDEX connections_kind_idx ON connections (kind_id);
```

```sql
-- 00054_knowledge.sql (goal db init creates the extension as the admin; the migration fails with
-- "extension vector is missing: run goal db init" otherwise)
CREATE TABLE knowledge_sources (
  id             uuid PRIMARY KEY,
  org_id         uuid NOT NULL DEFAULT goal_org() REFERENCES organizations(id) ON DELETE CASCADE,
  name           text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  title          text NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
  kind           text NOT NULL CHECK (kind IN ('upload', 'synced', 'live')),
  connection_id  uuid,
  config         jsonb NOT NULL DEFAULT '{}' CHECK (octet_length(config::text) <= 8192),
  sync_every     interval NOT NULL DEFAULT '1 hour' CHECK (sync_every >= interval '15 minutes'),
  next_sync_at   timestamptz,
  lease_until    timestamptz,
  status         text NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'syncing', 'failed')),
  last_synced_at timestamptz,
  last_error     text CHECK (length(last_error) <= 500),
  embed_model    text,                                    -- the model its chunks were embedded with
  created_by text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'upload') = (connection_id IS NULL)),
  FOREIGN KEY (org_id, connection_id) REFERENCES connections (org_id, id),   -- as built: NO ACTION, so an org's purge cascades in any order
  UNIQUE (org_id, name),
  UNIQUE (org_id, id)
);
CREATE INDEX knowledge_sources_due_idx ON knowledge_sources (org_id, next_sync_at) WHERE kind <> 'live';

CREATE TABLE knowledge_documents (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations(id) ON DELETE CASCADE,
  source_id   uuid NOT NULL,
  external_id text NOT NULL CHECK (length(external_id) <= 1024),
  version     text NOT NULL,                              -- git blob id, page version, upload sha256
  title       text NOT NULL,
  url         text,                                       -- synced: the link back
  mime        text NOT NULL,
  bytes       bigint NOT NULL CHECK (bytes >= 0),
  blob_sha256 text,                                       -- uploads: orgs/<org-id>/knowledge/<sha256>
  status      text NOT NULL CHECK (status IN ('pending', 'indexed', 'failed', 'skipped')),
  error       text CHECK (length(error) <= 500),
  seen_at     timestamptz NOT NULL,
  indexed_at  timestamptz,
  created_by  text NOT NULL,
  FOREIGN KEY (org_id, source_id) REFERENCES knowledge_sources (org_id, id) ON DELETE CASCADE,
  UNIQUE (source_id, external_id),
  UNIQUE (org_id, id)
);
CREATE INDEX knowledge_documents_org_source_idx ON knowledge_documents (org_id, source_id);

CREATE TABLE knowledge_chunks (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations(id) ON DELETE CASCADE,
  source_id   uuid NOT NULL,
  document_id uuid NOT NULL,
  ord         int NOT NULL,
  heading     text NOT NULL DEFAULT '',
  text        text NOT NULL CHECK (length(text) <= 8000),
  tsv         tsvector GENERATED ALWAYS AS (to_tsvector('simple', heading || ' ' || text)) STORED,
  embedding   halfvec(1024),
  embed_model text,
  FOREIGN KEY (org_id, document_id) REFERENCES knowledge_documents (org_id, id) ON DELETE CASCADE
);
CREATE INDEX knowledge_chunks_org_source_idx ON knowledge_chunks (org_id, source_id);
CREATE INDEX knowledge_chunks_document_idx ON knowledge_chunks (document_id);
CREATE INDEX knowledge_chunks_tsv_idx ON knowledge_chunks USING gin (tsv);
CREATE INDEX knowledge_chunks_embedding_idx ON knowledge_chunks USING hnsw (embedding halfvec_cosine_ops);
```

```sql
-- 00052_touches.sql
CREATE INDEX item_events_person_idx ON item_events (item_id, at) WHERE data->>'actor' LIKE 'user:%';

CREATE TABLE item_touch_labels (
  event_id   bigint PRIMARY KEY REFERENCES item_events (id) ON DELETE CASCADE,
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations(id) ON DELETE CASCADE,
  item_id    uuid NOT NULL,
  reason     text CHECK (reason IN ('missing_knowledge', 'wrong_decision', 'required_gate', 'other')),
  reason_by  text,                                     -- user:<id> who chose it (phase 5 adds agent:<name>)
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE
);
CREATE INDEX item_touch_labels_org_item_idx ON item_touch_labels (org_id, item_id);
-- AFTER INSERT trigger on item_events, for events whose data.actor starts with 'user:' and
-- that are not lease_taken or created: one row, reason 'required_gate' when data.planned (or,
-- before 4-23, for approved, gate_approved and merged): goal_touch_planned(kind, data).
-- View item_touches (security_invoker): one row per touch, DISTINCT ON (item_id, actor, at)
-- ordered by the naming precedence, with its event's reason. LabelTouch upserts the row, so
-- older touches can get a reason too.
```

A touch that groups several events (a merge's `merged` and `approved`) reads the label of the event that names it (§4.9).

- No `agent_runs` change: a box is named by its run id, and reaping reads `Store.RunsOver`.
- `knowledge_chunks` is partitioned by hash of `org_id` when it passes about 10 million rows (later; the queries already filter by org).
- sqlc queries in `queries/connections.sql` and `queries/knowledge.sql`. Conformance: `connectiontest` (create, update, replace secret, resolve, two-org RLS, no plaintext in any column, AAD mismatch refused, rekey), `knowledgetest` (lease claim and expiry, replace chunks, unseen deletion, hybrid ranking fixtures, sources filter, two-org RLS with the vector scan).

## 7. Bucket layout changes

```
s3://<bucket>/
  orgs/<org-id>/blobs/<sha256>        definition files (unchanged)
  orgs/<org-id>/knowledge/<sha256>    uploaded knowledge documents, written once with PutIfAbsent (new)
  catalog/templates/  catalog/skills/  (unchanged; catalog/connectors/ was never built and is dropped)
```

An upload's object is deleted when no document of the organization references its hash any more; the organization's purge removes `orgs/<org-id>/` as today. The bucket needs no versioning (ADR 0005 amendment).

## 8. API

### 8.1 Permissions (`api/permissions.yaml`)

```yaml
  - {name: connection:read,   level: org, org_roles: [owner, admin, member], keys: true, pat: true}
  - {name: connection:manage, level: org, org_roles: [owner, admin], fresh: true}
  - {name: knowledge:read,    level: org, org_roles: [owner, admin, member], keys: true, pat: true}
  - {name: knowledge:manage,  level: org, org_roles: [owner, admin], keys: true}
```

They replace spec 3's proposed `credential:read|manage|grant`, `vault:manage`, `connector:manage`, `connection:test` and `approval:decide` (deferred with approvals).

### 8.2 Endpoints (tags `connections`, `knowledge`)

| Method and path | Scope | Does |
| --- | --- | --- |
| `GET /v1/orgs/{org}/connection-kinds` | `connection:read` | platform and org kinds: name, title, description, fields (name, title, secret, required, default), modes, `model`, hooks, docs, `platform` |
| `PUT /v1/orgs/{org}/connection-kinds/{kind}` | `connection:manage` | create or replace an org kind (`ConnectionKindFile`, the kind file as JSON, parsed by `connkind.Parse`); as built, 400 `connection-kind-invalid` whose detail names every broken rule (connkind reports them as one error), also when `metadata.name` is not the path's; 409 `connection-kind-name-taken` |
| `DELETE /v1/orgs/{org}/connection-kinds/{kind}` | `connection:manage` | 409 `connection-kind-in-use` while a connection uses it |
| `GET /v1/orgs/{org}/connections?factory=&kind=` | `connection:read` | `Connection` list: name, kind, description, fields, `secrets_set`, status, `used_by` (factories of current versions, knowledge sources), last used, last test |
| `POST /v1/orgs/{org}/connections` | `connection:manage` | `{name, kind, description?, fields, secrets}` → 201 `Connection` (never a value); 400 `field-required`, `field-unknown`; 404 `connection-kind-not-found`; 409 `connection-name-taken`; 503 `connections-unavailable` for secrets without a master key |
| `GET /v1/orgs/{org}/connections/{connection}` | `connection:read` | one `Connection` with the kind's env names and docs |
| `PATCH /v1/orgs/{org}/connections/{connection}` | `connection:manage` | `{name?, description?, fields?, secrets?}`; `secrets` replaces the named secret fields; a rename is 409 `connection-in-use` while a current version names it |
| `DELETE /v1/orgs/{org}/connections/{connection}` | `connection:manage` | 204; 409 `connection-in-use` (listing what) while a current version or a knowledge source uses it |
| `POST /v1/orgs/{org}/connections/{connection}/test` | `connection:manage` | runs the kind's `test` from goald (public addresses only, 10 s, no redirects) → `{ok, status?, error?, at}` (a failed test is a 200 with `ok: false`; `status` when a response came); 409 `connection-not-testable` |
| `GET /v1/orgs/{org}/knowledge/sources` | `knowledge:read` | `KnowledgeSource` list with status, counts, last and next sync, `used_by` |
| `POST /v1/orgs/{org}/knowledge/sources` | `knowledge:manage` | `{name, title, kind, connection?, config?, sync_every?}` → 201; 400 `connection-cannot-sync`; 409 `knowledge-source-name-taken` |
| `GET`, `PATCH`, `DELETE /v1/orgs/{org}/knowledge/sources/{source}` | read, manage, manage | one source; change title, config, schedule; delete with its documents (409 `knowledge-source-in-use` while a current version lists it) |
| `POST /v1/orgs/{org}/knowledge/sources/{source}/sync` | `knowledge:manage` | 202; the source is due now |
| `GET /v1/orgs/{org}/knowledge/sources/{source}/documents?after=&limit=` | `knowledge:read` | one page of documents (title, url, status, error, bytes, indexed_at) and `next_after` |
| `POST /v1/orgs/{org}/knowledge/sources/{source}/documents` | `knowledge:manage` | `multipart/form-data` files of an upload source → 202 with the documents (`pending`); 413 `upload-too-large`, 415 `unsupported-document-type`, 409 `not-an-upload-source` |
| `GET /v1/orgs/{org}/knowledge/documents/{document}` | `knowledge:read` | metadata and the indexed text |
| `GET /v1/orgs/{org}/knowledge/documents/{document}/content` | `knowledge:read` | the uploaded original (`Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`) |
| `DELETE /v1/orgs/{org}/knowledge/documents/{document}` | `knowledge:manage` | an uploaded document; 409 `not-an-upload-source` otherwise |
| `GET /v1/orgs/{org}/knowledge/search?q=&source=&limit=` | `knowledge:read` | `searchKnowledge`: what `knowledge__search` returns, over every source or the named ones |

**Touches** (tag `work`):

| Method and path | Scope | Does |
| --- | --- | --- |
| `GET /v1/orgs/{org}/factories/{factory}/metrics/touches?from=&to=&type=&weeks=` | `factory:read` | `getTouchMetrics`: `TouchStats` for the line (§4.9): finished items, touches per item (mean, median), touchless rate, planned and unplanned per item, by station, by kind, by reason, by definition version (`?version=` narrows to one: a number or `current`), weekly trend (`weeks`, 1–26, default 12, ending on `to`), line stops by people |
| `GET /v1/orgs/{org}/metrics/touches?from=&to=` | `org:read` | `getOrgTouchMetrics`: the organization's total and each factory the principal may read |
| `PUT /v1/orgs/{org}/items/{item}/touches/{event}/reason` | `item:operate` | `setTouchReason`, `{reason}` → 204: a person's choice, audited (`touch.labelled`); 404 `touch-not-found` for an event that is not a touch of the item |

Changed shared schemas: `Item` gains `touches` (`ItemTouches` = `{count, planned, unplanned, list: [Touch]}`, `Touch` = `{event_id, kind, station, at, actor, planned, reason, reason_by}`; getItem only); `ItemCard` gains `touch_count`; the board's `line` gains `touches` (`TouchHeadline` = `{finished, per_item_mean, per_item_median, touchless_rate}` over the board's window); `getFactoryOverview`'s factories gain `touches` (`TouchTrend` = `{current, previous}`: the overview's window against the one before).

Upload routes take bodies up to `GOAL_UPLOAD_MAX_BYTES`; every other route keeps the 1 MiB cap. Secret values appear in no response, log line, event or audit entry. Changed shared schemas: `Problem.code` adds the codes above; `Factory` gains nothing (the builder reads connections by `?factory=`). Removed from the registry: `/v1/oauth/callback` and `/oauth/client-metadata.json` (deferred with OAuth connections).

### 8.3 Stream

On `/v1/orgs/{org}/stream`, through `goal_org_events`: `connection` (`{connection_id, name, change: created|updated|deleted|tested}`; as built, notified by the connections adapter in the change's transaction and read back from the change's audit entry, so a forged one is dropped) and `knowledge` (`{source_id, name, status}` when a sync starts or ends). Query invalidation: `['org', id, 'connections']`, `['org', id, 'connection-kinds']`, `['org', id, 'knowledge']`.

## 9. CLI changes

- `goal connection list|show <name>|create <name> --kind <kind> [--field k=v]… [--secret k=-|--secret-file k=<path>]…|update <name> …|delete <name>|test <name>`: secret values only from stdin (`-`, one value) or a file, never from argv. As built (4-9): `list [--kind k] [--factory f]`; `create … [--description d]`; `update <name> [--name n] [--description d] [--field k=v]… [--secret k=-] [--secret-file k=<path>]…`, whose fields merge into the connection's (`k=` clears one); `test` exits 1 when the test fails.
- `goal connection kinds` and `goal connection kind push <file.yaml>` / `kind delete <name>`.
- `goal knowledge sources|add <name> --kind upload|synced|live [--connection c] [--config k=v]…|sync <source>|upload <source> <file>…|rm <document>|search "<query>" [--source s]…`.
- `goal connection import-env` (4-14, one-time): creates `github` (kind `github`, from `GOAL_GITHUB_TOKEN`) and `claude` (kind `claude-subscription` from `CLAUDE_CODE_OAUTH_TOKEN`, or `anthropic` from `ANTHROPIC_API_KEY`) in the current organization from the CLI's environment, skipping existing names.
- `goal db rekey` (needs only the database, as goal_owner through `GOAL_MIGRATE_DATABASE_URL` like `goal db migrate`, whom row-level security does not bind, and `GOAL_MASTER_KEYS`; goald may keep running), `goal db init` creates the `vector` extension.
- `goal status [--factory f] [--window 7d]` (new): per factory, WIP, items done, lead time p85, **touches per finished item**, touchless rate and the station with the most unplanned touches, and without `--factory` the organization's total (`getOrgTouchMetrics`); `goal item <ref>` lists the item's touches (4-24: on stderr, so its JSON on stdout stays the item alone for scripts).
- Removed: nothing the CLI uses today; `goal factory validate` shows the quick fixes of §4.4.

## 10. Web UI

- **Routes** (org pages under C's `o.$org._org` layout): `/o/:org/connections`, `/o/:org/connections/new`, `/o/:org/connections/:connection`, `/o/:org/knowledge`, `/o/:org/knowledge/new`, `/o/:org/knowledge/:source`. The factory sidebar's Connections and Knowledge open the same pages filtered to the factory (`?factory=`). The org sidebar gains both entries.
- **Data**: `@goal/api-client`; query keys `['org', id, 'connections', {factory?}]`, `['org', id, 'connection-kinds']`, `['org', id, 'knowledge', 'sources']`, `['org', id, 'knowledge', 'source', name, 'documents']`; SSE `connection` and `knowledge` invalidate them.
- **Connections**: a table on desktop, cards under 768 px; status badge with text (never color only); "New connection" and row actions only with `connection:manage` in `can`. New: kind picker (search, platform and org kinds), then generated fields; secret inputs `type=password`, `autocomplete=off`, cleared after submit; Test before or after create. Detail: "Replace secret" opens a dialog per secret field; Delete shows `used_by` and needs the name typed. A fresh sign-in (403 `reauth-required`) opens phase 3's re-auth dialog and retries.
- **Knowledge**: sources with live status from the stream; documents with failure reasons; drop zone with per-file progress and refusals (size, type); "Try a search" shows ranked snippets with source and link.
- **Builder**: Basics shows the model connection select (kinds with `model`), repository URL and connection, size and tools (chips with mise spec validation); Connections lists the org's connections with checkboxes for the factory and per agent, kind modes as badges, a link to the Connections page, and the knowledge sources the factory may search.
- **Touches**: a headline tile on the Floor and each factory tile ("1.4 touches per item, 38% touchless", trend against the previous window), the Touches panel on the Floor (by station, kind, definition version, planned against unplanned and reason, with a link to the finished items: the contract has no filter of items by where they were touched, so the bars do not link one by one), the Board header's figures, each station's touches per finished item and a count on cards with any touch, and on the Item the count and list with a reason select (`item:operate`). Query key `['org', id, 'factory', f, 'touches', range]` (4-24: the organization's total is `goal status`'s; no screen shows it, so the web has no `['org', id, 'touches', window]`); item events on the stream invalidate it with the board's metrics.
- **States**: loading skeletons, empty states with the next action ("No connections yet. Add the GitHub token your agents use."), problem details inline. Accessibility: labelled inputs, focus kept in dialogs, keyboard-operable drop zone, axe clean. Layout per `design-system/goal/MASTER.md`, full viewport.

## 11. Security and tenancy

### 11.1 Controls kept

| Control | How |
| --- | --- |
| Organizations apart | RLS on every new table, composite foreign keys, `goal_app` without BYPASSRLS (`CheckAppRole`), platform kinds only through a definer function |
| Secrets at rest | AES-256-GCM with the master key, AAD binding org, connection and key id; never in a response, log, event or audit row; write-only API; fresh sign-in for every change |
| Who changes connections | owners and admins (`connection:manage`), audited in the same transaction |
| One box per run | a new container per run, nothing mounted, deleted at the end, reaped if goald dies; runs never share a box |
| No private reach | boxes and goald's connection-driven requests reach public addresses only; private, loopback, link-local and metadata addresses refused (rebinding-safe) |
| Honest checks | check exit codes come from the runtime, never from the agent; checks get no connection env or task token |
| Stored AWS keys stay home | runs get STS credentials minted for the run |
| Cloudflare env | the Worker builds a box's env only from goald's request; Secrets Store values go to their own container only; snapshots off for goald |

### 11.2 Threat rows and their tests

| # | Threat | Control | Test |
| --- | --- | --- | --- |
| 1 | Org B reads org A's connection or chunks | RLS, composite FKs | `TestConnections_RLSAcrossOrgs`, `TestKnowledge_RLSAcrossOrgsWithVectorScan` |
| 2 | Database dump reveals credentials | sealed values, AAD | `TestConnections_NoPlaintextInAnyColumn`, `TestConnections_AADMismatchRefused` |
| 3 | A member or key changes or reads a credential | `connection:manage`, fresh, write-only | access golden matrix rows; `TestAPI_ConnectionResponsesHoldNoSecret` |
| 4 | A connection URL reaches goald's network (auth, RustFS, Mailpit) | public-only dialer for goald's requests (tests, sync, STS); a remote MCP URL is dialed from the box under row 5 | `TestConnectionTest_RefusesPrivateURL` |
| 5 | An agent reaches the host or a private address | egress policy (local), Cloudflare networking (prototype) | `egress` table tests; `sandboxtest` "no private address"; 4-1 report |
| 6 | An agent forges a passing check | runtime exit codes | `TestEngine_CheckResultFromExitStatus` |
| 7 | Stored AWS keys reach a box | minting | `TestEngine_AWSStoredKeysNeverInEnv` |
| 8 | A search returns sources the factory does not list | filter in the one query | `TestKnowledge_Conformance/SearchOnlyListedSources` (fake and PostgreSQL) |
| 9 | Secrets in events or logs | scrubbing | `TestEngine_RelayScrubsConnectionValues`, log capture test |
| 10 | The master key on Cloudflare leaks into a box | Worker env allow-list | Worker test `box start carries only goald's env` |
| 11 | Uploaded content served as active HTML | attachment + nosniff | `TestAPI_DocumentContentIsAttachment` |
| 12 | Wrong or missing master key | startup check | `TestGoald_RefusesUnknownMasterKeyID` |

### 11.3 Accepted risks

1. **An agent holds its connections' credentials** as environment variables for the run's life, remote MCP servers' included. A prompt-injected agent can use or send them anywhere on the internet, and can call a connection's MCP server or API past `tools.allow/deny`, which only the worker enforces. The limit is each token's own scope: use fine-grained, narrow tokens (chosen repositories, read-only API keys, a narrow AWS role) and separate connections for factories that read untrusted input.
2. **Agents have the public internet**, so code and data their tokens reach can leave.
3. **Any editor of a factory can use any connection of the organization** by naming it; admins own the values, editors choose their use. Separate organizations separate people who must not share credentials.
4. **Pushes run the repository's CI.** Protect default branches and CI secrets at the code host.
5. **Synced sources are flattened**: a factory listing a source sees everything the connection sees. Indexed text can carry prompt injection.
6. **The master key opens every organization's credentials**; backups keep ciphertext of deleted credentials until they expire. Escrow the key offline (on Cloudflare it cannot be read back from Secrets Store); revoke a leaked credential at its provider too.
7. **goald in local Compose holds the Docker socket**, which is root on that machine: for one's own laptop, never a shared host.
8. **Boxes on the local agents' network can reach each other's listening ports** (another run's PostgreSQL).
9. **A task token stays valid until its expiry** (task timeout + lease TTL) after its run; it reaches only Goal's tools and bundle for that run.
10. **A goald redeploy on Cloudflare ends runs in flight**; they are claimed again.
11. **The Claude subscription kind is for one person's own runs**; a shared install uses an API key or Bedrock.
12. **The embedding provider sees indexed text** (choose a local model to avoid it).

## 12. Observability

- Logs: `sandbox created|deleted` (run, size, seconds), `sandbox exec` (step `prepare|agent|check`, exit, seconds), `connection used` (run, connection names; never values), `knowledge sync` (source, listed, fetched, deleted, chunks, embed tokens, seconds, error).
- Metrics (OpenTelemetry once goald exports them; log attributes until then): `goal_sandboxes_running`, `goal_sandbox_start_seconds`, `goal_sandbox_prepare_seconds`, `goal_kb_sync_seconds`, `goal_kb_chunks`, `goal_embed_tokens_total`.
- Item events: `sandbox_started` and `prepared` (with seconds and tool count) before `agent_started`; check events as today.
- Touches: `goal_item_touches` (a histogram per finished item, by factory) once metrics are exported; the API's figures meanwhile.
- Audit actions: `touch.labelled`, `connection.created|updated|secret_replaced|deleted|tested|used`, `connection_kind.created|updated|deleted`, `knowledge_source.created|updated|deleted`, `knowledge_document.uploaded|deleted`.

## 13. Testing

- **Engine** with `porttest.Sandboxes` and `porttest.Connections`: lifecycle order, prepare failure fails the run, contract retry in the same box, checks through exec with no connection env, minting, env conflicts, delete on every path, reaping by `RunsOver`, autoscaling (no box while nothing is claimable), merge across repositories with partial failure and retry.
- **Conformance suites**: `sandboxtest` (fake; `docker` integration; `cloudflare` against a staging account in 4-21, not in `task check`), `connectiontest` and `knowledgetest` (fake and PostgreSQL 18 with pgvector), `objectstoretest` on RustFS with versioning off.
- **Gateway**: task-token auth, knowledge tools, `/run` bundle; no connection's tools are served.
- **Units**: kind schema and template rendering (`connkind`, table-driven, every refusal), chunker, HTML extraction, RRF fusion, settings.
- **Worker** (Vitest): prepare (bundle, git credential helper reads env, mise invocation, clone and branch), MCP config with stdio and remote servers (headers as `${NAME}`, never values), `tools.allow/deny` in the permission check, connections section of the prompt.
- **Control Worker** (Vitest with the Workers runtime): routing, sandbox API auth, env allow-list, alarm destroy.
- **Web** (Playwright on the mock API, 375/768/1440 px, axe): create, test, replace secret and delete a connection; add a source, upload, search.
- **Touches** (`storetest`, fake and PostgreSQL): one action counted once (merge, a send-back with its `lease_taken`), the naming precedence, agents, the engine and API keys never counted, `created` excluded, planned against unplanned with and without `data.planned`, touchless rate, by definition version, by station and kind, the window by finish time, two-org RLS; the trigger's label rows.
- **End to end** on Compose: the goal factory takes an item to done with a GitHub connection and a model connection.

## 14. Rollout and migration

From today (goald on the host, `env:GOAL_GITHUB_TOKEN` and `runtime.image` in published versions, `GOAL_TASK_ENV`, `GOAL_AGENT_EGRESS`, a versioned RustFS bucket):

1. **Prototypes first** (4-1, 4-2): nothing changes for users.
2. **Storage** (4-3): the RustFS bucket keeps versioning on; it is harmless and nothing relies on it. goald refuses to start while a legacy import is pending.
3. **Sandboxes** (4-4 to 4-7): published versions keep working. In the interim `runtime.image` is ignored (the base image runs every factory; `field-removed` warns), the clone uses the repository token as before but inside the box, and `GOAL_TASK_ENV`'s model token reaches the agent exec. Pause factories, upgrade, resume: runs in flight on the old runner finish or are requeued.
4. **Connections** (4-8 to 4-13): create connections (`goal connection import-env` or the UI), then publish each factory once with the builder's quick fixes (`runtime.modelConnection`, `runtime.repository.connection`, org connections instead of inline ones and `env:` references).
5. **Cutover** (4-14): goald refuses to run a version that still has `env:` references or no model connection, with the block reason "publish a version with connections (spec 4 §4.4)"; the interim rule in the specs README ends; `GOAL_TASK_ENV`, `GOAL_GITHUB_TOKEN` and `GOAL_AGENT_EGRESS` are ignored with a warning.
6. **Knowledge** (4-16): switch `compose.yaml`'s PostgreSQL image to pgvector (same major, same volume), `goal db init`, migrate. As built: the first `goal up` (`task up`) after it recreates the `postgres` container once on the new image and the same `goal-pg` volume (compose sees the image change; the data is kept: verified on a `pg_basebackup` copy of an install's 18.6 data directory), then `goal db init` creates the extension and goald migrates `00054`. Stop goald first, as for any upgrade. `task test-integration` keeps the running container (`--no-recreate`), so it needs pgvector in it: recreate it once (`docker compose up -d postgres`). A PostgreSQL without pgvector fails `goal db init` naming what to do, and `00054` with "extension vector is missing: run goal db init".
7. **Cloudflare** (4-21, 4-22): a fresh install; moving a local install's data is `pg_dump`/`pg_restore` plus a bucket copy, with the same master key.

8. **Touches** (4-23) are computed from the history already recorded, so existing items count from the first release; the trigger adds labels for new events only (older touches have no reason, and older approvals and merges count as planned).

No table is rewritten; every migration is additive except 00050's drop of the legacy flag.

## 15. Work breakdown

Each row is one PR on `goal/4-<n>-<slug>`, passing `task check`, plus `task migrate-check` and `task test-integration` when it touches SQL or an adapter, plus `task web:check`/`web:e2e` for web. Skills (AGENTS.md): the Go skills on every Go PR; PostgreSQL skills with migrations; UI skills on web PRs; `openapi-spec-generation` and `api-design-principles` with `api/`; `docker-compose-patterns` and `multi-stage-dockerfile` with compose or images; `mcp-builder` with the gateway or worker MCP; `security-and-hardening` and `golang-security` with secrets, egress or the Worker; Infra skills with `terraform/` and `deploy/`. 4-1 and 4-2 are prototypes: throwaway code outside `main`, results written into this spec.

| # | Pull request | Acceptance criteria | Verified by |
| --- | --- | --- | --- |
| 4-1 | **Prototype: a run on Cloudflare Containers.** A throwaway Worker + DO starts `goal-agent`, execs a 30-minute NDJSON stream, deletes it; alarm destroy; outbound handlers for `gateway.internal` and `sandbox.internal`; goald container kept warm | start latency, exec streaming, exit codes, internet on, private ranges and `169.254.169.254` unreachable, a box's env has only what was sent, image size limits; written up in §5.12 | manual run with a report |
| 4-2 | **Prototype: managed PostgreSQL.** goose `00001–00049` + pgvector on PlanetScale Postgres and Neon (owner's accounts) | roles and `LOGIN` grants, RLS with `CheckAppRole`, `SECURITY DEFINER` with pinned `search_path`, `LISTEN/NOTIFY` over direct TLS from a container and after failover, advisory locks, `set_config(…, true)`, `CREATE EXTENSION vector` + HNSW `halfvec`; integration suites green; provider chosen (§17) | integration suites against each |
| 4-3 | **ObjectStore without versions** (§5.7) + delete `factorystore`, the legacy import and `GOAL_LEGACY_IMPORT`; `00050` | s3store passes the suite on RustFS with versioning off; builder cache by ETag; goald refuses a pending import | integration, `task check` |
| 4-4 | **`ports.Sandboxes`**, `porttest.Sandboxes`, `sandboxtest`, `domain.DecodeWorkerLine` | suite green on the fake; decoder tests moved | unit |
| 4-5 | **`goal-agent` image** (Claude Code via the Agent SDK, git, gh, mise, Go, Node, Python, PostgreSQL 18 server binaries; uid 1000) + `goal-agent prepare` + gateway `GET /run` | prepare builds `/goal` from the bundle, clones and checks out the branch, runs mise for the repository and `runtime.tools`; image smoke | Vitest, gateway tests, `task worker:image` smoke |
| 4-6 | **docker adapter on Sandboxes + Compose**: goald and auth images and services, `goal-egress` service, `agents` network, one egress policy (public), `GOAL_AGENT_EGRESS` and `docker.Egress` removed (ADR 0009 amendment) | `sandboxtest` passes against Docker; a box reaches a public host and not the host, RustFS or a private address; `goal up` brings the stack up; host-binary mode still works | integration, egress tests, manual |
| 4-7 | **Engine on Sandboxes** (§4.5, §4.6): lifecycle, checks through exec, retries in the box, reaping by `List`; delete `Runner`, `Task`, `Workspaces`, `Checker`, `Reaper`, `gitworkspace`, `docker.AgentFiles`; `runtime.image` ignored with `field-removed` | engine tests; an item runs to done on Compose with today's env token | engine tests, e2e |
| 4-8 | **Connections core**: `ports.MasterKey` + `masterkey`, `schema/connection-kind.schema.json`, `internal/connkind` with the platform kinds, `domain` kinds and `Render`, `00051`, `ports.Connections` + postgres adapter + fake + `connectiontest`, `goal db rekey` | threat rows 1, 2, 12; rekey reseals all; platform kinds synced at start | conformance, integration |
| 4-9 | **Connections API, permissions, CLI, test probe**, public-only dialer, SSE `connection`; `00053` (the rename grant) | §8.1–8.3; row 3, 4 | handler tests, golden matrix, CLI tests |
| 4-10 | **Definition schema** (§4.4): new fields, removals with quick fixes, connections check through `authoring.Credentials` | every code of §4.4 with its fix; fixtures updated | config and authoring tests |
| 4-11 | **Runs get their connections**: resolve, render, env per exec, stdio and remote MCP in the worker (direct from the box, headers from env), `tools.allow/deny` in the worker, connections section in the prompt; delete `ports.Sidecars`, `gateway/sidecars.go`, docker sidecars; scrubbing | rows 6, 9; a run with a remote and a stdio MCP connection, neither through the gateway | engine, worker tests |
| 4-12 | **AWS STS**: `ports.Minter`, `awssts`, kinds `aws` and `bedrock` | row 7; AssumeRole and GetSessionToken paths | stub STS endpoint |
| 4-13 | **CodeHost per kind** (`github` changed, `gitlab` new) + merge of every pull request of the item (§4.7) | partial merge then retry; host mismatch refused | engine tests, httptest code hosts |
| 4-14 | **Cutover**: remove `GOAL_TASK_ENV`, `GOAL_GITHUB_*`, `env:` references, `domain.SecretPolicy`, the gateway's upstream forwarding (`upstream.go`, `policy.go`); `goal connection import-env`; README interim rule ended | §14 step 5; the goal factory runs on connections only | e2e, settings tests |
| 4-15 | **Web: Connections** (list, new, detail; Define-Basics runtime, Define-Connections, New-Connections; phone) | §10; boards of §3 | Playwright 375/768/1440 + axe, Vitest |
| 4-16 | **Knowledge core**: `00054`, `ports.Knowledge` + postgres + `knowledgetest`, `ports.Embedder` + `openaiembed`, `internal/knowledge` (extract, chunk, fuse, ingestion loop with leases), uploads in the bucket; compose PostgreSQL on pgvector | row 1, 8; leases survive a killed goald; keyword-only without an embedder | conformance, integration, unit |
| 4-17 | **Knowledge API, CLI, gateway tools** `knowledge__search`, `knowledge__read`, SSE `knowledge` | §8; row 11 | handler and gateway tests |
| 4-18 | **`gitdocs` reader** | incremental by blob id; deletion; globs; GitHub and GitLab hosts | integration against a local git server |
| 4-19 | **`confluence` reader** (kind `confluence`, `sync: confluence`) | versions, deletion, storage format to text | httptest fixtures |
| 4-20 | **Web: Knowledge** (sources, new source, documents, search; phone) | §10 | Playwright + axe |
| 4-21 | **Cloudflare target**: `internal/adapters/cloudflare`, `api/sandbox.yaml`, `deploy/cloudflare` control Worker (routing, goald and auth containers, sandbox API, run DOs with deadline alarm) and wrangler config | `sandboxtest` green against staging; row 10; an item to done on staging | Worker Vitest, staging run |
| 4-22 | **Terraform for the platform on Cloudflare** (R2, DNS, Secrets Store, route, Access for staging); delete the AWS Terraform; runbook (deploy, master key escrow and rotation, restore) | `terraform validate`/`plan` clean; nothing per org | `task check`, plan review |
| 4-23 | **Human touches: metrics and reasons** (§4.9, §4.10): `00052`, `FlowStats.Touches`, `OrgTouches`, `ItemTouches`, `Store.LabelTouch`, `data.planned` on decisions, `getTouchMetrics` (with the by-version breakdown), `getOrgTouchMetrics`, the reason endpoint, `touches` on `Item`, `touch_count` on cards, the board's and the overview's headline | §13's touch cases on fake and PostgreSQL; golden matrix rows | storetest, handler tests |
| 4-24 | **Human touches: screens and CLI**: Floor headline and Touches panel, factory tiles, Board header, cards, the Item's list with its reason select; `goal status`, `goal item` touches | §10 Touches; boards of §3 | Playwright 375/768/1440 + axe, CLI tests |
| 4-25 | **Docs as built**: architecture.md, AGENTS.md, worker README, threat rows checked against tests | every §11.2 row names a passing test | review |

Migration numbers rise with landing order (4-3 → `00050`, 4-8 → `00051`, 4-23 → `00052`, 4-9 → `00053`, then 4-16 → `00054`); a PR that lands out of order takes the next free number on main and fixes this text. 4-3, 4-4 and 4-8 can start in parallel after the prototypes; 4-21 needs 4-7; 4-23 needs only 2.2-E and can be built any time after the prototypes; the web PRs need their API PRs.

## 16. Cross-spec contracts

**Phase 4 provides**
- `ports.Sandboxes`, `Connections`, `MasterKey`, `Minter`, `Knowledge`, `Embedder`, `SourceReader`; `CodeHost` per kind; `ObjectStore` without versions.
- The gateway as Goal's MCP server for runs (Goal's tools and `GET /run`, task token, no connection proxied), where phase 5's eval tokens and later item tools plug in; connections' MCP servers are configured by the worker in the box.
- Permissions `connection:read|manage`, `knowledge:read|manage`; endpoints and events of §8; query keys of §10.
- `Knowledge.Search` with an explicit sources list, which phase 6's `search_knowledge` tool calls with every source the person may read and phase 5 reuses for the Learned source.
- The Cloudflare topology (§5.12) phase 6's OAuth endpoints route through.
- Human touches per item (§4.9) per definition version, `item_touch_labels` (§4.10), `getTouchMetrics`, `getOrgTouchMetrics`, `Item.touches`: phase 5's retro trigger and its "check after publish".
- For the canvas designer: the boards of §3.

**Phase 4 needs**
- Phase 3: `api/permissions.yaml`, the fresh-session rule, the principal in `ctx`, audit in the same transaction, `goal_org_events`, the re-auth dialog.
- 2.2: B's `authoring.Credentials` extension point, the definitions cache by version, E's run events and lease spans, D's builder sections.

## 17. Open questions

1. **PostgreSQL provider for Cloudflare** (PlanetScale Postgres or Neon): decided by the owner after prototype 4-2, which needs the owner's accounts.
2. **Default embedding model** (Workers AI `bge-m3` or `qwen3-embedding-0.6b`, 1024 dimensions): decided in 4-16 by a recall check on the goal factory's docs; both fit the schema. **Decided: `@cf/baai/bge-m3`.** The check: this repository's 26 Markdown files (839 chunks), 30 paraphrased questions each with the files that answer it, through the real pipeline and the one search query on PostgreSQL with pgvector, document-level ranks. bge-m3 hybrid: hit@1 0.77, hit@5 1.00, MRR 0.868 (vector alone 0.854); qwen3-embedding-0.6b hybrid: 0.67, 1.00, 0.814 (vector alone 0.672, without the query instruction it is trained with); keywords alone: 0.57, 0.87, 0.726. bge-m3 also embedded the corpus in 26 s against 2 min 10 s. `GOAL_EMBED_MODEL` names another.

Unscheduled, not blocking: per-organization quotas (concurrent runs, knowledge bytes and embedding tokens), PDF and Office extraction in a box, per-factory restriction of a connection, approvals, OAuth connections, the GitHub App, a read-only tool cache.
