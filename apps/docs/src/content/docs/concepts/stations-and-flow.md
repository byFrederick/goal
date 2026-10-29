---
title: Stations and the flow
description: What a station is, the four kinds of station, and how work moves through a factory's line.
sidebar:
  order: 3
---

A station is one step of a factory's line. Work moves from one station to the next, in the order the stations are listed in `board.stations`. The line and the rules that govern how work moves along it make up the factory's flow.

A factory's line can have any number of stations, for example spec, build, review and integrate. The names, the kinds and the order are the factory's own choice. Goal does not name any station in code.

## Kinds of station

Each station has a `kind`, one of four.

| Kind | What happens there |
| --- | --- |
| `queue` | Holds items and releases them to the next station. No agent or person works at it. |
| `agent` | Agents work the items. Each item is claimed by a free slot, and the agent's output moves it on. |
| `human` | A person approves or rejects each item. Approval moves it on. |
| `done` | Finished work. Items arrive here and stay. |

An agent station names one agent with `agent`, or picks one by lane with `route`. A station can have more than one slot, set by `agents`. A station's `wip` caps how many items it works on at once. At an agent station the cap is the smaller of `agents` and `wip`.

An agent station can also have a [gate](/docs/concepts/gates-and-human-touches/). A gate checks the agent's output and can send the item back. A human station's `action` says what happens on approval.

## How an item moves

1. A new item starts at the first station of the line. A trigger can start it at another station.
2. At each station the item waits in the `queued` state. An agent station claims it when a slot is free. A human station waits for a person, and the item is in `waiting_human`.
3. When a station finishes the item, the item moves to the next station in the list, or to a rework station if a gate sent it back.
4. The item reaches the done station and its state becomes `done`.

Two things can change this path. A [gate](/docs/concepts/gates-and-human-touches/) can send the item back to an earlier station, which is [rework](/docs/concepts/rework/). An item can also be split into [parts](/docs/concepts/assemblies-and-parts/), which move on as separate items.

The station's output must pass the contract named by its `output` field and the commands in its `checks` before the item moves on.

## Flow settings

The line's rules for how work is released and paced sit in `board.flow`. Some of them are used today and some are not:

- `board.flow.assemblies`, `board.flow.dependencies` and `board.flow.retry` decide how parts, branches, overlaps and retries behave. Each has its own page.
- `board.flow.expedite` and `board.flow.andon` are covered in [expedite and priority](/docs/concepts/expedite-and-priority/) and [andon](/docs/concepts/andon/).
- `board.flow.targets` gives the flow metrics their targets.
- `board.flow.release` and `board.flow.constraint` are accepted by the schema. The engine does not act on them yet. Queue stations release everything they hold.
- A station's `buffer` is accepted by the schema. The engine does not keep buffers yet.

The [WIP and budget](/docs/concepts/wip-and-budget/) page covers the caps the engine does enforce.

## Where the flow shows up

- The **Board** screen shows one column per station.
- The **Floor** screen shows each station's load and what waits for people.

## Related

- [Work items and work types](/docs/concepts/work-items/) explains what moves along the line.
- [Lanes](/docs/concepts/lanes/) explains how a station picks its agent by lane.
- Every field is listed in the [definition reference](/docs/reference/definition/).
