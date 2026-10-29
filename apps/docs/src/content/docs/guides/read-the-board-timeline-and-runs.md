---
title: Read the Board, the item Timeline and Runs
description: Find what is moving, what is waiting and what each agent run did, on the Board, the item page and the Runs page.
---

You end knowing where each item is, why it is waiting, and what an agent run did. The Board shows the whole line, the item page shows one item's history, and the Runs page lists agent runs across the organization.

## The Board

Open a factory's Board at `/o/<org>/f/<factory>/board`.

- **Columns** are the line's stations, in the order work moves through them. Each card is an item.
- **Lanes** split each column by agent role, so you can see which role has the work.
- **Cards** show what the run on the item is doing now, and its plan's steps when it has one.
- **Waiting** items say what they wait on: a person's decision, another item, or a clock. A waiting card and its row in the list use the same words.
- **Expedite** items appear in their own tinted card in their station.

Filter the Board by assembly, agent role or status, and group it by assembly or not at all. On a phone, the Board has one tab per station.

The Items page at `/o/<org>/f/<factory>/items` lists the same items as a table, with saved views and CSV export.

## Needs you

`/o/<org>/needs-you` lists what waits on a person, across factories: approvals, decisions, questions and alerts. Work from it, then open each item to act on it. [Act on an item](../act-on-an-item/) covers the actions.

## The item page and its Timeline

Open an item from the Board or the Items page. Its URL is `/o/<org>/f/<factory>/items/<ref>`, where `<ref>` is the item's number.

The page has four tabs:

- **Timeline** shows each run's events as grouped steps in plain words: the station, the agent, what it did and how it ended.
- **Spec** shows the request and the acceptance criteria from the spec the agents wrote.
- **Changes** shows the outputs the stations produced, and says so when nothing has been produced yet.
- **Checks** shows the checks that have run on the item, and says so when none have.

Beside the tabs, the side column shows the item's details, its cost, the items it overlaps with, and the rules its runs received.

The Trace and Teardown controls on the item page are not available yet; the page shows them disabled.

The same history is available from the CLI:

```sh
goal item acme-line#3
goal follow acme-line#3
```

`goal item` prints the item and its outputs as JSON, and its human touches on standard error. `goal follow` prints the events from the start and keeps following.

## Runs

`/o/<org>/runs` lists the organization's agent runs, newest first, across every factory. Each row shows the factory, the item, the station, the agent, the model, when the run started, how long it took, its turns, its cost and its status.

Narrow the list with the search box and the filters. The CLI does the same:

```sh
goal runs --factory acme-line --status running
goal runs --since 2026-10-26T00:00:00Z --limit 20
```

`--status` takes `running`, `succeeded`, `failed` or `cancelled`. A long list prints a `--cursor` for the next page.

## Line health

```sh
goal status --factory acme-line --window 7d
goal andon acme-line
```

`goal status` shows work in progress, items done, lead time and how many human touches each finished item took. `goal andon` shows whether the line runs and the recent andon pulls.

## Related

- [Act on an item](../act-on-an-item/)
- [Submit and follow items](../submit-and-follow-items/)
- [Concepts](/docs/concepts/) for stations, lanes and runs.
