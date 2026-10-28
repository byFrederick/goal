# ADR 0029 — Bake-offs are eval runs of one agent across models

Status: accepted · 2026-10-28 · Builds on [ADR 0002](0002-jonah-evals.md) ([phase 5 amendment](0002-jonah-evals.md#amendment--phase-5-the-minimal-loop): Jonah is a goald loop, eval runs get the model connection and a clone only, a weekly learning budget), [ADR 0025](0025-judges-and-evals-as-the-screens-show-them.md) (decisions 3 and 4: eval cases and eval runs of one version) and [ADR 0016](0016-ui-revamp.md) (the screens are the contract)

## Context

The binding Jonah screen [`bake-off`](../design/screens/jonah/bake-off.html) ([picture](../design/screens/jonah/bake-off.jpg)) asks "Which model for frontend-engineer?". It runs three models on the same five cases with three tries each. It shows a recommendation with an "Apply to the definition" button, a plot of pass rate against cost per item, the tries passed per case out of 3, and a results table. The table has a pass rate, a cost per item, a time per item, the turns and a "Judged plain language" column, one row per model, and it marks one row "Recommended" and one "Now". The agent screen ([`build/agent`](../design/screens/build/agent.jpg)) is where a person starts one ("Bake off this agent"). The [glossary](../reference/glossary.md) lists a bake-off as planned.

Eval runs are built (`internal/jonah/evals.go`, `db/migrations/00068_evals.sql`, `internal/core/domain/eval.go`). An eval run runs a factory's cases (`evals/cases/<name>.yaml`) on one definition, named by its content hash and its version. Each case names its `agent`, its number of `tries` (1 to 10) and one check. Each try is an `eval_results` row (`try`, `state` pass, fail or error, `cost_usd`, `turns`, `run_id`) that goald instances claim under a lease. `Evals.Work` runs a try through the `EvalRunner` port as a task of kind `eval` (`domain.TaskKindEval`), whose token may name no connection. An infrastructure error is retried once and then recorded as error. `Evals.Estimate` prices each try at the mean cost of the case's last finished tries, or else at the median cost of its agent's 20 newest finished runs. `Evals.Start` queues a run.

[Spec 5](../history/specs/5-continuous-learning.md) §4.4 adds a weekly learning budget for retros and evals, which refuses an eval run whose estimate exceeds what is left (409 `learning-budget-exhausted`). That budget is not built yet. Nothing in Go knows any model name today: an agent's `model` and the factory's `runtime.model` are definition fields matching `^claude-[a-z0-9-]+$` (`contracts/definitions/factory.schema.json`).

A bake-off compares models for one agent on cases that eval runs already have. The store, engine, API, CLI and UI parts of the bake-off work need one shape to build against.

## Decision

1. **A bake-off is an eval run of kind `bake-off`.** It runs one agent of the factory, named by its station and agent slug, on the factory's current definition version. It has two to four arms. Each arm is a model id matching `^claude-[a-z0-9-]+$`, and no two arms are the same. Exactly one arm is the agent's current model in that version, the one the screens label "Now". An ordinary eval run is of kind `eval`, which is what every existing row becomes.
2. **Its tries are eval tries with the arm's model.** It runs the version's eval cases whose `agent` is that agent, as `Evals` runs them. Each case runs the bake-off's tries per case (default 3, 1 to 10) on every arm. That number replaces the case's own `tries`, so every arm gets the same number of tries. Each try is an ordinary eval try (`EvalRunner`, a task of kind `eval`, pinned to the version), with the agent's model overridden by the arm. It is graded by the case's check, retried once after an infrastructure error, and recorded with its arm. Like every eval try, it gets only the model connection and a repository clone.
3. **The report has one row per arm.**
   - **Pass rate**: the tries that passed, out of all its tries.
   - **Cost per item**: the mean cost of a try.
   - **Time per item**: the mean duration of a try.
   - **Turns**: the mean turns of a try.
   - **Judged share**: the share of its tries that the advisory judge passed. The advisory judge is the version's enabled judge that scores the agent. When there are several, it is the first by slug. Its column is named after the judge, for example "Judged plain language". There is no column when no judge scores the agent. It informs the reader and never changes the recommendation.
   - **Per case**: the tries passed out of the tries per case.

   A try recorded as error counts as not passed and stays in the means of what it spent.
4. **The arm with the highest pass rate is recommended, and then the one with the lowest cost per item.** There is no recommendation while the run is not done, or when any arm errored on every try. The headline sentence compares the recommended arm with the current one, for example "claude-haiku-5-5: the same pass rate at a tenth of the cost". When the current arm wins, the headline says so.
5. **Cost is estimated and capped as for eval runs.** The estimate is cases × tries per case × arms × the price of a try. A try is priced as `Evals.Estimate` prices it: the mean cost of the case's last finished tries, or else the agent's median run cost. The weekly learning budget applies to bake-offs as it does to eval runs. A bake-off whose estimate exceeds what is left of the budget is refused with 409 `learning-budget-exhausted` and the estimate.
6. **Default arms come from a goald setting, never from Go code.** A request that names no arms gets the agent's current model plus the models in `GOAL_BAKEOFF_MODELS`, a comma-separated list declared in `contracts/env.yaml`. Duplicates of the current model are dropped, and the list is cut at four arms. With an empty setting and no arms named, the request is refused because one arm is not a bake-off. No model name appears in Go, apart from test fixtures.
7. **Run again repeats a bake-off, and Apply writes the draft. Neither publishes.** Run again starts a new bake-off of the same agent with the same arms and tries per case, on the factory's current version, which may be newer than the original's. Apply to the definition sets `agents[].model` for that agent in the factory's draft to the arm's model. If no draft is open, it opens one from the current version first. If the agent's file changed since the bake-off's version, Apply is refused with 412 `draft-conflict`, as every stale draft write is. The person then publishes the draft as usual, through the eval gate. Jonah never publishes it.
8. **Viewing needs `factory:read`. Starting, running again and applying need `definition:edit`.** These are the permissions eval runs use, and `contracts/api/permissions.yaml` already has both.

## Rejected

- **A separate set of bake-off tables** (`bake_offs`, `bake_off_arms`, `bake_off_results`). That would mean a second claim loop, lease handling, cost accounting, purge path and row-level security policy for tries that are eval tries in every way but one: the model. One eval run with a kind and an arm on each result keeps a single loop, a single place where cost is recorded and a single purge.
- **A model list hard-coded in Go**, for the default arms or for checking an arm. A new model would then need a goald release, and the list would go stale between releases. The id pattern checks the shape. Which models exist is a deployment setting (`GOAL_BAKEOFF_MODELS`), and the model connection's provider decides whether a model runs.
- **Comparing whole agent configurations** (model, harness, prompt) in one bake-off. A prompt or skills change is a definition edit, which a draft and its eval gate already test. A bake-off varies only the model, so its result maps to one field that Apply can write.
- **Recommending by cost alone, or by the judged share.** A cheaper model that fails more cases is not better. The judges are not calibrated ([ADR 0025](0025-judges-and-evals-as-the-screens-show-them.md), Consequences), so the judged share stays advisory.

## Consequences

- Bake-offs reuse the eval loop's leases, retries, grading and cost. The store gains a kind, the agent and the arms on `eval_runs`, and an arm on each `eval_results` row. The engine gains the model override on an eval try. Those are the store and engine parts of the bake-off work.
- A bake-off costs about as many tries as there are arms times an eval run of the agent's cases. The estimate is shown before it starts, and the learning budget bounds it once that budget is built. The estimate prices every arm at the current model's history, so it is rough for a much cheaper or dearer model.
- The judged share needs an eval try scored by a judge. Judges score sampled production runs today ([ADR 0025](0025-judges-and-evals-as-the-screens-show-them.md), decision 2). The engine part scores each bake-off try with the advisory judge and adds that cost to the run.
- Operators choose the default arms through `GOAL_BAKEOFF_MODELS`. The engine part declares it in `contracts/env.yaml`, and `docs/reference/environment.md` is generated from it.
- The API, CLI and the bake-off and agent screens build on these names: kind `bake-off`, arms, "Now", "Recommended", "Judged <judge>", the 409 `learning-budget-exhausted` and 412 `draft-conflict` answers.
