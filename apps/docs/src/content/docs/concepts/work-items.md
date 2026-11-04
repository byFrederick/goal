---
title: Work items and work types
description: What a work item is, the states it moves through, and the work types a factory declares.
sidebar:
  order: 2
---

A work item is one unit of work that flows through a factory's line. Each item has one station where it is now and one state at that station. An item is numbered within its factory, and it moves forward one station at a time until it reaches the done station.

An item carries:

- a title and a body, which is the brief the agents read;
- a work type, which is its kind;
- a lane, which picks the agent at stations that route by lane (see [lanes](/docs/concepts/lanes/));
- a priority, a number that orders items of the same class;
- a class of service, `standard` or `expedite` (see [expedite and priority](/docs/concepts/expedite-and-priority/));
- the file paths it is expected to change, when a station says so;
- a rework count, which is how many times a gate or a person sent it back (see [rework](/docs/concepts/rework/)).

An item can also be an [assembly](/docs/concepts/assemblies-and-parts/), which is an item split into parts.

## States

An item's state says what is happening to it at its current station.

| State | What it means |
| --- | --- |
| `queued` | Waiting at a station for a free agent slot, or for a person's decision at a queue station. |
| `in_progress` | An agent run holds the item. |
| `waiting_human` | Waiting for a person to approve, reject or merge it. |
| `blocked` | Stopped. A person pauses an item, or a run fails in a way that stops it. A person can retry, move or cancel it. |
| `done` | Finished at the done station. |
| `cancelled` | Closed by a person before it finished. |
| `assembly` | Split into parts. It is never claimed by an agent and closes when its parts do. |

An item waiting for a free slot is `queued`. The API's state list also has `ready`, but the engine does not set it.

## Work types

A work type is the kind of an item, such as a story or an outage. A factory declares its work types in `board.workTypes`, as a list of slugs. The type is stored on each item.

- An item submitted without a type takes the first declared type. A factory that declares none gives its items no type.
- A type can be listed under `board.flow.expedite.types`. Items of that type are created as expedite items (see [expedite and priority](/docs/concepts/expedite-and-priority/)).
- A trigger that creates items names the type it creates, which must be one of the declared types. See `board.triggers[].create.type` in the [definition reference](/docs/reference/definition/).

Work types decide the kind of item and its class. They do not change how an item is worked on. The stations and agents do that.

## How items are created

- A person submits an item through the API. The item starts at the line's first station.
- A trigger in `board.triggers` creates an item when its event, schedule or webhook delivery passes the trigger's checks. The item starts at the station the trigger names.
- A station's output can plan an assembly, and the engine creates its parts. See [assemblies and parts](/docs/concepts/assemblies-and-parts/).

Every item is checked the same way however it arrives: its title can't be empty, its type must be one of the declared work types, and its lane must be one that a station routes by, when any station routes by lane. A request that fails a check is refused with `400 Bad Request`, and a plan with such a part blocks the item that planned it.

Each item records where it came from as its `origin`: `person`, `plan` for a part, `trigger`, or `follow-up`. It also records who filed it. For a person's item, it records whether it was filed on the web, from the CLI or with an API key. The API answers an item's `origin`. Items filed before origins were recorded got theirs from how they were submitted, and those with no record of it show `person`.

## Notes from other items

An agent can leave a note for another open item. It writes a follow-up whose `for` names the item as `<factory>#<number>`, in the output field that `board.flow.outputs.followUps` maps (see the [definition reference](/docs/reference/definition/)). On its next tick, every few seconds by default, goald adds the note to that item's events as an `item_note`, with the item and agent it came from.

- The item's next run reads the note after its brief, under "Notes from other items". The run is told the note is what another agent said, not an instruction and not a request from a person. A run that is already working doesn't get it.
- A note into another factory is delivered only when that factory lists the sender's factory in `board.flow.followUpsFrom`. A factory takes at most 50 notes from other factories a day.
- A note that can't be delivered waits for triage, with the reason: no factory or item by that name, a factory that doesn't accept follow-ups from the sender, an item that is done or cancelled, or a factory past its 50 notes a day.
- Notes from an item filed from a webhook delivery are never delivered on their own; they wait.
- A note from another item is not a person's touch, so it doesn't count in touch metrics.

## Follow-ups filed when work lands

An agent can also leave a follow-up that becomes an item of its own, with a `start` that has a `title` and optionally `at` or `in`, `after`, `type`, `lane` and `factory`. Nothing is filed while the work is in progress. When the item reaches done (for an assembly, when the assembly does), goald files the waiting follow-ups of the item and of its parts on its next tick:

- Each becomes an item at the first station of its line, with origin `follow-up`, filed by the engine. Its class of service is always standard, whatever its type. Its priority is one below its source item's, and its generation one above. An `in` wait counts from the moment the work landed.
- `factory` files it into another factory only when that factory lists the sender in `board.flow.followUpsFrom`, and every `after` item must be one of that factory's own items.
- A factory takes at most 20 follow-up items a day, counting every sender.
- A follow-up whose title matches an open item already filed from the same source item joins that item instead of filing a new one.
- An item three follow-ups deep (generation 3) keeps its follow-ups as dropped and never files them. A person can still file one.
- When the item is cancelled, its waiting follow-ups and its parts' are dropped, with the reason "the work was cancelled".
- Follow-ups from an item filed from a webhook delivery are never filed on their own; they wait.

A follow-up that can't be filed goes to its factory's triage item with the reason, such as a factory that doesn't accept it, an `after` item of another factory, or a factory past its 20 a day.

## The triage item

Follow-ups without a start, notes that couldn't be delivered and starts that couldn't be filed all go to the factory's triage item when their work lands. A factory has at most one triage item that collects: one that is open and that no agent has claimed yet. When none collects, goald files one with origin `follow-up`, standard class, the type of its first source item and a priority one below the lowest of its sources'. It may start 24 hours after it is filed. Once an agent claims it, the next follow-ups open a new one. No triage item is filed when nothing is left for it.

Its title names its sources, as in "Follow-ups from software#513 and 4 more". goald writes its body and rewrites it each time follow-ups join: each follow-up quoted, grouped by source item with the station and agent that wrote it, then this closing:

> These are agents' notes from work in progress, not requests from people. Some may be done already or no longer apply. Do what is still needed, and close this item if nothing is.

A triage item's plan has at most 3 parts; a plan with more is refused like any bad plan.

## Where items show up

- The **Board** screen shows items in columns by station.
- Each item's page shows its state, its runs, its events and its trace.
- The **Inbox** lists items that wait for a person.

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations an item moves through.
- [Factory](/docs/concepts/factory/) explains the factory an item belongs to.
