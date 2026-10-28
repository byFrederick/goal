# Factory definition reference

A factory definition is a file tree with `factory.yaml` at its root ([ADR 0005](../adr/0005-factory-definitions-in-object-storage.md)): `agents/` (prompts), `skills/`, `rules/`, `contracts/` and `evals/`. `contracts/definitions/factory.schema.json` is the reference for every key of `factory.yaml`, and `contracts/definitions/evals.schema.json` for judges and eval cases ([below](#judges-and-eval-cases)). `internal/config` loads a tree and validates it (`config.Check`): every file parses, `factory.yaml` satisfies the schema, and every name and path it uses points at something that exists. `goal factory validate` (and `goal validate`, its offline form), the builder and publish all run the same checks. This page covers what the schema alone cannot explain.

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

A station's output contract is the definition's own JSON Schema; goald validates each output against it and reads a few fields from an accepted one: the gate's verdict, `violations` (above), `lane` and `plan` ([ADR 0012](../adr/0012-assemblies-and-parts.md)), and the dependency field below.

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

## Judges and eval cases

Jonah's two file kinds live under `evals/` ([ADR 0025](../adr/0025-judges-and-evals-as-the-screens-show-them.md)). `contracts/definitions/evals.schema.json` defines both, and `config.Check` validates them with the factory (`internal/config/evals.go`). Judge runs and eval runs are built in `internal/jonah` but goald does not run them yet; the publish eval gate is described under [Publishing with eval cases](#publishing-with-eval-cases).

### Judges: `evals/judges/<slug>.md`

A judge scores a sample of the finished production runs of the agents it names. Its file name is its slug, and cases refer to it by that slug. The YAML front matter holds its settings, and the Markdown body is the rubric the judge run reads.

```markdown
---
title: Review reason actionable
criterion: A sent-back reason says what to change and how to prove it
agents: [reviewer]          # spec.agents names; empty or omitted: every agent
outcomes:
  - score: 1
    label: Actionable
    meaning: Names what failed, what to change and the proof
  - score: 0.5
    label: Vague
    meaning: Says something is wrong but not what would fix it
  - score: 0
    label: Missing
    meaning: No reason, or only "looks wrong"
passMark: 0.7               # a score below it fails and goes to Jonah
sample: 0.25                # share of the agents' finished runs it scores; 0 pauses it
model: claude-sonnet-5-5    # omitted: runtime.model
enabled: true               # default true
---
Only score runs where the reviewer sent the change back. …
```

### Eval cases: `evals/cases/<name>.yaml`

A case is one task an agent must get right. Its file name is its name. `tries` says how often it runs, and it passes when every try passes. `check` names exactly one way to decide a try: `contract: true` (the output satisfies the agent's station contract), `judge: <slug>` (the judge's score reaches its pass mark) or `referencePatch: ./path` (a known-good patch, relative to the root of the definition, such as `./evals/fixtures/…`).

```yaml
apiVersion: goal/v1
kind: EvalCase
metadata:
  description: A criterion without evidence is sent back with a clear reason
spec:
  agent: reviewer
  input:
    title: Retry policy for lost leases
    body: "…"
    type: feature           # optional: work type, lane, earlier stations' outputs, starting commit (ref)
    outputs: {spec: {}}
  tries: 3
  check:
    judge: review-reason-actionable
```

### Checks

Each problem is a `DefinitionProblem` in section `files`. Its `File` is the judge's or case's path, and its line is in that file.

| Problem | Code |
| --- | --- |
| A case or a judge's front matter fails the schema: a missing or unknown key, `passMark`, `sample` or an outcome's `score` outside 0 to 1, empty `outcomes`, `tries` outside 1 to 10, no `check` or more than one, a `kind` other than `EvalCase` | `schema-required`, `schema-unknown-key`, `schema-enum`, `schema-invalid` |
| A judge file has no front matter, or an empty one | `judge-front-matter-missing` |
| A judge's front matter or a case does not parse | `yaml-invalid` |
| Two outcomes of a judge share a score, or a label (case-insensitive) | `judge-outcome-duplicate` |
| A judge's `agents` or a case's `agent` is not in `spec.agents` | `unknown-agent` |
| A case's `check.judge` is not a file in `evals/judges/` | `unknown-judge` |
| A case's `check.referencePatch` is not a file in the definition; a path that leaves the tree never matches | `patch-not-found` |
| A judge's or case's file name is not a slug (3 to 40 lowercase letters, digits and hyphens) | `eval-name-invalid` |

Only files directly in `evals/judges/` (`.md`) and `evals/cases/` (`.yaml`, `.yml`) are judges and cases. Fixtures such as reference patches go elsewhere, for example `evals/fixtures/`.

### Publishing with eval cases

Every publish of a draft that has eval cases runs them on the draft first ([ADR 0025](../adr/0025-judges-and-evals-as-the-screens-show-them.md), decision 4). The gate is in `authoring.Service.Publish` (`internal/authoring/evalgate.go`), after the checks pass, so every way to publish goes through it: `POST …/draft/publish`, `goal factory publish` and push, and adding a case or a judge from the UI. It compares runs by the draft's exact content, `domain.ManifestSHA256` of its files, so any change to any file needs a new run:

- **No cases.** A draft without files in `evals/cases/` publishes as before.
- **No run yet.** When no run of the content exists, or its newest run ran one case only (`goal eval run --case`), the publish starts a run of every case (trigger `publish`) and refuses with 409 `evals-pending`, naming the run in `eval_run_id`. The draft stays open.
- **Pending.** While that run is queued or running, the publish refuses with 409 `evals-pending` and starts no other run.
- **Failing.** When the run finished with a case that has any try that is not `pass` (a `fail`, an `error`, or a try a cancelled run never ran), the publish refuses with 409 `evals-failing`, with `eval_run_id` and `failing_cases`. Fix the case or the definition, which changes the content, and publish again.
- **Passing.** When every try of every case passed, the draft publishes.

The gate is off while goald does not wire it (`authoring.Deps.EvalGate` is nil): goald has no `EvalSuites` or `EvalRunner` adapter yet, so today a factory with cases publishes without running them, unlike ADR 0025 decision 4. The evals routes answer 503 `evals-unavailable` for the same reason.

`POST …/evals/cases` (and the evals screen's "Add a case from a past run") writes `evals/cases/<name>.yaml` from a past run's agent and item, with `check: {contract: true}` or `{judge: <slug>}` and the requested tries, into a new draft, and publishes it through the same gate.

## Triggers: `board.triggers`

A trigger turns an event, a schedule or a webhook delivery into a work item ([ADR 0026](../adr/0026-intake-triggers.md)). Triggers are part of `factory.yaml`, so saving one publishes a new version. `internal/config` parses and checks them (`config.Trigger`, `internal/config/triggers.go`). The engine that matches events, runs the checks and fires schedules is not built yet. `board.intake` is deprecated and ignored: the schema still accepts it so existing definitions load, and nothing reads it.

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
- `on` (required): the event key. One of `github.check_run.failed`, `github.issues.labeled`, `schedule`, or `webhook.<endpoint name>` with an optional `.<event>`, matched against the delivery's `event` field. Slack, Linear, Jira and Sentry keys are not accepted yet.
- `cron` and `timeZone`: only with `on: schedule`, where `cron` is required. `cron` is five fields: minute, hour, day of month, month, day of week. A field takes numbers, `*`, ranges (`1-5`), steps (`*/15`, `0-30/10`), lists (`0,30`) and, for months and weekdays, names in any case (`JAN`, `mon`). Weekdays are 0 to 6, Sunday first. `@daily`, `?` and seconds are not accepted. `timeZone` is an IANA name and defaults to `UTC`; `Local` is refused.
- `filter`: filters on the event's fields, all of which must match: `branch`, `repo` (owner/name), `label`, `paths` (globs; any changed path matches), `authors` (any of) and `skipBots`.
- `only`: checks before an item is created, in this order. `failuresInARow: n` passes when the same check failed on the last n events for it. `dedupe: <field>` passes when no open item this trigger created holds the same value of that event field.
- `create` (required): `type` (one of `board.workTypes`) and `station` (a station id) are required. `lane` is a key of that station's `route`, `expedite: true` puts the item in the expedite flow, and `title` and `brief` are templates. A placeholder is `{{field}}` or a dotted `{{field.sub}}`, spaces inside the braces allowed, and takes the event's field. Event text reaches agents only as untrusted data (ADR 0026, decision 5).
- `limits`: `perDay` (at most n items from this trigger since the start of the UTC day) and `pauseOnBudget` (skip events while the factory's weekly budget is spent).

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
- **Use credentials.** `attached_only` is the definition's behavior: the agent gets the connections its `connections` list names. `off` gives the agent none of them, neither their env nor their tools in the run bundle. The runtime's model connection and repository connection still reach the run, because it cannot start without them.
- **Write outside the item's branch** is not enforced. Comments, issues, tickets and messages go through connections' CLIs and MCP servers, and no engine or gateway step can ask a person for one. Its value is stored and changes nothing at run start.

The engine signs the model, the denied commands and the denied web tools into the run's task token, so a policy change never reaches a run already in its box. A run keeps the policy it started with.
