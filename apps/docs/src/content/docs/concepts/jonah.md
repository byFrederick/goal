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

A bake-off runs one agent's eval cases on two to four models, and by default one of them is the agent's current model. It compares the models by pass rate, then by cost per item. Applying the result writes the winning model into the draft. `goal bake-off run <agent>` starts one.

## Reviews and proposals

A review is one agent run of kind `jonah` per factory. It reads the factory's sent-back events, the `rejected` and `gate_rejected` touches with their reasons, in the window since the last review. It also reads the current version's files and the proposals rejected in the last 90 days, so it does not repeat them. It returns observations and proposals.

A proposal is one hypothesis with file edits. Each edit has the whole new content of one file and the SHA-256 of the file it was made from. A review may edit only `factory.yaml`, `agents/`, `skills/`, `rules/` and `contracts/`, never `evals/`.

- A review runs when a person asks for it or when enough new sent-back touches have built up. The threshold is `learning.jonah.proposeAfter`, 5 by default. Each factory has at most one review at a time, and the weekly spend is capped by `learning.jonah.weeklyUsd`.
- On a threshold review, an observation becomes a proposal only once its count reaches the threshold. Until then it shows as "Watching, n of proposeAfter".
- Each factory numbers its proposals from 1. A proposal is `proposed`, `published`, `applied`, `rejected` or `superseded`.
- Approve opens a draft from the current version, writes the proposal's files into it and publishes it as the person who approved. It is refused when the factory already has an open draft, or when a file changed since Jonah read it, so a stale proposal cannot overwrite newer work.
- Edit writes the proposal into the draft, so the person can continue in the builder.
- Reject needs a reason, and later reviews read it.
- To undo a published proposal, restore the earlier version.

The decision behind reviews is ADR 0034 (`docs/adr/0034-jonah-proposals.md` in the repository). The evals and judges are described by ADR 0002 and ADR 0025 there.

## Where the review loop stands

goald starts the review loop and sweeps for threshold reviews every minute by default (`GOAL_JONAH_REVIEW_EVERY`). The manual trigger is not available yet: the web app's "Look for improvements now" is disabled, `goal jonah look` refuses, and the API answers a manual review with `review-loop-not-running`. Learned facts, eval cases proposed by a review, and judges proposed by a review are not part of reviews yet.

## Where you meet it

- `goal jonah` lists proposals. `show` displays the evidence and a unified diff, and `approve`, `edit` and `reject` decide one.
- `goal judge` and `goal eval` manage judges and eval runs. `goal bake-off` runs the comparisons.
- `goal spend` shows what runs cost, judges and reviews included.

The flags are in the [CLI reference](../../reference/).
