# ADR 0023 — Judges and evals as the screens show them

Status: accepted · 2026-10-27 · Amends [ADR 0002](0002-jonah-evals.md) ([phase 5 amendment](0002-jonah-evals.md#amendment--phase-5-the-minimal-loop), decisions 3, 4 and 5) · Builds on [ADR 0016](0016-ui-revamp.md) (the screens are the contract) and [ADR 0005](0005-factory-definitions-in-object-storage.md) (definitions are files in the bucket, published as versions)

## Context

The Jonah screens are binding by ADR 0016: [`judges`](../design/screens/jonah/judges.html), [`judge`](../design/screens/jonah/judge.html) and [`evals`](../design/screens/jonah/evals.html) (each with its `.jpg`). They show two things.

- **Judges** are named and editable. Each has a title, a "What it checks" sentence, the agents it scores, a Markdown rubric, scored outcomes, a pass mark, a sample share ("0% pauses it"), a model and an on switch. It scores a share of the runs of its agents, and a score under the pass mark goes to Jonah. The page says judges live under `evals/judges/` and that editing one publishes a new version.
- **Evals** are cases under `evals/cases/`. Each names an agent and what checks it: a contract, a judge or a reference patch. Cases run on one definition version ("Cases passing every try, v21"), "on every publish", and "a failing case blocks the publish".

The [phase 5 amendment of ADR 0002](0002-jonah-evals.md#amendment--phase-5-the-minimal-loop) and [spec 5](../history/specs/5-continuous-learning.md) §2 and §4.4 say otherwise. In them a judge is only an advisory `rubric` inside a case, and an eval run compares a candidate with a baseline. Jonah may also auto-publish an edit once the eval set has five cases, and production runs are not sampled.

Today `contracts/definitions/evals.schema.json` still describes ADR 0002's original `Evals` and `EvalCase` kinds. Nothing embeds it, and `internal/config` only checks that the files under `evals/` parse.

## Decision

1. **A judge is a definition file, `evals/judges/<slug>.md`.** Its slug is its file name. Its YAML front matter holds `title`, `criterion` (what it checks, in one sentence), `agents` (the `spec.agents` names it scores; empty or omitted means every agent), `outcomes` (each a `score` from 0 to 1, a `label` and a `meaning`), `passMark` (0 to 1), `sample` (0 to 1; 0 pauses the judge), `model` (omitted: the factory's `runtime.model`) and `enabled` (default true). The Markdown body is the rubric. Outcome scores and labels are unique.
2. **A judge scores a deterministic sample of finished production runs.** It samples the finished production runs of the agents it scores, at its `sample` share, and the same run always gets the same sampling decision. Each score is a short judge run in a box. The box has the factory's default size and connections, read only, and sees the run's events, its spec and its pull request. The judge run returns one outcome and a one-sentence reason. A score below the pass mark fails and goes to Jonah. Judges never gate production: a score changes nothing about the item it reads.
3. **An eval case is `evals/cases/<name>.yaml`, kind `EvalCase`.** Its name is its file name. It names its `agent`, a `metadata.description`, an `input` (the item as it reaches the agent, optionally with earlier stations' outputs and a starting commit), the number of `tries`, and exactly one `check`: `contract: true` (the agent's output contract), `judge: <slug>` (passes when the judge's score reaches its pass mark) or `referencePatch: <path>` (a known-good patch in the definition).
4. **An eval run runs cases on one definition version, and every publish runs them.** A case passes when every try passes. A try that fails for an infrastructure reason counts as `error`, never as `fail`. Every publish runs the cases on the draft first, and a failing case blocks the publish. A factory with no cases publishes as before.
5. **The UI writes files and publishes.** Editing a judge or adding a case from the UI writes the file into the factory's draft and publishes a new version, through the same checks and eval gate as any publish.
6. **`contracts/definitions/evals.schema.json` holds the two kinds**, `EvalCase` and `Judge` (front matter). The `Evals` kind is gone. `config.Check` validates every judge and case with the factory, and reports each problem as a `DefinitionProblem` in the judge's or case's own file ([factory-definition.md](../engineering/factory-definition.md#judges-and-eval-cases)).

This replaces three points of the ADR 0002 phase 5 amendment:

- Decision 3's advisory rubric inside a case. Judges are their own files, cases point to them by slug, and a judge's pass mark decides. Cases have no `rubric`, `expect`, `split` or `station`.
- Decision 4's comparison of a candidate with a baseline. An eval run runs one version, and a failing case blocks that version's publish.
- Decision 5's auto-publish once the eval set has at least five cases. Every publish is gated by the cases, whatever their number.

The rest of the amendment stands: Jonah is a goald loop (decision 1), retros (decision 2), people decide through one review list (decision 5, apart from the auto-publish rule), and Jonah never changes `evals/` without a person. Spec 5 §2 and §4.4 are superseded where they differ. The spec stays as it is, because `docs/history/` is a record.

## Consequences

- One file format serves the editor, the UI and Jonah. A judge or case is reviewed and versioned like a prompt, and a broken one is a definition problem before anything runs it.
- Production runs now cost judge runs. The `sample` share and the pause at 0 are the controls, and the judges screen shows the cost per judge.
- Publishing gets slower and costs model spend when a factory has cases. A factory without cases is unaffected.
- Judges are not calibrated against people's verdicts. A judge's errors reach Jonah as false failures. This risk is accepted because judges never gate production. A case checked by a judge does gate a publish, so the owner chooses which judges cases use.
- Later parts build on this: tables for judge scores and eval runs, the judge mode of goal-agent, the sampling loop, the eval loop and publish gate, the API and CLI, and the screens.
