# ADR 0039 — Jonah learns from the line: evidence from every item, tries on past items, a watch on every change

Status: proposed · 2026-11-03

- **Replaces:**
  - [ADR 0025](0025-judges-and-evals-as-the-screens-show-them.md): judges go, and eval cases become past items kept as tests (decisions 8–12).
  - [ADR 0029](0029-bake-offs-are-eval-runs-of-one-agent-across-models.md): bake-offs become model proposals tried on past items (decision 13).
  - The phase 5 amendment of [ADR 0002](0002-jonah-evals.md), where it differs.
- **Amends:**
  - [ADR 0034](0034-jonah-proposals.md), Jonah proposals. It changes what a review reads, when it runs, how it edits and what it may change (decisions 1–7). Proposals, their numbers and states, Approve, Edit and Reject stay.
  - [ADR 0037](0037-secrets-and-environment-files.md), decision 5: a try's agent gets none of its environment's `env`, `files` or MCP servers (decision 9).
  - [ADR 0038](0038-knowledge-is-files.md): decision 11's directions for factory memory are decided here (decision 7). Its amendment of ADR 0025, which let memory-only versions skip the eval gate, is moot, because the gate goes.
- **Builds on:**
  - [ADR 0037](0037-secrets-and-environment-files.md): the model key, and the environments that runs and tries use.
  - [ADR 0038](0038-knowledge-is-files.md): `memory.yaml`, and files in a run's box.
  - [ADR 0015](0015-overlapping-in-flight-work.md), [ADR 0018](0018-agents-ask-questions.md) and [ADR 0019](0019-item-watch-live-trace-teardown.md): items, questions and answers, and the teardown.
- **Screens:** the canvas pages "Jonah" and "Mobile", exported to [docs/design/screens/](../design/screens/README.md). They are binding ([ADR 0016](0016-ui-revamp.md), [ADR 0036](0036-design-system-v3-plain-and-motion-binding.md)).

## Context

Jonah is built:
- about 16,000 lines of code and 14,000 of tests;
- reviews and proposals (ADR 0034);
- judges, eval cases and a publish gate (ADR 0025);
- bake-offs (ADR 0029);
- the CLI and seven pages.

The production organization, measured on 2026-11-03:
- **Jonah has never run.** It is on with its defaults, but it has spent $0 and written no observation and no proposal. Manual reviews are switched off in the API, the page and the CLI (`reviewLoopRunning = false`).
- **It counts the wrong send-backs.**
  - A review starts after five sent-back touches, and a touch needs a person as its actor. An agent gate's `gate_rejected` is not a touch.
  - In 7 days, 143 of 375 finished items went back 274 times:
    - the review agent sent back 173;
    - the integrate agent 49;
    - people moved items back 31 times;
    - lost sandboxes forced 13 retries;
    - failed checks 8.
  - People rejected none, so Jonah counted 0.
- **It reads little.** A review sees up to 200 people's send-backs, the files it may edit, and the proposals rejected in the last 90 days. It never sees answers to questions, notes, failed runs or cost.
- **Most of it can't run.**
  - goald never builds the judge loop, the eval loop or the publish gate. Six of their ports exist only as test fakes, and their 16 routes answer 503.
  - No factory has a judge or a case. The software factory's `evals/` folder was dropped in v29, when a schema change made goald unload the factory.
- **Learning happens by hand.**
  - The software factory published 39 versions in 9 days.
  - Nearly every one fixes something seen on the line, and its message says what. For example, v30: "Integrator: also fix lint findings the merge itself creates instead of rejecting".
- **The line can tell what a change did, if it is counted per station.** v39 moved frontend-engineer from Haiku to Sonnet. Frontend items, counted by when they finished:
  - cost per item fell from $2.38 to $1.91, rework included;
  - the share sent back for an unmet acceptance criterion went from 20% to 30%, within the noise at that count.

  A bake-off prices a try. The line prices an item with its rework.
- **Past work can't be replayed today.**
  - An item's `outputs` keep only each station's latest output.
  - No run stores its own output, the brief it got, or the commits it started from and pushed.

