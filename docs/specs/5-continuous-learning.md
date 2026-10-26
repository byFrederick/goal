# Phase 5 — Continuous learning (Jonah)

Status: owner-approved direction (2026-10-24), not built · follows phase 4 · migrations `00060–00069` (`00060–00062` planned) · OpenAPI tag `learning` · PRs `5-1…5-8` · decision records [ADR 0002](../adr/0002-jonah-evals.md) (amended for this phase), [ADR 0008](../adr/0008-portable-runs-connections-and-knowledge.md)

In *The Goal*, Jonah never fixes the plant; he asks questions until Alex sees the constraint and changes it himself. Here Jonah is each factory's improvement coach. It turns what people had to tell a factory into knowledge, standard work and tests, proves a change before it ships, and shows afterwards whether it helped. A person decides, unless a factory opts in to narrow automation. There is no new service: everything is agent runs, item events, definition drafts and versions, and the knowledge port.

## 1. Summary

When an item finishes with human touches (phase 4's metric), an ordinary agent run, the **retro**, reads those touches and proposes **facts** for the organization's Learned knowledge source, **standard-work edits** to the definition (a prompt rule, a skill, a check, a contract) and a **regression eval case**. Eval cases live in the definition's `evals/` folder; a draft is evaluated against the current version with runs pinned to each, deterministic expectations first and an LLM judge rubric second, within a per-factory budget. One review list decides: facts may be auto-accepted, definition changes go into the draft and publish as a new version (auto-publish only when evals pass with no regression, off by default), and touches per definition version show afterwards whether the change really helped.

## 2. Scope

**In**
- The Learned source: one per organization, entries linked to the item, factory and station that taught them; searched by the factory that produced them by default; the top entries given to each run up front; agents told to search the knowledge base before asking a person or guessing.
- Retros: a policy in the definition, a retro run per finished item that had touches, proposals of three kinds plus a suggested reason per touch.
- Eval cases (`evals/cases/*.yaml`), eval runs of a candidate version against a baseline on the production stack, deterministic graders and an advisory judge rubric, a report (pass rate, regressions, improvements, cost, touches expectation), a weekly learning budget.
- One review list (Jonah), auto-accept of facts and auto-publish on no regression as opt-in policies, restore as the undo.
- After publish: touches per definition version with what changed and why; repeat-guidance flags (guidance that matches an accepted Learned entry).
- API, CLI (`goal jonah`, `goal eval`), web boards, inbox entries.

**Out** (later, unscheduled)
- ADR 0002's sealed holdout, autonomy levels L0–L3, online sampling of production runs with bias correction, the paired statistics and Holm correction, a `Judge` port. Judges stay **advisory** until calibrated against people's verdicts (TPR and TNR on a held-out set, validate-evaluator); calibration and gating judges come later.
- Run transcripts for retros (retros read events, outputs and touches, not full transcripts).
- Eval runs that use outside tools: an eval run gets the model connection and a repository clone only (§4.4).
- Jonah changing `evals/` or anything outside `factory.yaml`, `agents/`, `skills/` and `contracts/`; online A/B traffic splitting; automatic prompt optimization loops.

## 3. Canvas boards covered

| Board | Element | Delivered by |
| --- | --- | --- |
| `Jonah.dc.html`, `Mobile-Jonah.dc.html` | the factory's review list: proposals grouped by kind (facts, standard-work edits, eval cases), each with the item and touches that caused it, its reason, its eval result when it has one; Accept, Edit, Dismiss; counts by station and reason (where to automate next); the learning budget used | 5-7 |
| `Remediation.dc.html` (proposal detail) | one standard-work edit: the diff, the guidance quoted, the eval cases it adds, the A/B result, Apply to draft, Publish or Dismiss | 5-7 |
| `Eval-Run.dc.html` (new) | an eval run, candidate against baseline: pass rate of each, regressions and improvements by case, deterministic results and the judge's critique (marked advisory), cost and turns per version, touches expectation, Cancel | 5-7 |
| `Learned.dc.html` (new) | the Learned entries: text, factory, station, the item that taught them, uses (searches that returned them), repeat flags; Edit, Delete | 5-7 |
| `Define-Publish.dc.html` | publish shows the latest eval run of the draft; publishing with regressions needs a confirmation | 5-5 |
| `Define.dc.html` (Versions section) | touches per finished item and touchless rate per version, the proposals each version took, "consider restoring" when a version is worse | 5-6 |

## 4. Domain and engine changes

### 4.1 Policy in the definition

```yaml
spec:
  learning:
    retro: on-touch          # off | on-touch (default: every finished item with a touch) | on-unplanned-touch
    autoAcceptFacts: false   # facts go to Learned without review
    autoPublish: false       # §4.5: only with evals passing and no regression
    weeklyUsd: 10            # learning budget: retros and eval runs together
    evals:
      trials: 1              # runs per case per version; a case passes when all its trials pass (pass^k)
      maxConcurrent: 2       # eval runs of this factory at once
  knowledge:
    learned: {own: true}     # search this factory's Learned entries (default); list `learned` in sources for all of them
```

`learning.librarian` and `learning.kaizen` (never built) are removed with a `field-removed` quick fix. `domain.Line` gains `Learning`.

### 4.2 The Learned source

One built-in knowledge source per organization, name `learned`, kind `learned`, written only by Goal, created on first use. An entry is a knowledge document with a title, a short text (at most 2 000 characters, stored in the bucket like an upload), and its factory, station, item and the item events that taught it. Accepted entries are indexed by phase 4's ingestion loop like any document. A factory searches its own entries by default (`knowledge.learned.own`); listing `learned` in `knowledge.sources` widens it to every factory's entries. Each run's `/run` bundle carries the five best entries for the item (a search on its title and body over what the factory may search, the run's station first), which the worker puts in the system prompt under "What people taught this factory", and the prompt tells agents to search with `knowledge__search` before asking a person or guessing.

