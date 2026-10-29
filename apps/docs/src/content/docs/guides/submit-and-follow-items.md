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

Open the factory's Board and choose New item. The sidebar's New item does the same from any page, and the keyboard shortcut is `C`. Choose the factory, fill in the brief and choose Create item. The new item opens on the Board in the station where it entered the line.

## Create items from events and schedules

A factory can also create items on its own, from an event or a schedule. Those triggers are set in the factory definition (`board.triggers`) and listed on the factory's Intake page at `/o/<org>/f/<factory>/intake`.

On that page you can:

- create a trigger with New trigger,
- try a trigger on one of its recent past events, which shows whether it would have created an item and why, without creating one,
- see the items each trigger created in the last seven days.

The same information is available from the CLI:

```sh
goal triggers list acme-line
goal triggers show acme-line <trigger>
```

Webhook endpoints feed triggers from outside Goal. `goal webhooks create acme-line github-main --kind github` creates one and prints its signing secret once. Store the secret in the sender at once; Goal does not show it again.

## What comes next

When the item reaches a station that needs a person, it waits. To find those items, read [Read the Board, the item Timeline and Runs](../read-the-board-timeline-and-runs/). To act on one, read [Act on an item](../act-on-an-item/).
