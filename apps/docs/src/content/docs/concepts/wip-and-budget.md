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

A factory's weekly budget is `runtime.budget.weeklyUsd`. A `weeklyUsd` of 0, or none, means no cap: the factory is never used up. Otherwise it caps what the factory's agent runs may cost over the last 7 days, counted as a rolling window of 7 times 24 hours. The cost counted is the cost of the runs the factory started in that window, except Jonah's: they show in the factory's spend but never use up its weekly budget ([Jonah](/docs/concepts/jonah/)).

Spend is checked once per engine tick. A factory is exhausted when its spend reaches the cap. Then the engine claims no new agent work for it. Queue releases and decisions by people carry on as before.

What happens to the runs already in progress depends on `runtime.budget.onExhausted`:

- `drain`, the default, lets the runs in progress finish. No new agent work starts.
- `stop` also cancels the runs in progress. Their items return to the queue of the station they were at.

Because spend is checked once per tick and nothing is reserved when work is claimed, a factory can go past its cap. The overshoot is bounded by the agent slots, the per-run cap and one retry for each run.

## Per-run limits

Two limits apply to each run rather than to the factory:

- `runtime.budget.perItemUsd` caps what one agent run may cost (the web UI calls it "Most per run"). An item that passes through several agent stations can spend it at each. An agent can set its own cap.
- `runtime.budget.maxTurnsPerTask` caps the turns of one task.

The engine passes these to each run as limits. It does not add them up against the weekly budget.

## Settings that are not enforced

- `runtime.budget.dailyUsd` is accepted by the schema, but nothing enforces it and the web UI no longer sets it. Only the weekly budget stops work. The day figure Home shows as Spend today is the weekly budget ÷ 7.

## Trigger limits

A trigger can skip events while the weekly budget is spent: `board.triggers[].limits.pauseOnBudget`. A trigger can also cap the items it creates per day with `board.triggers[].limits.perDay`. Both are described in the [definition reference](/docs/reference/definition/).

## Where the budget shows up

- **Spend** shows the last 7 days against the weekly cap, and an "Over budget" chip on a factory whose budget is used up.
- **Needs you** lists the used-up budget, as described next.

## A used-up budget on Needs you

A factory whose weekly budget is used up has an entry on **Needs you** titled "Weekly budget used up · $X of $Y". The line starts no new agent work while the entry is open. The entry carries `resumes_at`, the time the budget has room again on its own: the earliest moment the spend of the runs still in the 7-day window falls below the cap, as each run leaves the window 7 days after it started. The engine resumes on its first tick after that time.

Raising `weeklyUsd` removes the entry at once. To free the line sooner, raise the cap in the factory definition. `onExhausted` decides what happens to the runs in progress, as described above.

## Slot caps

A slot is one agent running. Two caps limit how many run at once, whatever each station's `wip` allows:

- an organization cap, across all its factories, and
- a cap for each factory.

Each is a whole number from 1 to 1,000, or none for no cap. Set them in the organization's **Settings → Slots**: one field for the organization and one for each factory. `goal org update <slug> --max-slots N` sets the organization cap (0 means no cap); see the [CLI reference](/docs/reference/cli/).

Running trigger checks count against both caps, as well as running agents. A full cap holds new work until a slot frees.

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations the WIP caps belong to.
- [Andon](/docs/concepts/andon/) explains the other way the engine stops a line.
