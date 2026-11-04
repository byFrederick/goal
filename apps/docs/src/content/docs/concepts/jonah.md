---
title: Jonah (evals and judges)
description: Jonah is Goal's learning part, with judges that score finished runs, eval cases that gate a publish, and reviews that propose definition changes.
sidebar:
  order: 24
---

Jonah is how a factory learns from its own runs. Judges score finished runs against a rubric. Eval cases run an agent on a fixed task before a version is published. Reviews read the runs that people sent back and propose changes to the definition. Jonah never changes a factory by itself: a proposal changes it only when a person approves it, and judges and eval cases change only when a person edits them.

## Judges

A judge is a file `evals/judges/<slug>.md`. Its front matter says what it checks, which agents it scores, its outcomes (each with a score from 0 to 1 and a label), its pass mark and its sample share. Its Markdown body is the rubric.

A judge scores a share of the finished production runs of its agents. The share is its `sample`, from 0 to 1, and 0 pauses it. A judge run is one model call with no tools. It answers with a reason and one of the judge's outcome labels. The score comes from the outcome, never from the model, and a judge scores a run at most once. A judge never gates a run.

`goal judge list` shows the factory's judges. `goal judge try <slug>` queues the judge on the 10 newest finished runs of its agents, and those tries are kept out of the 30-day figures.

## Eval cases

An eval case is a file `evals/cases/<name>.yaml`. It holds one task that an agent must get right, how many tries it gets, and one check: the agent's output contract, a judge, or a reference patch.

Before a version is published, the cases run on the draft. The publish gate publishes only when the newest eval run of every case, on the draft's exact content, has passed every try. A draft without cases publishes as before.

`goal eval run` starts an eval run by hand, and `goal eval show <run>` shows its state, cost and the tries of each case.

## Bake-offs

A bake-off runs one agent's eval cases on two to four models, and by default one of them is the agent's current model. It compares the models by pass rate, then by cost per item. Applying the result writes the winning model into the draft. `goal bake-off run <agent>` starts one. The Bake-offs tab lists the factory's bake-offs, newest first; each opens its own page.

## Reviews and proposals

A review is one agent run of kind `jonah` per factory, in a box it can only read. Its agent has the Read, Grep and Glob tools and nothing else: no repository, no secrets, no MCP servers, and at most 40 turns under what the weekly budget has left. The box holds:

- the current version's files;
- the evidence: one file per item that finished since the last successful review and needed help, with counts across them and the versions published in that time;
- the `rejected` and `gate_rejected` touches in that window, with their reasons;
- the open proposals, and those published or rejected in the last 90 days, the rejected ones with their reasons;
- the open observations of earlier reviews, each with its id and its count so far;
- the knowledge sources that apply to every run, and the factory memory.

Goal's own prompt tells it to group the evidence by cause and propose one change per cause, as small as it can be, citing its items. It must not repeat a rejected proposal or something a recent version already did. When a cause is not a definition problem, such as a lost sandbox, a provider outage or a person's choice, it says so. It returns observations and proposals.

An observation is a cause with the items it rests on and a count. Observations persist across reviews: a review that sees a cause again names the open observation, adds its items and adds its own count to the count so far. An observation is *watching* until a proposal addresses it, then *proposed*. One whose cause is not a definition problem is *not Jonah's*, and later reviews no longer update it.

A proposal is one hypothesis, stored as a set of exact edits. Each edit names a file and gives replacements: an old text that occurs exactly once in the file, and the new text that replaces it. A new file is given whole. A proposal also names the one number it should move: goald computes today's value from the line, and the review's value after is shown as an estimate.

goald drops a proposal, and logs `jonah proposal dropped` with the reason, when:

- it edits a file outside the allowlist. A proposal may edit `factory.yaml` and the `.md`, `.yaml` and `.json` files under `agents/`, `skills/`, `rules/` and `contracts/`. It may not edit scripts, `evals/` or any other file.
- it changes a part of `factory.yaml` outside the allowlist. A proposal may change an agent's `model` (to a model the organization's policy allows) and `skills`, the `skills` list (local `./skills/` sources only), `rules`, `contracts`, and a station's `wip`, `buffer` and `gate.maxRework`. goald compares the parsed file before and after. Agents and skills are matched by name and stations by id. A change to anything else is refused, including a station's checks, kind, action or agents, tools, budgets, triggers, intake, environments and `learning`, and a reorder of agents or stations.
- an old text does not occur exactly once in the version's file, or it gives whole content for a file that exists;
- the definition with its edits applied has a blocking problem that the version did not have;
- its title, summary or edits hold an invisible or bidirectional Unicode character, which can hide instructions from the person reading it.