### 4.3 Retro

1. When an item reaches done and the policy matches its touches (phase 4 §4.9), the engine queues a retro in the same transaction (`learning_retros`, state `pending`).
2. `internal/jonah`, a goald loop, claims pending retros with leases (`FOR UPDATE SKIP LOCKED`, like the ingestion loop) while the learning budget allows, and runs each as an ordinary agent run through `engine.RunTask` (the run lifecycle of phase 4 §4.5 extracted from the line, so line, retro and eval runs share it): a small box, the factory's model connection, no repository, no other connection, the built-in retro prompt (goald's, like the worker's system prompt), `agent_runs.kind = 'retro'` with its cost.
3. The retro's bundle holds the item (title, body, outputs, its runs' outcomes, contract and check failures), its touches with their text and reason, and the definition version's files the item last ran on.
4. Its output (a JSON schema enforced by the worker) is at most ten proposals and a reason per touch:
   - `fact`: `{title, text, station?, events}` → a Learned entry.
   - `edit`: `{path, base_sha256, content, summary, events}` → a file of the definition replaced, only under `factory.yaml`, `agents/`, `skills/` or `contracts/`.
   - `eval`: `{name, content}` → `evals/cases/<name>.yaml`, schema-checked before it is stored; its `repository.ref` comes from the item's first run (`prepared` now reports the commit it cloned) and its expectations from the touch ("given this item, the agent should have done X without being told").
   - `touch_reasons`: `[{event, reason}]`, stored as suggestions (`reason_by = agent:retro`), never over a person's choice.
5. Proposals are rows (`learning_proposals`, state `proposed`), linked to the retro run, the item, the factory and the version they were made against. A malformed one is dropped and logged; the retro never touches the item.

### 4.4 Evals

**A case** is one file in the definition's `evals/cases/`, validated with the factory at save and publish:

