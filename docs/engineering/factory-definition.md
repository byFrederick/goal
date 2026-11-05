# Factory definition reference

A factory definition is a file tree with `factory.yaml` at its root ([ADR 0005](../adr/0005-factory-definitions-in-object-storage.md)): `agents/` (prompts), `skills/`, `rules/`, `contracts/`, `environments/` and, optionally, `memory.yaml`. `contracts/definitions/factory.schema.json` is the reference for every key of `factory.yaml`, `contracts/definitions/environment.schema.json` for environment files ([below](#environment-files)) and `contracts/definitions/memory.schema.json` for the factory memory ([below](#factory-memory-memoryyaml)). `internal/config` loads a tree and validates it (`config.Check`): every file parses, `factory.yaml` satisfies the schema, and every name and path it uses points at something that exists. `goal factory validate` (and `goal validate`, its offline form), the builder and publish all run the same checks. This page covers what the schema alone cannot explain.

## Rules

Factory rules ([ADR 0014](../adr/0014-factory-rules.md)) are guidance a factory owns, whatever repository it works on: delivery policy, secrets hygiene, evidence, tone, cost. A rule is always on for the agents it targets. A skill, by contrast, loads only when the model chooses it. Put what can be checked in a mechanism instead (a contract, a gate, a tool permission, branch protection), and keep rule text for judgment that cannot be checked.

`spec.rules` declares the rules, in the order they apply, the way `spec.skills` declares skills:

```yaml
spec:
  rules:
    - { name: secrets, source: ./rules/secrets.md }
    - { name: go-errors, source: ./rules/go-errors.md }
```

- `name` is a slug, unique among the factory's rules.
- `source` is a markdown file in the definition's `rules/` folder (`^\./rules/.+\.md$`). It must exist and stay inside `rules/`.

A rule file is short markdown with YAML frontmatter. The body after the frontmatter is the rule's text:

```markdown
---
description: Wrap every returned error with what was being done.
appliesTo:
  agents: [engineer]
  stations: [build]
  lanes: [backend]
  workTypes: [feature, incident]
paths: ["**/*.go"]
---

Wrap with %w and say what failed.
```

| Key | Required | Meaning |
| --- | --- | --- |
| `description` | yes | One line saying what the rule is for. |
| `appliesTo` | no | Which runs get the rule. Optional lists: `agents` (names in `spec.agents`), `stations` (ids in `board.stations`), `lanes` (lane keys of any station's `route`) and `workTypes` (entries of `board.workTypes`). Omitted, the rule applies to every run of every agent of the factory. |
| `paths` | no | Globs, kept as Claude Code's path scoping: the rule loads only when the agent works on files that match. |

No other key is allowed. A misspelt one, such as `applies_to`, is an error, not a scope that is silently dropped.

**Matching.** A rule applies to a run when every `appliesTo` list it gives contains the run's value: its agent, its station, its item's lane and its item's work type. A rule that gives `lanes` does not apply to an item without a lane. The effective rules keep the order of `spec.rules`. `domain.EffectiveRules` is the one implementation of this matching.

**Validation.** Every problem with a rule is an error, so validation and publish refuse it. The error is located at the rule's entry in `factory.yaml` (the builder shows it in the Agents section, under `/agents/rules/<i>`), and its message names the rule and the value at fault:

| Code | When |
| --- | --- |
| `rule-not-found` | the `source` file does not exist |
| `rule-invalid` | the source leaves `rules/` or is not `.md`; the file has no frontmatter, or its frontmatter does not parse or has an unknown key; `description` is missing or longer than one line; an `appliesTo` list is empty; `paths` has an empty glob |
| `unknown-agent`, `unknown-station` | an `appliesTo.agents` or `appliesTo.stations` entry the definition does not declare (`rule secrets: appliesTo.agents: unknown agent "ghost"`) |
| `unknown-lane` | an `appliesTo.lanes` entry that no station's `route` has as a key. A definition with no routes declares no lanes. |
| `unknown-work-type` | an `appliesTo.workTypes` entry not in `board.workTypes` |
| `duplicate-name` | two rules with the same name |
| `schema-*` | an entry that does not match the schema, such as an unknown key or a `source` outside `./rules/` |

Rule files are content-addressed blobs of the definition version like every other file of it, so a run keeps the rules of the version it pinned. Delivering the effective rules to a run's box, and recording them in its events, is ADR 0014 §4.

### Review contract pattern

A review station's output contract may carry an optional `violations` array, one entry per factory rule the reviewed work broke ([ADR 0014](../adr/0014-factory-rules.md) §6):

```json
{
  "decision": "reject",
  "reason": "see the violations",
  "violations": [
    { "rule": "go-errors", "reason": "the store error is returned bare" }
  ]
}
```

- `rule` is the `name` of a rule in `spec.rules`; `reason` is one line saying how it was broken.
- The key is a convention, not a contract goald ships: the definition's own contract declares it (an array of objects with `rule` and `reason` strings). goald reads `violations` from any accepted station output that has it, whatever the station or contract.
- When the output is accepted (it passed its contract and checks, whether its gate approves or rejects), goald appends one `rule_violated` item event per entry, with data `{rule, reason, station, agent}`, in the same transaction that accepts the output, so a retry or a lost lease never counts it twice. Counting those events by `rule` shows which rules break most.
- An entry naming a rule the run's definition version does not declare, or no rule, is ignored and logged; the others of the same output still count. A `violations` value that is not a list records nothing and never blocks the item.

## Output contracts

A station's output contract is the definition's own JSON Schema; goald validates each output against it and reads a few fields from an accepted one: the gate's verdict, `violations` (above), `lane` and `plan` ([ADR 0012](../adr/0012-assemblies-and-parts.md)), and the dependency and follow-ups fields below.

### Dependencies: `board.flow.dependencies.after`

`board.flow.dependencies.after` names the output property, any name the definition chooses, that lists the open items the work must follow ([ADR 0015](../adr/0015-overlapping-in-flight-work.md) §2). No property is fixed by the platform; with the key unset, goald reads none and nothing below happens.

```yaml
board:
  flow:
    dependencies:
      after: follows
```

```json
{ "follows": ["acme#93"] }
```

- The value is an array of item refs `<factory>#<number>` (the factory's `metadata.name` or its id): open items of the same factory, outside the output's own `plan`. Absent, `null` or `[]` means none; a ref listed twice counts once.
- When a station finishes with a non-empty list, goald adds a `linked` edge by the agent (`agent:<name>`) to each item. For a single item the edge goes from the item, before it moves on; when the output also has a plan that becomes an assembly, it goes from every part with no in-plan `needs`, in the same transaction that creates the parts. The claim gate then holds the item, or those parts, until each named item is done or cancelled. A gate's rejection reads no list.
- The output fails, as a malformed plan does (`contract_failed` naming the ref, one more attempt with the reason, then blocked), and no edge is written, when an entry is not an item ref, or names an item of another factory, an item that does not exist, one that is done or cancelled, the item itself or its own assembly, or an item that already waits for this one (the edge would close a cycle).

**Planning-station convention.** When a request overlaps work in flight, a planning station does one of two things: name that item in the mapped `after` field, so the new work follows it, or declare the overlapping paths in `touches` (a single item) or in the plan's `paths` (each part), so the overlap hold serializes them. A factory's prompt for its planning agent should say so; goald enforces neither.

### Follow-ups: `board.flow.outputs.followUps` and `board.flow.followUpsFrom`

`board.flow.outputs.followUps` names the output property that holds follow-ups: work an agent noticed outside its item ([ADR 0040](../adr/0040-one-door-for-every-source.md), decisions 5 and 6). Any station's contract may have it. With the key unset, goald reads no follow-ups, and they stay text in the item's output.

```yaml
board:
  flow:
    outputs:
      followUps: followups
    followUpsFrom: [00000000-0000-4000-8000-000000000020]
```

```json
{ "followups": [
  "Add an index on agent_runs.item_id",
  { "text": "The API needs this field too", "for": "acme#647" },
  { "text": "Remove the old column", "start": { "in": "7d", "title": "Drop work_items.source" } }
] }
```

- **Kept from every accepted run.** goald writes each accepted output's follow-ups to `follow_ups` in the transaction that accepts the output, with the run, station and agent that wrote them. A rerun of the station keeps the earlier ones. An output that fails its checks keeps none.
- **Once per item.** A follow-up whose text matches one already kept for the item, after trimming, folding case and collapsing whitespace, is kept once.
- **The first 10.** goald reads the first 10 entries of each output and ignores the rest. It reads them leniently, so an entry never fails the run.
- **Entry shape.** An entry is a string or an object:
  - A string is text only, for triage.
  - An object has `text` (required) and optionally `for` or `start`. `for` is an open item `<factory>#<number>` that gets the follow-up as a note. `start` is `{at, in, after}` as an item's start, with `title` (required) and optionally `type`, `lane`, `repositories` and `factory`: the follow-up becomes an item of its own. With both, `start` wins. With neither, it goes to triage.
  - A `for` or `start` that does not parse, or a `for` whose factory is longer than 100 characters, sends the follow-up to triage, with the reason.
  - Any other entry, such as a number or an object without text, is kept as dropped, with the reason.
- **Where it goes.** Each kept follow-up is `waiting` with its target (a note, a start or triage), `delivered`, `filed` with its item, or `dropped` with its reason. Notes are delivered and starts filed on every tick; the sections below say how, and the [follow-ups guide](../../apps/docs/src/content/docs/guides/follow-ups.md) says what a person sees. A start is filed when its item lands (`done`, or for an assembly the assembly), through `domain.File` with origin `follow-up`, and `start.in` counts from that moment; a start that cannot be filed, and a follow-up for triage, join the factory's collecting triage item.
- **Notes delivered.** On every tick, each goald instance delivers the waiting notes of every organization with loaded lines ([ADR 0040](../adr/0040-one-door-for-every-source.md) decision 7; `Store.DeliverNotes`). A note becomes an `item_note` event on the item its `for` names, by the engine (actor `system:engine`), with data `text`, `follow_up_id`, `from_item` (the source's `<factory>#<number>`), `from_item_id`, `from_factory_id`, `station` and `agent`, and the follow-up becomes `delivered`. The item's next run reads it after its brief, under "Notes from other items", framed as another agent's words. The gateway does not stream it into a run already working, and it is not a person's touch ([ADR 0039](../adr/0039-jonah-learns-from-the-line.md) decision 7). A `for`'s factory is matched by `metadata.name` or `metadata.id` against the organization's loaded lines, paused or stopped ones too.
- **Notes to triage.** A note that cannot go where it says becomes a triage follow-up, still `waiting`, whose reason starts with the ref it named (`for acme#12: …`, `domain.RouteNote`): no factory by that name in the organization, a factory that does not list the sender in `followUpsFrom`, no item with that number, an item that is done or cancelled, or a factory that already took 50 notes from other factories in the last 24 hours. Notes within the sender's factory need no `followUpsFrom` and do not count toward the 50.
- **Untrusted sources.** A note whose source item is untrusted (filed from a webhook delivery) is never delivered on its own; it stays `waiting` ([ADR 0040](../adr/0040-one-door-for-every-source.md) decision 21).
- **Plain-array contract.** A station contract that declares the mapped property must declare it as a plain array of strings or objects: no `$ref`, no `maxItems`, and no `required` on its items. Load refuses any other declaration (`follow-ups-not-plain-array`), because a strict shape could fail a run over a note.

`board.flow.followUpsFrom` lists the factories, by `metadata.id`, whose follow-ups this factory accepts. It is empty by default, so only the factory's own follow-ups reach it; a note from any other factory goes to triage.

### Closing an item: `board.flow.outputs.close`

`board.flow.outputs.close` names the output property whose text closes the item when nothing is left to do ([ADR 0040](../adr/0040-one-door-for-every-source.md) decision 11). With the key unset, no output closes its item.

```yaml
board:
  flow:
    outputs:
      close: close
```

```json
{ "close": "Nothing to do: the index this asks for already exists." }
```

- **Which items.** Only an item whose origin is `trigger` or `follow-up`, such as a triage item, can be closed. A person's item and a part never can.
- **The text.** goald trims it. Empty or only whitespace closes nothing, and the item moves on as usual. Otherwise it must be 10 to 500 characters.
- **Refusals.** The output fails its check, and goes back to the agent with the reason like any other bad output, when the item is a person's or a part, when the same output also has a `plan` on a line that materializes plans, or when the text is outside the limits or not a string. The reason tells the agent to ask a person instead ([ADR 0018](../adr/0018-agents-ask-questions.md)). The check runs before the plan is assembled, so an item never becomes an assembly and is closed in the same step.
- **The outcome.** The transition that accepts the output cancels the item at its station: state `cancelled`, lease outcome `advanced`. Its `cancelled` event has the agent as actor and data `reason` (the text), `station`, `agent` and `closed: true`, and the audit log gets `item.cancelled` by the agent. This is not a person's Cancel, which takes a running run's lease. A gate that rejects ignores the close.
- **Follow-ups of a closed triage item** keep File, as those of any cancelled triage item do, so a person can take any of them back.

## Station context

A station's optional `context` list names what goald preloads into the `GOAL_FEEDBACK` of every run at the station. Its only value is `inflight` ([ADR 0015](../adr/0015-overlapping-in-flight-work.md) decision 1); a station without `context` gets nothing preloaded.

```yaml
- id: plan
  kind: agent
  agent: planner
  context: [inflight]
```

With `inflight`, the run's feedback gets an `## In flight` section, read once per claim, after the `## Assembly` section of a part:

- It lists the factory's open items and assemblies (neither done nor cancelled), leaving out the item itself, its assembly and its assembly's other parts. Each line gives the ref (`<factory>#<number>`), title, state, station and paths: the item's `work_items.paths`, or for an assembly the union of its open parts' part paths (its done parts are already on main).
- It holds at most 30 entries, ranked by how many of an entry's globs have a literal prefix (up to the first `*`, `?`, `[` or `{`) that appears in the item's title or body, highest first, then by most recently updated. Entries without paths come last. The section says how many of the open entries it shows.
- It is ranked from the factory's 500 newest open items; older ones are not listed.
- It states what it lists and nothing else: how agents use it (naming an overlapping item in `after`, declaring overlapping paths) belongs in the factory's prompts and rules.
- A store error while reading the open items blocks the item; it never runs without the section.

### Changed since your plan

With `board.flow.assemblies.changedSince: true` (default false), plans bind late ([ADR 0015](../adr/0015-overlapping-in-flight-work.md) decision 5):

- When a station with `context: [inflight]` finishes an item, goald records the head commit of the default branch of `runtime.repository` as the item's base commit, through the line's code host connection for the repository's host. A plan's parts inherit their assembly's. Nothing is recorded without a repository or such a connection.
- Every later run of an item with a base commit and paths, a rework after a merge conflict included, gets a `## Changed since your plan` section in its feedback when the default branch has commits since then that changed files the item's paths match. Each line names the item that merged the commit (`<factory>#<number>` and title, matched by its `merged` event's sha) or else the commit's short sha and subject, and lists the matching files (at most 20). Commits that change none of the item's files add nothing; a comparison over 100 commits says it was truncated.
- It states what it lists and nothing else: telling agents to re-derive the brief's paths against current main belongs in the factory's prompts and rules.
- A code-host error is logged: the item finishes without a base commit, or runs without the section.

## Assembly integration branches

`board.flow.assemblies.integration` makes an assembly's parts merge into one integration branch, `goal/a<root number>-<root title slug>`, that lands on the default branch at a gate ([ADR 0033](../adr/0033-assembly-integration-branches.md)). Every key is optional:

| Key | Values | Default | Meaning |
| --- | --- | --- | --- |
| `mode` | `trunk`, `branch` | `trunk` | `trunk`: each part merges into the default branch. `branch`: the parts merge into the assembly's integration branch. |
| `refresh` | `onLand`, `eachPart` | `onLand` | When the integration branch takes in the default branch: when it lands, or before each part. |
| `minParts` | integer, 0 or more | `0` | An assembly with fewer parts stays on trunk. |
| `landAt` | a station id | the first station with `action: merge` after the station whose plan made the assembly | Where the integration branch lands. |

A definition without the block is trunk, `onLand`, 0 and no `landAt`. The checks report:

- `unknown-station` when `landAt` names no station;
- `integration-land-at-not-merge` when `landAt` names a station that is not an agent station with a `gate` and `action: merge`;
- `integration-without-merge-station` when `mode` is `branch` and no station has `action: merge`.

Nothing reads the settings yet: the store, the engine and the worker follow in the parts of the assembly ADR 0033 describes.

## Repositories: `runtime.repositories` and an agent's code

A factory's code is a list of repositories, each with its access ([ADR 0041](../adr/0041-code-and-github-without-an-app.md), decisions 2 and 3). No repositories is a factory with no code.

```yaml
spec:
  runtime:
    repositories:
      - name: app                          # a slug, unique in the factory
        url: https://github.com/acme/app   # one repository, or https://github.com/acme/* for an owner's every repository
        token: "{{github-acme}}"           # required for write
        access: write                      # read or write
        default: true                      # optional
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
      code: write                          # or read
```

- **Entries** have no `forge` and no `base`: the organization's code hosts give the kind, and runs use the host's default branch.
- **The default** is the entry an item uses when it names none. At most one entry has `default: true`, and it must name one repository. The only entry is the default when it names one repository. An owner pattern alone leaves the factory with no default.
- **The shorthand** `runtime.repository` reads as one `write` entry that is the default (`Runtime.RepositoryEntries`, `internal/config/repositories.go`). Its name is the URL's last segment in lower case without `.git`, or `repo` when that isn't a slug, so `https://github.com/acme/web-app.git` is `web-app` and `https://github.com/acme/ui` is `repo`. The shorthand keeps its `forge` and an optional token, so every published version loads.
- **An agent's `code`** is `write` or `read`. Without it, an agent writes when the factory has a `write` entry and reads otherwise (`Factory.AgentCode`), so definitions written before `code` keep writing.
- **Which token:** for each repository, the most specific entry that covers it wins, one repository before an owner pattern. An agent that only reads takes a `read` entry's token whenever one covers the repository.
- **The line** keeps one `domain.Repository`, taken from the default entry (`Factory.Line`). Run start, merges and base commits use it until runs start with an item's repositories.

`config.Check` reports these under the check `repositories`. Publish refuses the errors; `Load` skips the check, so a version published before it keeps running.

| Code | Severity | When |
| --- | --- | --- |
| `repository-forms-both` | error | the definition has both `runtime.repository` and `runtime.repositories` |
| `duplicate-name` | error | two entries have the same `name` (check `references`) |
| `repository-url-invalid` | error | an entry's `url` is neither one repository (`https://<host>/<owner>/<name>`) nor an owner pattern ending in `/*` |
| `repository-token-missing` | error | an entry with `access: write` has no `token` |
| `repository-default-many` | error | a second entry has `default: true` |
| `repository-default-pattern` | error | an owner pattern has `default: true` |
| `merge-needs-write` | error | a station has `action: merge`, or `board.flow.assemblies.integration.mode` is `branch`, and the factory has no `write` entry or no agent whose code is `write` |
| `reader-write-token` | warning | an agent that only reads would get a `write` entry's token, because no `read` entry covers it: "reviewer only reads, but the only token for services can write." |
| `repository-not-supported` | error | a second entry, `access: read` or `code: read`. Runs start and merge in one repository that writes until Goal supports several, and this refusal (`checker.notSupportedYet`) goes then. It also means `reader-write-token` can't show on a version that publishes yet. |

Publish also refuses a repository on a host the organization doesn't list (`repository-host-unlisted`, in the secrets check below).

## Environment files

An environment file, `environments/<name>.yaml`, says what an agent's box installs and what it can reach ([ADR 0037](../adr/0037-secrets-and-environment-files.md), decisions 5 to 8). Every factory has `environments/default.yaml`. An agent names its file with `environment: <name>`, and an agent without one gets `default`. `config.Check` loads and checks the files (`internal/config/environment.go`), and the published version keeps every key on `domain.Environment`.

**At run start** the engine resolves the agent's file from the pinned version (`internal/core/engine/environment.go`, decision 10):

- It opens every `{{secret}}` of `env`, `files`, `mcp.<server>.headers`, a stdio server's `env` and `runtime.modelKey` together, once, through the secrets store, which checks each secret's scope against the factory and writes one `secret.used` per secret. A missing or out-of-scope secret fails the run with a reason that names the secret and the environment and never a value. Every opened value is scrubbed from what the run reports.
- The agent's box gets `env` with the values filled in, each `files` entry's content in a platform-reserved variable `GOAL_FILE_<n>` (numbered from 1 by sorted path, `domain.FileEnv`), and the value of each secret an MCP header or stdio `env` names in `GOAL_SECRET_<NAME>` (the name upper-cased, every other character `_`, `domain.SecretEnv`). The resolved MCP servers name `${GOAL_SECRET_<NAME>}` in place of the reference, so they hold no value.
- The box takes the file's `size` (medium when omitted) and its `image` (Goal's agent image when omitted). The Docker and Cloudflare sandboxes fail the run with an error that names an image they cannot run. A box on Cloudflare runs only the control Worker's own images, so publishing warns at `spec.image` of each file that sets one (`environment-image`, a warning that does not block).
- The organization policy's Use credentials `off` drops `env`, `files` and `mcp`; the model key and the repository still reach the run.

**In the run bundle** the gateway describes the agent's file, never a value (`internal/gateway/bundle.go`, decisions 5, 8 and 11; the fields are in [the worker's README](../../apps/worker/README.md#specjson)). The bundle's `tools` are the file's `tools`, and `spec.environment` carries the MCP servers with their secrets as `${GOAL_SECRET_<NAME>}`, `setup` in order, each file path with its `GOAL_FILE_<n>`, and the names of `env` and of the tools. A version on secrets gets no connection in the bundle. Each server's tool modes come from `domain.ServerToolModes`, as a list the worker matches first to last:

- The file's `tools` patterns, most specific first: an exact name, then globs with more characters other than `*`, a tie going to the pattern that sorts first.
- An active standing rule for the factory, environment, server, tool and agent role lifts a tool that resolves to `ask` to `allow`, as its own exact entry. Nothing lifts `off`.
- The organization policy's cap (`domain.OrgPolicy.MCPToolModes`, a tool name or glob to `allow`, `ask` or `off`, empty for none) makes a mode stricter, never looser: the gateway reads the policy once per bundle and each entry takes the stricter of its mode and the cap's, after any standing rule, so a rule never lifts an `ask` the cap keeps (decisions 8 and 12).

**At publish** the policy check reports each `mcp.<server>.tools` entry the cap makes stricter (`config.MCPCapProblems`, called from authoring's `ownChecks` next to `ModelProblems`): a warning, `mcp-tool-mode-capped`, at that entry, naming the server, the tool, the file's mode and the effective one. An exact tool name in the cap that one of the file's globs covers is reported at that glob. It does not block publishing, since the cap applies to every run anyway. A tool the file does not list gets no warning.

The task token does not carry the policy's Use credentials, so with it `off` the bundle still names the file's servers, files and variables, whose values the box does not get. `goal-agent run` applies `spec.environment` (`apps/worker/src/environment.ts`): it writes the files at every run and runs setup once per box, again after a failed setup and again in a new box ([the worker's README](../../apps/worker/README.md#specjson)).

```yaml
apiVersion: goal/v1
kind: Environment
metadata: {name: default}            # optional; it must be the file name when present
spec:
  size: medium                       # small | medium | large; medium when omitted
  image: ghcr.io/acme/box:2026.11    # optional; Goal's agent image when omitted
  tools: [go@1.26, node@22, gh@2]    # mise specs, after the repository's own mise.toml
  setup:                             # run in order after the clone and the tools
    - pnpm install --frozen-lockfile
  env:
    GH_TOKEN: "{{github-token}}"
    DATABASE_URL: "postgres://app:{{db-pass}}@db/app"
    SENTRY_ORG: acme
  files:                             # absolute paths or ~/; each value is a reference
    ~/.kube/config: "{{kube-staging}}"
  mcp:
    linear:                          # remote: url and optional headers
      url: https://mcp.linear.app/mcp
      headers: {Authorization: "Bearer {{linear-key}}"}
      tools: {create_comment: ask, "delete_*": off}
    playwright:                      # stdio: command and optional env
      command: [npx, "@playwright/mcp@0.0.41"]
```

- `env` holds plain values and `{{secret}}` references, as the whole value or part of it.
- `files` keys are absolute paths or start with `~/`, and each value is one reference.
- An `mcp` server's name is a slug: 3 to 40 lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit (`$defs.slug`), the name environment tests and standing rules take. A name like `play_wright` is a schema error at that server.
- An `mcp` server is remote (`url`, optional `headers`) or stdio (`command`, optional `env`), never both. `tools` maps a tool name or glob to `allow`, `ask` or `off`; the most specific match wins, and a tool nothing matches is `allow`.

### Secret references

`{{name}}` names one of the organization's secrets, and it is the only placeholder. It is valid in an environment file's `env`, `files`, `mcp.<server>.headers` and a stdio server's `env`, and in `runtime.repository.token`, a `runtime.repositories` entry's `token`, `runtime.modelKey` and a trigger's `only.modelAgrees.key`; the last four take one whole reference. Each counts as a use of the secret (`Factory.SecretRefs`), so the secret can't be deleted while a current version uses it, and Access lists it. A trigger's `create.title` and `create.brief` keep their `{{field}}` event placeholders. A literal `{{` is not supported.

`runtime.repository` is `{url, token}`, the shorthand described in [Repositories](#repositories-runtimerepositories-and-an-agents-code). A URL on `github.com` or `gitlab.com` picks its code host; any other host needs `forge: github` or `forge: gitlab`.

### Environment checks

`config.Check` reports these as errors, each in the file and at the line of its key:

| Code | Check | When |
| --- | --- | --- |
| `environment-default-missing` | environments | `environments/default.yaml` does not exist |
| `environment-unknown` | environments | an agent names an environment that has no file |
| `environment-name-invalid` | environments | a file name is not a slug |
| `environment-name-mismatch` | environments | `metadata.name` is not the file name |
| `tool-spec-invalid` | environments | a `tools` entry is not a mise tool spec; the fix removes it |
| `secret-placement` | environments | a `{{...}}` is anywhere but the places above |
| `secret-ref-invalid` | environments | a reference does not name a secret, a `{{` is unclosed, or `modelKey` or `modelAgrees.key` is not one whole reference |
| `forge-missing` | environments | a repository on another host names no `forge`; the fixes set it |
| schema codes | schema | a file does not satisfy `environment.schema.json` |

Publish then runs the secrets check (`authoring.SecretsCheck`, `internal/authoring/secrets.go`) against the organization's secrets, reading names and scopes, never a value:

| Code | Severity | When |
| --- | --- | --- |
| `secret-missing` | error | the organization has no secret of a referenced name |
| `secret-not-in-scope` | error | the secret's factory scope leaves the factory out |
| `reserved-env` | error | an `env` name is one the platform keeps (`domain.ReservedRunEnv`: `GOAL_*`, `HOME`, `PATH`, the proxy variables, `LD_*`, `MISE_*`…) |
| `plain-key` | error | a plain `env` value starts with `sk-`, `ghp_`, `github_pat_`, `glpat-`, `xoxb-` or `xoxp-` |
| `plain-key` | warning | a plain `env` value's name ends with `_TOKEN`, `_KEY`, `_SECRET` or `_PASSWORD` |
| `knowledge-source-unknown`, `knowledge-secret-not-in-scope`, `knowledge-copies-checkout` | see [Knowledge](#knowledge-knowledgesources) | a source in `knowledge.sources` |
| `merge-needs-code-host` | error | a station has `action: merge` and no repository that writes has a `token` |
| `repository-host-unlisted` | error | a `runtime.repository` or `runtime.repositories` URL's host, with its port, is neither built in (`github.com`, `gitlab.com`) nor one of the organization's code hosts (`SecretsCheck.Hosts`) |
| `github-tools-not-read-only` | warning | an agent whose code is `read`, or a trigger check's agent, has an environment with a GitHub MCP server (`config.MCPServer.GitHub`: a remote server on `api.githubcopilot.com`, or a stdio command `github-mcp-server`) whose latest passing environment test lists a tool without `readOnlyHint` that the server's `tools` resolve to `allow` (`domain.ResolveToolMode`); located at `spec.agents[i].code` or the trigger's `check.agent`, naming up to 5 tools (`SecretsCheck.Tests`) |
| `github-tools-unknown` | warning | such a GitHub server has no kept environment test, or its latest one failed |

`plain-key` carries the fix "Move to Secrets", which replaces the value with a reference named after the variable in kebab case (`GH_TOKEN` becomes `{{gh-token}}`). A problem in an environment file has the section `environments` and the pointer `/environments/<name>/<keys below spec>`; the Form view does not show that section yet.

### Keys moved to environment files

These keys are errors under the check `moved` (code `moved-to-environment`). Each message says where the key went, and each fix removes it, but the one under a trigger, which is edited in YAML only:

| Key | Moved to |
| --- | --- |
| `spec.connections` | `env`, `files` and `mcp` in an environment file |
| `spec.agents[].connections` | the agent's `environment` |
| `runtime.modelConnection` | `runtime.modelKey` |
| `runtime.size`, `runtime.tools` | `size` and `tools` in each environment file |
| `runtime.repository.connection` | `runtime.repository.token` |
| `board.triggers[].only.modelAgrees.connection` | `only.modelAgrees.key` |

A draft that has one does not publish. A version published before ADR 0037 has them and no `environments/` folder: `config.Load` skips the `moved` check, and the environment checks for such a version, so it loads and its line keeps its connections, model connection, size and tools. The engine runs it as before until Goal converts it.

## Knowledge: `knowledge.sources`

`knowledge.sources` lists the organization's knowledge sources the factory's runs read, at most 10 ([ADR 0038](../adr/0038-knowledge-is-files.md), decisions 4 and 5). An entry is a source's name, or an object that also says which runs read it and when:

```yaml
spec:
  knowledge:
    sources:
      - handbook
      - name: runbooks
        appliesTo: { stations: [investigate, fix] }
        readWhen: Before you touch production, find the runbook for the alert.
```

| Key | Required | Meaning |
| --- | --- | --- |
| `name` | yes | The source, a slug. |
| `appliesTo` | no | The runs that read the source, with the same lists and the same matching as a [rule's `appliesTo`](#rules): `agents`, `stations`, `lanes` and `workTypes`. Omitted, every run of the factory reads it. A list may not be empty. |
| `readWhen` | no | One line of at most 160 characters. It replaces the source's own read-when line in this factory's prompt. |

`config.Knowledge.Names()` gives the names in order; `config.Check` keeps each entry as `config.KnowledgeSource`. Run start does not read `appliesTo` or `readWhen` yet.

**Checks.** `config.Check` reports the first three rows. The secrets check (`authoring.SecretsCheck`) reports the rest at publish and in validation, against the organization's source rows:

| Code | Severity | When |
| --- | --- | --- |
| `knowledge-too-many` | error | `knowledge.sources` lists more than 10 sources |
| `knowledge-source-duplicate` | error | `knowledge.sources` lists a source more than once, as a name or as an object, which would have a box unpack it twice; located at the second entry. Remove that entry. |
| `unknown-agent`, `unknown-station`, `unknown-lane`, `unknown-work-type` | error | an `appliesTo` entry the definition does not declare, located at that entry (`knowledge source runbooks: appliesTo.stations: unknown station "ghost"`) |
| `knowledge-source-unknown` | error | the organization has no source of the name; the fix removes the entry |
| `knowledge-secret-not-in-scope` | error | the source reads with a secret whose factory scope leaves the factory out, so listing it would hand the factory that secret's reach |
| `knowledge-copies-checkout` | warning | the source is a Repository source, or a synced source read by gitdocs, of the repository in `runtime.repository.url`: the same host and path, ignoring case, a trailing slash and `.git`. Its runs already have those files on the item's branch. The warning does not block validation or publishing. |

For example, a factory whose `runtime.repository.url` is `https://github.com/acme/app` and that lists a Repository source of `https://github.com/acme/app.git` validates and publishes with the `knowledge-copies-checkout` warning.

## Factory memory: `memory.yaml`

`memory.yaml`, at the root of the definition beside `factory.yaml`, is the factory memory: short facts the factory's runs should know ([ADR 0038](../adr/0038-knowledge-is-files.md), decision 6). Rules say how to act, skills how to do a task, and the factory memory what is true here. Facts for the whole organization belong in a knowledge source every factory lists. The file is optional; without it the factory has no memory.

```yaml
- text: staging-db disk alerts below 85% are noise; the nightly vacuum clears them.
  from: [acme-sre#225, acme-sre#231]
  added: 2026-11-01
```

| Key | Required | Meaning |
| --- | --- | --- |
| `text` | yes | One fact, at most 300 characters. |
| `from` | no | The items the fact came from, as `<factory>#<number>`. |
| `added` | yes | The date the fact was added, `YYYY-MM-DD`. |

`config.Check` loads the file into `Factory.Memory()` (`internal/config/memory.go`) and reports these as errors in `memory.yaml`, in the Files section:

| Code | When |
| --- | --- |
| schema codes | the file does not satisfy `memory.schema.json`: it is not a list, an entry has an unknown key or lacks `text` or `added`, `added` is not a date or a `from` entry is not an item reference |
| `memory-too-long` | an entry's `text` is over 300 characters, located at that entry |
| `memory-too-big` | the file is over 8 KB (8,192 bytes) or holds over 50 entries |

The limits are `domain.MaxMemoryText`, `domain.MaxMemoryBytes` and `domain.MaxMemoryEntries`. Runs do not receive the factory memory yet.

## Triggers: `board.triggers`

A trigger turns an event, a schedule or a webhook delivery into a work item ([ADR 0026](../adr/0026-intake-triggers.md)). Triggers are part of `factory.yaml`, so saving one publishes a new version. `internal/config` parses and checks them (`config.Trigger`, `internal/config/triggers.go`), and the engine matches events, runs the checks and fires schedules ([architecture.md](architecture.md), "Intake triggers in the engine"). The disabled triggers are the definition's suggestions, which Intake and the onboarding Intake step offer to turn on ([ADR 0040](../adr/0040-one-door-for-every-source.md) decision 17). `board.intake` is deprecated and ignored: the schema still accepts it so existing definitions load, nothing reads it, and it gets the warning `field-ignored` ([Ignored keys](#ignored-keys)).

```yaml
board:
  workTypes: [feature, fix]
  triggers:
    - name: morning-sweep
      on: schedule
      cron: "0 7 * * MON-FRI"
      timeZone: Europe/Berlin
      create: { type: fix, station: build, lane: backend, title: "Sweep flaky tests" }
    - name: red-main
      on: github.check_run.failed
      filter: { branch: main, repo: acme/app, skipBots: true }
      only: { failuresInARow: 2, dedupe: check.name }
      create: { type: fix, station: build, expedite: true, title: "Fix {{check.name}} on main" }
      limits: { perDay: 3, pauseOnBudget: true }
    - name: deploy-failed
      on: webhook.deploys.failed
      create: { type: fix, station: inbox, brief: "{{service}} failed to deploy to example.com" }
```

The keys of a trigger:

- `name` (required): a slug, unique within the factory. It names the trigger in the API, the UI and the firing log.
- `enabled`: default `true`. A disabled trigger matches nothing.
- `on` (required): what the trigger fires on. `schedule` fires on its `cron`. `<sender>.<event>`, such as `github.check_run`, takes that event from every webhook of the sender that is open to the factory, which suits a template that cannot know an organization's webhook names. `<webhook>.<event>`, such as `github-acme.check_run`, takes that event from one organization webhook, and `<webhook>` alone takes all of its events. A sender alone (`github`) is refused (`on-needs-event`). The senders are `github`, `gitlab`, `standard`, `pagerduty`, `sentry` and `token`; their events are in the sender catalog (`internal/catalog/senders`). `webhook.<name>[.<event>]`, `github.check_run.failed` and `github.issues.labeled`, as written before, still load and read as `<name>[.<event>]`, `github.check_run` with `conclusion: failure` and `github.issues` with `action: labeled`. The event is matched against the delivery's event name.
- `cron` and `timeZone`: only with `on: schedule`, where `cron` is required. `cron` is five fields: minute, hour, day of month, month, day of week. A field takes numbers, `*`, ranges (`1-5`), steps (`*/15`, `0-30/10`), lists (`0,30`) and, for months and weekdays, names in any case (`JAN`, `mon`). Weekdays are 0 to 6, Sunday first. `@daily`, `?` and seconds are not accepted. `timeZone` is an IANA name and defaults to `UTC`; `Local` is refused.
- `filter`: filters on the event's normalized fields, all of which must match: `branch`, `repo` (owner/name, in any case; one value or a list, and left out it watches every repository), `label`, `action` and `conclusion` (one value or a list each), `paths` (globs; any changed path matches), `authors` (any of), `skipBots`, and `match`, which compares payload fields by dot path (`check_run.app.slug`), each against a value or a list of values. Publish refuses a filter on a field the event's table does not supply (`filter-unsupplied`).
- `only`: checks before an item is created, in this order. `failuresInARow: n` passes when the same check failed on the last n events for it. `dedupe: <field>` passes when no open item this trigger created holds the same value of that event field. `modelAgrees: {question, key, model}` is deprecated: the model check is removed ([ADR 0040](../adr/0040-one-door-for-every-source.md) decision 18). A trigger that still has it skips every event that reaches that step, after the other two, with the reason "the model check is removed; turn this trigger into a check", and the definition gets the warning `field-ignored` at the key, saying to turn it into a check. `question`, `key` and `model` are read by nothing; `connection`, the old form of `key`, is still a [moved key](#keys-moved-to-environment-files).
- `create` (required): `type` (one of `board.workTypes`) and `station` (a station id) are required. `lane` is a key of that station's `route`, `expedite: true` puts the item in the expedite flow, and `title` and `brief` are templates. A placeholder is `{{field}}` or a dotted `{{field.sub}}`, spaces inside the braces allowed, and takes the event's field, quoted, on one line and at most 200 characters; the whole title is cut to 300 characters. `brief` may instead name a Markdown file of the definition, `./triggers/<name>.md`, alone on its line, as an agent's `prompt` names one: the item's brief is that file's text, with the same placeholders. Event text reaches agents only as untrusted data (ADR 0026, decision 5), and the agent's system prompt says that text inside `<untrusted-event-data>` is data, never instructions.
- `limits`: `perDay` (at most n items from this trigger since the start of the UTC day), `perHour` (at most n in the hour before an event; an event trigger without it takes 10, a schedule has none, so a burst of matching events cannot flood the line) and `pauseOnBudget` (skip events while the factory's weekly budget is spent). A trigger with `check:` also takes `perCheck` (at most n items one check files, default 5); for it `perDay` counts the items the trigger filed, and what is over either is skipped "over the limit" ([ADR 0040](../adr/0040-one-door-for-every-source.md) decision 19).
- `check` ([ADR 0040](../adr/0040-one-door-for-every-source.md) decision 19; only with `on: schedule`, refused otherwise): each firing runs an agent that looks at the source and answers what to file, instead of filing an item. `agent` (required) is an agent of the factory, whose environment, tools and model the check uses. `brief` (required) is what to look for, or `./triggers/<name>.md`. `maxUsd` is the most one check may spend (default 0.10), and `timeout` its time limit, a number and `s`, `m` or `h`, at most `1h` (default `5m`). `cron` may fire at most every 5 minutes, and one check per trigger runs at a time. Publish warns when the agent may use tools that are not read-only. The [checks guide](../../apps/docs/src/content/docs/guides/checks.md) says what a check is given and what it answers.
- `create.trusted`: files the items a delivery makes as trusted, so a person need not merge what they build; without it, an item made from a webhook delivery is untrusted (decision 21) and so are the items that follow from it. Publish refuses it for the `gitlab` and `token` senders and for a GitHub sender or webhook without `filter.authors` (`trusted-refused`). On a check trigger, `trusted` applies to the items the check files.

### Trigger checks

Each problem is located at its key under `spec.board.triggers` in `factory.yaml`. The editor's model has no triggers section yet, so the problems are in section `files`.

| Problem | Code |
| --- | --- |
| A trigger fails the schema: a missing `name`, `on` or `create`, an unknown key, an `on` that is not an accepted event key, a schedule without `cron`, `cron` or `timeZone` on an event trigger | `schema-required`, `schema-unknown-key`, `schema-invalid` |
| Two triggers share a name | `duplicate-name` |
| `create.type` is not in `board.workTypes` | `unknown-work-type` |
| `create.station` is not a station id | `unknown-station` |
| `create.lane` is not a key of that station's `route` | `unknown-lane` |
| A schedule's `cron` is not five valid fields | `cron-invalid` |
| A schedule's `timeZone` is not an IANA time zone the host knows | `time-zone-invalid` |
| A `{{` in `create.title` or `create.brief` is unclosed, empty or not a dotted field name | `placeholder-invalid` |
| `create.brief` names `./triggers/<name>.md` and the definition has no such file | `brief-not-found` |
| `only.modelAgrees` is set (a warning, never a refusal) | `field-ignored` |
| `limits.perCheck` on a trigger without `check:` | `per-check-without-check` |
| `on:` names a sender without an event (`github`, not `github.check_run`) | `on-needs-event` |
| A filter reads a field the sender's event table doesn't give that event, such as `filter.paths` on `github.pull_request`; publish checks the webhook form (`on: <webhook>.<event>`) against the webhook's sender | `filter-unsupplied` |
| `create.trusted` on the `gitlab` or `token` sender, or on a GitHub sender or webhook without `filter.authors` | `trusted-refused` |
| `on:` names a webhook that the organization doesn't have or that isn't open to the factory (publish only) | `webhook-not-open` |

The last four are the `deliveries` check (ADR 0040 decisions 14 and 21). Publish refuses them, and a version already live still loads with them, so its factory keeps running. `github.check_run.failed` and `github.issues.labeled` read as `github.check_run` with `conclusion: failure` and `github.issues` with `action: labeled`, and `webhook.<name>[.<event>]` as `<name>[.<event>]`. An event trigger without `limits.perHour` takes 10.

The time zone check reads the host's zoneinfo, so a process without tzdata refuses every zone but `UTC`.

## Starter item: `starter.item`

A template can carry one small, safe first item, at the root of `factory.yaml` next to `spec` ([ADR 0032](../adr/0032-self-serve-onboarding.md), decision 3). Onboarding's last step, "Watch your first item", submits it once, through `submitItem`, to a new factory that has no items yet; a factory with items gets none. The section is optional, and the engine does nothing with it.

```yaml
starter:
  item:
    title: Add a contributing guide to the README
    type: docs
    body: |
      Add a **Contributing** section to the README: how to set up the
      repository, run the tests and open a pull request.
```

- `item` (required in `starter`): the only key of the section.
- `title` (required): the item's title, 1 to 200 characters, as `submitItem` takes it.
- `type`: one of `board.workTypes`. When absent, the item gets the first work type, as `submitItem` gives it.
- `body`: the item's brief in markdown, at most 20000 characters.

The factory model (`FactoryModel`, returned by `getTemplate`, the draft and a version) carries it as `starter` with the same keys, absent when `factory.yaml` has none. It is not a Visual editor section and has no ETag: the editor leaves it as written, and YAML mode edits it.

### Starter checks

Each problem is located at its key under `starter.item` in `factory.yaml`. The editor's model has no starter section, so the problems are in section `files`.

| Problem | Code |
| --- | --- |
| The section fails the schema: no `item`, no `title`, an empty or too long `title`, a too long `body`, an unknown key | `schema-required`, `schema-invalid`, `schema-unknown-key` |
| `type` is not in `board.workTypes`; with no `board.workTypes`, every `type` is unknown | `unknown-work-type` |

## Jonah: `learning.jonah`

Jonah reviews a factory's line and turns what it sees there into proposals for the definition ([ADR 0039](../adr/0039-jonah-learns-from-the-line.md)). `learning.jonah` sets whether it runs, its model, its budget and when an observation becomes a proposal. Every key is optional, and a factory without `learning`, or without `jonah` in it, gets the defaults.

```yaml
spec:
  learning:
    jonah:
      enabled: true
      model: claude-sonnet-5-5
      share: 5%
      proposeAfter: 3
```

- `enabled` (boolean, default `true`): whether Jonah reviews the factory at all.
- `model` (a `claude-` model id, optional): the review run's model. When absent, it is `runtime.model`; when both are absent, the run takes the organization's default model, as an agent without a model does. The organization policy still checks the model when the run starts.
- `share` (a percent string from `0%` to `100%`, such as `5%` or `2.5%`, default `5%`): Jonah's budget, as a share of what the line's runs other than Jonah's cost over the last 7 days. Each review may spend at most a fifth of the budget, and its run's cost cap is what the budget has left, under that fifth. While Jonah's runs of the last 7 days cost the budget or more, no review is queued, a manual one is refused with `learning-budget-exhausted`, and publishes go on. A line that spent nothing in the last 7 days gives Jonah no budget, and `0%` turns its spend off.
- `proposeAfter` (integer, 1 to 50, default `3`): how many items with the same cause an observation needs, counted across reviews, before a daily review turns it into a proposal. A review a person asked for may propose below it. The daily review itself is queued once a day for a factory with new evidence, whatever this number.

Jonah's runs show in the factory's spend, but they never count toward `runtime.budget.weeklyUsd`, so they never trigger its `onExhausted`.

`config.Factory.Line()` carries the keys, with the defaults applied, as `domain.Line.Jonah`, a `domain.JonahPolicy`, with `share` as a fraction (`0.05`); `domain.DefaultJonahPolicy()` holds the defaults, and `JonahPolicy.Budget` computes the budget and the per-review cap. Like the rest of `learning`, the section is not part of the Visual editor: YAML mode and the Jonah section of factory settings edit it. A value out of range or an unknown key is a schema problem (`schema-invalid`, `schema-unknown-key`) at its line in `factory.yaml`, in section `files`.

### Ignored keys

`learning.jonah.weeklyUsd`, `learning.librarian`, `learning.kaizen`, `board.intake` and a trigger's `only.modelAgrees` do nothing. They stay in the schema, marked deprecated, so a definition that has them still loads, publishes and runs. Each one gets the warning `field-ignored` (check `ignored`) at its line in `factory.yaml`, in section `files`, saying to remove it, or, for `only.modelAgrees`, to turn the trigger into a check. They are not removed forms: unlike `field-removed`, a `field-ignored` key is not recorded in `domain.Line.Removed`, so the engine never refuses a version for it. `ignoredFields` in `internal/config/checks.go` takes the list of paths to warn about.

### `evals/`

A definition's `evals/` folder does nothing: no part of Goal reads it, and kept tests ([ADR 0039](../adr/0039-jonah-learns-from-the-line.md) decision 12) are rows in the database, not files. A definition that still has the folder loads, publishes and runs. It gets one warning, `evals-ignored`, in section `files`, saying to remove the folder. A Jonah proposal can never edit a file under `evals/`.

## Organization policy

The organization policy ([ADR 0024](../adr/0024-organization-policy.md)) is a ceiling over every factory definition. A definition or an agent can choose less, never more. Goal applies it in two places.

**When a version is validated or published.** Check `policy` (`config.ModelProblems`) compares `spec.runtime.model` and each agent's `model` with the policy's allowed models. A model outside them is an error, code `model-not-allowed`, located at that key; its message names the agent (or `runtime`) and the model. A definition that names no model passes. A definition that published before can fail its next publish once the policy narrows.

**When a run starts.** The engine reads the policy once per claim, before the first run:

- The run's model is the agent's `model`, else `spec.runtime.model`, else the policy's default model. An organization that never saved a policy has the first of `GOAL_MODELS` as its default. The run records that model.
- When that model is no longer allowed, no run starts and no box is created. The item waits for a person at its station (`waiting_human`), and the reason names the model.
- The policy's denied commands are added to the agent's `tools.deny` rules in the run bundle, whatever the version says. A denied command fails its tool call and shows on the item's timeline as a denied tool call (`tool_denied`).
- The policy's box time limit caps the run: each run's timeout is the smaller of goald's task timeout and the limit, and the box lives no longer than the limit.

The four tool defaults ("What agents may do without a person") narrow the agent through the mechanism that already decides each one. An agent that allows less keeps less:

- **Read the web and docs.** `off` or `ask` adds `WebFetch` and `WebSearch` (the `builtins.web` tools) to the agent's deny rules, whatever its `builtins.web` says. `ask` is served as `off`, as it is for builtins, because no tool approval is enforced. `allowed` leaves the agent's setting. Documentation MCP servers are connections, so Use credentials covers them.
- **Merge a pull request.** `review_approval` is the definition's behavior: an agent gate whose station `action` is `merge` merges on approval. `off` stops that merge. The approval is recorded, and the item waits for a person at the gate (`waiting_human`) with a reason that names the organization policy; a person's merge (`goal merge <item>`) then merges it.
- **Use secrets** (the API field `use_credentials`, [ADR 0037](../adr/0037-secrets-and-environment-files.md) decision 12). `environment`, the default, is the definition's behavior: the agent gets what its environment names, or on connections the connections its `connections` list names. `attached_only` is its deprecated alias: a save stores it as `environment`, and every read returns `environment`. `off` gives the agent none of them, neither their env nor their tools in the run bundle. The runtime's model connection and repository connection still reach the run, because it cannot start without them.
- **Write outside the item's branch** is not enforced. Comments, issues, tickets and messages go through connections' CLIs and MCP servers, and no engine or gateway step can ask a person for one. Its value is stored and changes nothing at run start.

The engine signs the model, the denied commands and the denied web tools into the run's task token, so a policy change never reaches a run already in its box. A run keeps the policy it started with.
