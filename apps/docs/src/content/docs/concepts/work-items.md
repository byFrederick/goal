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
- the file paths it is expected to change, when a station says so. A path in a repository other than the item's first starts with that repository's `owner/name:`, as in `acme/web:src/**`; a path without a prefix is in the item's first repository;
- a rework count, which is how many times a gate or a person sent it back (see [rework](/docs/concepts/rework/)).

An item can also be an [assembly](/docs/concepts/assemblies-and-parts/), which is an item split into parts.

## States

An item's state says what is happening to it at its current station.

| State | What it means |
| --- | --- |
| `queued` | Waiting to be claimed: for a free agent slot, for the items it needs to be done, for an open CI wait to end, or for its start time. |
| `in_progress` | An agent run holds the item. |
| `waiting_human` | Waiting for a person: to approve, reject or merge it, to answer an open question, or to decide on a tool approval. Approve is refused while a question or tool approval is open; answer or decide it first. |
| `blocked` | Stopped. A person pauses an item, or a run fails in a way that stops it. A person can retry, move or cancel it. |
| `done` | Finished at the done station. |
| `cancelled` | Closed before it finished. A person can cancel it. An agent can close an item whose origin is `trigger` or `follow-up` through `board.flow.outputs.close`. An assembly whose parts were all cancelled is cancelled too. |
| `assembly` | Split into parts. It is never claimed by an agent and closes when its parts do. |

An item waiting for a free slot is `queued`. The API's state list also has `ready`, but the engine does not set it.

## Start times

An item can wait to start, at the line's first station, where no agent works on it. Three things set the wait:

- `at` is a time. A time without a zone is read in the time zone of the person's notification settings, or UTC when none is saved or the item is filed with an API key.
- `in` is a wait from now, in whole hours, days or weeks, such as `36h`, `7d` or `2w`. It counts from the moment the item is filed, or for a follow-up from the moment the work landed. Use `at` or `in`, not both.
- `after` names open items of the same factory that must be done first. It can go with `at` or `in`.

A start is at most 365 days ahead. The item moves on within seconds after its time has passed and its items are done. The wait does not count as lead time.

A person sets a start in New item's Start control, with `goal run` and the API when filing, and changes it with `goal start` or the item's banner, **Change**. **Start now** clears the time. It is on the item's page and under Coming up in Intake. An item that waits only for other items has no Start now, because those items still hold it. Once the item has left the first station, its start cannot change. The commands, the API paths and where each screen shows a waiting item are in [Submit and follow items](/docs/guides/submit-and-follow-items/#start-an-item-later).

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

Each item records where it came from as its `origin`: `person`, `plan` for a part, `trigger`, or `follow-up`. It also records who filed it, and the API answers it as `created_by`: a person is named as the sidebar names them, or "A former member" after they leave the organization. Reading one item, and its teardown, names the person of each of its touches the same way. For a person's item, it records whether it was filed on the web, from the CLI or with an API key. The API answers an item's `origin`. Items filed before origins were recorded got theirs from how they were submitted, and those with no record of it show `person`.

Reading one item (`GET /v1/orgs/{org}/items/{item}`) also answers `came_from` for an item with origin `follow-up` or `trigger`:

- For a follow-up's item, `came_from.follow_ups` lists the follow-ups filed as it or onto it, oldest first. Each names the item whose run wrote it, the agent and station of that run, and the item whose landing filed it: the source item, or its assembly when the source is a part. Items of other factories are named the same way, as `<factory>#<number>`. The list is empty, never absent, when none of them can be read.
- For a trigger's item, `came_from.trigger` names the trigger and its kind: `check` for a trigger with a check, `schedule` for another schedule, `webhook` otherwise. The kind is absent when the factory's current definition no longer has the trigger. For a check, `key` is the key the check filed the item under. A check keeps only each key's last item, so an item whose key a later check filed again has no `key`.

Items of any other origin have no `came_from`.

## An item's repositories

Every item records the repositories it works on when it is filed, at most 10. Each one must be covered by one of the factory's `runtime.repositories` entries. Where they come from depends on how the item was filed:

- A person's item takes the repositories the person names: the Repositories field of New item, `goal run --repo`, or `repositories` in the API. New item starts with the default entry's repository.
- A trigger's or a check's item takes the ones its `create.repositories` names.
- A part takes the `repositories` its plan entry names. A part whose entry names none, and a part added to an assembly, takes its assembly's, in the assembly's order.
- A follow-up's item takes its `start.repositories`. When the start names none, it takes its source item's repositories that the receiving factory's entries cover.
- A triage item takes its sources' repositories, in the order its sources joined. When a source joins an open triage item, its repositories that the triage item lacks join it, until it has 10; the rest are left out.

A named value that no entry covers is refused, with the value quoted: a person's request with `400 Bad Request`, a plan by blocking the item that planned it, and a follow-up's start by sending it to triage with the reason. An item that gets no repository this way takes the default entry's. A factory without a default entry gives such an item none.

The item page's Details lists the repositories in the order they joined, each with how it joined: named at filing, default, an agent pushed to it, or named by a station's output. A factory that covers only one repository has no Repositories row.

An item's list grows in two ways while it is open. A repository a run pushes to joins it, joined `push`. A station's output can name repositories in the field `board.flow.outputs.repositories` maps; those the item lacks join it when the output is accepted, joined `output`. An item never has more than 10: a push past 10 is left off, and an output whose names would go past 10 fails its check, so the agent tries again.

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
- The **Needs you** screen lists items that wait for a person.

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations an item moves through.
- [Factory](/docs/concepts/factory/) explains the factory an item belongs to.
