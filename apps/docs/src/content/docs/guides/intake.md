---
title: Review what enters a factory
description: See where a factory's items came from, what is waiting to start, and which triggers create work, and try a trigger on a past event.
---

You end knowing where each of a factory's items came from and which triggers and webhooks create work, and you can add, switch off or try a trigger. Every item, from whatever source, enters the factory through the same door: it is checked against the factory's work types, lanes and stations, and it records where it came from.

## Where items come from

Each item records an origin and who filed it:

| Origin | What filed it |
| --- | --- |
| `person` | A person, on the web, from the CLI or with an API key. |
| `plan` | An assembly's plan, as a part. |
| `trigger` | A schedule, a webhook delivery or a check. |
| `follow-up` | An agent's follow-up that asked for an item of its own, or the factory's triage item. |

The factory's Intake page at `/o/<org>/f/<factory>/intake` counts the factory's newest items by source. The API answers more: `GET /v1/orgs/{org}/factories/{factory}/intake` counts the items created in a window of `7d`, `30d` (the default) or `90d` by origin, lists the items that have not started (`coming_up`, at most 50, with their total), the open triage item, and the organization's webhooks that are open to the factory. An item filed before origins were recorded counts as `person`.

The CLI prints the same picture:

```sh
goal intake acme-line --window 7d
```

It lists the items that have not started (when each may start, or the needs it waits for, and where it came from), the factory's items by origin over the window (default `30d`), the follow-ups waiting for triage and the items they join from, the triggers with checks marked, and the webhooks open to the factory, each receiving, with no trigger, or closed. `--json` prints it as JSON.

## Items that have not started

An item with a start time, or one waiting for other items, waits at the line's first station until it may start. The API lists these as `coming_up`: items with a start first, soonest first, then items that wait only for open needs, oldest first. To change a start or start an item now, read [Start an item later](../submit-and-follow-items/#start-an-item-later).

## Triggers

A trigger is a rule in the factory definition's `board.triggers` that creates an item from a schedule, a webhook delivery or a check. The Intake page lists each trigger with whether it is on, when it fires, what it creates, the items it created on each of the last 7 days and why it may not be creating any. From the CLI:

```sh
goal triggers list acme-line
goal triggers show acme-line red-main
```

`show` prints the trigger, what its items did in the last 7 days and its recent events: created, merged, skipped, missed or checked. A schedule that missed instants by more than 5 minutes does not fire late; it records one `missed` event per gap, with how many instants the gap covers.

A trigger matches an event with `on`, narrows it with `filter` and `only`, and creates the item with `create`. Event text in a title is quoted and cut to 200 characters. Every key is in the [definition reference](/docs/reference/definition/).

An event trigger creates at most 10 items an hour unless `limits.perHour` says otherwise. A schedule has no hourly limit.

### Untrusted items

An item made from a webhook delivery is untrusted: its brief carries the event's text inside `<untrusted-event-data>`, which agents treat as data and never as instructions, and a person merges what the item builds. Follow-ups from an untrusted item wait instead of being delivered or filed. A trigger's `create.trusted: true` lifts this for deliveries you vouch for; publishing refuses it for the `gitlab` and `token` senders and for GitHub without `filter.authors`.

## Suggestions

The API and `goal intake` return a definition's disabled triggers as `suggestions`.

The web app does not show those yet. Its Intake page and its New trigger dialog (under "Or start from a suggestion") offer a fixed set of suggested triggers, grouped as For the factory, Incidents, Code health, Pull requests and Security. Use opens the trigger page with the source and event chosen. A suggestion whose event Goal cannot create yet has Use turned off.

## Try a trigger on a past event

Try shows what a saved trigger would have done with one of its 20 newest events, without creating an item, recording an event or changing a limit. It reports each step (the filter, the checks and the limits) and why it stopped, and the item it would create. It works for a schedule's firings and for a webhook's deliveries while the webhook still keeps their body (30 days).

On the trigger's page, choose Try with a past event and pick an event. From the CLI, take the event id from `goal triggers show`:

```sh
goal triggers try acme-line red-main --event <id>
```

The CLI prints the steps; `--json` prints them as JSON. A missed event, or a delivery whose body is no longer kept, cannot be tried. Try for a trigger with a check runs the check instead; see [Ask an agent what to file](../checks/).

## Next

- To receive events from outside Goal, read [Receive events with organization webhooks](../webhooks/).
- To look at a source that cannot send webhooks, read [Ask an agent what to file](../checks/).
- To see what agents leave for later, read [Follow-ups](../follow-ups/).
