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

A review is one agent run of kind `jonah` per factory. It reads the factory's sent-back events, the `rejected` and `gate_rejected` touches with their reasons, in the window since the last review. It also reads the current version's files and the proposals rejected in the last 90 days, so it does not repeat them. It returns observations and proposals.

A proposal is one hypothesis with file edits. Each edit has the whole new content of one file and the SHA-256 of the file it was made from. A review may edit only `factory.yaml`, `agents/`, `skills/`, `rules/` and `contracts/`, never `evals/`.

- A review runs when a person asks for it or when enough new sent-back touches have built up. The threshold is `learning.jonah.proposeAfter`, 5 by default. Each factory has at most one review at a time, and the weekly spend is capped by `learning.jonah.weeklyUsd`.
- On a threshold review, an observation becomes a proposal only once its count reaches the threshold. Until then it shows as "Watching, n of proposeAfter".
- Each factory numbers its proposals from 1. A proposal is `proposed`, `published`, `applied`, `rejected` or `superseded`.
- Approve opens a draft from the current version, writes the proposal's files into it and publishes it as the person who approved. It is refused when the factory already has an open draft, or when a file changed since Jonah read it, so a stale proposal cannot overwrite newer work.
- Edit writes the proposal into the draft, so the person can continue in the builder.
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

Undo this change (`POST …/jonah/proposals/{number}/undo`, which needs `definition:edit`) writes a new proposal that reverses the published edits against the current version, whatever has been published since. The new proposal names the one it undoes, waits in Proposals like any other and publishes only when a person approves it. A file the proposal added stays in place; the reversed edits stop referring to it. The undo is refused with `undo-conflict`, naming the file, when the text the change wrote no longer appears exactly once. While an undo of the proposal is open, the Needs you entry goes away and a second undo is refused with `undo-open`.

The decision behind reviews is ADR 0034 (`docs/adr/0034-jonah-proposals.md` in the repository). The evals and judges are described by ADR 0002 and ADR 0025 there.

## Where the review loop stands

goald starts the review loop and sweeps for threshold reviews every minute by default (`GOAL_JONAH_REVIEW_EVERY`). The manual trigger is not available yet: the web app's "Look for improvements now" is not available yet, `goal jonah look` refuses, and the API answers a manual review with `review-loop-not-running`. Learned facts, eval cases proposed by a review, and judges proposed by a review are not part of reviews yet.

A goald without judges or evals shows the Judges, Evals and Bake-offs pages empty, with creating a judge, an eval case or a bake-off not available yet.

## Where you meet it

- The Jonah overview shows each judge's pass rate over the last 30 days against its pass mark: the latest period in black, the earlier ones in grey, and the pass mark as a dashed line. Each chart opens that judge.
- `goal jonah` lists proposals. `show` displays the evidence and a unified diff, and `approve`, `edit` and `reject` decide one.
- `goal judge` and `goal eval` manage judges and eval runs. `goal bake-off` runs the comparisons.
- `goal spend` shows what runs cost, judges and reviews included.

The flags are in the [CLI reference](../../reference/).
