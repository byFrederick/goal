---
title: WIP and budget
description: The work-in-progress caps the engine enforces, and the weekly spending budget that stops agent work when it is used up.
sidebar:
  order: 10
---

WIP, work in progress, is the number of items a station is working on at once. A budget is the most a factory may spend on agent runs. Both are caps on how much agent work the engine starts. This page lists the caps the engine enforces today. It does not list the ones the schema accepts but the engine does not yet apply.

## WIP caps

A station's `wip` is the most items it works on at once. An agent station's capacity is the smaller of its `agents` (the slots it runs) and its `wip`. The engine never claims more items at a station than its capacity.

The `wip` cap applies at agent stations, where agents claim work. Two WIP settings are not applied yet:

- `board.flow.expedite.wip`, the cap on expedite items. See [expedite and priority](/docs/concepts/expedite-and-priority/).
- A station's `buffer`, the items kept ready in front of a station.

## The weekly budget

A factory's weekly budget is `runtime.budget.weeklyUsd`. It caps what the factory's agent runs may cost over the last 7 days, counted as a rolling window of 7 times 24 hours. The cost counted is the cost of the runs the factory started in that window.

Spend is checked once per engine tick. A factory is exhausted when its spend reaches the cap. Then the engine claims no new agent work for it. Queue releases and decisions by people carry on as before.

What happens to the runs already in progress depends on `runtime.budget.onExhausted`:

- `drain`, the default, lets the runs in progress finish. No new agent work starts.
- `stop` also cancels the runs in progress. Their items return to the queue of the station they were at.

Because spend is checked once per tick and nothing is reserved when work is claimed, a factory can go past its cap. The overshoot is bounded by the agent slots, the per-run cap and one retry for each run.

## Per-run limits

Two limits apply to each run rather than to the factory:

- `runtime.budget.perItemUsd` caps what one task may cost. An agent can set its own cap.
- `runtime.budget.maxTurnsPerTask` caps the turns of one task.

The engine passes these to each run as limits. It does not add them up against the weekly budget.

## Settings that are not enforced

- `runtime.budget.dailyUsd` is accepted by the schema. The engine does not enforce a daily cap. Only the weekly budget stops work.

## Trigger limits

A trigger can skip events while the weekly budget is spent: `board.triggers[].limits.pauseOnBudget`. A trigger can also cap the items it creates per day with `board.triggers[].limits.perDay`. Both are described in the [definition reference](/docs/reference/definition/).

## Where the budget shows up

The factory's screens show the spend against the weekly cap, and "Budget used up" once the cap is reached.

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations the WIP caps belong to.
- [Andon](/docs/concepts/andon/) explains the other way the engine stops a line.
