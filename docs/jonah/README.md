# Jonah — evaluation and self-remediation for Goal

Status: design, ready for implementation (phase J0 done) · 2026-10-23 · Decision record: [ADR 0002](../adr/0002-jonah-evals.md)

In *The Goal*, Jonah never fixes the plant. He asks Alex Rogo questions until Alex can see the constraint himself, then lets him change it. This subsystem works the same way. It measures every agent, station and factory, finds where quality is lost (first at the constraint), and proposes one change at a time with evidence. A human decides. Jonah never edits the measuring stick.

Files that make up this design:

| File | What it is |
| --- | --- |
| `docs/jonah/README.md` | This plan |
| `docs/adr/0002-jonah-evals.md` | Key decisions and rejected alternatives |
| `schema/evals.schema.json` | JSON Schema (2020-12) for `evals.yaml` and case files |
| `factories/goal/evals/`, `factories/sre/evals/` | Starter suites, cases, judges and fixtures |
| `docs/jonah/schema.sql` | Draft tables, tested on PostgreSQL 18; becomes migrations in J1 |

---

## 1. Purpose

Goal runs factories of agents. Prompts, skills, tool policies, models, contracts and WIP limits all change, and every change can make a station worse in ways nobody sees until rework piles up at the constraint. Jonah exists to:

1. **Measure** quality at three levels: each agent, each station (gates and checks), each factory (flow and outcomes).
2. **Catch regressions before they ship** (offline suites on pull requests and new models) and **drift after they ship** (online sampling of production runs).
3. **Turn failures into tests**: every escaped defect, andon pull and human override can become an eval case.
4. **Propose fixes with evidence**: diagnose a failure mode, draft a diff to factory files, prove offline that it wins without regressions, and hand it to the `goal` factory as a pull request with a human gate. Then watch it, and roll it back if production disagrees.

Non-goals: Jonah does not replace the human gates, does not train models, does not edit Go or TypeScript code, and never changes graders, cases, holdout sets or its own policy.

## 2. The framework, mapped to Goal

Source: Anthropic, *Demystifying evals for AI agents* (2026-01-09), <https://www.anthropic.com/research/demystifying-evals-for-ai-agents>. Paraphrased; Goal names on the right.

