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

## Merge stations take turns

An agent station with `action: merge` claims at most one item per merge target at a time, so the pull requests that land on one branch do not race each other's CI. The other items with that target wait in the queue until the item holding the turn is done, sent back or blocked. An item held for CI after its push (see [wait for CI](#wait-for-ci)) keeps the turn while CI runs, so no other item merges into the same target in between. Items with other targets still take the free slots. [Integration branches](/docs/concepts/integration-branches/#one-merge-per-target-at-a-time) explains which branch is an item's target.

A station's `merges` field sets this:

| Value | What the station does |
| --- | --- |
| `serialize` | The default. One item per merge target at a time. |
| `allow` | Several items into the same merge target at once. |

`merges` applies only to a station with `action: merge`. Nothing is claimed at a human station, so there a person decides the order.

## Wait for CI

A station's `waitFor` makes goald wait for CI on the commits an item's runs pushed: a run at the station starts only once CI on the item's current commits has finished. goald polls the code host itself, so the agent never polls for CI and no run box waits. The setting names no host, CI system, workflow or check: goald reads every check the code host reports for the commit.

The commits are the latest each repository was pushed to. For each repository the item's runs pushed a change to, goald takes the head of the latest push. When one of the item's pull requests is in that repository, it takes the pull request's current head instead, since someone else may have pushed to it. An item no run pushed to has nothing to wait for, so a new item starts its run at once.

When the item comes up for a claim and CI has not finished on one of its commits, goald starts a wait on that commit and puts the item back in the station's queue without starting a run. The item is not claimed again until every wait has ended. A wait ends when every check finished, when no check appeared within `grace`, or when `max` passed. The run then starts, and its brief opens with a `## CI` section: for each repository, the commit, how CI ended, and each check's name and result with its link and failed jobs. A failed check does not stop the run; the agent reads it in that section.

### After a run pushes

When a run at the station pushed commits and its output would move the item forward (a gate's approval, or a station without a gate finishing), goald holds that move until CI on what the run pushed has finished. The item goes back to the station's queue with one wait per repository the run pushed to, and the output is not applied yet. At an `action: merge` station nothing is merged while CI runs.

When the wait ends, the item's next claim acts on it:

- CI passed, or no check appeared within `grace`: goald applies the held output without running the agent again. The item moves on, and an `action: merge` station merges its pull requests then.
- A check failed, or CI did not finish within `max`: the station runs again. Its brief's `## CI` section lists the failing checks, and the agent fixes and pushes again, or rejects.
- The token cannot read CI: the item blocks for a person, as below.

Each push starts a new CI round. Once a visit to the station has had `rounds` rounds, the next push blocks the item for a person, and the reason names the checks that failed in the last round. A move to another station, or a person's retry, starts a new visit and a new count.

A rejection goes back to the gate's `onReject` station at once, without waiting for CI. A run that pushed nothing moves the item on at once, as at a station without the setting.

When the token cannot read the repository's CI, the item blocks for a person. The reason names the repository, the commit, where the token comes from (the `runtime.repositories` entry or `runtime.repository.token`) and the permission it needs, such as read access to Checks and Commit statuses on GitHub or `read_api` on GitLab. Give the token that permission, then retry the item: goald waits for CI again. Hidden CI never counts as passed.

The short form uses the defaults:

```yaml
waitFor: ci
```

The long form changes any of them. A field left out takes its default, and `max` must be above `grace`:

```yaml
waitFor: { ci: { grace: 5m, max: 60m, rounds: 3 } }
```

| Field | Default | Meaning |
| --- | --- | --- |
| `grace` | `5m` | How long after a wait starts a first check may take to appear. A commit with no check by then has no CI, and the wait ends. |
| `max` | `60m` | The longest goald waits on one commit. The wait ends then: before a run, the run starts and the `## CI` section says CI timed out; after a push, the station runs again. |
| `rounds` | `3` | How many CI rounds after a push one visit to the station gets. The push after the last round blocks the item for a person, naming the failing checks. |

The decision is ADR 0044 (`docs/adr/0044-goald-waits-for-ci.md` in the repository).

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
