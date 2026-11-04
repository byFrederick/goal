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

## Where items show up

- The **Board** screen shows items in columns by station.
- Each item's page shows its state, its runs, its events and its trace.
- The **Inbox** lists items that wait for a person.

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations an item moves through.
- [Factory](/docs/concepts/factory/) explains the factory an item belongs to.