```yaml
apiVersion: goal/v1
kind: EvalCase
metadata:
  name: split-migration-from-code
  station: build                       # the station whose agent the case runs
  from: {item: goal#42}                # optional: the item it came from
spec:
  item: {title: "Add a goal version subcommand", body: "…", type: feature}
  inputs: {spec: {…}}                  # earlier stations' outputs, as the item had them
  repository: {ref: 3f2a…}             # the commit the agent starts from (the factory's runtime.repository)
  expect:                              # deterministic first
    contract: true                     # the output satisfies the station's contract
    checks: true                       # the station's checks pass in the box
    output:                            # JSON assertions on the output
      - {pointer: /summary, matches: "(?i)migration"}
      - {pointer: /pull_request, exists: false}
    files:                             # assertions on the box's working tree after the run
      - {path: "db/migrations/*.sql", changed: true}
  rubric: |                            # optional, advisory: one failure mode, Pass or Fail
    PASS: the change keeps the migration in its own commit without being told.
    FAIL: migration and code are mixed in one commit.
```

`contracts/definitions/evals.schema.json` is replaced by this one kind (the never-used `Evals` kind of ADR 0002 goes; a definition holding one gets a warning `evals-format-old`).

**An eval run** compares a candidate version (usually the draft) with a baseline (the current version): every case × each version × `trials`, each an ordinary run through `engine.RunTask` pinned to that version, at the case's station with its agent, prompt, skills, tools and contract. Prepare clones the repository at `repository.ref` on a throwaway branch; the agent gets the model connection only (the repository's connection is in the prepare exec, not the agent's), and the gateway serves it only the knowledge tools (the task token says `eval`), so evals never push, comment or call outside APIs. Then goald grades: contract, checks (exec in the box, runtime exit codes), output and file assertions; then, when the case has a rubric, `goal-agent judge` runs in the same box (no tools; rubric, brief, output and `git diff` in; `{critique, result}` out, critique first) and its verdict is recorded as **advisory**. A run with an infrastructure error is retried once and then counts as `error`, never as a failure.

**The report**: per version, pass rate (cases whose every trial passed), regressions (passed on the baseline, failed on the candidate), improvements, errors, cost and turns per case, the judge's verdicts apart, and the **touches expectation**: the unplanned touches the original items needed for the cases that now pass. Eval runs and their results are rows; cases are referenced by name and the sha256 of their file in each version.

**Cost**: before starting, goald estimates cases × trials × 2 × the station's median run cost and refuses an estimate over the remaining learning budget (409 `learning-budget-exhausted`, with the estimate); while running, a run that would pass the budget stops the eval run as `budget`. Eval runs use their own concurrency cap (`learning.evals.maxConcurrent`, and `GOAL_EVAL_MAX_RUNS` per goald), never the line's slots, and yield nothing to production because they do not take its leases.

### 4.5 Decide

