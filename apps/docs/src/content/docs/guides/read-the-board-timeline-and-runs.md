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
- **Expedite** items sit in a yellow **Expedite** box in each column, all at the same height across the board; the label is in the first column's box. The boxes show whenever the line has an expedite lane, even when empty. From 1280 px wide every station fits without scrolling sideways; on narrower screens the board scrolls inside its own frame. On a phone the lane shows just the **Expedite** chip above the station's expedite cards.

A card that moves to another column or lane, because an agent finished or because you moved it, travels there instead of jumping, and the cards around it shift to make room. When your system asks for reduced motion, the card appears in its new place at once. A card you drag follows the pointer and settles into place when you drop it; the drop then opens the Move menu to confirm.

The search box is in the top bar. The **Board | List** switch shows there from 1024 px wide; on narrower screens, open the list from **Items** in the sidebar. Four dropdown buttons next to the title filter by work type, lane, assembly and agent role; each choice is kept in the page's address, so a link reproduces the view. On a phone, the **Filter** button shows them, with status and grouping by assembly, and the Board has one tab per station.

The Items page at `/o/<org>/f/<factory>/items` lists the same items as a table, with saved views and CSV export.

## Needs you

`/o/<org>/needs-you` lists what waits on a person, across factories: approvals, decisions, questions and alerts. Work from it, then open each item to act on it. [Act on an item](../act-on-an-item/) covers the actions.

## The item page and its Timeline

Open an item from the Board or the Items page. Its URL is `/o/<org>/f/<factory>/items/<ref>`, where `<ref>` is the item's number.

The page has six tabs. The tab row sits right under the title and scrolls sideways on a phone. Move between them with the arrow keys; Home and End jump to the first and last. The selected tab is in the URL as `?tab=`, and Timeline leaves it out:

- **Timeline** shows each run's events as grouped steps in plain words: the station, the agent, what it did and how it ended.
- **Spec** shows the request and the acceptance criteria from the spec the agents wrote.
- **Changes** shows the outputs the stations produced, and says so when nothing has been produced yet.
- **Checks** shows the checks that have run on the item, and says so when none have.

The selected tab and the note field are the main column. From wide screens the details column sits to the right of them; on narrower screens it comes after the tab's content. It shows the item's details, its cost, the items it overlaps with, and the rules its runs received. The Trace and Teardown tabs hide it and use the full width.

- **Trace** shows every run of the item, oldest first, on one time axis. Each run lists its tool calls, checks, notes and human touches as rows. Select a row to see its detail beside the list. On a narrow column the axis shows fewer, round time marks, so labels never overlap. On a phone, each run is a list and the detail opens under the row. Open it from **Full trace** on the Latest steps card. It shows "No tool calls yet." before any run, and new steps appear as agents work.
- **Watch live** (the button in the item's header) opens the Trace tab and scrolls to the newest step of the run that is working (smoothly, or at once if your system reduces motion), and keeps new tool calls, results and agent messages in view as they arrive. Scroll up to stop following; select **Watch live** again to scroll back to the newest step and follow again. The button is disabled, with the tip "No run is working on this item", when no run is working or the item is not in progress.

- **Teardown** shows where the item's lead time, cost and human help went, worked out from its runs and events each time you open the tab. It has four figures: lead time against the factory's target, cost against the item's budget, the rework loops with their time and cost, and the human touches. Below them, **Time by station** splits each station's time into working and waiting. Stations follow the line's order, so a station the item was sent back to keeps its place; a station no longer in the line comes last. **Runs** lists each run with its agent, model, turns, time, cost and outcome. **Why it went back** lists each failed check or send-back with its reason, and links to that step in the Trace tab. **Human touches** lists each time a person stepped in and how long the item waited for them, and links to the Timeline. An item no agent has picked up shows "No breakdown yet". Runs takes the full width when nothing went back and no person stepped in; otherwise it shares the row with those two lists on wide screens. Below 1000 px the Waiting column is hidden and Runs is a list.

The same history is available from the CLI:

```sh
goal item acme-line#3
goal follow acme-line#3
```

`goal item` prints the item and its outputs as JSON, and its human touches on standard error. `goal follow` prints the events from the start and keeps following.

`goal item <item> --teardown` prints the item's teardown instead: where its lead time, cost and human help went. It shows the lead time against its class's target and the cost against the item's budget ("no target" or "no budget" when there is none, "so far" while the item is open), then the time by station, the runs, why it went back and the human touches:

```console
$ goal item acme-line#3 --teardown
lead time 2h 5m, target 3h; cost $1.20, no budget
rework: 1 loop, 40m, $0.30
human touches: 1, waited 12m

Time by station
STATION  WORKING  WAITING  TOTAL
build    1h       5m       1h 5m
review   45s      59m 15s  1h
total    1h       1h 4m    2h 5m

Runs
STATION  AGENT     MODEL            TURNS  TOOK  COST   OUTCOME
build    engineer  claude-opus-5-5  31     50m   $0.90  finished
build    engineer  claude-opus-5-5  9      10m   $0.30  check failed
total    2 runs                     40     1h    $1.20

Why it went back
  rejected at review, to build: the tests do not cover the empty list (40m, $0.30)

Human touches
  review  2026-10-24 15:05 UTC  waited 12m
```

An item that does not exist, or that you cannot see, exits with an error and prints nothing.

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
