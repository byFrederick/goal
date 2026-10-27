# ADR 0023 — Organization policy: a ceiling over factory definitions

Status: accepted · 2026-10-27 · Builds on [ADR 0016](0016-ui-revamp.md) (the `organization/models` screen is binding) and [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (runs in boxes, the egress proxy, connections that carry credentials)

## Context

The [Models and policies screen](../design/screens/organization/models.html) ([picture](../design/screens/organization/models.jpg)) is subtitled "Defaults for every factory in Goal. A factory or an agent can choose less, never more." It shows the models agents may use with one default, the box sizes and a box time limit ("Stop a box after 3 hours, whatever the run is doing"), four tool policy defaults, the commands no agent may run, and the addresses agents connect from. The Spend and billing screen shows the organization's spend. ADR 0016 makes both screens binding. Nothing in Goal holds these settings today:

- Each factory definition chooses its own model (`spec.runtime.model` and an agent's `model`, pattern `^claude-[a-z0-9-]+$` in [factory.schema.json](../../contracts/definitions/factory.schema.json)). `GOAL_MODELS` in [contracts/env.yaml](../../contracts/env.yaml) only lists the models the factory editor offers ([ADR 0022](0022-model-presets-in-the-new-factory-wizard.md)); nothing stops a definition from naming another one.
- Each agent's tool permissions come from its definition: `agents.<name>.tools` (`allow` and `deny` in Claude Code's permission syntax) or `agents.<name>.builtins` (the groups `read`, `edit`, `run`, `web` and `subagents`, each `off`, `ask` or `allowed`). No rule holds across factories.
- `runtime.size` picks `small`, `medium` or `large` (`domain.SandboxSize` in [internal/core/domain/run.go](../../internal/core/domain/run.go)); each sandbox backend maps a size to CPU and memory. No setting limits how long a box runs.
- `runtime.budget` caps a factory's spend (`dailyUsd`, `weeklyUsd`, `perItemUsd`, `maxTurnsPerTask`). Each run records `cost_usd` (`RunResult.CostUSD`), which the worker reports from the run's token counts.
- The egress addresses depend on where the operator runs Goal (their own Docker host, or Cloudflare's ranges), and only the operator knows them.

The parts of assembly "Spend and billing, Models and policies" (the policy store, API and enforcement, the tool policy, the spend store and API, and the two screens) build against this record.

## Decision

1. **The organization policy is one row per organization in PostgreSQL, edited through the API.** It is not a file in the bucket and not part of a factory definition: it is mutable state that applies to every factory, and the bucket holds only immutable content-addressed blobs ([AGENTS.md](../../AGENTS.md) rule 3). The table is under row-level security like every organization table. An organization without a row has the platform default policy (decision 2's defaults), so nothing changes for it until someone saves. The policy names no factory, station or agent ([AGENTS.md](../../AGENTS.md) rule 1): it applies to all of them alike. The "Used by" lines on the screen are computed from the published definitions, never stored in the policy.

2. **The policy has these fields, and only these.**
   - **Allowed models**: a non-empty subset of the ids in `GOAL_MODELS`, with exactly one of them marked the default. The default is the model an agent gets when neither its definition nor its factory names one. Default policy: every model in `GOAL_MODELS`, the first one the default.
   - **Denied commands**: a list of Claude Code Bash permission patterns, such as `Bash(terraform apply:*)` or `Bash(git push --force:*)`, that no agent in the organization may run. The screen shows each pattern's command. Default policy: an empty list.
   - **Tool policy defaults**, four of them:
     - *Write outside the item's branch* (comments, issues, tickets, messages): `off`, `ask` or `allowed`; default `ask` ("Ask a person").
     - *Merge a pull request*: `off` or `review_approval`; default `review_approval`, meaning only a review station with merge on approval merges.
     - *Read the web and docs* (search, fetch, documentation tools): `off`, `ask` or `allowed`; default `allowed`.
     - *Use credentials*: `off` or `attached_only`; default `attached_only`, meaning an agent gets only the credentials attached to it ([ADR 0017](0017-credentials.md), decision 6).
   - **Box time limit**: how long a box may run before the engine stops it, whatever the run is doing; a whole number of minutes, default 180 (3 hours).

   The box sizes `small`, `medium` and `large` are not policy fields. They stay platform-defined (`domain.SandboxSize`, mapped by each sandbox backend), and the screen shows each size with the factories whose `runtime.size` uses it.

3. **A factory's definition and an agent may narrow the policy, never widen it.**
   - A definition or an agent may name any allowed model in `spec.runtime.model` or an agent's `model`. Publishing a version that names a model outside the allowlist is refused, with an error that names the field and the model.
   - An agent's `tools.deny` and a `builtins` group set to `off` may deny more than the policy. Publishing a version whose `agents.<name>.tools.allow` or `builtins.run.commands` allows a command that a denied pattern matches is refused, with an error that names the agent and the pattern.
   - An agent's `tools` or `builtins` may be stricter than a tool policy default (`allowed` to `ask` or `off`, `ask` to `off`), never looser. A looser setting is served as the policy's value.
   - The box time limit is the longest a box runs; nothing in a definition raises it.
   - A run started from a version published before the policy changed gets the policy as it is at run start: the denied commands are added to its agent's deny rules, and the run is refused, with an error that names the model, when its model is no longer allowed. The policy is read at run start; a run already in its box keeps the policy it started with.

4. **The egress addresses come from install config, in a new public variable `GOAL_EGRESS_ADDRESSES`.** It is a comma-separated list of CIDR ranges (`192.0.2.0/24,2001:db8::/32`) that the screen shows under "Where agents connect from". The operator sets it, because only the operator knows where boxes connect from; it is not part of the policy row and the API cannot change it. It is `class: public` in [contracts/env.yaml](../../contracts/env.yaml), like `GOAL_MODELS`. It is unrelated to `GOAL_EGRESS_GATEWAY`, which is goald's gateway listener as the egress proxy reaches it. The part whose code first reads the variable adds it to contracts/env.yaml. When it is unset, the screen says the operator has not listed the addresses.

5. **Spend is the sum of runs' `cost_usd`, and it is Goal's estimate, not a bill.** The figure is computed from each run's token counts at the model's list price, as the worker reports it. It does not include discounts, taxes, a provider's plan or subscription, or usage outside Goal. The API, the CLI and the screen call it an estimate. The factory budgets (`runtime.budget`) keep working on the same figure and are not part of the policy.

6. **Rejected alternatives.**
   - *A policy file in the bucket, or an organization-level definition.* Bucket content is immutable and content-addressed, and a policy is edited in place; versioning it like a definition would add publishing to a settings screen.
   - *Policy fields in each factory's definition.* Each factory could then widen them, which is the opposite of the screen's promise.
   - *Making the box sizes policy.* The CPU and memory of a size depend on the sandbox backend, which the operator chooses, not the organization.

## Consequences

- An owner sets the models, denied commands, tool defaults and box limit once for every factory, and no definition can get around them.
- Publishing gains two checks (decision 3) against the organization's policy; a definition that published before can fail its next publish after the policy narrows, with an error that names what to change.
- The engine reads the policy at run start, adds its denies to the agent's rules, refuses a disallowed model and stops a box at the time limit. The policy-enforce and tool-policy parts build this with the in-memory fakes in `internal/ports/porttest`.
- The policy-store part adds the table in a new migration with row-level security; the policy-api part adds `GET` and `PUT /v1/orgs/{org}/policy` and its permission to the contracts.
- The operator sets `GOAL_EGRESS_ADDRESSES` on each install; without it the screen shows no addresses.
- Readers may still take the spend figure for an invoice; every place that shows it says it is an estimate.
- Each later part links this ADR from the docs and change-map entries it changes.
