# Factory definition reference

A factory definition is a file tree with `factory.yaml` at its root ([ADR 0005](../adr/0005-factory-definitions-in-object-storage.md)): `agents/` (prompts), `skills/`, `rules/`, `contracts/` and `evals/`. `contracts/definitions/factory.schema.json` is the reference for every key of `factory.yaml`. `internal/config` loads a tree and validates it (`config.Check`): every file parses, `factory.yaml` satisfies the schema, and every name and path it uses points at something that exists. `goal factory validate` (and `goal validate`, its offline form), the builder and publish all run the same checks. This page covers what the schema alone cannot explain.

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
