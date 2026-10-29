---
title: Andon
description: How a person stops a factory's whole line with the andon cord, what keeps running while it is pulled, and the rework-rate warning that does not stop anything.
sidebar:
  order: 11
---

The andon cord stops a factory's whole line. A person pulls it, with a reason, and the line stays stopped until a person clears it. The cord is for a problem that should stop all new work, not one item.

## What a pull does

While the cord is pulled, the engine does not move work for the factory:

- it does not release items from queue stations, and
- it does not start any new agent work.

Work that is already running finishes and moves on. People keep working as usual: they can submit items, and approve, reject, merge and retry them. A new submission enters the first station and waits there.

A pull needs a reason of 1 to 500 characters. Clearing it can carry an optional note of up to 2000 characters. Each pull and clear is written to the audit log.

The cord is one of three things that can stop a line. A pause on the line and a used-up weekly budget are the other two, and each stops work on its own. Clearing the cord does not restart a line that is paused or over budget. See [WIP and budget](/docs/concepts/wip-and-budget/) for the budget.

## Where the cord shows up

- The API has the pull (`pullAndon`), the clear (`clearAndon`) and a read of the open pull and its recent pulls (`getAndon`).
- An open pull appears in the **Inbox**, as an entry that needs a person.

## The rework-rate warning

The schema has `board.flow.andon.reworkRate` and `board.flow.andon.window`. They do not pull the cord. They set a warning.

`window` is how many of the last verdicts at one gate are counted, and it defaults to 10. `reworkRate` is the share of those verdicts that may be rejections. When one more rejection would reach that rate, the gate's entry in the andon read carries a warning, and the Inbox lists it as a rework warning.

The engine never pulls the cord on the rework rate. Automatic pulls are not built yet, and only a person can pull or clear the cord. For how rejections are counted and what sends work back, see [rework](/docs/concepts/rework/).

## Related

- [Gates and human touches](/docs/concepts/gates-and-human-touches/) explains the gates whose verdicts the warning counts.
- [Factory](/docs/concepts/factory/) explains the line the cord stops.
