---
title: Submit and follow items
description: Start work from the goal CLI or the web app, follow it until a person has to act, and read what it produced.
---

You end with a work item in a factory's line, and you know where it is and what it produced. An item enters the line at the station its work type starts at, and agents and people move it on from there.

## Submit from the CLI

```sh
goal run acme-line "Add a health check endpoint to the API" --type feature
```

`goal run` submits the item and then prints its timeline until a person has to act or the item is done. The brief is the text in quotes. Other flags:

- `--type` picks one of the factory's work types. Without it, the factory's first work type is used.
- `--body` adds a longer text to the brief.
- `--priority N` sets the item's priority.
- `--detach` submits the item and returns at once, without following it.

`goal run` prints the new item's reference, such as `acme-line#3`. Use that reference in the other commands.

## Start an item later

An item can wait to start: until a time, after other items are done, or both. It waits at the line's first station, where no agent works on it, and then moves on within seconds of the time passing and its items being done.

```sh
goal run acme-line "Rotate the staging certificates" --in 7d
goal run acme-line "Release the mobile app" --at 2026-11-20T09:00 --after acme-line#12
```

- `--at` is a time. A time without a zone, such as `2026-11-20T09:00`, is read in the time zone of your notification settings, or UTC when you saved none or submit with an API key.
- `--in` is a wait from now in whole hours, days or weeks: `36h`, `7d`, `2w`. Use `--at` or `--in`, not both.
- `--after` names an item of the same factory that must be done first. Repeat it for more items. An item that is already done is left out; a cancelled or unknown one is refused.

A start is at most 365 days ahead. `goal run` with a start prints when the item starts and returns without following it, as `--detach` does.

To change the start of an item that has not started, or to start it now:

```sh
goal start acme-line#14 --in 2d
goal start acme-line#14 --now
```

`--now` clears the time, so the first station releases the item on its next tick. Items it waits for still hold it; remove them with the item's needs. Once the item has left the first station, its start cannot change.

The API takes the same start: `start: {at, in, after}` when you submit an item, `PUT /v1/orgs/{org}/items/{item}/start` to change it and `DELETE` on the same path to start it now.

The wait does not count as lead time. An item's lead time counts from when it left the line's first station, or from when it was filed when it entered at a later station.

## Follow an item again

```sh
goal follow acme-line#3
```

This prints the item's events again from the start and keeps following. Use it after `--detach`, or after you closed a terminal.

## Read an item

```sh
goal item acme-line#3
```

This prints the item as JSON, with its outputs, such as the spec an agent wrote. Its human touches (who acted, where, whether it was planned, and why) go to standard error.

An item is `<factory>#<number>`, its id, or a bare number when only one factory has an item with that number. Add `--factory F` to pick the factory for a bare number.

## Submit from the web app

Open the factory's Board and choose New item. The sidebar's New item does the same from any page, and the keyboard shortcut is `C` (every shortcut is listed in [Navigate and use shortcuts](../navigate-and-shortcuts/)). Choose the factory, fill in the brief and choose Create item. The new item opens on the Board in the station where it entered the line. An item can also wait to start; the CLI and the API set a start, as [Start an item later](#start-an-item-later) shows.

Agents can leave work for later too; see [Let agents leave follow-ups](../follow-ups/).

## Create items from events and schedules

A factory can also create items on its own, from an event or a schedule. Those triggers are set in the factory definition (`board.triggers`) and listed on the factory's Intake page at `/o/<org>/f/<factory>/intake`. Each trigger has its own page there, where you set its event, filters, payload matches and limits and try it on a past delivery; read [Review what enters a factory](../intake/).

On that page you can:

- create a trigger with New trigger. The dialog's "Or start from a suggestion" lists the factory's suggested triggers; Use opens the trigger page with that source and event chosen. A suggestion whose event Goal cannot create yet has Use turned off.
- try a trigger on one of its recent past events, which shows whether it would have created an item and why, without creating one,
- see the items each trigger created in the last seven days.

The same information is available from the CLI:

```sh
goal triggers list acme-line
goal triggers show acme-line <trigger>
```

Webhooks feed triggers from outside Goal; see [Receive events with organization webhooks](../webhooks/). For what the Intake page and `goal intake` show, read [Review what enters a factory](../intake/). A trigger can also ask an agent what to file; see [Ask an agent what to file](../checks/).

## What comes next

When the item reaches a station that needs a person, it waits. To find those items, read [Read the Board, the item Timeline and Runs](../read-the-board-timeline-and-runs/). To act on one, read [Act on an item](../act-on-an-item/).
