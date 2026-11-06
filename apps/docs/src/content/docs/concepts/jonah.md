---
title: Jonah (learning from the line)
description: Jonah learns from what the line already records. It reviews the items that needed help, proposes small changes to the definition, tries them on past items and watches every change after it is published.
sidebar:
  order: 24
---

Jonah is how a factory learns from its own line. Once a day it reads the items that needed help, groups them by cause and proposes the smallest change to the definition that should fix each cause. Before you see a proposal, Goal tries it on the past items it cites and on the station's kept tests. After a change is published, Goal watches the stations it reaches and says whether it helped. Jonah never changes a factory by itself: a proposal changes it only when a person approves it.

The pieces, in the order they happen:

1. The [evidence](#evidence): what the line recorded about each item that needed help.
2. A daily [review](#reviews-and-proposals) that reads the evidence and writes observations, proposals and [memory facts](#memory-facts).
3. [Tries on past items](#tried-on-past-items) and [kept tests](#kept-tests), which show how a proposal does before anyone decides it.
4. [The watch](#the-watch-on-every-change) on every published change, with [Undo this change](#undo-this-change).

All of it runs on [Jonah's budget](#jonahs-budget), a share of what the line spends.

## Evidence

The evidence is a folder of small Markdown files that goald writes for each review from the line's own records, with no model call. An item is evidence when it finished in the review's window and something needed help at any station:

- **A send-back:** a gate's rejection, by an agent or a person, with its category; a person's reject; a failed check or contract; or a person moving the item to an earlier station.
- **A person's touch:** a note, an answer to an agent's question, a retry, a move, a dependency added or removed, or an approval after a hold.
- **A failed run:** blocked, out of turns, out of its item budget, or retried after a lost sandbox.
- **An outlier:** a run whose cost or turn count is at least three times its station's median over the last 30 days.

These are the engine's own event kinds, so no configuration names them and the same list fits every factory. The folder holds:

- **`items/<number>.md`**, one file per item: its title, body, type, lane and origin; each run's station, agent, model, version, turns, cost, outcome and the knowledge pinned for it; each send-back with its station, who sent it (an agent, a check or a person), its category and its reason; each question with its answer, and each note and touch. Reasons, notes and answers are kept whole up to 4 KB each. The files are built from milestone events only, never from tool calls.
- **`README.md`**, with the counts across the items: send-backs by station, sender and category, touches by kind, and the disagreements between a gate and a person. A disagreement is an item a person moved to or past a gate that sent it back, or one a person sent back after a gate passed it.
- **`versions.md`**, the versions published in the window, with who published each and its message, so the review sees what people already fixed.

The evidence is untrusted text: a review can only read it and propose, and every proposal waits for a person.

## Reviews and proposals

A review is one agent run of kind `jonah` per factory, in a box it can only read. Its agent has the Read, Grep and Glob tools and nothing else: no repository, no secrets, no MCP servers, and at most 40 turns under the review's cap from [Jonah's budget](#jonahs-budget). The box holds:

- the current version's files;
- the [evidence](#evidence) of the items that finished since the last successful review;
- the `rejected` and `gate_rejected` touches in that window, with their reasons;
- the open proposals, and those published or rejected in the last 90 days, the rejected ones with their reasons;
- the open observations of earlier reviews, each with its id and its count so far;
- the knowledge sources that apply to every run, and the factory memory.

Goal's own prompt tells it to group the evidence by cause and propose one change per cause, as small as it can be, citing its items. It must not repeat a rejected proposal or something a recent version already did. When a cause is not a definition problem, such as a lost sandbox, a provider outage or a person's choice, it says so. It returns observations, proposals and memory facts (see [Memory facts](#memory-facts)).

An observation is a cause with the items it rests on and a count. Observations persist across reviews: a review that sees a cause again names the open observation, adds its items and adds its own count to the count so far. An observation is *watching* until a proposal addresses it, then *proposed*. One whose cause is not a definition problem is *not Jonah's*, and later reviews no longer update it.

A proposal is one hypothesis, stored as a set of exact edits. Each edit names a file and gives replacements: an old text that occurs exactly once in the file, and the new text that replaces it. A new file is given whole. A proposal also names the one number it should move: goald computes today's value from the line, and the review's value after is shown as an estimate.

goald drops a proposal, and logs `jonah proposal dropped` with the reason, when:

- it edits a file outside the allowlist. A proposal may edit `factory.yaml` and the `.md`, `.yaml` and `.json` files under `agents/`, `skills/`, `rules/` and `contracts/`. It may not edit scripts, `evals/` or any other file. It may not edit `memory.yaml` either: memory changes only through memory facts.
- it changes a part of `factory.yaml` outside the allowlist. A proposal may change an agent's `model` (to a model the organization's policy allows) and `skills`, the `skills` list (local `./skills/` sources only), `rules`, `contracts`, and a station's `wip`, `buffer` and `gate.maxRework`. goald compares the parsed file before and after. Agents and skills are matched by name and stations by id. A change to anything else is refused, including a station's checks, kind, action or agents, tools, budgets, triggers, intake, environments and `learning`, and a reorder of agents or stations.
- an old text does not occur exactly once in the version's file, or it gives whole content for a file that exists;
- the definition with its edits applied has a blocking problem that the version did not have;
- its title, summary or edits hold an invisible or bidirectional Unicode character, which can hide instructions from the person reading it.

How a proposal moves on:

- goald queues one review a day for each factory with Jonah on that has new evidence, at an hour of the day set by the factory's id. A person can queue one at once. Each factory has at most one review at a time.
- On a daily review, an observation becomes a proposal only once its count across reviews reaches `learning.jonah.proposeAfter`, 3 by default. Until then it shows as "Watching, n of proposeAfter". A review a person asked for may propose below it.
- Each factory numbers its proposals from 1. A proposal is `proposed`, `published`, `applied`, `rejected` or `superseded`. A new proposal supersedes an open one only when their edits overlap: both edit the same file and their old texts cover some of the same text there, or either gives that file whole. Proposals that touch other parts of a file both stay open.
- Approve applies the proposal's replacements to the current version's files, opens a draft from the current version, writes the results and publishes it as the person who approved. A change to the same file since the review does not stop it. Approve is refused when the factory already has an open draft. It is also refused as stale when an old text no longer occurs exactly once, so a proposal cannot overwrite newer work.
- Edit applies the replacements to the draft's copy of each file, so the person can continue in the builder. It is refused with `draft-conflict` when an old text no longer occurs exactly once in the draft.
- Reject needs a reason, and later reviews read it.
- To undo a published proposal, use Undo this change (see the next section). Restore also works, but it reverts everything published since.

## Memory facts

A review may propose facts for the factory's memory, `memory.yaml`, but only in a person's own words. Memory goes into every run's prompt, so goald keeps a fact only when it can check that a person wrote it.

- **The source.** A fact is the words of a person's note, answer to an agent's question, or send-back reason (a reject, a gate's reject or a move), when they hold beyond their item. An example is "Phones are 390 wide; check 375 too". Jonah gives each fact's text and the items it comes from, such as `#12`.
- **The check.** goald keeps a fact only when every item it names finished in the review's window, and each of them has an event by a person whose text holds the fact word for word. Runs of spaces and line breaks count as one space. A paraphrase is dropped, and so is anything an agent or a check wrote, because a fact inferred from agents' runs could carry injected instructions into every later prompt.
- **Also dropped:** a fact over 300 characters, one with an invisible or bidirectional Unicode character, and one that memory already has. goald logs `jonah fact dropped` with the reason, never the fact's text.
- **One proposal per review.** goald writes the review's kept facts into one proposal of `memory.yaml`, titled with the number of facts it adds. Each new entry has `from`, its items as `<factory>#<number>`, and `added`, the day the review finished. Its evidence quotes each matched event. goald writes the whole file again, so comments in `memory.yaml` are not kept.
- **Near the limits.** Memory holds at most 50 entries and 8 KB. A fact may name in `replaces` the facts it replaces, which the proposal removes. A fact that names a fact memory does not have is dropped, and so is a fact that would still take memory past a limit.
- **An open memory proposal** is carried into the new one, which supersedes it, so no proposed fact is lost.
- A memory proposal is not held to `proposeAfter`: one person's checked words are the evidence. It moves no number, and it runs no tries. A person approves, edits or rejects it like any other proposal.

## The watch on every change

Goal watches every published version at each station it changes. A station's outcome for an item counts for the definition version of that item's run at the station, so an item that started before a publish still counts for the version it ran on.

- **What is compared.** For a Jonah proposal, the number it said it would move (its estimate's metric at its station). For a person's publish, the send-back rate and the cost per item at each station the change reaches. A change reaches a station when it changes what the station's agent gets: its agent file, a rule that applies to it, a skill it loads, its contract or its model.
- **Before and after.** Before covers the versions since the last change that reached the station. After runs from the change until the next change that reaches the station, so publishes that touch other stations do not end it.
- **The verdict** comes from a 90% interval of after minus before, where lower is better. It reads *Helped* or *Worse* when the interval clears the minimum effect: 5 points for a rate, 10% of before for cost or lead time. It reads *No change* when the interval lies inside that minimum, and *Too early* otherwise, with the items counted on each side. A watch that is still too early after 14 days, or after 150 items following the change, ends as *No clear change*. The verdict is final then.
- goald recomputes the watches every 15 minutes. The API shows them in each version's `watches` and in a proposal's `watch`.

### Undo this change

When a Jonah proposal's own number gets worse, Needs you shows a `watch_worse` entry that is not tied to an item. A person's publish never raises one; its watches show on the version.

Undo this change (`POST …/jonah/proposals/{number}/undo`, which needs `definition:edit`) writes a new proposal whose replacements are the published ones reversed, checked against the current version, whatever has been published since. The new proposal names the one it undoes, waits in Proposals like any other and publishes only when a person approves it. A file the proposal added stays in place; the reversed edits stop referring to it. The undo is refused with `undo-conflict`, naming the file, when the text the change wrote no longer appears exactly once. While an undo of the proposal is open, the Needs you entry goes away and a second undo is refused with `undo-open`.

## Try another model

Try another model proposes a different model for one agent, as an ordinary proposal. Run `goal agent try-model <agent> <model>`, or call `POST …/agents/{agent}/model-proposal` (it needs `definition:edit`).

- **The estimate comes first.** Goal picks the agent's five newest finished items: done items whose first run of a claim by the agent is at a station of the current version that can be tried. It shows how each went on the line, such as "passed first time, $0.42" or "sent back 2 times at review, $1.10". It plans three tries per item and prices them at each item's original run cost × 3. It also shows what is left of Jonah's budget. Nothing is stored yet. `goal agent try-model` asks before it goes on; `--yes` skips the question.
- **Then the proposal.** Once you confirm, goald writes a proposal against the current version. Its one edit is a replacement in `factory.yaml` that changes the agent's `model`, checked like a review's edits. The items with their line results are what it was tried on, and its page shows the estimate and the tries in place of Why and Found in, since the proposal has no evidence of sent-back runs. Three tries per item are queued with it, each rerunning the item's original run on the new model, in a box with only the model key; they run while Jonah is on for the factory. From there you approve, edit or reject it like any other proposal, and the watch follows it once it is published.
- **Refusals.** The model must be one the organization's policy allows, and not the agent's model already (`model-not-allowed`). An agent that needs MCP servers or secrets cannot be tried (`agent-not-triable`). An agent with no finished item gets `no-finished-items`. Tries that would cost more than what is left of Jonah's budget get `learning-budget-exhausted`, with the estimate.

## Tried on past items

Before you decide a proposal, goald tries it on past items. A proposal that changes what an agent gets, at a station whose agents need no MCP server or secret, is tried once on each item it cites, at most five, newest first. Each try reruns the item's original run with the proposal applied, in a box with only the model key. A model proposal gets three tries per item. A proposal that changes only memory or the flow, or reaches only stations that can't be tried, has no tries.

Tries run only while Jonah is on, while no item waits at an agent station, and while Jonah's budget for the last 7 days has money left. Their cost counts against that budget.

`GET …/jonah/proposals/{number}` shows the tries as `tried`:

- **Each item** with its tries by attempt. Each try is queued, running or done. A done try is `fixed` or `not_fixed`, with its reason first; `unknown` when its grade can't be trusted; or `error` when it could not be carried out, such as a lost box or a commit that is gone. An `error` is never counted as a failing try.
- **A model proposal's items** show how each went on the line beside its three tries, such as "sent back once at merge, $2.00". An item that is no longer among the agent's newest finished items shows no line result.
- **The cost and the time:** what the tries and the grader's checks cost together, and the seconds from the first try queued to the last one finished, once all are done.
- **The state:** `trying` while a try is queued or running, `done` when every try is done, and `budget_used`, shown as "Not tried: budget used", while a try is not done and Jonah's budget has run out. Waiting tries run and are graded once the 7-day window has room again.
- **The grader:** `grader` has the grader's two hit rates once it has been checked on an item (see below). `original_hit_rate` is the share of original results it read as not fixed, and `final_hit_rate` the share of final results it read as fixed. Each is 0 until it has a check.

Code grades a try first. A try whose output breaks the station's contract, as the proposal changes it, or fails one of the station's checks is `not_fixed`, with what failed as its reason.

A try at a gate station, where the proposal changes the gate's own agent, rules or contract, is then graded by what people did with the item after its original run, never by a model and never by the gate's own reasons. Goal reads the item's events from the original run's start up to its next claim at the gate or at a station before it:

- **Reject is right** when a person sent the item back: a person's rejection at the gate, a person's rejection back to the gate or before it, or a person's move back there. A person who moves the item forward to the gate overrules it, so that move does not count.
- **Approve is right** when the item merged and no person sent it back afterwards.
- **No answer** when the item is still open, was cancelled, or only the gate agent sent it back.

The try is `fixed` when its decision matches, and `not_fixed` when it differs or its output is not a gate verdict, as when the try's run failed. It is `unknown` when there is no answer. Its reason names the person's verdict or the merge.

A try at any other station whose run failed is `not_fixed`, as it passes no code grading. One that passes the contract and the checks goes to the grader: one model call, on the current line's model, with no tools, that reads why the item went back and a result, and answers `fixed`, `not_fixed` or `unknown` with its reason. The reason the item went back is the one given with its first send-back after its original run.

The grader is checked both ways first, once per proposal and item:

- **The original result**, the run that was sent back, should come out `not_fixed`.
- **The final accepted result**, the item's newest run at the station that succeeded, should come out `fixed`. An item with no such run has no final result, and that check counts as a miss.

When the grader misreads either one, its grade can't be trusted on that item: the item's tries are `unknown`, their reason names the result it misread, and they get no grade of their own. Otherwise the grader grades each try's output and change, and the try takes its answer and reason. A grade that gives no valid answer, or whose run fails, is `unknown` too.

The grader's runs count against Jonah's budget. When the budget runs out before a grade, the try waits with its run kept, and `tried.state` is `budget_used`. Once the budget has room again the try is graded without being run again.

A proposal that reaches a station is also tried on that station's [kept tests](#kept-tests): one try of each, or three for a model proposal. `tried.kept_tests` lists each of those tries with the test's item number, its station, its attempt and its result: `running` while it is queued or runs, then `passed`, `failed` or `error`. A proposal tried before kept tests ran on proposals has none.

### Try again

Try again adds tries to an open proposal, so you get more evidence before you decide it. Run `goal jonah try <number>`, or call `POST …/jonah/proposals/{number}/try` (it needs `definition:edit`).

- **What it adds.** One more try of each item the proposal was tried on, or three more for a model proposal. Each new try reruns the same run of the item, at the same station and on the same model, and is queued like the proposal's first tries. `tried` lists the new attempts after the earlier ones.
- **The estimate comes first.** Goal prices each new try at its item's original run cost and shows what is left of Jonah's budget. Nothing is queued yet. `goal jonah try` asks before it queues; `--yes` skips the question. Through the API, `{"confirm": true}` queues the tries.
- **When it is refused.** 409 `learning-budget-exhausted`, with the estimate, when the tries cost more than what is left of Jonah's budget. 409 `proposal-decided` for a proposal that is no longer proposed, and 409 `proposal-not-tried` for one that has no tries yet: Jonah queues its first ones. 422 `proposal-not-triable` for a proposal that can't be tried, such as one that changes only memory or the flow.

## Kept tests

A kept test is a past item that a person keeps, at a station it passed the first time, as a test. The item must keep passing there. Every change that reaches the station is tried on it: each proposal, and the draft you are about to publish. A failing kept test warns, and it never blocks a publish.

An item passed a station the first time when it was claimed there once, no gate rejected it there, and it then moved on. Its first run there must also have finished with its commit recorded, because a test reruns that run. An item sent back to the station counts as failing only once it is claimed there again.

- `GET …/jonah/tests` lists the factory's kept tests by station in line order, then newest first. Each shows the item's number and title, the station, who kept it and when, and `last_result`: the result of its newest finished try from a proposal or a publish (`passed`, `failed` or `error`), when it finished and the try's run. `last_result` is null for a test never run. It needs `factory:read`.
- `POST …/jonah/tests` with `{"item": <number>, "station": "<id>"}` keeps one. It needs `definition:edit`. It is refused with 404 `item-not-found` for an unknown item, 400 for a station that is not an agent station of the current line, 409 `not-passed-first-time` when the item did not pass the station the first time, and 409 `kept-test-exists` when the item is kept at that station already.
- `DELETE …/jonah/tests/{test}` removes one, and the item stays. It needs `definition:edit` and answers 404 `kept-test-not-found` for an unknown test.

From the CLI:

- `goal test keep <item> --station S [--factory F]` keeps an item, by its number, as a test at a station.
- `goal test list [--factory F]` lists the kept tests: station, item number and title, last result and when, who kept it, and the test's id.
- `goal test rm <id> [--factory F]` removes one.

Removing a kept test removes its tries. Deleting the factory or the organization removes its kept tests. In the web app, an item's Teardown tab keeps it as a test (Keep as a test), and the Tests tab lists and removes them.

### When a kept test passes

A kept test's try reruns the item's first run at the station, on the changed definition, in a box with only the model key. It passes when its run succeeds, its output satisfies the station's contract, the station's checks pass and, at a station with a gate, the output approves, as the gate did for the item the first time. Any other outcome fails it, with why: a failing check, a broken contract, a gate that rejects, or a run that failed. A try that could not be carried out, such as a lost box or a commit that is gone, is `error`, never `failed`.

### Kept tests at publish

When the publish dialog opens, it calls `POST …/draft/kept-tests` (`definition:publish`). goald finds the agent stations the draft reaches, the ones whose agent gets something different, and queues one try of each kept test there. The try runs on the current version with the draft's files. `GET …/draft/kept-tests` (`factory:read`) answers the same and starts nothing.

- **Reuse:** each try records a key of the exact content it runs on. A kept test that already has a try of the draft's content, from a proposal or an earlier publish, reuses it and queues nothing. So publishing a tried proposal does not rerun its kept tests, and calling the route again starts nothing new.
- **Order:** a publish's tries are claimed before any proposal's. Like every try, they wait while an item waits at an agent station, and while Jonah's budget for the last 7 days is used up.
- **The answer:** `state` is `trying`, `done` or `budget_used`, as for a proposal's tries. Each test comes with its `result`: null until its try is queued, then `running`, `passed`, `failed` or `error`, with the reason, the error and the try's run.
- **Tests that can't be tried** are answered `error` with why, and nothing is queued for them: a station whose agent needs MCP servers or secrets, an item with no finished first run there, a draft that deletes a file, or a factory with Jonah off.
- **No draft:** the route answers 404 `no-draft`.

Publishing never reads these results: a failing kept test is a warning. Approving a proposal publishes it without the dialog and starts no kept tests of its own.

## Jonah's budget

Jonah spends a share of what the line spends. Its budget is `learning.jonah.share`, 5% by default, of what the factory's runs other than Jonah's cost in the last 7 days. It covers reviews, tries, kept tests and Try another model.

- **Per review:** a review may spend at most a fifth of the budget, and its run's cost cap is what the budget has left, under that fifth.
- **When it runs out:** no daily review is queued, a review you ask for is refused with `learning-budget-exhausted`, and tries stay queued, shown as "Not tried: budget used". They run once the 7-day window has room again. Publishes go on.
- **The factory's budget:** Jonah's runs show in the factory's spend, but they never count toward `runtime.budget.weeklyUsd`, so they never trigger its `onExhausted` and never stop production work.
- A line that spent nothing in the last 7 days gives Jonah no budget, and `0%` turns its spend off.

The keys of `learning.jonah` are in the [definition reference](/docs/reference/definition/), and factory settings edit them in their Jonah section.

## How often it runs

goald runs the review loop every minute by default (`GOAL_JONAH_REVIEW_EVERY`). Each round queues the daily reviews that are due and runs the queued ones. The try loop runs every minute too (`GOAL_JONAH_TRY_EVERY`): it queues the tries of new proposals and runs the queued ones. The watches are recomputed every 15 minutes.

- **Look for improvements now:** `goal jonah look` and `POST …/jonah/reviews` queue a review at once. The answer is `review-in-flight` while one is pending or running, and `learning-budget-exhausted` once Jonah's runs of the last 7 days cost its budget.
- **The window:** a review reads what finished since the last successful review ended, or the last 30 days before the first. A failed review does not move it.
- **Retries:** a review whose run fails or whose answer does not parse is tried again, at most twice. After the third failure it ends `failed`, and its error shows as the last review in `GET …/jonah`. A spent budget or a deleted version fails it at once.

## Where you meet it

- The Jonah page has three tabs, each at its own address: Overview, Proposals and Tests. Overview shows the proposals waiting for your review, After publish (each published change's number before and after, with Undo this change on one that got worse), what Jonah noticed this week and how Jonah works here. Look for improvements now, in the top bar, starts a review at once.
- A proposal's page shows its estimate, the diffs, why (the items and their reasons) and Tried on past items. A memory proposal lists its facts with the quotes they come from. A model proposal has no Why or Found in; it shows its estimate and its three tries per item beside each item's result on the line. Approve and publish, Edit and Reject with a reason sit in the top bar, and on a phone Approve stays in the bar. While one of them waits on the server its button shows the work in progress ("Publishing v{N}…" for Approve and publish), and Approve then shows a check.
- The Tests tab lists the kept tests by station, with who kept each and when. A person who can edit the definition removes one from its row menu.
- `goal jonah` lists proposals. `show` displays the evidence and a unified diff, and `approve`, `edit` and `reject` decide one.
- `goal jonah try` tries a proposal again on its past items.
- `goal agent try-model` proposes another model for an agent.
- `goal test keep`, `list` and `rm` manage kept tests.
- `goal spend` shows what runs cost, Jonah's included.
- An item's Teardown keeps the item as a test, an agent's page has Try another model, the publish dialog shows the kept tests the draft reaches, Versions shows each change's watch, and Needs you shows a proposal whose number got worse.

The flags are in the [CLI reference](../../reference/). The decisions behind Jonah are ADR 0034 and ADR 0039 (`docs/adr/0034-jonah-proposals.md` and `docs/adr/0039-jonah-learns-from-the-line.md` in the repository).
