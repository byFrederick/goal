# ADR 0014 — Factory rules: always-on guidance for a factory's agents

Status: accepted · 2026-10-26 · Builds on [ADR 0005](0005-factory-definitions-in-object-storage.md) (definitions as content-addressed blobs, versions that runs pin) and [ADR 0012](0012-assemblies-and-parts.md) (the assembly that carries this out)

## Context

A factory's agents get guidance from several places, and today the places blur. A repository explains how its code works in `AGENTS.md` and `.claude/rules` (assembly software#84 adds path-scoped rules to this repository). A factory's definition gives each agent a prompt and skills. What the definition cannot say is how the factory itself works, whatever repository it works on: how a change is delivered, how secrets are handled, what evidence a result needs, the tone of what agents write, how much they may spend. So those rules hide in skills: the software factory's `goal-conventions` skill carries "the rules agents break most" next to the procedures of its box. A skill is loaded on demand, at the model's discretion, from its description; a rule inside one applies only when the model happens to load it, and nothing shows whether it did.

Some of that guidance should not be text at all. A rule an agent can ignore is weaker than a contract its output must satisfy, a gate it must pass, a tool it is not allowed or a branch it cannot push to. Text is for the judgment those cannot express.

The owner wants rules a factory owns, that are always on for the agents they target, that no repository has to carry and that a run can show it ran with. Everything stays factory config: no station, agent, lane or work type is named in Go, SQL or TypeScript (AGENTS.md rule 1).

## Decision

1. **Four kinds of guidance, one job each.**
   - *Repository rules* (`AGENTS.md`, `.claude/rules`) describe how one codebase works. They travel with the code and apply to anyone, or any factory, working on it.
   - *Factory rules* (new) describe how one factory works whatever it works on: delivery policy, secrets hygiene, evidence, tone, cost discipline. They are always on for the agents they target.
   - *Skills* are procedures the model loads on demand, at its discretion.
   - *The prompt* is one agent's role and job.

   A rule must never depend on a skill: what an agent must always follow is a factory rule (or a repository rule), never a section of a skill. `goal-conventions` is today's counterexample, and its rule content moves out of the skill.
2. **Mechanism before instruction.** What can be checked belongs in a mechanism: a contract (the output's schema), a gate, a tool permission, branch protection. Rule text is only for judgment that cannot be checked. A rule that restates a mechanism is deleted; a rule that could be a mechanism becomes one. The builder's help text for rules repeats this.
3. **Format.** A definition has a `rules/` folder, and `factory.yaml` declares its rules the way it declares skills:

   ```yaml
   rules:
     - { name: secrets, source: ./rules/secrets.md }
   ```

   A rule file is short markdown with frontmatter:
   - `description`: one line, required.
   - `appliesTo`, optional: lists `agents`, `stations`, `lanes` or `workTypes`. A rule applies to a run when every list given contains the run's value; omitted, it applies to every agent of the factory. An unknown agent or station name is a validation error (`goal factory validate` and publish refuse it).
   - `paths`, optional: globs, kept as Claude Code's path scoping, so a rule about some files loads only when the agent works on code that matches them.

   Rule files are content-addressed blobs of the definition version like every other file of it (ADR 0005); a run pins the rules of the version it runs, so publishing a new version never changes the rules of a run in flight.
4. **Delivery.** goald computes each run's effective rules, the declared rules whose `appliesTo` matches the run's agent, station, lane and work type, and sends them in the run bundle (`GET /run`). `goal-agent prepare` writes each to `$HOME/.claude/rules/<name>.md` (the worker's `CLAUDE_CONFIG_DIR`, `paths.claudeConfig`), keeping its `paths` frontmatter, and never into the repository working tree, so a factory rule can never be committed. The worker's SDK loads both user and project settings (`settingSources: ['user', 'project']`), so the repository's rules and the factory's rules both apply. The run's events record the rules it ran with, each rule's name and blob sha256, and the run page shows them.
5. **Precedence.** Repository rules describe the code, factory rules the process. On a conflict the factory rule wins inside that factory's runs, and the agent says so in its output. A slot for organization rules, above both, is reserved as later work; nothing is built for it now.
6. **Review.** A review station's agent receives the same rules as the agents whose work it reviews, by the same matching. By convention a review station's contract may carry an optional `violations` list, one `{rule, reason}` per broken rule, `reason` one line. goald counts violations per rule in the item's events, so Jonah (phase 5) can later see which rules break most; how Jonah uses those counts is out of scope.

## Consequences

- The later parts of assembly software#95 build this: the schema and validation of `rules` and rule files (`schema/factory.schema.json`, `internal/config`, `goal factory validate`); the effective rules in the run bundle and the run's events (`internal/gateway`); `goal-agent prepare` writing them to `$HOME/.claude/rules` (`worker/`); the per-rule violation count in the item's events; the builder's model and the definition API carrying rules (`internal/factorymodel`, `api/openapi.yaml`) and its Rules editor; the run page's rules and each agent's effective rules (`web/`).
- `goal-conventions`' rule content moves out of the skill into the software factory's rules. That is a definition change in the bucket, not a change in this repository; the skill keeps its procedures.
- A rule always costs context in every run it targets. Rules stay short, scoped with `appliesTo` and `paths`, and give way to a mechanism whenever one can check the same thing.
- Writing to `$HOME/.claude/rules` relies on Claude Code loading user-level rules from `CLAUDE_CONFIG_DIR/rules`. The worker part confirms it; if the SDK behaves differently, this ADR is amended.
- A run's recorded rule names and sha256 make "which rules did this run follow" a question with an answer, for people and for Jonah.
