# ADR 0002 — Jonah: evaluation and self-remediation

Status: accepted as amended for phase 5 (2026-10-24; [amendment](#amendment--phase-5-the-minimal-loop)) · Amended 2026-10-27 by [ADR 0025](0025-judges-and-evals-as-the-screens-show-them.md) ([amendment](#amendment--2026-10-27-judges-and-evals-as-the-screens-show-them)) · first proposed 2026-10-23 · amended by [ADR 0005](0005-factory-definitions-in-object-storage.md) (definitions and visible cases live in the bucket) · Plan: [docs/history/jonah/README.md](../history/jonah/README.md)

## Context

Every factory's behavior lives in config: prompts, skills, tool policies, models, contracts, WIP limits. Each change can quietly make a station worse, and in a pull-based line the damage shows up late, as rework at the constraint. Goal needs to measure agents, stations and factories; catch regressions before a definition change ships and drift after; and turn failures into fixes without letting an automated optimizer game its own measurements. Constraints: PostgreSQL is the only database (ADR 0001); `goald` is stateless, hexagonal and runs on a laptop or on AWS (ADR 0003); model capacity is shared with production and, on a laptop, comes from one person's subscription.

The design follows Anthropic's *Demystifying evals for AI agents* (tasks, trials, graders, transcripts, outcomes; capability vs regression; pass@k vs pass^k; isolation; calibrated model graders) and the eval skills installed in `.claude/skills`.

## Decision

1. **Jonah is part of `goald`, not a separate service.** A core package (`internal/jonah`, with pure `stats` and `grade` subpackages) depends on `domain` and `ports` only. A new `Judge` port has Anthropic API, Bedrock and worker (subscription) adapters. All state is in PostgreSQL; schedules run under advisory locks; trials, samples and Jonah tasks are `SKIP LOCKED` queues.
2. **Evals run on the production stack.** Trials use the same `Runner` (Docker or ECS), worker image, prompts, skills and tool policy as production, with the MCP gateway in replay mode and a fresh, squashed workspace per trial. Infrastructure errors are recorded as `error`, never as agent failures.
3. **Config per factory in `factories/<id>/evals/evals.yaml` in the definitions bucket** ([ADR 0005](0005-factory-definitions-in-object-storage.md)), validated by `contracts/definitions/evals.schema.json`, separate from `factory.yaml`. Humans own it; Jonah can never change it.
4. **Visible cases in the bucket, sealed holdout in Postgres.** One YAML file per case (`tune` and `check` splits) under `factories/<id>/evals/cases/`, versioned by the bucket, validated on every `goal factory push` and synced to Postgres by content hash. Holdout cases exist only in Postgres, enforced by a CHECK, and Jonah sees only holdout verdicts.
5. **Graders: code first, then calibrated judges, then humans.** Code graders in Go are generic kinds configured per factory. Judges call Claude for exactly one failure mode, return a binary verdict with the critique first (or `unknown`), and gate only after TPR and TNR on a sealed, once-only test split reach the configured bar; otherwise they are advisory. Online rates are bias-corrected.
6. **Statistics are fixed before the run.** Unbiased pass@k and pass^k per case, averaged over cases; cluster bootstrap intervals; a paired decision rule with minimum effect, non-inferiority on every guard suite, a cost ceiling, a holdout gate and Holm correction. Inconclusive is not a pass.
7. **Remediation ships as a versioned definition change.** Jonah proposes one diff per hypothesis, limited by an allowlist to definition files (never `evals/`, schemas, migrations or its own code), and proves it offline. With your approval, `goald` validates the candidate definition, writes the changed files to the bucket as new object versions and records the previous object versions as the factory's rollback pin. The change is then watched and, on a trigger, pinned back to those versions immediately. The `goal` factory still opens pull requests, but only for Goal's own code.
8. **Autonomy levels L0–L3, default L1 then L2.** L3 can only auto-apply low-risk flow-config changes. Risk classes are fixed in code. A human override caps autonomy; `L0` is the kill switch.
9. **Guardrails live in the database too.** Path allowlist CHECK, "no change without a passing comparison" via CHECK and a cascading FK, immutable decided comparisons, one proposal in flight per factory, append-only audit and case versions, human-only override and pin release.
10. **Factory-level evaluation is measured online, constraint first.** Flow and outcome metrics come from `item_events`; Jonah orders its attention by the constraint (Goldratt's five focusing steps) rather than by raw scores.
11. **Evals are a subordinate lane.** Production first; the eval lane has its own WIP, time windows, budget and a maximum share of model spend; it yields to queued production work and backs off on rate limits.

## Rejected

- **An eval platform as a required dependency** (Langfuse, Braintrust, LangSmith, Arize Phoenix). Good tools, but each adds a second datastore and a second source of truth for runs and labels. Langfuse or Phoenix may receive exports later; Postgres stays the record.
- **MLflow-based evaluation** (the agent-evaluation skill's prescribed path). Python runtime and a tracking server next to a Go daemon; its datasets and scorers would duplicate our tables. We keep its practices (dry run, cost estimate) and not its stack.
- **A containerized eval framework** (for example Harbor). Strong for benchmarks, but it would run agents through a different harness than production; evaluating "the agent" means evaluating our harness and model together.
- **Cases only in Postgres.** Loses diffs, object-version history and validation next to the prompts they test, and editing with schema autocompletion in a working copy.
- **Cases, including the holdout, only in the bucket.** Every factory's evals are readable by the tooling and the operators that edit definitions, so the holdout would not be sealed.
- **Eval config inside `factory.yaml`.** Mixes a file Jonah may change with one it must never change, and would require changing `factory.schema.json`.
- **Likert-scale or holistic judges.** Hard to calibrate and unactionable; binary judges per failure mode instead.
- **Similarity metrics (ROUGE, BERTScore, embeddings) as graders.** They measure overlap, not correctness.
- **A judge from another model family.** Recommended by eval-harness-first, but Goal is Claude-only by design (one auth path, one quota). Mitigated by human calibration, deterministic graders on critical gates and spot checks; recorded as a risk.
- **Automatic prompt optimization loops** (many small rewrites per round, selected by score). Overfit quickly and spend quota; one hypothesis per change with a held-out decision instead.
- **An optimizer that writes definitions or edits graders on its own.** Jonah has no write access to the bucket; `goald` writes an approved proposal, never Jonah's task, and never under `evals/`.
- **Online A/B traffic splitting.** One owner and low volume give too little traffic for timely answers; offline paired comparisons plus a watched rollout with automatic rollback instead.
- **Judging every production run.** Cost and quota; sampled, stratified online evaluation with bias correction is enough to detect drift.

## Consequences

- Every production run and trial records the exact config it used (the object versions of its definition files), which also powers kaizen and rollback.
- Eval suites become the tests for agents and factories (`docs/engineering/testing.md`): a change to a prompt is reviewed with its eval comparison, the way a code change is reviewed with its tests.
- Humans carry real work: writing seed cases, labeling for calibration, curating the holdout, approving proposals. The phases in the plan keep that load bounded and deliver value before any self-remediation exists.
- `contracts/definitions/evals.schema.json` is a second public schema; `goal validate`, the UI editor and generated TypeScript types all use it.
- The engine gains two general features: items created at a given station with pre-filled outputs, and configuration pins.

## Amendment — phase 5: the minimal loop

Status: accepted · 2026-10-24 · replaces decisions 1–9 and 11 where they differ; decision 10 stands. Specified in [phase 5, continuous learning](../history/specs/5-continuous-learning.md); follows [ADR 0008](0008-portable-runs-connections-and-knowledge.md). The plan in `docs/history/jonah/README.md` stays as background; where it differs, spec 5 wins.

**Context.** The original decisions assumed a heavy optimizer: a sealed holdout, calibrated gating judges, paired statistics, autonomy levels and a separate eval lane. The owner runs a personal platform and wants the smallest loop that turns human touches (phase 4's headline metric) into knowledge, standard work and tests, proves a change before it ships, and shows afterwards whether it helped.

**Decision.**

1. **Jonah is a goald loop** (`internal/jonah`, domain and ports only) that runs ordinary agent runs through the engine's run lifecycle (`engine.RunTask`) in sandboxes pinned to a definition version. There is no `Judge` port: a judge is a short run in the same box with the factory's model connection.
2. **Retros** on items that finished with touches propose three things: facts for the organization's Learned knowledge source, edits to `factory.yaml`, `agents/`, `skills/` or `contracts/`, and regression eval cases.
3. **Cases live in the definition's `evals/cases/`** as one `EvalCase` kind (`contracts/definitions/evals.schema.json` is replaced by it; the `Evals` kind goes). Expectations are deterministic first (contract, checks, output and file assertions); a judge rubric is advisory until calibrated. No sealed holdout in this phase.
4. **An eval run** compares a candidate version with a baseline on the same cases; a case passes when all its trials pass; a regression is a case that passed on the baseline and fails on the candidate. Eval runs get the model connection and a repository clone only, so they act on nothing outside their box. A weekly learning budget per factory caps retros and evals together.
5. **People decide** through one review list. Two opt-in switches replace the autonomy levels: auto-accept facts, and auto-publish an edit only when it touches `agents/` or `skills/` alone, the eval set has at least five cases, nothing errored and nothing regressed. Jonah never changes `evals/` without a person. Restore is the undo, and touches per definition version are the check after publish.

**Consequences.** Much less to build and to explain; the measuring stick (cases) still changes only by review. Overfitting to retro-made cases and judge errors are accepted risks while judges stay advisory; calibration, the holdout and online sampling can return later on the same tables.

## Amendment — 2026-10-27: judges and evals as the screens show them

Status: accepted · 2026-10-27 · changes decisions 3, 4 and 5 of the phase 5 amendment; decided in [ADR 0025](0025-judges-and-evals-as-the-screens-show-them.md).

The binding Jonah screens ([ADR 0016](0016-ui-revamp.md)) show judges as their own files, `evals/judges/<slug>.md`, that score a sample of production runs against a pass mark. They show eval cases, `evals/cases/<name>.yaml`, that run on one version and gate every publish. ADR 0025 records that design. It replaces decision 3's advisory rubric inside a case, decision 4's comparison of a candidate with a baseline, and decision 5's auto-publish once there are five cases. The rest of the phase 5 amendment stands.