- **One review list** per factory (and the organization's inbox for its editors: entry kind `proposal`). Facts: accept (as is or edited), dismiss. Edits and eval cases: apply to the factory's draft (opening one from the current version if none is open; refused with 409 `draft-conflict` when the file changed since `base_sha256`, so the editor merges it in the builder), which queues an eval run of the draft, or dismiss. Publishing is the existing publish (`definition:publish`); restore is the undo.
- **Auto-accept facts** (`learning.autoAcceptFacts`): facts skip review and are audited as accepted by `system:jonah`.
- **Auto-publish** (`learning.autoPublish`, off by default): when no draft is open, Jonah applies its own edit proposals of one retro to a new draft, runs the evals and publishes as `system:jonah` only if every changed path is under `agents/` or `skills/` (prompt rules and skills; never `factory.yaml`, `contracts/` or `evals/`), the eval set has at least 5 cases, every case ran without `error`, there is no regression, the pass rate is at least the baseline's, and cost per case is at most 1.5 × the baseline's. Otherwise the draft stays with its report for a person. One auto-publish in flight per factory.
- Jonah never edits `evals/` on its own: eval case proposals always need a person, so the measuring stick changes only by review.

### 4.6 Check after publish

- **Per version**: phase 4's touches per finished item and touchless rate by definition version, next to each version's proposals and eval report. When a version has at least 10 finished items and its unplanned touches per item are at least 25% above the previous version's, the Versions section and the review list say "consider restoring vN" (no automatic restore).
- **Repeat guidance**: with an embedder configured, the knowledge loop embeds each new guidance text (a person's reason or note, from phase 4's `item_touch_labels`) and flags it when it is within cosine similarity `GOAL_REPEAT_GUIDANCE_SIMILARITY` (0.85) of an accepted Learned entry the run could have searched: the knowledge existed but was not used. Repeats are counted in the touch metrics and listed on the Learned entry.

## 5. Ports and adapters

No new port. Additions:

```go
// Knowledge
PutLearned(ctx context.Context, e domain.LearnedEntry, by domain.Actor) (domain.KnowledgeDocumentID, error)
Learned(ctx context.Context, q domain.LearnedQuery) ([]domain.LearnedEntry, error)
NextRepeatChecks(ctx context.Context, limit int) ([]domain.Guidance, error)
RecordRepeat(ctx context.Context, event int64, of *domain.KnowledgeDocumentID, similarity float32) error
// domain.KnowledgeQuery gains OwnLearned domain.FactoryID

// Store (internal/adapters/postgres; porttest fake; storetest)
QueueRetro / ClaimRetro / FinishRetro
AddProposals, Proposals, Proposal, DecideProposal       // learning_proposals
CreateEvalRun, ClaimEvalTrial, RecordEvalResult, FinishEvalRun, EvalRuns, EvalRun
LearningSpendSince(factory, since)                       // retros and evals
SuggestTouchReasons(run, []domain.TouchReasonSuggestion) // never over a person's choice
```

- `internal/engine`: `RunTask(ctx, domain.RunRequest{Version, Station, Agent, Kind, Brief, Inputs, RepositoryRef, Connections})`, the run lifecycle extracted from `runOnce` (lines call it under their lease; Jonah without one). `domain.RunKind`: `station`, `retro`, `eval`.
- `internal/jonah` (new core package: `domain`, `ports`; depguard like `knowledge`): the retro and eval loops, grading (pure: assertions, pass^k, the no-regression rule), the auto-publish rule. It calls `RunTask` and authoring's draft and publish through interfaces it declares; `cmd/goald` wires the engine and `authoring.Service` into it.
- Worker: modes `goal-agent retro` (retro schema) and `goal-agent judge`; `prepared` reports `repository_sha`; the bundle's `learned` entries and the knowledge-first instruction.
- Gateway: tokens with `kind: eval` serve only the knowledge tools. Phase 6 folds the gateway's tools into Goal's one MCP server, where an eval token is a run principal holding the knowledge rows only (spec 6 §4.2); the rule does not change.

## 6. Data model

| Migration | PR | Content |
| --- | --- | --- |
| `00060_learned.sql` | 5-1 | source kind `learned` (`kind IN ('upload','synced','live','learned')`, `(kind IN ('upload','learned')) = (connection_id IS NULL)`, one per org); `knowledge_documents` + `factory_id`, `station`, `item_id`, `guidance_events bigint[]`, `created_by` (person or `system:jonah`); `knowledge_chunks` + `factory_id` with an index `(org_id, source_id, factory_id)`; `item_touch_labels` + `guidance boolean`, `repeat_of uuid` (FK to the document, `ON DELETE SET NULL`), `similarity real`, `repeat_checked_at timestamptz`, and a partial index for the pending repeat checks |
| `00061_retros.sql` | 5-2 | `agent_runs.kind text NOT NULL DEFAULT 'station' CHECK (kind IN ('station','retro','eval'))`; `learning_retros (org_id, item_id PK, factory_id, version_id, state, lease_until, run_id, attempts, error, created_at)`; `learning_proposals (id, org_id, factory_id, item_id, retro_run_id, version_id, kind CHECK fact\|edit\|eval, state CHECK proposed\|accepted\|applied\|dismissed\|superseded, title, summary, payload jsonb ≤ 256 KiB, path text CHECK (path ~ '^(factory\.yaml\|agents/.+\|skills/.+\|contracts/.+\|evals/cases/[a-z0-9-]+\.yaml)$'), draft_version_id, eval_run_id, decided_by, decided_at, created_at)` with indexes `(org_id, factory_id, state)` |
| `00062_evals.sql` | 5-4 | `eval_runs (id, org_id, factory_id, candidate_version_id, baseline_version_id, trigger CHECK manual\|proposal\|auto, state CHECK queued\|running\|done\|failed\|cancelled\|budget, estimate_usd, cost_usd, summary jsonb, created_by, created_at, finished_at)`; `eval_results (eval_run_id, case_name, case_sha256, version_id, trial, state CHECK pending\|running\|pass\|fail\|error, lease_until, run_id, contract boolean, checks boolean, assertions jsonb, judge jsonb, cost_usd, turns, PK (eval_run_id, case_name, version_id, trial))` |

All org tables follow the README's tenancy rules (org_id, RLS, composite FKs to factories, items and versions, an index starting with `org_id`). Proposals and eval rows are never deleted except by the organization's purge; decided rows keep their decision.

## 7. Bucket layout changes

Learned entries' text is stored as `orgs/<org-id>/knowledge/<sha256>`, like uploads (phase 4 §7). Eval cases are definition files (blobs) like any other.

## 8. API

Tag `learning`; no new permission: reading is `factory:read`, deciding and starting evals is `definition:edit` on the factory, publishing stays `definition:publish`, Learned entries are `knowledge:read` and their edits `definition:edit` on the entry's factory.

| Method and path | Scope | Does |
| --- | --- | --- |
| `GET /v1/orgs/{org}/factories/{factory}/jonah/proposals?state=&kind=&after=&limit=` | `factory:read` | the review list, with counts by station, kind and reason |
| `GET /v1/orgs/{org}/factories/{factory}/jonah/proposals/{proposal}` | `factory:read` | one proposal: payload, the diff against its base (edits), the touches and item, its eval run |
| `POST …/jonah/proposals/{proposal}/accept` | `definition:edit` | `{title?, text?, content?}`: a fact into Learned; an edit or case into the draft and an eval run queued; 409 `draft-conflict`, `proposal-decided` |
| `POST …/jonah/proposals/{proposal}/dismiss` | `definition:edit` | `{reason?}` |
| `POST /v1/orgs/{org}/factories/{factory}/evals/runs` | `definition:edit` | `{candidate: "draft" \| <version id>, baseline?: <version id>}` (baseline defaults to the current version) → 202 with the estimate; 409 `learning-budget-exhausted`, `no-eval-cases` |
| `GET …/evals/runs?after=&limit=`, `GET …/evals/runs/{run}` | `factory:read` | runs and one report with per-case results |
| `POST …/evals/runs/{run}/cancel` | `definition:edit` | stops pending trials |
| `GET /v1/orgs/{org}/factories/{factory}/jonah/versions?limit=` | `factory:read` | per version: touches per finished item, touchless rate, items finished, its proposals and eval report, the restore hint |
| `GET /v1/orgs/{org}/knowledge/learned?factory=&station=&after=&limit=` | `knowledge:read` | Learned entries with their origin, uses and repeats |
| `PATCH`, `DELETE /v1/orgs/{org}/knowledge/learned/{entry}` | `definition:edit` on its factory | edit or remove an entry |

`InboxEntry` gains kind `proposal` (count of proposals waiting, for people with `definition:edit`). Stream event `learning` (`{factory_id, proposal_id?, eval_run_id?, change}`) on `goal_org_events`. Audit actions: `learning_proposal.accepted|dismissed`, `eval_run.started|cancelled`, `learned_entry.updated|deleted`; publishes by `system:jonah` are `definition.published` as today.

## 9. CLI changes

- `goal jonah [--factory f] [--state proposed]`, `goal jonah show <proposal>`, `goal jonah accept <proposal> [--edit]` (`$EDITOR`), `goal jonah dismiss <proposal> [--reason r]`.
- `goal eval run [--factory f] [--candidate draft|vN] [--baseline vM] [--follow]` (prints the estimate first), `goal eval show <run>`.
- `goal status` (phase 4) gains the proposals waiting and the last eval run.

## 10. Web UI

Routes under the factory: `/jonah` (review list), `/jonah/proposals/:proposal`, `/jonah/evals/:run`; org page `/o/:org/knowledge/learned`; the Definition's Versions section and publish dialog gain the per-version touches and the eval report. Query keys `['org', id, 'factory', f, 'jonah', 'proposals'|'evals'|'versions']`, `['org', id, 'knowledge', 'learned']`; the `learning` stream event invalidates them. Diffs use the builder's existing diff view; judge verdicts carry an "advisory" badge; every action follows `can`. Phone layouts for the review list and a proposal (accept and dismiss on the go). Accessibility and layout per `docs/design/MASTER.md`; Playwright at 375, 768 and 1440 px with axe.

## 11. Security and tenancy

- Org isolation by RLS on every new table; proposals reference only their own organization's items, factories and versions (composite FKs).
- Retro and eval runs get only the model connection (and a clone for evals), no other connection and no outside tools, so a prompt-injected item cannot make Jonah act outside its box.
- Proposals are model-written: edits are confined to definition paths by code and by a `CHECK`; auto-publish is narrower still (`agents/`, `skills/`), off by default, needs a passing eval set without regression and is undone by restore; eval cases always need a person.
- Accepted risks: a factory evaluated only on its own retro-made cases can overfit them (people's review and the touch metric per version are the checks); an advisory judge can be wrong (it never gates); auto-accepted facts reach later runs until someone edits them (each links the item that taught it).

## 12. Observability

Logs `jonah retro` (item, proposals, cost), `eval run` (cases, pass rates, regressions, cost), `jonah published` (version, eval run). The learning budget used per factory appears on the review list. Retro and eval runs are `agent_runs` rows with their kind, so spend and turns stay in one place and the line's metrics exclude them.

## 13. Testing

- `internal/jonah` with fakes: retro queued only by the policy, the budget stops claims, proposals validated (paths, schema, size), touch-reason suggestions never override a person, grading (assertions, pass^k, errors retried once), the no-regression rule and every auto-publish refusal, one auto-publish per factory.
- `storetest` for retros, proposals and eval rows (leases, RLS across orgs); `knowledgetest` for Learned entries (own entries by default, `learned` widens, chunks filtered by factory), repeat detection with a fake embedder above and below the threshold.
- Engine: `RunTask` used by line, retro and eval runs with the same lifecycle tests; an eval run's token serves only knowledge tools and its agent exec has no repository credential.
- Worker (Vitest): retro and judge schemas, the knowledge-first instruction and the up-front entries in the prompt.
- Web: Playwright for the review list, a proposal applied and evaluated, an eval report, Learned edits.
- End to end on Compose: an item rejected once yields a fact and a case; a later similar item's run receives the fact; an eval run of the edit passes and the version's touches are shown.

## 14. Rollout and migration

Additive. Factories without `learning` get the defaults (retro on touch, nothing automatic, 10 USD a week); a factory sets `retro: off` to opt out. Existing `evals/` files of the old format are ignored with a warning. Touches and per-version figures exist from phase 4, so the Versions section has history from the first day; Learned entries start empty.

## 15. Work breakdown

Skills: the Go skills on Go PRs; PostgreSQL skills with migrations; UI skills on 5-7; `openapi-spec-generation` and `api-design-principles` with `contracts/api/`; the "Agents and evals" skills (llm-evaluation, eval-harness, eval-harness-first, error-analysis, write-judge-prompt, validate-evaluator, agent-evaluation, claude-api) on 5-2 to 5-6.

| # | Pull request | Acceptance criteria | Verified by |
| --- | --- | --- | --- |
| 5-1 | **Learned knowledge**: `00060`, the `learned` source, `PutLearned`, `Learned`, own-entries search, the up-front entries in the bundle, the knowledge-first instruction, the Learned API and its CLI listing | own entries searched by default, `learned` widens; the prompt carries at most five entries | knowledgetest, gateway and worker tests |
| 5-2 | **Retros**: `learning` policy and quick fixes, `00061`, `engine.RunTask`, `internal/jonah`'s retro loop with leases and the learning budget, `goal-agent retro`, proposals and touch-reason suggestions, `prepared.repository_sha` | §13's retro cases | engine, jonah, worker tests, storetest |
| 5-3 | **Review**: proposals API, accept and dismiss (facts to Learned; edits and cases to the draft with the base check), inbox `proposal`, stream `learning`, `goal jonah` | draft conflict refused; decisions audited | handler and CLI tests |
| 5-4 | **Evals**: the `EvalCase` schema replacing `contracts/definitions/evals.schema.json`, authoring validation, `00062`, the eval loop, grading, `goal-agent judge` (advisory), eval-only tokens, the estimate and budget, `goal eval` | an eval run compares two pinned versions; regressions found; nothing outside the box is called | jonah, engine, gateway tests; e2e on Compose |
| 5-5 | **Decide automation**: auto-accept facts, auto-publish with every refusal of §4.5, the publish dialog's eval report | each refusal path; restore undoes an auto-publish | jonah tests, Playwright |
| 5-6 | **Check after publish**: `jonah/versions`, the restore hint, repeat-guidance checks in the knowledge loop | the hint at the thresholds; repeats flagged and counted | storetest, knowledgetest |
| 5-7 | **Web**: Jonah review list, proposal detail with diff and A/B, eval run, Learned, phone layouts | boards of §3 | Playwright 375/768/1440 + axe |
| 5-8 | **Docs as built**: architecture.md, worker README, ADR 0002's status | review | review |

Migration numbers rise with PR order (5-1 → `00060`, 5-2 → `00061`, 5-4 → `00062`).

## 16. Cross-spec contracts

**Phase 5 needs** from phase 4: human touches per item by definition version and `item_touch_labels` (§4.9–4.10), the knowledge port and ingestion loop, `ports.Sandboxes` and the run lifecycle, the `/run` bundle, the embedder; from 2.2-B: drafts, publish, restore and versions; from phase 3: `definition:edit`, `definition:publish`, `factory:read`, `knowledge:read`.

**Phase 5 provides**: `engine.RunTask` and `agent_runs.kind`; the Learned source; `EvalCase` files; eval runs as rows; `system:jonah` as an actor; eval tokens limited to the knowledge tools and the factory's own Learned entries in a run's search, both of which phase 6's run principals carry over. Phase 6 makes the gateway and Goal's MCP server one server with one tool registry: agent tools that come later (asking a person, a note on the item) are rows there marked for runs, and the review list may later be a read-only row for people (spec 6 §4.3).

## 17. Open questions

None blocking. Defaults chosen here and adjustable from evidence: the learning budget (10 USD a week), the auto-publish thresholds (5 cases, cost × 1.5), the restore hint (10 items, +25%), the repeat similarity (0.85). Calibrated, gating judges and a sealed holdout (ADR 0002) wait until factories have enough labeled verdicts.