What the leading products, OpenAI and Anthropic, settled on (sources in `research/agent-learning` and `research/agent-evals`, outside this repository):
- **A change is a diff with its evidence, and a person approves it.**
  - Warp opens a pull request on the factory definition after 25 failures or 7 days, linking the failed runs ([docs](https://docs.warp.dev/factories/measure-and-improve/self-improvement/)).
  - Factory batches failures daily ([post](https://factory.com/news/factory-signals)).
- **Tests come from real failures.** Both vendors start with 20–50 tasks taken from real failures, grade with code first and a model where code falls short, and rerun the tasks on change ([Anthropic](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents), [OpenAI](https://developers.openai.com/cookbook/examples/evaluation/building_resilient_prompts_using_an_evaluation_flywheel)).
- **Checked changes help, unchecked ones hurt.**
  - Skills checked on real tasks went from 24% to 93% ([GEPA](https://gepa-ai.github.io/gepa/blog/2026/02/18/automatically-learning-skills-for-coding-agents)).
  - Unchecked self-written skills scored 8–11 points below no skills at all ([SkillsBench](https://arxiv.org/abs/2602.12670)).
- **A model grader is checked against people's verdicts.**
  - LLM judges match people on easy cases and are near chance on hard ones ([JudgeBench](https://arxiv.org/abs/2410.12784)).
  - Agents grade their own work too kindly ([Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps)).
- **Comparisons need repeats, intervals and a noise floor.**
  - Infrastructure alone moved Terminal-Bench 2.0 by 6 points ([post](https://www.anthropic.com/engineering/infrastructure-noise)).
  - Anthropic asks for standard errors and paired differences ([post](https://www.anthropic.com/research/statistical-approach-to-model-evals)).
- **No hosted eval product.** OpenAI's hosted Evals closes on 2026-11-30 ([deprecations](https://developers.openai.com/api/docs/deprecations)).

The owner's rules for this design:
- **Simple.**
- **Scalable.**
- **Generic.** It works for any factory, whatever its stations, agents, gates, work types or way of working, with a repository or without. Nothing in Go, SQL or TypeScript names a factory, station, agent or work type (AGENTS.md rule 1).

A person meets six ideas: evidence, observation, proposal, try, kept test and watch. An independent review of the first draft against the code changed fourteen things, and they are folded in below:
- what a try's box gets;
- what runs record;
- a test that never blocks;
- grading a change to a gate;
- the budget;
- a positive allowlist;
- a tries table;
- a watch per change;
- stored replacements;
- smaller evidence;
- verified memory facts;
- config removals;
- the build order;
- fewer concepts.

## Decision

### Evidence

1. **Jonah reads what the line already records.** An item becomes evidence when something needed help at any station:
   - **a send-back:**
     - a gate's verdict, by an agent or a person, with its category (`gate_rejected`);
     - a person's reject;
     - a failed check or contract;
     - a person moving the item upstream;
   - **a person's touch:** a note, an answer to an agent's question, a retry, a move, a dependency edit, or an approval after a hold;
   - **a failed run:** blocked, out of turns, out of its item budget, or retried after a lost sandbox;
   - **an outlier:** a cost or turn count at least three times its station's median over the last 30 days.

   These are the engine's own event kinds, so no configuration names them, and the same list fits every factory. A support factory's evidence is the replies a person sent back at its approve station. An SRE factory's evidence is the fixes its verify agent rejected.
2. **The evidence is a folder of small files, written by goald with no model call.**
   - **Which items:** only those that finished since the last successful review. History stays in the observations, which persist between reviews (decision 5).
   - **`items/<number>.md`**, one file per item, built from milestone events only, never tool calls:
     - the title, body, type and lane;
     - each run's station, agent, model, definition version, turns, cost and outcome;
     - each send-back with its station, who sent it (an agent, a check or a person), its category and its reason, verbatim;
     - each question and answer, each note, and each touch with its kind;
     - with ADR 0038, the knowledge files each run read.

     Reasons, notes and answers are kept whole up to 4 KB each.
   - **`README.md`** counts send-backs by station, sender and category, and touches by kind. It also counts the disagreements between a gate and a person: an item a person moved past a gate's send-back, or a gate's pass that a person sent back.
   - **`versions.md`** lists the versions published in the window, with who published each and its message, so Jonah sees what people already fixed.

### The review

3. **One review a day per factory, or one now.**
   - **When:** goald queues a review once a day for each enabled factory that has new evidence. The hour is spread by factory id, so organizations don't all review at once. "Look for improvements now" queues one at once: the API, the page and the CLI turn manual reviews on.
   - **One at a time:** at most one review is pending or running per factory.
   - **The window** starts where the last successful review ended. A review is retried at most twice, and then its error shows on the Jonah page.
4. **The review greps a read-only box.**
   - **The run:** kind `jonah`, with no repository, no secrets and no MCP servers.
   - **Its tools:** Read, Grep and Glob only, up to 40 turns, under the cost cap of decision 16.
   - **The box holds:**
     - `/definition/`, the current version's files;
     - `/evidence/`;
     - `proposals/`: open proposals; published ones with their watch (decision 14); and the ones rejected in the last 90 days, with their reasons;
     - `/knowledge/` and the factory memory, once ADR 0038's build has landed.
   - **Its prompt is goald's own and generic:**
     - group the evidence by cause;
     - propose one change per cause, as small as it can be, and cite its items;
     - never repeat a rejected proposal or something a recent version already did;
     - say when a cause is not a definition problem, such as a lost sandbox, a provider outage or a person's choice.
5. **A review updates observations and writes proposals.**
   - **An observation** is a cause with its items and a count that grows across reviews. It is in one of three states:
     - *proposed*: a proposal addresses it;
     - *watching*: not enough items yet;
     - *not Jonah's*: not a definition problem.
   - **A proposal** changes the definition (decision 6) or adds facts to memory (decision 7). It needs `learning.jonah.proposeAfter` items with the same cause (default 3). A manual review may propose with fewer, because a person asked.
6. **A definition proposal is a set of exact replacements, inside a positive allowlist.**
   - **The edits:** each edit names a file and replacements `{old, new}`, where `old` occurs exactly once. A new file is given whole.
     - goald stores the replacements, not whole files, and applies them again at Approve or Edit.
     - A proposal goes stale only when one of its `old` strings no longer occurs exactly once.
     - A new proposal supersedes an open one only when their replacements overlap.
     - Each result is validated with `config.Check`. A proposal that doesn't apply or validate is dropped and logged with the reason.
   - **The files:** `.md`, `.yaml` and `.json` under `agents/`, `skills/`, `rules/` and `contracts/`, and `memory.yaml`. No scripts or other files.
   - **In `factory.yaml`**, only these paths:
     - `agents[*].model`, among the models the organization's policy allows;
     - `agents[*].skills`;
     - `spec.skills[*]`, from local `./skills/` sources;
     - `spec.rules[*]` and `spec.contracts.*`;
     - `stations[*].wip`, `stations[*].buffer` and `stations[*].gate.maxRework`.

     goald compares the parsed file before and after, and drops a proposal that changes any other path, including stations' `checks`, `kind`, `action` and `agents`, tools, budgets, triggers, intake, environments and `learning`.
   - **Its number:** each proposal names one number it should move (`estimate.metric`, decision 14). goald computes today's value from the line, and the model's value after is shown as an estimate.
7. **Memory facts come only from people's words.**
   - **The source:** an answer to an agent's question, a note, or a person's send-back reason, when it holds beyond its item. An example is "Phones are 390 wide; check 375 too".
   - **The check:** goald keeps a fact only when it quotes that person's event and the quote matches the event's text.
   - **Not yet:** facts Jonah would infer from agents' runs. They wait until the quoted ones prove useful, so no prompt-injected run can write into every later prompt.
   - **The proposal:** a review's facts make one proposal of `memory.yaml`. Each fact keeps its items in `from` and the date in `added` (ADR 0038, decision 6). Near memory's limits, the proposal also removes the facts that new ones replace.
   - A memory change runs no tries.

### Tries on past items

8. **Runs record what a try needs.** From this ADR on, each run stores:
   - the brief it was sent, with its notes, answers and feedback;
   - its output;
   - the branch and base heads before prepare's rebase;
   - the head it pushed.

   A claim's retries share a prepared box, so only a claim's first run can be tried.
9. **A try reruns a station in a box with nothing but the model key.**
   - **The run:** a try is a run of kind `try` with no item, so it adds nothing to the past item's cost or history.
   - **The box:**
     - the station's environment, for its image, size, tools and setup only;
     - setup may use the environment's `env` to reach package registries;
     - the agent gets only the model key: no `env`, no `files`, no MCP servers and no git login.
   - **The input:** the recorded brief and earlier outputs, and a full clone of the recorded commit, fetched by SHA. A commit that is gone makes the try `error`.
   - **Stations that can't be tried:** a station whose agent needs MCP servers or secrets to do its work. Its proposals rely on the watch.
   - **Errors:** an infrastructure failure is `error`, never `fail`.
10. **Code grades first. A grader is checked in both directions, and a change to a gate is graded by people.**
    - **Code first:** the station's contract and its `checks` run in the box. A failing check fails the try.
    - **The grader** is one model call in its own context, with no tools. Its answer is structured: *fixed*, *not fixed* or *unknown*, with the reason first.
      - It reads the reason the item went back and the try's output and diff.
      - It is checked first on the item's original result, which should come out *not fixed*, and on its final accepted result, which should come out *fixed*.
      - The proposal shows both hit rates. When the grader misreads either one, that try is *unknown*.
    - **A change to a gate:** when a proposal changes a gate's own agent, rules or contract, its tries are graded against people's verdicts and later outcomes: a merge with no later send-back counts as right. They are never graded against that gate's own reasons.
11. **Jonah tries each proposal before anyone sees it.**
    - **Which proposals:** those that change what a triable station's agent gets. That is its agent file, a rule that applies to it, a skill it loads, its contract or its model; this is what "reaches a station" means everywhere. Memory-only and flow-only proposals aren't tried; the watch follows them.
    - **On what:** up to five of the items the proposal cites, newest first, plus the station's kept tests. One try each.
    - **What it shows:**
      - each item fixed, not fixed, unknown or error, and each kept test passed or failed;
      - the cost and the time;
      - the grader's two hit rates.
    - **More tries:** "Try again" adds tries.
    - **Over budget:** when the budget is used up, the proposal says "Not tried: budget used".
12. **Kept tests warn. They never block.**
    - **What one is:** a person keeps an item from its Teardown, at a station it passed the first time. The item must keep passing there.
    - **Storage:** kept tests are `kept_tests` rows, because they point at the line's records, which don't travel with a definition.
    - **On publish:** when the publish dialog opens for a change that reaches a station, Goal starts that station's kept tests and shows the results there. A failing test warns, and the person publishes or not.
    - **Approve** reuses a proposal's tries when the content matches.
    - **Removing one** is on the Tests tab and needs `definition:edit`. Purges remove a test with its item.
13. **Try another model is a proposal.** On an agent's page, a person picks one model that the organization's policy allows.
    - **The proposal:** Goal makes a proposal that changes `agents[].model`.
    - **The tries:** three per item, on the agent's five newest finished items and its kept tests. Each is shown beside how that item went on the line. The estimate shows first, and the person confirms.
    - From there it is an ordinary proposal: approve, edit or reject, then the watch.

### After publish

14. **Every change is watched, station by station.**
    - **What is compared:**
      - a proposal's own number;
      - for a person's publish: the send-back rate and the cost per item at each station the change reaches.
    - **How outcomes are counted:** each station's outcome is credited to the version of the run at that station. The watch keeps running across later publishes that don't reach that station.
    - **The verdict** comes from a 90% interval of the difference, after against before:
      - *Helped* or *Worse* when the interval clears a minimum effect: 5 points for a rate, 10% for cost or lead time.
      - *No change* when the interval lies inside that minimum.
      - *Too early* otherwise, with the items counted so far.
      - *No clear change* after 14 days or 150 items a side.
15. **Worse comes to Needs you, with Undo this change.**
    - **When:** only a proposal's own number raises a Needs you entry. People's publishes show their watch on Versions and on the Jonah page.
    - **Undo this change** is a proposal Goal writes by reversing the change's replacements. It works whatever has published since. Nothing undoes itself.

### Budget

16. **One budget, a share of the line's spend.**
    - **The size:** `learning.jonah.share` (default 5%) of what the factory's line spent in the last 7 days. It covers reviews, tries and Try another model.
    - **Per review:** each review is capped at a fifth of the budget.
    - **The factory's budget:** Jonah's spend shows in the factory's spend but never triggers its `onExhausted`, so it can't stop production work.
    - **When it runs out:** reviews wait, proposals say "Not tried: budget used", and publishes go on.

### In the definition

17. **One small block, all of it optional:**
    ```yaml
    learning:
      jonah:
        enabled: true           # default
        model: claude-opus-5-5  # default: runtime.model
        share: 5%               # of the line's last 7 days of spend
        proposeAfter: 3         # items with the same cause
    ```
    - **Removed keys:** `weeklyUsd`, `learning.librarian` and `learning.kaizen` go through the field-removed path (`factory.schema.json`, `config.Load`). They warn; they don't fail the load the way v29's `evals/` did.
    - **`evals/`** gets the warning `evals-ignored` until it is removed.
    - **Defaults** change in the schema and in `DefaultJonahPolicy` together.
    - **`GOAL_BAKEOFF_MODELS` goes.** A person picks the model.

### Where it shows

18. **The screens:**
    - **The Jonah page** has three tabs: Overview, Proposals and Tests. The Overview shows:
      - waiting proposals;
      - After publish: each change's number before and after, and Undo this change on Worse;
      - what Jonah noticed;
      - how Jonah works here.
    - **A proposal:**
      - its number now and its estimate;
      - the diffs;
      - why: the items and their reasons;
      - Tried on past items;
      - details;
      - Approve and publish, Edit, and Reject with a reason.

      A memory proposal lists its facts and the quotes they come from. A model proposal shows its three tries per item beside each item's line result.
    - **The Tests tab** lists kept tests by station, with the last result and who kept each.
    - **The item's Teardown** has Keep as a test.
    - **The agent page** has Try another model.
    - **The publish dialog** shows the kept tests of the stations a change reaches.
    - **Versions** show each change's watch.
    - **Needs you** shows a proposal that made its number worse, with Undo this change.
    - **Factory settings** get a Jonah section.
    - **Phones** get Jonah's overview and a proposal with Approve.
    - **What goes:** the Judges, Judge, Evals, Bake-off and Bake-offs pages, and Send to Jonah on Runs.
    - **Copy stays short.** How something works sits behind an ⓘ tooltip next to its label, never in a paragraph.

### API, CLI and data

19. **The surface:**
    - **API:**
      - `GET …/jonah` answers the overview.
      - `POST …/jonah/reviews` works.
      - A proposal carries its replacements, `tried` and `watch`.
      - New routes: `POST …/jonah/proposals/{number}/try`; `GET`, `POST` and `DELETE …/tests`; `POST …/agents/{agent}/model-proposal`, with an estimate first.
      - A version carries its watches.
      - The judge, eval and bake-off routes go.
      - Permissions stay `factory:read`, `definition:edit` and `definition:publish`.
    - **CLI:**
      - `goal jonah` with `look`, `show`, `approve`, `edit`, `reject` and `try`;
      - `goal test keep <item> --station S`, `goal test list` and `goal test rm <id>`;
      - `goal agent try-model <agent> <model>`.

      `goal judge`, `goal eval` and `goal bake-off` print where they moved, for one release.
    - **Data:**
      - `agent_runs` gain `brief`, `output`, `branch_head`, `base_head` and `pushed_head`;
      - a `try` run kind, allowed in Go and in the `agent_runs` CHECK;
      - `tries`: item, station, original run, try run, model, verdict, diff and cost;
      - `kept_tests`, with row-level security and in the purge functions;
      - `watches`: change, station, metric, before, after, interval and verdict;
      - Needs you entries that aren't tied to an item;
      - `jonah_proposals` store replacements in place of whole files; `jonah_observations` gain `updated_at`; `jonah_reviews` gain `error`, and their `run_id` is written;
      - the `memory.yaml` path in the proposal path CHECK;
      - a later migration drops `judge_scores`, `eval_runs`, `eval_results` and `eval_run_arms`.
    - **Scheduling:** tries are claimed fairly across organizations, with a cap per factory. A publish's kept tests run first, and tries give way to production work.

### Safety

20. **Evidence is untrusted, and Jonah can only propose.**
    - **The review** has no repository, no secrets and no MCP servers. Its only tools read its own box, and its answer is structured.
    - **Proposals:**
      - A person approves every change.
      - The allowlist (decision 6) keeps out scripts, checks, tools, budgets and environments.
      - goald drops a proposal that holds invisible or bidirectional Unicode characters, which have been used to hide instructions in rules files ([Pillar](https://www.pillar.security/blog/new-vulnerability-in-github-copilot-and-cursor-how-hackers-can-weaponize-code-agents)).
    - **Memory facts** must quote a person, verified by goald.
    - **A try's agent** gets the model key and a clone only.
    - **The grader** reads the data in delimited blocks and can answer only fixed, not fixed or unknown.

### Moving over

21. **Nothing to convert.**
    - **Nothing exists to move:** no organization has a judge, an eval case, a bake-off or a proposal.
    - **Removed:**
      - the judge loop, the eval loop and its publish gate, and bake-offs, with their tables, routes and pages;
      - judge and case files, and `evals.schema.json`;
      - `reviewLoopRunning`;
      - `GOAL_BAKEOFF_MODELS`.
    - **Kept:** proposals, their decisions, their numbering and their tables.

## Rejected alternatives

- **Finish what's built (ADR 0025 and ADR 0029 as written).** Nothing is measured until someone writes judges and cases, and most factories won't. Every publish would wait for hand-written cases, and nothing would say whether a change helped.
- **Learn from the line without tries.** It is simpler, but nothing checks a change before it ships. SkillsBench measured unchecked self-written instructions doing worse than none.
- **Kept tests that block a publish.** Tries partly graded by a model flake: ten tests that each pass 90% of the time all pass only 35% of the time. A blocking gate would also stall an owner who publishes four times a day. A warning gives the same information.
- **Grading a gate's change by that gate's reasons.** Loosening the reviewer would then look like a fix on every item it sent back.
- **Judges that sample production runs.** They cost a model run per sample, and nobody checks them against people. The line's gates already judge every item.
- **Model trials as a feature of their own.** A model change is a one-line proposal. Trying it three times per item, beside the line's own result, answers the same question with one concept fewer.
- **Watching each version.** A version averages every station's changes, and the next publish, about 12 items later at this factory's pace, would cut it short.
- **Restore as the undo.** Restore makes an older version current, so it also undoes everything published since.
- **Auto-applying memory facts** (Bugbot, Devin memory). Memory goes into every run's prompt, which makes it a shared instruction. Writable memory fed by untrusted text is how memory gets poisoned ([MINJA](https://arxiv.org/html/2503.03704v5)).
- **Automatic prompt optimization** (DSPy, GEPA). It needs a few hundred tasks and a metric for each, and its many small rewrites overfit. One reviewed hypothesis per cause, tried on its own items, fits a factory's volume.
- **Hand-written eval cases in the definition, or a hosted eval product.** Both mean a second store of runs and labels for people to keep current.

## Consequences

- **Every factory learns from its first item, with nothing to write.** Proposals, tries and the watch use records the engine keeps.
- **A proposal arrives with evidence before and after.** It shows the items it rests on and how they did on a try, and later the number it named, with an interval.
- **People's own publishes get the same watch.** v39 would show its cost per frontend item going down, and its send-backs for an unmet criterion as too early to call.
- **Tries start with this ADR's run records.** For the first weeks, few past items can be tried.
- **Some stations can't be tried.** A station whose agent needs MCP servers or secrets relies on the watch alone.
- **A slow factory waits.** The watch's honest answer there is Too early, for longer.
- **The grader is a model.** Its two hit rates show on every proposal, and a miss shows as unknown, never as a pass.
- **Cost stays a share.** Jonah spends at most `share` of what the line spends, and it can't stop production work.
- **About 9,000 lines of code and 6,800 lines of tests go:** judges, case files, the eval loop and bake-offs. The tries table and its loop are new, modelled on the eval loop's claims and leases.
- **To watch later:** whether three items per cause and 5% are the right defaults.

## The build plan

- **One assembly:** each row is one pull request, in merge order.
- **The design branch:** `design/jonah` holds this ADR, the exported screens and the screens index. It is merged first.
- **When rows start:**
  - Rows 2–4 need neither ADR 0037 nor ADR 0038 and can start at once.
  - Rows 5 and 9 wait for ADR 0037's run-start part, which moves `internal/jonah/reviews.go` to `runtime.modelKey`.
  - Row 7 waits for ADR 0038's definition part (`memory.yaml`).
  - When this is filed, ADR 0038's definition part should skip its change to `evalgate.go`, which row 13 removes.
- **Shared files:**
  - Row 2's bundle change is additive, because the worker reads it only from row 5.
  - Rows 6 and 4 both touch the proposals table and run in this order.
  - The web client is generated from `openapi.yaml`, so row 13 merges after the UI rows.
- **UI rows** follow [MASTER.md](../design/MASTER.md) v3 and [MOTION.md](../design/MOTION.md). They match their screens at 1440, 1280, 1024, 768 and 390 pixels, in light and dark:
  - nothing past the viewport edge;
  - no clipped or spilling text;
  - 44 px tap targets on phones.

| # | Pull request | Lane | What it changes | Merges after |
|---|---|---|---|---|
| 1 | **Design docs.** Merge `design/jonah`. | docs | This ADR, the ADR index, the status lines of ADRs 0002, 0025, 0029, 0034, 0037 and 0038, and `docs/design/screens/**` with its README | — |
| 2 | **Run records.** Decision 8. | backend | `agent_runs.brief`, `output`, `branch_head`, `base_head` and `pushed_head`. The worker reports the heads before and after the rebase. The gateway stores the brief it sent. | 1 |
| 3 | **Evidence.** Decisions 1 and 2. | backend | The evidence builder in the domain, from milestone events. `README.md`'s counts by station, sender and category, with gate–person disagreements, and `versions.md`. The gateway serves the evidence as files, additively. | 1 |
| 4 | **Watch.** Decisions 14 and 15. | backend | `watches`, with outcomes credited per station to the run's version, intervals and the verdict rules. Needs you entries without an item. Undo this change proposals. `watch` on versions and proposals in the API. | 1 |
| 5 | **Review: schedule and box.** Decisions 3 and 4. | backend | The daily schedule, spread by factory id. Look for improvements now turned on. The window on success, two retries, `error` and `run_id`. Goal-agent's `jonah` mode with Read, Grep and Glob over the box, 40 turns and the cost cap. | 3 |
| 6 | **Review: proposals.** Decisions 5 and 6. | backend | Persistent observations. Stored replacements, re-applied at Approve and Edit. Superseding on overlap only. The file and path allowlist and the Unicode check. | 4, 5 |
| 7 | **Review: memory facts.** Decision 7. | backend | Facts quoted from people's events and checked against them. The `memory.yaml` path in the CHECK. | 6 |
| 8 | **Budget and config.** Decisions 16 and 17. | backend | `share` and the per-review cap. Jonah's spend kept out of `onExhausted`. The field-removed path for `weeklyUsd`, `librarian` and `kaizen`, and their builder and API fields. New defaults in the schema and `DefaultJonahPolicy`. | 5 |
| 9 | **Tries: the run.** Decisions 9 and 11. | backend | The `try` run kind and its migration, the `tries` table, the try box (the model key only, the credential dropped, a full clone by SHA), and the fair, capped try loop. | 2, 8 |
| 10 | **Tries: grading.** Decision 10. | backend | Checks first; the grader with its two hit rates; people's verdicts for gate changes; Try again; Not tried: budget used. `POST …/try` and `goal jonah try`. | 9 |
| 11 | **Kept tests.** Decision 12. | backend | `kept_tests`, its API and `goal test`; the run when the publish dialog opens; reusing a proposal's tries; purges. | 10 |
| 12 | **Try another model.** Decision 13. | backend | Model proposals with the estimate first, three tries per item and the line's results beside them. `goal agent try-model`. | 10 |
| 13 | **Removal.** Decision 21. | backend | The judge loop, the eval loop and gate, bake-offs, their tables, routes and CLI commands (which print where they moved), judge and case files, `evals.schema.json`, `GOAL_BAKEOFF_MODELS` and the `evals-ignored` warning. | 14, 15 |
| 14 | **Jonah pages.** | frontend | `features/jonah`: the Overview, Proposals and Tests tabs, and the definition, memory and model proposal pages. The judges, evals and bake-off pages go. | 4, 7, 11 |
| 15 | **Everything else that showed Jonah.** | frontend | As the exported screens show them: Keep as a test on the Teardown; Try another model on the agent page; the publish dialog's kept tests; Versions' watch; Needs you's Worse; the factory settings section; the phone screens. Send to Jonah goes from Runs. | 12, 14 |
| 16 | **Docs.** | docs | The docs site's `concepts/jonah.md`, the glossary (Evidence, Try, Kept test and Watch in; Judge, Eval case and Bake-off out), `factory-definition.md`, `architecture.md` and the change maps. | 13, 15 |