| Article concept | What it means | In Goal |
| --- | --- | --- |
| Task (test case) | One test with defined inputs and success criteria | **Eval case**: one YAML file (`EvalCase`) with input, fixture, reference and expectations |
| Trial | One attempt at a task; run several because outputs vary | **Trial**: one fresh agent container running one case version; `eval_trials` row |
| Grader | Logic that scores one aspect; a task can have several, each with checks | **Grader** in `evals.yaml` (code, judge or human); result in `grader_results` |
| Transcript (trace, trajectory) | Full record of a trial: messages, tool calls, reasoning, intermediate results | **Transcript**: the worker's stream-json, stored compressed in `transcripts`, joined to OTel spans |
| Outcome | Final state of the environment, not what the agent says happened | Station **output** that passed its contract, plus workspace or recorded-tool state (tests pass, no write calls) |
| Evaluation harness | Runs tasks concurrently, records steps, grades, aggregates | **Jonah harness** inside `goald`, using the same Runner as production |
| Agent harness (scaffold) | The system that lets the model act; evaluating an agent evaluates harness + model | **Worker** (Claude Agent SDK) + agent prompt + skills + tool policy + MCP gateway. Evals run this exact stack. |
| Evaluation suite | Tasks sharing a goal | **Suite** in `evals.yaml`, per agent or station, purpose capability or regression |
| Capability vs regression evals | Capability starts low (a hill to climb); regression stays near 100% and a drop means breakage; mature capability cases graduate | `purpose: capability` tracks progress, `purpose: regression` gates merges and proposals; graduation moves a case between them |
| pass@k / pass^k | At least one success in k trials vs all k succeed | pass@k for specs and plans (one good attempt is enough, rework exists); **pass^k for anything customer- or production-facing** (triage, gates) |
| Grader types | Code (fast, objective, brittle), model (flexible, needs calibration), human (gold standard, slow) | Code graders in Go first; Claude judges for one criterion each, calibrated on human labels; humans label in the UI queue |
| Grade outcomes, not paths | Do not require an exact tool sequence; allow partial credit | Graders check outputs and state; `tool-calls` only forbids unsafe tools. `score` records partial credit beside binary `passed` |
| Isolation | Fresh environment per trial; shared state causes correlated failures or leaks (an agent read earlier trials' git history) | New container, workspace from a squashed snapshot, gateway in replay mode, no shared volumes, infra errors recorded as `error`, never `fail` |
| Read the transcripts | Scores mean nothing until you know failures are fair | Transcript review is a first-class screen; error analysis precedes every new grader |
| Start from real failures; 20–50 tasks is enough to start | Manual checks, bug reports and support issues become tasks | Seed suites by hand; then andon pulls, gate rejections, overrides and escaped defects become **candidate cases** |
| Unambiguous tasks with reference solutions; 0% across many trials usually means a broken task | Two experts should agree on pass/fail | Every case has a `reference`; an **oracle trial** must pass before activation; zero passes across 10 trials **quarantines** the case |
| Balanced problem sets | Test where a behavior should and should not happen | Each "reject" case has an "approve" twin; each "escalate" case has a "do not escalate" twin |
| Saturation | 100% gives no improvement signal | Saturation flag after 3 runs above 90%; capability suites need new hard cases |
| Calibrate model graders; offer an "unknown" | Structured rubrics, one dimension per judge, human calibration | Binary judges with critique first, `unknown` routed to humans, TPR/TNR on a sealed test split |
| Swiss cheese | Evals, production monitoring, A/B, feedback, transcript review, human studies each catch what others miss | Offline suites + online sampling + flow metrics + human labeling queue; no A/B traffic split (one owner, low volume) |
| Coding / research / conversational agents | Unit tests plus a rubric for coding; groundedness, coverage and source quality for research; state checks and turn limits for conversation | `goal` factory: tests, scope, verification, rubric judge. `sre` factory: groundedness of evidence against recorded tool results, ruled-out alternatives, read-only state check |
| Eval-driven development; shared ownership | Write the eval before the capability; product people contribute tasks via PRs | Specs for new agent behavior come with capability cases; cases are reviewed like code in `factories/<name>/evals/` |

## 3. Evaluation levels

Theory of Constraints sets the order: **evaluate the constraint first**. An hour lost at the drum is an hour lost for the whole factory; an hour saved anywhere else is a mirage. Jonah reads the current constraint from the engine (`flow.constraint: auto` or a fixed station) and:

- runs the constraint's suites first in every schedule and samples its production runs at twice the configured rate;
- ranks signals by their effect on the constraint: rework that flows back into it, defects that reach it, and gate false rejects that waste its capacity;
- only proposes speed or capacity changes (WIP, agents, model upgrades) at the constraint; elsewhere it proposes quality fixes that stop bad work from reaching the drum.

This is Goldratt's five focusing steps applied to quality: **identify** the constraint, **exploit** it (stop wasting its time on rework and false rejects), **subordinate** everything else (WIP and release follow the drum), **elevate** (model or agent count, always high risk and human-approved), then **repeat**, because a fix moves the constraint.

| Level | What is evaluated | Graders and sources | Key metrics |
| --- | --- | --- | --- |
| **Agent** (per agent, per factory) | One agent at one station on frozen tasks: capability and regression suites, k trials per case | Contract, match, evidence grounding, diff scope, verification, tool-calls, transcript budget, command (tests), calibrated judges, human spot checks | pass@1, pass@k, pass^k; cost, turns, latency (p50/p95) per trial; tool-policy violations; contract compliance; unknown-verdict rate |
| **Station** | The station as a quality gate: reviewer agents, human gates and deterministic checks | Offline: balanced known-good / known-bad cases (`gate` grader). Online: later outcomes (human decision at the next gate, escaped defects, reverted merges), plus human spot checks of rejected items | Gate false accept rate, false reject rate (confusion matrix vs later outcome); check catch rate, flake rate, escape rate; rework rate; human override rate |
| **Factory** | The whole line on real work | Computed from `item_events`, `agent_runs`, `work_items`; no trials | Throughput/week, lead time p50/p85, WIP and age, rework rate, first-pass yield, escaped defect rate, cost per verified item, human override rate, constraint utilization; eval spend as cost of quality |

Definitions (all over a rolling `flow.windowDays` window, stored in `metric_snapshots`):

- **First-pass yield** = done items with no gate rejection and no failed check ÷ done items.
- **Rework rate** = `gate_rejected` ÷ (`gate_passed` + `gate_rejected`), per station and overall (the same signal the andon uses).
- **Escaped defect** = a `fix` item (goal) or re-filed incident (sre) linked to a done item within `flow.escapedDefectDays`. Requires a `defect_of` link event (work item J4-2).
- **Cost per verified item** = all run cost on done items, including rework attempts ÷ done items that passed every check and gate. Jonah's own spend is reported next to it, not hidden in it.
- **Human override rate** = human decisions that contradict the preceding agent gate ÷ human decisions after an agent gate.
- **Gate false accept** = agent gate approved, then a human rejected it or a defect escaped. **False reject** = agent gate rejected work that a human, on spot check, would have approved (rejections are sampled into the labeling queue to estimate this; otherwise it is invisible).

## 4. Graders

Order of preference, from the article and the error-analysis skill: **fix the obvious problem first** (a missing instruction, a missing tool) and add an evaluator only for failures you will iterate on; then **code graders**; **judges** only where judgment is unavoidable; **humans** to calibrate and spot-check.

### 4.1 Code graders (Go, in `goald`)

Generic primitives, configured per factory. The prior system had seven domain-specific graders (tool efficiency, verification, review quality, planning, reliability, cost-latency, security); each survives here as a configurable kind, not as code tied to one domain.

| Kind | Checks | Notes |
| --- | --- | --- |
| `contract` | Output validates against the station's contract (JSON Schema) | Same validator as the station check |
| `match` | Value at a JSON Pointer `equals`, is `oneOf`, matches `pattern`, has `min/maxItems` | Expectations come from the case; a missing value counts as empty |
| `evidence` | At least `min` evidence items; with `grounded: true`, every link or command appears in a tool call or tool result in the transcript | Deterministic groundedness for research-like agents |
| `diff-scope` | Changed files are within `allow` + the spec's `touches`, none in `forbid`; files the case lists as `added` exist, `unchanged` files are byte-identical | Outcome check that also stops reward hacking (editing tests) |
| `verification` | The station's check commands (or listed commands) ran **after the last edit** | The prior system's "verification" grader, generalized |
| `command` | Runs a command in the trial workspace after the agent exits; `failToPass` tests must fail on the snapshot and pass after; `passToPass` keep passing | **Hidden tests**: files from `fixture.files` are copied in only at grading time, so the agent cannot see or edit them |
| `tool-calls` | Forbidden tool patterns never used; required tools used; unmatched replay calls counted | Never an exact sequence: grade the outcome, not the path |
| `transcript` | Turns, cost, minutes, tool errors under limits; no secret-shaped strings in outputs | Cost-latency and security graders of the prior system |
| `state` | Files exist or contain patterns in the final workspace | For outcome checks that are not tests |
| `gate` | A gate station's decision equals the case's expected `approve`/`reject` | Station-level suites |

A trial **passes** when every required grader passes (`scoring.mode: all`, the default) or when the weighted score reaches `threshold` (`weighted`). `score` (0–1) records partial credit for diagnosis, never instead of `passed`. A grader that errors (timeout, broken fixture) makes the trial `error`, which is excluded from pass rates and counted separately; ten errors in a run stop it.

### 4.2 Model graders (judges)

Design from write-judge-prompt and validate-evaluator:

- **One failure mode per judge.** No holistic "is this good" judge, no Likert scales. Severity is expressed by separate binary judges.
- **Prompt structure** (four parts): task and the one criterion; precise PASS / FAIL / UNKNOWN definitions taken from error analysis; 2–4 few-shot examples (a clear pass, a clear fail, a borderline) drawn **only from the train split** of human labels; structured output with the **critique before the verdict**. Inputs are wrapped in tags and declared as data, not instructions (prompt-injection defense).
- **Inputs are minimal and declared** in `evals.yaml` (`inputs: [output/hypothesis, output/evidence, transcript.tool_results]`); `goald` extracts and truncates them. Long transcripts are reduced to tool results and the final output.
- **Output**: `{critique: string, verdict: "pass"|"fail"|"unknown"}` via structured outputs (API key or Bedrock) or a JSON-schema-validated single-turn query (subscription, through the worker). Invalid JSON is retried once, then the grader result is `error`.
- **Unknown** never counts as pass. It goes to the labeling queue (`judge-unknown`) and counts as a miss in calibration.
- **Model**: `claude-opus-5-5` at effort `high` by default. Most agents under test run Sonnet or Haiku, so the judge is a stronger tier. Where the agent itself runs Opus (architect, reviewers), self-preference is a real risk; calibration on human labels is the control, and a higher tier is an option (open question 3).
- **Pinning**: the 5.x aliases have no dated snapshot ids, so each `judge_versions` row records the configured alias **and** the model id the API reports. A change in the reported id marks the judge `calibrating` again.
- **Pairwise** judges compare baseline and candidate outputs blind, in both orders; only consistent verdicts count. They are advisory evidence on proposals, never the decision.

**Different model family.** The eval-harness-first skill asks for a judge from a different family than the model under test. Goal is Claude-only by design (one auth path, one quota). We accept this deviation and control it with calibration against human labels, deterministic graders for every critical gate, human spot checks of online verdicts, and judges that stay advisory until calibrated. This is recorded as risk R3.

### 4.3 Judge calibration

1. Collect ~100 human labels for the judge's criterion (target 50 pass / 50 fail; at least `minPerClass` of each), from the labeling queue. Labels are trace-level; one label feeds every judge through its failure-mode tags.
2. Assign each label once, at random and stratified by verdict, to **train** (~15%, few-shot source), **dev** (~45%, iterate) or **test** (~40%, sealed).
3. Iterate the prompt on dev until TPR and TNR stop improving. Inspect every disagreement: false pass means the fail definition is too weak; false fail means the pass definition is too strict.
4. Run **once** on the sealed test split. The database allows one test calibration per judge version (`judge_calibrations_test_once_key`). Decision: both rates ≥ `calibration.gate` → **gating**; ≥ `advisory` → **advisory** (recorded and shown, never part of pass/fail); below → **rejected** (rewrite, decompose, or replace with a code grader).
5. Report online pass rates **bias-corrected** (Rogan–Gladen): θ = (p_obs + TNR − 1) / (TPR + TNR − 1), clipped to [0, 1], with a bootstrap confidence interval. Invalid when TPR + TNR − 1 is near zero.
6. **Recalibrate** when the prompt changes (new version, fresh test labels), the reported model changes, every `recalibrateDays`, or when online spot checks disagree with the judge more than its TNR/TPR predicts (binomial test at α = 0.05).

A suite's `required` judge only gates when its status is `gating`; otherwise it runs as advisory regardless of configuration.

### 4.4 Human graders

Humans label in the Goal UI (section 13). Uses: open coding during error analysis, calibration labels, online spot checks (`spotCheck.rate`), judge `unknown` verdicts, judge/human disagreements, sampled gate rejections (false reject estimate), and triage of candidate cases. Labels are append-only; a correction is a new row.

## 5. Datasets and case lifecycle

### 5.1 Where cases live

| Split | Where | Who reads transcripts | Used for |
| --- | --- | --- | --- |
| `tune` | Git: `factories/<name>/evals/cases/*.yaml` | Humans and Jonah's diagnostician | Error analysis and diagnosis |
| `check` | Git, same folder | Humans only; Jonah sees scores | Every comparison; picks winners |
| `holdout` | **Postgres only** (`eval_cases.storage = 'postgres'`, enforced by a CHECK) | Humans only; Jonah sees only the gate verdict | Final gate of every proposal and monthly audit, at most `holdout.maxRunsPerMonth` runs |

Why git for visible cases: they are reviewed in pull requests, versioned with the prompts they test, diffable, and editable by humans with schema autocompletion. Why Postgres for the holdout: the repository is readable by every agent in the `goal` factory, including the engineers who implement Jonah's own proposals; a holdout in git is not sealed. Every case version that runs is copied into `eval_case_versions` by content hash, so runners in ECS never need a checkout and results always point at the exact text that was run.

### 5.2 Case file format

One file per case, file name = `metadata.name`, `metadata.id` immutable (rename freely). Graders define **how** to check; the case's `expect` block defines **what** to expect, keyed by grader name. A suite selects cases by `purpose`, agent or station, and tags.

```yaml
apiVersion: goal/v1
kind: EvalCase
metadata: { id: 3511f9ff-…, name: investigator-bad-deploy-env-var, tags: [investigator, deploys], origin: { kind: incident } }
spec:
  agent: investigator
  station: investigate
  purpose: capability
  split: tune
  input: { type: incident, title: "ALARM: checkout-api-5xx-rate", body: "…", outputs: { triage: {…} } }
  fixture: { cassette: ./fixtures/acme-checkout-5xx.cassette.jsonl }
  reference: { output: { hypothesis: "…", evidence: [ … ], risk: medium } }
  expect:
    hypothesis: { pattern: "(?i)PAYMENTS_API_URL|payments\\.internal" }
    risk: { oneOf: [medium, high] }
```

Fixtures:

- **Repository snapshots** (`goal` factory): `fixture.repo.ref` names a tag; the harness exports that tree as a single squashed commit (no history, no remotes, no other branches), applies an optional `patch`, and serves it by hash through the gateway. Hidden tests (`fixture.files`) are copied in only after the agent exits.
- **Cassettes** (`sre` factory): JSONL records of `{connection, tool, match, response}`. In replay mode the gateway answers matching calls from the cassette (regex or exact on arguments, whitespace-normalized). Unmatched `kb_search` returns no results; other unmatched calls return a "no recording" error, are counted, and above a threshold flag the case for coverage review rather than failing the agent. Write tools are never served.
- **Recording**: `goal eval record <run>` turns a production run's tool calls into a cassette, with secrets and personal data scrubbed, for review in a pull request.

### 5.3 Lifecycle

```
             ┌──────────── human promotes (PR) ───────────┐
candidate ──▶ draft ──(oracle passes, null fails)──▶ active ──(3 runs ≥ 90%)──▶ graduated to regression
   ▲                                                  │
   │                                       zero passes in 10 trials,
production failures                        or flake rate > 30%
(andon, gate rejections,                              ▼
 overrides, escaped defects,                     quarantined ──(fixed)──▶ active
 online fails, kaizen)                                │
                                                      ▼
                                                   retired
```

- **Sources**: hand-written seeds (start with 20–50 per factory from what is checked by hand today); production failures, which Jonah turns into `candidate` rows with the item and run attached (`goal eval export-case GOAL-42` does the same by hand, as the prior system did for its ~12 hand-labeled incidents); synthetic cases from `cases.sources[kind=synthetic]` dimensions.
- **Synthetic generation** (generate-synthetic-data): 3–5 dimensions that target expected failures, 20 tuples drafted and reviewed by a human first, then more tuples by Claude, then a **separate** prompt turns each tuple into a realistic input. A human filters them; survivors become `draft` files in a PR.
- **Activation**: a draft becomes active only when its `reference` passes every grader (oracle) and an empty or constant output fails them (null). This catches broken graders and ambiguous tasks before they cost trials.
- **Balance**: every gate suite holds both approve and reject cases; every severity suite holds both escalate and do-not-escalate cases.
- **Saturation**: a capability suite above `saturation.passRate` for `saturation.runs` runs is flagged; its cases are proposed for graduation and the dashboard asks for harder cases.
- **Discrimination**: after each comparison, cases that every arm always passes or always fails are marked non-discriminating (from skill-creator's analyst pass); high-variance cases are reviewed for flakiness.
- **Ownership**: humans own every case file and the holdout. Jonah may create `candidate` rows and suggest failure-mode names; it can never write files under `evals/` (section 7.4).

## 6. Offline, online and human evaluation

### 6.1 Offline (suites on frozen cases)

Triggers (`suite.triggers`): `schedule` (cron inside `lane.windows`), `pull-request` (a PR that changes the factory's agents, skills, contracts or `factory.yaml`), `model-change` (a new model id reported for an agent's alias), `proposal` (Jonah verification), `andon`.

Run protocol:

1. **Plan**: select cases, estimate cost (cases × trials × arms × median historical cost, plus judges), refuse if over budget, and for a new or changed suite run `dryRunCases` once first (agent-evaluation skill: catch broken tools before a full run).
2. **Enqueue** trials with a random `order_key` so baseline and candidate interleave in time (no time-of-day confound).
3. **Execute** each trial in a fresh container through the `Runner` port, worker in `eval` mode, gateway in replay mode.
4. **Grade**: post-agent graders in the same container (commands, hidden tests), code graders in `goald`, judges in the judge lane.
5. **Aggregate** into `eval_runs.summary`; any number shown is recomputable from `eval_trials` and `grader_results`.

### 6.2 Online (sampled production runs)

- On run completion every instance receives the NOTIFY; the sampling decision is deterministic (`hash(run_id) < rate`, plus outlier and failure rules) and the insert is `ON CONFLICT DO NOTHING`, so N instances sample each run once. A catch-up job under an advisory lock covers missed notifications.
- Strategies (error-analysis skill): `random` (always on, finds unknown failures), `failure` (rejected, reworked, andon), `outlier` (turns, cost, latency extremes), `uncertainty` (judges disagree or answer unknown), `stratified` (per lane or work type).
- Graders: the factory's `online.graders` (code graders and calibrated judges; no command graders, the workspace is gone). `spotCheck.rate` of judged samples also go to a human.
- **Drift**: per agent and grader, compare the last `drift.window` samples with the reference window (the 28 days before the last config change). Open a signal when the bias-corrected pass rate drops by more than `maxDrop` and a two-proportion test rejects equality at `alpha`, with at least `minSamples` in each window. Cost, turns and latency use an EWMA chart against the same reference.

### 6.3 Human review

The labeling queue (`review_queue_items`) is fed by every source above. **Error analysis sessions** follow the error-analysis skill: Jonah samples ~100 traces (random + stratified + failures), the human open-codes the first 30–50 (first thing that went wrong, observed not explained), Jonah proposes 5–10 clusters, the human confirms or edits them into `failure_modes`, then labels the rest. Stop when 20 consecutive traces reveal no new failure mode. Re-run after every significant change (new model, prompt rewrite, incident).

## 7. Self-remediation

### 7.1 The loop

```
 detect ──▶ diagnose ──▶ propose ──▶ verify offline ──▶ ship (goal factory PR) ──▶ watch ──▶ keep
   ▲          error        one diff     baseline vs        human approves            │
   │          analysis     per          candidate,         and merges               ▼
   │          over         hypothesis   regression +                          rollback (pin)
   └─ signals transcripts               targeted + holdout                    + revert item
```

1. **Detect** — `remediation_signals` from: online drop, regression suite failure, andon pull, kaizen report finding, gate accuracy drift, cost/turn drift, model change, flow target miss, or a human ("ask Jonah"). One open signal per fingerprint.
2. **Diagnose** — a Jonah task (Claude, in the eval lane) reads up to 100 transcripts: failing ones and some passing ones for contrast, only from production samples and the `tune` split. It records the **first** thing that went wrong in each, clusters them against the confirmed taxonomy (new modes are `proposed` until a human confirms), and **attributes** each cluster to a lever, with the questions Jonah would ask:

   | Lever | Typical evidence | Question |
   | --- | --- | --- |
   | `prompt` | The instruction is missing or ambiguous; behavior differs by phrasing | "What did we never tell the agent?" |
   | `skill` | The agent searched for a procedure, or used an outdated one | "Where would an expert look this up?" |
   | `tool-policy` | Denied-tool attempts, workarounds, missing tool | "What was it trying to do that it could not?" |
   | `connection` | MCP errors, timeouts, empty results from a source | "Did it fail, or did the world fail it?" |
   | `model` | Failure began with a model change, or persists across prompt variants at this tier | "Is this beyond this model?" |
   | `flow-config` | Starved or flooded constraint, queues, WIP limits, buffer misses | "Where is the work waiting, and why?" |
   | `contract` | Contract violations, or downstream stations missing a field | "What does the next station need that it does not get?" |
   | `knowledge` | The answer exists in the KB but was not retrieved, or does not exist | Routed to the librarian, not a config change |
   | `grader` / `case` / `harness` | The output was right and the grader said no; the task was ambiguous; the environment broke | **Never Jonah's to fix**: a human item is opened |

   The output is a `diagnosis` (failure mode, frequency, impact on the constraint, lever, hypothesis, quoted evidence). Following the error-analysis skill, Jonah first asks whether a plain fix exists (a missing instruction) before suggesting a new grader.
3. **Propose** — one change per hypothesis (one behavior, possibly several hunks), as a unified diff against the current commit, limited to the allowed files (7.4). Up to `maxCandidatesPerSignal` alternatives. Each is checked before any trial runs: `goal validate` on the candidate tree, path allowlist, size limits, and a **leak check** that rejects a diff sharing long word sequences with any case input or reference (memorizing cases instead of fixing behavior).
4. **Verify offline** — baseline (current config) and candidate run interleaved on: the targeted cases (the failure mode's `tune` and `check` cases), every regression suite in `verify.guardSuites`, with `verify.trials` trials. Decision rule in section 11.4. If it passes, the candidate runs once against the **holdout**; Jonah learns only pass or fail. Every comparison is stored (`eval_comparisons`) and is immutable once decided.
5. **Ship** — `goald` creates a work item in the `goal` factory with the spec pre-filled (what changes, why, the evidence summary and links) and enters it at `approve-spec`, the human gate. After approval the build station applies the diff **verbatim** (a check compares the branch diff with `diff_sha256`), runs the usual checks, the reviewer reviews, and a human merges. The DB refuses a proposal past verification without a passing comparison, and refuses `shipped` without a work item.
6. **Watch** — after merge, `remediation_rollouts` watches the next `watchItems` items or `watchDays` days: targeted online pass rate, regression suites, rework rate, cost per item, andon.
7. **Roll back** automatically when a `rollbackOn` condition holds: `goald` **pins** the factory to the previous definition (`factory_config_pins`, effective on every instance immediately), opens an **expedite** revert item in the `goal` factory so git catches up, and reopens the signal with the new evidence. A human releases the pin when the revert merges.

Concurrency and attribution: one proposal in flight per factory (unique index), a `cooldownDays` pause per agent or station after a rollout, and all changes go through `factory_definitions`, so every production run and trial records the exact config it used.

### 7.2 Autonomy levels

| Level | Jonah may | Human does |
| --- | --- | --- |
| **L0 Report** | Run suites, online evals and flow metrics; write the Jonah section of the kaizen report | Everything else |
| **L1 Diagnose** (default for a new factory) | Open signals, run error analysis, draft diagnoses and diffs, suggest candidate cases | Reads the proposal; clicks "Verify" to spend eval budget; decides whether to ship |
| **L2 Propose with evidence** (default once ready) | Everything in L1, plus run verification within budget and open the work item in the `goal` factory | Approves at `approve-spec`, reviews, merges |
| **L3 Auto-merge low risk** | Everything in L2, plus merge **low-risk flow-config** changes (`autoMerge` steps: WIP ±1, buffer ±2, agents ±1) after all checks, evidence and holdout pass | Can revert any time; reviews a weekly digest |

Readiness: Jonah runs one level below the configured `autonomy` until `readiness` holds (enough regression and holdout cases, every judge used by a regression suite gating). Risk classes are fixed in code, never configurable upward: **low** = flow-config within `autoMerge` steps; **medium** = prompt, skill, tool-policy narrowing; **high** = model, tool-policy widening, connection, contract, budgets. L3 never auto-merges medium or high risk. A human override (`jonah_overrides`, `max_autonomy`) caps any factory instantly; `L0` is the kill switch. Jonah cannot raise its own level: the level lives in `evals.yaml`, which Jonah cannot edit, and overrides require a `human:` actor.

### 7.3 Goodhart protection

When a measure becomes a target it stops being a good measure. Controls:

- **Sealed holdout** in Postgres, run only as a final gate and monthly audit, at most `maxRunsPerMonth` times, verdict only. Repeated adaptive queries to a holdout leak it; the cap bounds that.
- **Three splits** with a wall: Jonah reads `tune`, scores on `check`, gates on `holdout`. A gap that widens between tune and check pass rates is reported as overfitting.
- **Many measures, not one**: a proposal must win on its target, not regress any guard suite, stay within cost, and then not hurt production flow metrics, which Jonah cannot influence (human overrides, escaped defects, lead time).
- **Graders, judges, cases and policy are read-only to Jonah**, enforced by path allowlist in the app and the database.
- **Leak check** on diffs, and the instruction to describe failure behavior, never case content (eval-hillclimb guidance).
- **Fresh cases** keep arriving from production failures, so the suite tracks reality rather than history.
- **Calibrated judges and human spot checks**, so judge blind spots are measured rather than exploited.

### 7.4 Guardrails (enforced, not advisory)

| Guardrail | Enforcement |
| --- | --- |
| Jonah edits only `factories/<name>/factory.yaml`, `agents/*.md`, local `skills/*/SKILL.md`, `contracts/*.schema.json` | `jonah_paths_allowed()` CHECK on `remediation_proposals.files`; same rule in Go; build-station check compares the PR diff to the approved one |
| Never edits graders, cases, judges, holdout, schemas, migrations, Jonah code or policy | Not in the allowlist; `evals/` additionally protected by CODEOWNERS (human review required) |
| Holdout never used for tuning | Holdout content only in Postgres; Jonah's transcript tool serves `tune` and production only; DB role for Jonah tasks has no `SELECT` on holdout rows (row-level security); verdict-only API |
| No change without eval evidence | CHECK + FK cascade: past `verifying`, a proposal must reference a comparison whose decision is `pass`; comparisons are immutable once decided |
| One change at a time | Unique index: one proposal in flight per factory; cooldown per subject |
| Budget and rate limits | `remediation.limits` (per proposal, per month, proposals per week, candidates per signal) checked before any trial is enqueued; `spent_usd ≤ budget_usd` CHECK |
| Human decides | L1/L2 by default; L3 only low risk; failure modes confirmed only by `human:` actors (CHECK) |
| Everything auditable | `jonah_audit` append-only (trigger), every decision with actor and data; plus the `goal` item's own `item_events` |

## 8. Configuration

Each factory may have `factories/<name>/evals/`:

```
evals/
  evals.yaml            suites, graders, judges, lane, online, flow targets, remediation policy
  cases/*.yaml          one EvalCase per file (tune and check splits)
  judges/*.md           judge prompts
  fixtures/             cassettes (*.cassette.jsonl), reference patches, hidden test files
```

Both kinds validate against `schema/evals.schema.json` (`kind: Evals` or `kind: EvalCase`), so the YAML editor in the UI and in editors gives autocompletion. `goal validate` will also check the cross-references a schema cannot express (J1-2): `metadata.factoryId` equals the factory's id; graders, judges, contracts, agents and stations exist; prompt, cassette, patch and file paths exist; cassette lines parse and name real connections; case ids are unique and file names equal `metadata.name`; every `expect` key is a grader used by a suite that selects the case; every suite selects at least one case.

Excerpt (`factories/sre/evals/evals.yaml`):

```yaml
apiVersion: goal/v1
kind: Evals
metadata: { factoryId: 75dfe662-f947-401b-90af-5b396d51310a }
spec:
  lane: { wip: 2, maxShare: 0.15, windows: ["21:00-06:00"], yieldToProduction: true, budget: { dailyUsd: 6 } }
  cases:
    sources:
      - { kind: files, path: ./cases/*.yaml }
      - { kind: postgres, splits: [holdout] }
  graders:
    - { name: evidence-grounded, kind: evidence, pointer: /evidence, min: 2, grounded: true }
    - { name: read-only, kind: tool-calls, forbiddenTools: ["Bash(aws * delete*)", "Bash(aws * put*)"] }
    - { name: diagnosis-claims-supported, kind: judge, judge: diagnosis-claims-supported }
  suites:
    - name: triager-regression
      level: agent
      agent: triager
      purpose: regression
      trials: 3
      graders: [triage-contract, triage-type, triage-severity, triage-size, triage-next, budget]
      gate: { metric: "pass^k", k: 3, min: 0.9 }
      triggers: [schedule, pull-request, model-change, proposal]
  remediation:
    autonomy: L1
    verify: { trials: 5, alpha: 0.05, minEffect: 0.15, margin: 0.05, costTolerance: 0.15, holdout: required }
    rollout: { watchItems: 15, watchDays: 14, rollbackOn: { regressionFailure: true, andon: true, onlineDrop: 0.12 } }
```

Why a separate file, not a section of `factory.yaml`: it has a different owner (Jonah may propose changes to `factory.yaml`, never to evals), a different review path (CODEOWNERS), and it keeps `factory.schema.json` unchanged.

## 9. Data model

Draft: [`schema.sql`](schema.sql). It applies after `migrations/00001_init.sql` and is split into numbered migrations in J1. All names from YAML are `text`; engine vocabularies are `text` + CHECK; every foreign key is indexed (verified with a catalog query); keys are UUIDv7 generated in Go.

| Table | Purpose |
| --- | --- |
| `factory_definitions` | Every loaded or candidate `factory.yaml`, by sha256; attribution and rollback |
| `agent_runs.definition_sha256` (new column) | Which config produced each production run |
| `eval_configs` | Every loaded `evals.yaml`, by sha256 |
| `eval_suites` | Current suites per factory (level, purpose, agent, station) |
| `eval_cases` | Case identity, split, state, origin (item, run), storage (git or postgres; holdout must be postgres) |
| `eval_case_versions` | Immutable, content-addressed case content; trials point here |
| `eval_fixtures`, `eval_case_version_fixtures` | Cassettes, snapshots and files by hash, inline or by URI |
| `eval_runs` | One arm (baseline or candidate) of one suite run, with purpose, config shas, cost, cached summary |
| `eval_trials` | One attempt: status, passed, score, error class, usage, lease; a partial unique index enforces the eval lane's WIP |
| `transcripts` | Compressed stream-json for a production run, a trial or a Jonah task |
| `online_samples` | Production runs selected for online grading |
| `grader_results` | One verdict per grader per trial or sample, with rationale, evidence, judge version, cost |
| `judges`, `judge_versions`, `judge_calibrations` | Judge status, exact prompt and model, TPR/TNR per split; sealed test once per version |
| `failure_modes` | The taxonomy from error analysis, with its lever; confirmed only by humans |
| `review_queue_items`, `human_labels` | Labeling queue and append-only trace-level labels with calibration split |
| `remediation_signals` | Detected problems, deduplicated by fingerprint |
| `jonah_tasks` | Diagnose and propose tasks run through the Runner |
| `diagnoses` | Failure mode, lever, hypothesis, evidence, constraint at the time |
| `remediation_proposals` | The diff, files (allowlisted), risk, autonomy, budget, state, link to the `goal` work item |
| `eval_comparisons` | Baseline vs candidate (+ holdout) decision with stats; immutable once decided |
| `remediation_rollouts` | Watch window and outcome after merge |
| `factory_config_pins` | Immediate rollback to a previous definition, released only by a human |
| `jonah_overrides` | Human cap on autonomy (kill switch) |
| `metric_snapshots` | Rollups per level, subject, metric and window, with CI |
| `quota_events` | Rate-limit and usage-limit events per lane |
| `jonah_audit` | Append-only log of every decision; NOTIFY `jonah_events` for the UI |
| `model_usage_daily` (view) | Spend per lane per day: production, eval, judge, jonah |

## 10. Runtime architecture

Jonah follows the repository's hexagonal rules (ADR 0003): the core depends only on `domain` and `ports`; everything external is an adapter; wiring lives in `cmd/goald/main.go`; `goald` is stateless and any instance can die at any time.

```
                         ┌──────────────────────────────── goald (N instances) ───────────────────────────────┐
  UI (embedded or Vercel)│ adapters/http  /v1/jonah/*  (api/openapi.yaml)      SSE /v1/events (jonah_events)   │
  goal CLI ─────────────▶│        │                                                                            │
                         │        ▼                                                                            │
                         │  internal/jonah  ── scheduler (advisory locks) · planner · grading · online sampler │
                         │        │            drift · remediation state machine · policy · rollout watcher   │
                         │        │  uses: internal/jonah/stats (pure)  internal/jonah/grade (code graders)    │
                         │        ▼                                                                            │
                         │  ports: Store  Runner  ModelAuth  Clock  Secrets  + Judge (new)                     │
                         │        │                                                                            │
                         │  adapters: postgres · docker | ecs · gateway (MCP; replay/record; fixtures by hash) │
                         │            judge: anthropic (API key) | bedrock | worker (subscription)            │
                         └────────────────────────────────────────────────────────────────────────────────────┘
                                   │ Runner.Start(envelope{mode: eval|judge|jonah})
                                   ▼
                         worker container (same image as production): agent run → grading phase → upload
```

Packages (each with one responsibility and a caller):

| Package | Responsibility |
| --- | --- |
| `internal/domain` (+ eval types) | Case, Suite, Trial, Verdict, Signal, Proposal, Autonomy and Risk rules; pure |
| `internal/config` (+ `evals.go`) | Load and validate `evals.yaml` and case files against the embedded schema plus cross-references |
| `internal/jonah` | The core: plans runs, schedules, grades, samples, detects drift, runs the remediation state machine, applies policy |
| `internal/jonah/stats` | pass@k, pass^k, Wilson and bootstrap intervals, paired and non-inferiority tests, Rogan–Gladen, Holm. Pure and table-tested |
| `internal/jonah/grade` | Code graders as pure functions over (output, transcript summary, workspace report) |
| `internal/ports` (+ `Judge`, Store methods) | `Judge.Grade(ctx, JudgeRequest) (Verdict, Usage, error)`; eval queries on `Store` |
| `internal/adapters/postgres` | Eval tables, `SKIP LOCKED` trial queue, advisory-lock jobs |
| `internal/adapters/gateway` | Replay and record modes, fixture and snapshot serving by hash, eval-mode tool policy |
| `internal/adapters/judge` | `anthropic` (Messages API, structured outputs, prompt caching, Message Batches for offline grading), `bedrock`, `worker` (subscription) |
| `cmd/goal` | `goal validate` (evals too), `goal eval run|compare|record|export-case|calibrate` |
| `worker/` | Envelope `mode: eval`: no push, gateway URL in replay mode, then a separate grading phase (hidden files, commands) the agent never sees; `mode: judge`: one turn, no tools, JSON schema output |
| `web/src/features/jonah/` | Screens in section 13, through `packages/api-client` |

Stateless and cloud-ready:

- **No local disk.** Cases, fixtures, snapshots and transcripts live in Postgres (or object storage by URI for large blobs, served through the gateway by hash). A trial container needs only its envelope and the gateway URL.
- **Queues.** Trials, online samples and Jonah tasks are claimed with `FOR UPDATE SKIP LOCKED`; leases expire and requeue. The eval lane's WIP is a partial unique index on `(factory_id, slot)` for running trials, so N instances cannot exceed it.
- **Singletons.** Suite schedules, drift detection, metric rollups, rollout watching, catch-up sampling and retention each run under `pg_try_advisory_lock(hashtext('jonah:<job>:<factory id>'))`.
- **Realtime.** `jonah_audit` inserts NOTIFY `jonah_events`; each instance LISTENs and fans out to its own SSE clients. On Vercel the UI is static and talks to goald on AWS; nothing in Jonah runs in a Vercel Function.
- **Same Runner.** Trials, judges in subscription mode and Jonah tasks are `Runner.Start` calls: Docker on a laptop, ECS `RunTask` on AWS. Containers reach only the gateway and the model endpoint.
- **Idempotency.** Every step is keyed (trial id, grader name; sample id, grader name; proposal state transitions with optimistic checks), so a crash mid-step is safe to retry.
- **API contract.** Jonah endpoints and event payloads are added to `api/openapi.yaml` and `api/events.schema.json` first; Go server and TS client are generated.

## 11. Metrics and statistics

### 11.1 Per-case and per-suite estimators

With n trials of a case and c passes (unbiased estimators):

- pass@k = 1 − C(n − c, k) / C(n, k) (requires n ≥ k)
- pass^k = C(c, k) / C(n, k) (unbiased for p^k of that case; averaging per case avoids pretending all cases share one p)
- Suite metric = mean over cases (each case weighs the same, however many trials it ran).

Errors (`status = error`) are excluded from n and reported separately; a run with more than 10% errors is not used for decisions.

### 11.2 Intervals

- One proportion (a case, a judge's TPR): Wilson score interval.
- Suite-level: cluster bootstrap (resample cases, then trials within each case), 2,000 resamples, 95% percentile interval. Trials within a case are correlated; treating them as independent understates uncertainty.
- Online corrected rates: Rogan–Gladen with bootstrap over the judge's test confusion matrix and the samples.

### 11.3 How many trials and cases

The noise floor of a pass rate is roughly 1/√(cases × trials) (claude-api eval guidance). A difference smaller than the floor is noise.

| Cases × trials per arm | ≈ 95% half-width | Use |
| --- | --- | --- |
| 10 × 5 | ±14 points | Targeted cases for one failure mode: only large effects |
| 20 × 5 | ±10 points | Targeted cases, typical proposal |
| 30 × 3 | ±10 points | Regression suite, nightly |
| 50 × 3 | ±8 points | Regression suite before L2 |
| 100 × 3 | ±6 points | Comparing models |

Minimums: regression suites k = n = 3 (pass^3); capability suites n = 5 (pass@1 … pass@5); comparisons n = `verify.trials` (≥ 3, default 5). Paired designs (same cases in both arms) tighten these further. Before verification Jonah computes the noise floor for the targeted cases and refuses to spend budget when `minEffect` is below it (eval-hillclimb: prove the eval can see the win).

### 11.4 Decision rule for a proposal

Let d_i = candidate pass rate − baseline pass rate on case i (paired).

1. **Target**: on the targeted cases, mean(d) ≥ `minEffect` and the lower bound of the one-sided (1 − α) paired-bootstrap interval > 0. A sign test over cases must agree.
2. **No regressions**: on every guard suite, the lower bound of mean(d) > −`margin` (non-inferiority), and no case tagged `critical` drops from pass^k = 1.
3. **Cost**: the upper bound of mean cost ratio ≤ 1 + `costTolerance`; turns and latency likewise reported.
4. **Holdout**: non-inferiority on the holdout (lower bound > −`margin`). Jonah receives pass or fail only.
5. **Multiplicity**: with m candidates for one signal, α is Holm-adjusted across them.

Inconclusive (interval spans the threshold) is not a pass; Jonah may ask for more trials once, within budget, and otherwise reports "no evidence of improvement". Stopping early on a lucky streak is not allowed: trial counts are fixed before the run.

### 11.5 Flow statistics

Lead time is reported as p50/p85/p95 with bootstrap intervals; throughput per week with a Poisson interval; rates (rework, first-pass yield, override) with Wilson intervals. Rollout watch uses the same tests as drift, comparing the watch window with the 28 days before the merge.

## 12. Quota and cost

Evals compete with production for the same model capacity. Applying the Theory of Constraints to Goal itself: **model capacity is a constraint resource, and the eval lane is subordinated to it**.

| Lane | Priority | Limit |
| --- | --- | --- |
| production | 1 | station WIP and `runtime.budget` |
| judge (online grading) | 2 | `online.sample.maxPerDay`, counts against `lane.maxShare` |
| eval (offline trials) | 3 | `lane.wip`, `lane.windows`, `lane.budget`, `lane.maxShare` |
| jonah (diagnose, propose) | 4 | `remediation.limits`, counts against `lane.maxShare` |

Rules:

- **Yield to production**: a trial does not start while any agent station has queued work and a free slot. Running trials finish (no kill), unless an expedite item is waiting.
- **Windows**: scheduled suites start only inside `lane.windows` (overnight by default); proposal and PR runs may start any time but still yield.
- **Share**: eval + judge + Jonah spend over a rolling 7 days ≤ `lane.maxShare` of the factory's model spend (`model_usage_daily`). The planner refuses runs that would exceed it, the daily or monthly budget, or the per-proposal budget, before enqueueing.
- **Backoff**: a 429, overload or "usage limit reached" on any lane writes `quota_events`; the eval lane pauses for `retry_after` (or 15 minutes) and the affected trials are requeued as `error: rate-limited`, never failures.
- **Cheap first**: dry run of 3 cases; deterministic graders before judges (no judge call when a required code grader already failed); prompt caching of judge prefixes; Message Batches (API key) for offline judge calls that can wait.

By auth mode:

| Mode | Where | What is limited | How Jonah meters |
| --- | --- | --- | --- |
| `claude-subscription` | Laptop, one person | Rate-limit windows and weekly usage of the plan, shared with the owner's interactive use | Estimated dollar-equivalent from OTel cost events; `quota_events` from rate-limit responses; conservative `maxShare` (0.15) and night windows |
| `anthropic-api-key` | AWS or Vercel deployments | Real spend and per-minute rate limits | Exact cost from usage; budgets in USD; Batches API for offline judges |
| `bedrock` | AWS | Real spend, account quotas per model and region | Exact cost; Bedrock batch inference for offline judges where available |

Starter estimate for the `goal` factory: architect regression (30 cases × 3 trials, ~$0.40 each) ≈ $36 per run; engineer regression (20 × 3, ~$3) ≈ $180 per run, so it runs weekly and proposals use targeted subsets; review gate (20 × 3, ~$0.60) ≈ $36. With `dailyUsd: 8` the nightly schedule rotates suites rather than running all of them each night.

## 13. UI

Screens live in `web/src/features/jonah/` and follow `design-system/goal/MASTER.md`: Inter with optical sizing and tabular numerals, the user palette, radii 8–16 px, solid fills only (no gradients), no pink. Color carries meaning, never alone: every state also has an icon and a label.

| Token | Use in Jonah |
| --- | --- |
| `--run` (teal) | Pass, actual value, candidate improvement (hatched teal bars) |
| `--rework` (rust) | Fail, regression, rollback, drift alert (replaces red) |
| `--amber` | Only the constraint station, wherever it appears |
| `--you` (violet) | Waiting on a human: labels to give, proposals to approve |
| `--ok` (green) | Healthy suite, kept rollout |
| hatched gray | Target or threshold |

1. **Factory evals overview** (`/jonah/:factory`): header with the constraint (amber) and its quality; large-numeral tiles for throughput, lead time p85, first-pass yield, rework rate, escaped defects, cost per verified item, override rate, each against its target (hatched gray) with signed deltas; suite health table (suite, purpose, pass@1, pass^k, Δ vs last run with interval, saturation, last run); online drift sparklines per agent; a **Needs you** list (labels, calibrations, proposals awaiting approval, open signals).
2. **Agent page**: pass@k vs pass^k curves for k = 1…5 per suite; cost, turns and latency distributions; failure-mode breakdown (bias-corrected online rates); case table with one dot per trial (teal pass, rust fail, outlined error).
3. **Station page**: gate confusion matrix (offline and online), false accept and false reject with intervals; checks with catch, flake and escape rates.
4. **Run and comparison**: paired per-case dots baseline → candidate, effect with interval against `minEffect`, guard suites with non-inferiority bars, cost ratio, holdout verdict (pass/fail only), and links to transcripts.
5. **Transcript review and labeling queue** (build-review-interface): one trace at a time, full transcript with tool calls linked to their results, system prompt and long tool output collapsed, markdown rendered, code highlighted, JSON pretty-printed; case input, reference and rubric in a side panel; Pass / Fail / Defer with a notes field ("first thing that went wrong"), failure-mode tags once a taxonomy exists; keyboard 1 / 2 / D / U and arrows; progress "12 of 87"; auto-save. Judge verdicts are hidden until the human labels, to avoid anchoring, then shown with the disagreement highlighted. Output is sanitized; no remote images.
6. **Judge calibration**: per judge version, TPR and TNR on dev and test with intervals, status, disagreement table (input, human, judge, critique), and the action to start a new version.
7. **Remediation proposals**: Jonah's diagnosis written as questions and answers, quoted evidence with links, the diff (Monaco diff view), risk and autonomy level, budget spent, the comparison summary, and actions: verify (L1), approve to ship (opens the `goal` item), reject with reason, more trials. After merge: the rollout timeline with each rollback condition and its current value.
8. **Jonah settings** (read-only view of `evals.yaml` policy) with the human override control (cap autonomy, kill switch).

Under 768 px the Jonah tab shows the Needs-you list and the labeling queue first; every action has a 44 px target.

## 14. Security

- **Least privilege.** Jonah tasks run in containers with read-only tools: a builtin `jonah` MCP connection that serves production samples and `tune`-split transcripts and nothing else. No git, no GitHub token, no network beyond the gateway. Proposals are created by `goald`; the `goal` factory's engineers apply them under that factory's own PAT and tool policy.
- **Eval containers** get no production credentials: the gateway replays cassettes and never forwards to real systems in eval mode; `aws`-type connections have no credentials in eval mode at all.
- **Prompt injection.** Transcripts, tool results and case inputs are untrusted. Judges and the diagnostician receive them inside tagged blocks declared as data; their outputs are schema-validated; Jonah's authority is bounded by the allowlist, the DB constraints and the human gate, not by its prompt.
- **Secrets and personal data.** The gateway keeps credentials out of agent context; transcripts are additionally scanned at ingest for secret patterns and personal data and redacted (`transcripts.redacted`). Cassettes recorded from production are scrubbed and reviewed in a PR. The `transcript` grader fails trials that print secret-shaped strings.
- **Holdout protection.** Row-level security on `eval_cases`/`eval_case_versions` for the role Jonah's queries use; verdict-only API; access to holdout content is logged in `jonah_audit`.
- **Encryption and retention.** RDS encryption with KMS in AWS; production transcripts expire after 90 days unless attached to a case, label or comparison; eval evidence is kept.
- **Audit.** `jonah_audit` and `eval_case_versions` are append-only by trigger; comparisons are immutable once decided; overrides and pin releases require a `human:` actor.
- **Supply chain.** Fixtures and snapshots are content-addressed; skills referenced by proposals must already be pinned in `skills-lock.json` (Jonah cannot add a new external skill: that is high risk).

## 15. Risks

| # | Risk | Mitigation |
| --- | --- | --- |
| R1 | Goodhart: Jonah tunes prompts to the cases | Section 7.3; holdout cap; leak check; production flow metrics as the final judge |
| R2 | Small samples produce false wins | Fixed trial counts, paired bootstrap, noise-floor check before spending, Holm correction, inconclusive ≠ pass |
| R3 | Same-family judges share blind spots (Claude judging Claude) | Calibration on human labels, deterministic graders on critical gates, human spot checks, advisory until gating |
| R4 | Evals starve production of quota, or annoy the owner on a subscription | Lowest lane priority, yield rule, windows, `maxShare`, backoff on rate limits |
| R5 | Flaky environments make failures correlated | Fresh containers, squashed snapshots, replay gateway, `error` ≠ `fail`, quarantine on flake rate |
| R6 | Cassettes go stale as real systems change | Unmatched-call coverage reports; re-record from recent runs; cases tied to recorded dates |
| R7 | Attribution confounded by concurrent changes or model updates | One proposal in flight; cooldown; config sha on every run; model id recorded per trial; re-baseline on model change |
| R8 | Saturated suites hide progress | Saturation flag, graduation, steady inflow of production failures |
| R9 | Reviewer burden: too many labels and proposals | Weekly proposal cap; Needs-you list ordered by constraint impact; error analysis in bounded sessions |
| R10 | Complexity before value | Phases deliver standalone value: J1 is just "run a suite", J2 adds CI value; remediation comes last |
| R11 | Subscription token terms and limits | Subscription only for one person on a laptop (README); hosted deployments use API key or Bedrock |
| R12 | A sealed holdout still leaks through repeated verdicts | `maxRunsPerMonth`, verdict only, refresh holdout from new production failures quarterly |

## 16. Phases

Goal's main phases: **0** foundation (done) · **1** first agent end to end · **2** Goal builds Goal · **3** knowledge + SRE factory · **4** flow and learning.

| Jonah phase | Aligns with | Scope | Exit gate (measurable) |
| --- | --- | --- | --- |
| **J0 Design** | 0 → 1 | This plan, ADR 0002, schema, starter configs and cases, SQL draft | Done: examples validate (Python jsonschema and Go jsonschema v6), SQL applies on PG 18 with 21 constraint checks rejected as intended; owner accepts ADR 0002 |
| **J1 Capture and run** | 1 | Transcripts and config shas on every run; eval files in `goal validate`; offline harness MVP (`goal eval run`), Docker runner, code graders (contract, match, transcript, tool-calls), stats package | Architect suites run end to end with ≥ 10 cases × 3 trials; oracle passes 100% and null fails 100%; an induced API error is recorded as `error`; re-grading stored outputs reproduces 100% of code-grader verdicts; cost, turns and latency recorded per trial |
| **J2 Evals as CI for Goal** | 2 | Eval lane (WIP, windows, share, yield, backoff) under advisory locks; repo snapshots and hidden tests; command, diff-scope, verification graders; judge adapters and calibration; labeling queue and overview screens; PR trigger posts comparisons on PRs that change `factories/goal/**` | `goal` factory has ≥ 30 regression and ≥ 15 capability cases; ≥ 1 judge gating (TPR and TNR ≥ 0.9 on a sealed test with ≥ 30 per class); 14 nights of scheduled runs with eval share ≤ 15% and p95 production claim wait within 10% of the pre-J2 baseline; 100% of PRs changing goal agents show a comparison |
| **J3 SRE, online, gates** | 3 | Gateway record and replay; SRE suites; online sampling, judging, Rogan–Gladen, drift; station gate metrics and sampled rejection spot checks; `export-case` from production items | `sre` has ≥ 20 active cases; replay is deterministic (same cassette and calls → identical responses in 100 repeated trials); ≥ 90% of sampled runs graded within 1 hour; an injected degradation (a shadow config with a removed instruction) is detected within one drift window; review-gate confusion matrix on ≥ 50 items |
| **J4 Flow and diagnosis (L1)** | 4 | Flow metrics and constraint-first ordering; Jonah section in the kaizen report; defect links; error-analysis sessions; failure taxonomy; signals; diagnostician task | Flow metrics equal hand-computed values on a fixture event log; for 3 seeded faults (removed instruction, over-tight tool deny, starved constraint from a WIP limit) the diagnosis names the right lever in ≥ 2 of 3 and a human confirms the failure mode; zero proposals touch a forbidden path |
| **J5 Verify and ship (L2)** | 4 | Proposal builder, leak check, comparisons with the decision rule and holdout gate, `goal` factory integration, rollout watch, pins and revert items, proposals screen | A seeded regression goes detect → PR with evidence → human merge → kept; a seeded bad change is pinned back automatically within its watch window and an expedite revert item appears; every step is in `jonah_audit`; a proposal exceeding its budget is refused before any trial |
| **J6 Low-risk autonomy (L3)** | after 4 | Auto-merge of low-risk flow-config changes; human-station auto-approval for Jonah items (needs a `factory.schema.json` addition); weekly digest | Entry: ≥ 10 L2 proposals with ≥ 70% human acceptance and no rollback among accepted low-risk ones. Exit: 30 days of L3 on one factory with no unrecovered regression and every auto-merge reverted cleanly when tested |

### 16.1 Work items for the `goal` factory

Spec-ready titles; lane in brackets; acceptance criteria are checkable.

**J1**

1. **Store transcripts and config versions for every agent run** [backend] — `factory_definitions`, `transcripts` and `agent_runs.definition_sha256` added by new migrations from `docs/jonah/schema.sql`; a finished run has a zstd transcript row with sha256 and size; the run's `definition_sha256` matches the loaded factory; integration test proves two goald instances storing transcripts concurrently do not conflict.
2. **Validate evals.yaml and case files in `goal validate`** [backend] — embeds `schema/evals.schema.json`; reports every cross-reference error listed in section 8; the starter files for goal and sre pass; a broken fixture set reports each error at once (table test).
3. **Repository snapshots for eval fixtures** [backend] — tag `evals/base-1` exists; `goal eval snapshot <ref>` produces a single-commit tree with no history or remotes, stored by sha256; the gateway serves it by hash; a test proves `git log` inside the snapshot shows one commit.
4. **Offline harness MVP: `goal eval run <factory> <suite>`** [backend] — plans trials with interleaved order keys; runs each in a fresh Docker container through the Runner port; records `error` vs `graded`; prints pass@1, pass@k, pass^k with intervals; oracle and null checks run for draft cases.
5. **Code graders and statistics** [backend] — `contract`, `match`, `transcript`, `tool-calls`; `internal/jonah/stats` with pass@k and pass^k estimators, Wilson and cluster bootstrap; table tests against hand-computed values.
6. **Worker eval mode** [backend] — envelope `mode: eval` denies push and network beyond the gateway; grading phase runs after the agent exits and is invisible to it; Vitest on envelope parsing and the deny hook.

**J2**

1. **Eval lane scheduler and quota policy** [backend] — schedules under advisory locks; WIP via the slot index; yield, windows, share and budget checks before enqueue; 429s write `quota_events` and requeue as `error`; engine test with fakes shows production never waits on an eval trial.
2. **Command, diff-scope and verification graders with hidden tests** [backend] — `failToPass`/`passToPass`; hidden files copied after the agent exits; editing a forbidden or test file fails the trial; `backend-adds-migration-never-edits` passes with its reference patch and fails with an edit to `00001_init.sql`.
3. **Judge port and adapters with calibration** [backend] — `Judge` port; `anthropic`, `bedrock` and `worker` adapters; structured output with critique first; `goal eval calibrate` computes TPR/TNR per split and refuses a second test run; uncalibrated judges are advisory.
4. **Jonah API and events in the contract** [backend] — `/v1/jonah/*` and `jonah_events` in `api/openapi.yaml` and `api/events.schema.json`; generated server and client; handler tests.
5. **Labeling queue screen** [frontend] — section 13 screen 5; keyboard shortcuts; auto-save; judge verdict hidden until labeled; Playwright flow at 375, 768, 1440 px with axe checks.
6. **Evals overview screen** [frontend] — section 13 screen 1 with suite health and Needs-you list; constraint shown in amber only.
7. **Eval comparison on pull requests** [infra] — CI job runs the regression suites affected by a PR touching `factories/<name>/` and comments pass rates, deltas and intervals; CODEOWNERS protects `factories/*/evals/`.

**J3**

1. **Gateway record and replay** [backend] — cassette matching (exact, regex, normalized); unmatched handling per section 5.2; write tools never served in eval mode; determinism test over 100 repeated calls.
2. **SRE suites live** [backend] — the five starter cases active with oracle checks; ≥ 20 cases after one month from production candidates.
3. **Online sampling, judging and drift** [backend] — deterministic sampling across instances; catch-up job; Rogan–Gladen with bootstrap; drift signal; injected-degradation test.
4. **Gate accuracy metrics** [backend] — confusion matrix per gate from later outcomes; sampled rejections in the labeling queue; station page data.
5. **Export a production item as a case** [backend] — `goal eval export-case SRE-12` writes a draft case with input, cassette from recorded calls (scrubbed) and the human outcome as reference.

**J4**

1. **Flow metrics and constraint-first ordering** [backend] — metrics in section 3 computed into `metric_snapshots`; equal to hand-computed values on a fixture log; schedules and sampling favor the constraint.
2. **Defect links** [backend + frontend] — a `defect_of` link from a fix item or re-filed incident to the original done item; counted as an escaped defect within the window.
3. **Error-analysis sessions and failure taxonomy** [frontend] — sampling, open coding, Jonah-suggested clusters confirmed by a human, failure-mode tags in the queue.
4. **Signals and the diagnostician** [backend] — signals from all sources with fingerprint dedup; diagnose task through the Runner with the read-only `jonah` connection; diagnosis contract; seeded-fault test.
5. **Jonah in the kaizen report** [backend] — constraint, quality at the constraint, top failure modes with rates, open proposals.

**J5**

1. **Proposal builder** [backend] — diff against HEAD, allowlist, `goal validate` on the candidate tree, leak check, risk classification; DB rejects forbidden paths.
2. **Comparisons and the decision rule** [backend] — interleaved arms, section 11.4 rule, Holm, holdout gate with verdict-only output; immutable decisions.
3. **Ship through the goal factory** [backend] — item created at `approve-spec` with a pre-filled spec; build check verifies the branch diff equals the approved diff; PR links evidence.
4. **Rollout watch and automatic rollback** [backend] — watch window; pin on trigger; expedite revert item; human releases the pin; seeded bad change test.
5. **Proposals screen** [frontend] — section 13 screen 7 with diff, evidence and actions.

**J6**

1. **Auto-approve Jonah items at human stations for low risk** [backend] — schema addition to `factory.schema.json` (owner-approved), engine support, L3 policy checks, weekly digest.

## 17. Where the methods come from

| Source | Informed |
| --- | --- |
| Anthropic article (section 2) | Vocabulary; capability vs regression; pass@k vs pass^k; grader types and order; outcome over path; isolation; reference solutions; balanced sets; saturation; reading transcripts; Swiss-cheese layering |
| `error-analysis` | Section 6.3 sessions; "first thing that went wrong"; failure taxonomy before graders; fix-before-evaluator; sampling strategies for online evals |
| `write-judge-prompt` | One criterion per judge; four-part prompt; critique before verdict; binary only; few-shot from train split only; minimal inputs |
| `validate-evaluator` | Train/dev/test split; TPR/TNR not accuracy; sealed test once; Rogan–Gladen correction with bootstrap CI |
| `eval-harness-first` | Advisory-only fallback for uncalibrated judges; pin and recalibrate on model change; goldens versioned like code; baseline before any change; the different-family rule (deviation recorded as R3) |
| `eval-harness` | Eval-driven development; pass^3 for release-critical paths; never allow flaky graders in gates |
| `generate-synthetic-data` | Dimensions and tuples, human review of tuples, two-step generation |
| `build-review-interface` | Labeling screen: trace-level Pass/Fail/Defer, notes, shortcuts, auto-save, native rendering, sanitizing |
| `eval-audit` | Oracle and null checks; no similarity metrics as graders; re-audit after pipeline changes |
| `agent-evaluation` | Dry run on a few cases before a full run; runtime and cost estimate before starting; dataset size guidance. Its MLflow dependency is rejected (ADR 0002) |
| `llm-evaluation` | Pointwise, pairwise and reference-based judging styles; A/B and human evaluation as complementary layers |
| `langfuse` | Judge calibration as an approval loop with the human; observation-level annotation; Langfuse kept as an optional exporter only |
| `skill-creator` (eval loop) | Baseline and candidate launched together; mean ± spread with deltas; analyst pass for non-discriminating and high-variance cases; blind pairwise comparison; held-out selection to avoid overfitting |
| `claude-api` eval guidance (eval-hillclimb, eval-audit) | Noise floor ≈ 1/√(n·reps); one hypothesis per change; analyzer reads train only; inconclusive is not a win |
| Prior internal system | Generic grader families; hand-labeled incident cases exported from DB and traces; optional Opus judge; an optimizer limited to proposing changes as a PR |
| *The Goal* (Goldratt) | Constraint first; five focusing steps; Jonah's questions; subordinating evals to the quota constraint |

## 18. Open questions

1. **Lane for config changes** in the `goal` factory's spec contract: reuse `backend` (no config change now) or add a `config` lane routed to a dedicated agent?
2. **Pre-filled entry station**: the engine must accept an item created at `approve-spec` with `outputs.spec` filled. Confirm this is acceptable as a general engine feature (also useful for imported issues).
3. **Judges for Opus agents**: accept self-family Opus judges with calibration, or judge them with a higher tier where the account offers one, at higher cost?
4. **Dependency downloads in trials** (Go modules, npm): pre-warm in the worker image, or allow a read-only caching proxy through the gateway?
5. **Holdout curation effort**: who labels 20+ holdout cases per factory, and how often is it refreshed (proposed quarterly)?
6. **Subscription metering**: the plan's limits are not exposed as numbers; `maxShare` relies on cost estimates and rate-limit events. Is that precise enough, or should eval runs be limited to API-key mode once hosted?
7. **Agent SDK structured output for subscription judges**: confirm the worker's SDK version supports a JSON schema output for single-turn queries; otherwise validate JSON and retry.
8. **Factory-level offline evals** (an item flowing through every station with recorded human decisions): worth building after J5, or are online flow metrics enough?
9. **Optional exporters**: is OTLP export of eval runs to Langfuse or Phoenix wanted, given Postgres is the source of truth?

## 19. Sources

- Anthropic, *Demystifying evals for AI agents*, 2026-01-09. <https://www.anthropic.com/research/demystifying-evals-for-ai-agents>
- Skills in `.claude/skills/`: llm-evaluation, eval-harness, eval-harness-first (with references/judge-calibration.md), eval-audit, error-analysis, write-judge-prompt, validate-evaluator, generate-synthetic-data, build-review-interface, agent-evaluation, langfuse (references/judge-calibration.md, error-analysis.md), skill-creator (agents/comparator.md, analyzer.md, scripts/run_loop.py), claude-api (shared/evals/*).
- M. Chen et al., *Evaluating Large Language Models Trained on Code*, 2021 (unbiased pass@k estimator).
- W. J. Rogan and B. Gladen, *Estimating prevalence from the results of a screening test*, 1978.
- E. B. Wilson, *Probable inference, the law of succession, and statistical inference*, 1927.
- E. M. Goldratt and J. Cox, *The Goal*, 1984; G. Kim et al., *The Phoenix Project*, 2013.
