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
- **Column order:** a station with a buffer, such as Build, shows its **In progress** cards first, at the top. The **Ready, buffer n of m** cards sit below them, dimmed, under their own label. The same order holds on a phone.
- **Show N more:** a column shows its first six In progress cards, then a **Show N more** button that reveals the rest. In a station with a buffer, the button ends the In progress group, above the Ready cards. **Show fewer** folds the column back.
- **Assembly line:** a card that is part of an assembly shows the assembly's title, one pip per part (filled for each merged part) and its place, such as `2 of 4`.
- **Waiting** items say what they wait on: a person's decision, another item, or a clock. A waiting card and its row in the list use the same words.
- **Constraint:** the column of the line's constraint, the drum, carries a yellow mark and the word **Constraint** in its header. The buffer's **Ready, buffer n of m** group is in that column, whichever station it is. Which station that is comes from the factory definition's `flow.constraint`; without it, the line's detected constraint.
- **Expedite** items sit in a yellow-tinted **Expedite** row at the top of each column. Its caption gives the lane's use, such as `Expedite, 1 of 1 in use, jumps every queue`. The row stays when the lane is empty, and shows whenever the line has an expedite lane.

A card that moves to another column or lane, because an agent finished or because you moved it, travels there instead of jumping, and the cards around it shift to make room. When your system asks for reduced motion, the card appears in its new place at once. A card you drag follows the pointer and settles into place when you drop it; the drop then opens the Move menu to confirm.

The search box and the **Board | List** switch are in the top bar. Four dropdown buttons next to the title filter by work type, lane, assembly and agent role; each choice is kept in the page's address, so a link reproduces the view. On a phone, the **Filter** button shows them, with status and grouping by assembly, and the Board has one tab per station.

The Items page at `/o/<org>/f/<factory>/items` lists the same items as a table, with saved views and CSV export.

## Needs you

`/o/<org>/needs-you` lists what waits on a person, across factories: approvals, decisions, questions and alerts. Work from it, then open each item to act on it. [Act on an item](../act-on-an-item/) covers the actions.

## The item page and its Timeline

Open an item from the Board or the Items page. Its URL is `/o/<org>/f/<factory>/items/<ref>`, where `<ref>` is the item's number.

The page has four tabs. Move between them with the arrow keys; Home and End jump to the first and last. The selected tab is in the URL as `?tab=`, and Timeline leaves it out:

- **Timeline** shows each run's events as grouped steps in plain words: the station, the agent, what it did and how it ended.
- **Spec** shows the request and the acceptance criteria from the spec the agents wrote.
- **Changes** shows the outputs the stations produced, and says so when nothing has been produced yet.
- **Checks** shows the checks that have run on the item, and says so when none have.

The timeline and the note field are the main column on the left. To the right of them, the details column shows the item's details, its cost, the items it overlaps with, and the rules its runs received.

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
