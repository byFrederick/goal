# Factory definition reference

A factory definition is a file tree with `factory.yaml` at its root ([ADR 0005](../adr/0005-factory-definitions-in-object-storage.md)): `agents/` (prompts), `skills/`, `rules/`, `contracts/` and `evals/`. `schema/factory.schema.json` is the reference for every key of `factory.yaml`. `internal/config` loads a tree and validates it (`config.Check`): every file parses, `factory.yaml` satisfies the schema, and every name and path it uses points at something that exists. `goal factory validate` (and `goal validate`, its offline form), the builder and publish all run the same checks. This page covers what the schema alone cannot explain.

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
