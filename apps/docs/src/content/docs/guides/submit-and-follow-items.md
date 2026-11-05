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

Open the factory's Board and choose New item. The sidebar's New item does the same from any page, and the keyboard shortcut is `C` (every shortcut is listed in [Navigate and use shortcuts](../navigate-and-shortcuts/)). Choose the factory, fill in the brief and choose Create item. The new item opens on the Board in the station where it entered the line.

The Start control sets when the item may start:

- **Now** sends no start. The item is released as soon as the first station can take it.
- **Later** takes a date and a time, and the button becomes Schedule. A date and time are read in your notification time zone, or UTC when you saved none.
- **After items** takes the refs of items of the same factory, such as `goal#2, goal#3`. The item waits until they are done.

If Goal refuses the start, for example a time more than 365 days ahead or an item that is cancelled, the reason shows under the Start control and nothing is created.

## See what has not started

An item with a start time or after items waits at the first station and is marked everywhere it appears:

- The Board's first column lists it under **Later**, below the items that can start, soonest first, with its start date where the others show their age.
- Items shows "Starts Nov 19" in its status column.
- Intake lists it under **Coming up**, soonest first, with where it came from. **Start now** there, or in the item's banner, clears the start time; the item is released on the next tick. An item that waits only for other items has no Start now, because those items still hold it. See [Intake](../intake/).
- The item's page shows a banner, "Starts Thu, Nov 19 at 09:00", with **Start now** and **Change**. Change sets another time or other items; choosing Now in it starts the item.

Once the item has left the first station, its start cannot change, and the banner is gone.

## Read where an item came from, and its follow-ups

An agent that notices work outside its item records a follow-up. The item's page shows them on the Timeline tab, in **Follow-ups**, each with where it went:

- A **note** goes to the open item it names, right away. It reaches that item's next run after its brief.
- A **start** follow-up becomes an item of its own, in this item's factory or another, when this item's work lands. It can wait a number of days or for other items first.
- A follow-up with neither is for the architect: it joins the factory's triage item when this item lands. Intake lists the items whose follow-ups wait for triage.

A waiting follow-up has **Drop**, which stops it from ever being delivered or filed. A dropped follow-up, or one whose triage item was closed, has **File**. File opens New item with the follow-up's text and files it in this item's factory, with its origin recorded as a follow-up.

The rest of the page tells how the item came to be:

- **Came from**, in Details, says whether a person filed it (and through New item, the CLI or the API), it is a part of an assembly, a trigger filed it, or a follow-up did.
- **Notes from other items** lists the notes that other items' agents, and trigger checks, left on this item, with their source. They are not a person's touch.
- When an agent closed the item with a reason, the page says **Closed by** that agent, with the reason. Closing is not a person's Cancel; the item is cancelled all the same.

## Create items from events and schedules

A factory can also create items on its own, from an event or a schedule. Those triggers are set in the factory definition (`board.triggers`) and listed on the factory's Intake page at `/o/<org>/f/<factory>/intake`, which [Intake](../intake/) describes.

On that page you can:

- create a trigger with New trigger. The dialog's "Or start from a suggestion" lists the factory's suggested triggers; Use opens the trigger page with that source and event chosen. A suggestion whose event Goal cannot create yet has Use turned off.
- try a trigger on one of its recent past events, which shows whether it would have created an item and why, without creating one,
- see the items each trigger created in the last seven days.

The same information is available from the CLI:

```sh
goal triggers list acme-line
goal triggers show acme-line <trigger>
```

Webhooks feed triggers from outside Goal. A webhook belongs to the organization and reaches only the factories it is open to:

```sh
goal webhooks create github-main --sender github
goal webhooks open github-main acme-line --repos acme/api
```

`create` makes the secret `webhook-github-main` and prints its value once, alone on stdout. Store the value in the sender at once; Goal does not show it again. To sign with a secret that exists, such as one PagerDuty or Sentry issued, pass `--secret <name>`. `open` without `--repos` lets every repository's deliveries reach the factory, and `goal webhooks close github-main acme-line` stops them.

## What comes next

When the item reaches a station that needs a person, it waits. To find those items, read [Read the Board, the item Timeline and Runs](../read-the-board-timeline-and-runs/). To act on one, read [Act on an item](../act-on-an-item/).
