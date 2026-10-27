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

## Judges and eval cases

Jonah's two file kinds live under `evals/` ([ADR 0025](../adr/0025-judges-and-evals-as-the-screens-show-them.md)). `contracts/definitions/evals.schema.json` defines both, and `config.Check` validates them with the factory (`internal/config/evals.go`). Judge runs, eval runs and the publish gate are not built yet.

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