- goald queues one review a day for each factory with Jonah on that has new evidence, at an hour of the day set by the factory's id. A person can queue one at once. Each factory has at most one review at a time.
- Jonah's budget is `learning.jonah.share`, 5% by default, of what the line spent in the last 7 days. Each review may spend a fifth of it. When the budget is used up, reviews wait and publishes go on. Jonah's spend shows in the factory's spend, but it never stops production work through `runtime.budget.onExhausted`.
- On a daily review, an observation becomes a proposal only once its count across reviews reaches `learning.jonah.proposeAfter`, 3 by default. Until then it shows as "Watching, n of proposeAfter". A review a person asked for may propose below it.
- Each factory numbers its proposals from 1. A proposal is `proposed`, `published`, `applied`, `rejected` or `superseded`. A new proposal supersedes an open one only when their edits overlap: both edit the same file and their old texts cover some of the same text there, or either gives that file whole. Proposals that touch other parts of a file both stay open.
- Approve applies the proposal's replacements to the current version's files, opens a draft from the current version, writes the results and publishes it as the person who approved. A change to the same file since the review does not stop it. Approve is refused when the factory already has an open draft. It is also refused as stale when an old text no longer occurs exactly once, so a proposal cannot overwrite newer work.
- Edit applies the replacements to the draft's copy of each file, so the person can continue in the builder. It is refused with `draft-conflict` when an old text no longer occurs exactly once in the draft.
- Reject needs a reason, and later reviews read it.
- To undo a published proposal, use Undo this change (see the next section). Restore also works, but it reverts everything published since.

## The watch on every change

Goal watches every published version at each station it changes. A station's outcome for an item counts for the definition version of that item's run at the station, so an item that started before a publish still counts for the version it ran on.

- **What is compared.** For a Jonah proposal, the number it said it would move (its estimate's metric at its station). For a person's publish, the send-back rate and the cost per item at each station the change reaches. A change reaches a station when it changes what the station's agent gets: its agent file, a rule that applies to it, a skill it loads, its contract or its model.
- **Before and after.** Before covers the versions since the last change that reached the station. After runs from the change until the next change that reaches the station, so publishes that touch other stations do not end it.
- **The verdict** comes from a 90% interval of after minus before, where lower is better. It reads *Helped* or *Worse* when the interval clears the minimum effect: 5 points for a rate, 10% of before for cost or lead time. It reads *No change* when the interval lies inside that minimum, and *Too early* otherwise, with the items counted on each side. A watch that is still too early after 14 days, or after 150 items following the change, ends as *No clear change*. The verdict is final then.
- goald recomputes the watches every 15 minutes. The API shows them in each version's `watches` and in a proposal's `watch`.

### Undo this change

When a Jonah proposal's own number gets worse, Needs you shows a `watch_worse` entry that is not tied to an item. A person's publish never raises one; its watches show on the version.

Undo this change (`POST …/jonah/proposals/{number}/undo`, which needs `definition:edit`) writes a new proposal whose replacements are the published ones reversed, checked against the current version, whatever has been published since. The new proposal names the one it undoes, waits in Proposals like any other and publishes only when a person approves it. A file the proposal added stays in place; the reversed edits stop referring to it. The undo is refused with `undo-conflict`, naming the file, when the text the change wrote no longer appears exactly once. While an undo of the proposal is open, the Needs you entry goes away and a second undo is refused with `undo-open`.

The decision behind reviews is ADR 0034 (`docs/adr/0034-jonah-proposals.md` in the repository). The evals and judges are described by ADR 0002 and ADR 0025 there.

## Where the review loop stands

goald runs the review loop every minute by default (`GOAL_JONAH_REVIEW_EVERY`). Each round queues the daily reviews that are due and runs the queued ones.

- **Look for improvements now:** `goal jonah look` and `POST …/jonah/reviews` queue a review at once. The answer is `review-in-flight` while one is pending or running, and `learning-budget-exhausted` once Jonah's runs of the last 7 days cost its budget. The web app's button is not wired to it yet.
- **The window:** a review reads what finished since the last successful review ended, or the last 30 days before the first. A failed review does not move it.
- **Retries:** a review whose run fails or whose answer does not parse is tried again, at most twice. After the third failure it ends `failed`, and its error shows as the last review in `GET …/jonah`. A spent budget or a deleted version fails it at once.

Learned facts, eval cases proposed by a review, and judges proposed by a review are not part of reviews yet.

A goald without judges or evals shows the Judges, Evals and Bake-offs pages empty, with creating a judge, an eval case or a bake-off not available yet.

## Where you meet it

- The Jonah overview shows each judge's pass rate over the last 30 days against its pass mark: the latest period in black, the earlier ones in grey, and the pass mark as a dashed line. Each chart opens that judge.
- `goal jonah` lists proposals. `show` displays the evidence and a unified diff, and `approve`, `edit` and `reject` decide one.
- `goal judge` and `goal eval` manage judges and eval runs. `goal bake-off` runs the comparisons.
- `goal spend` shows what runs cost, judges and reviews included.

The flags are in the [CLI reference](../../reference/).
