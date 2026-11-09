---
title: Assemblies and parts
description: How an item becomes an assembly of parts, how parts wait for each other, and how an assembly closes.
sidebar:
  order: 7
---

An assembly is an item that has been split into parts. Each part is worked as a separate item, and the parts ship together. The assembly is the parent that holds them. A part is one of those items, counted in its assembly's progress.

## How an item becomes an assembly

A station's output can carry a `plan`: a list of parts, each with a `key`, `title`, `brief` and optionally `lane`, `paths`, `needs` and `repositories`. A part's `repositories` are the ones it works on; a part that names none takes its assembly's (see [an item's repositories](/docs/concepts/work-items/#an-items-repositories)). When the station finishes the item with a non-empty plan, and `board.flow.assemblies.materialize` is on, the item becomes an assembly. Its parts are queued at the station that planned them, and each part gets its brief.

`materialize` is on by default. When it is off, the plan is ignored and the item moves on to the next station as usual.

A gate that rejects work ignores the plan. An approving gate creates the assembly without merging anything, because the parts make the pull requests.

## How parts wait

A part can list the parts it `needs` in its plan. A part is not claimed while a part it needs is neither done nor cancelled. The item's waiting reason names the parts it waits for.

Items can also wait for each other on files, but parts of one assembly never do: the overlap hold skips an item's own assembly and its sibling parts, so parts wait on files only for items outside their assembly. When two such items' `paths` overlap in the same repository, the item that is further back on the line waits for the other one to finish. A path that starts with `owner/name:` is in that repository, and a path without a prefix is in its item's first repository, so `internal/**` in acme/api and `internal/**` in acme/web don't overlap. Repositories compare by owner and name, whatever their host. This is controlled by `board.flow.assemblies.overlap`, which defaults to `serialize`. See [integration branches](/docs/concepts/integration-branches/) for how parts share a branch.

A part gets a note in its run's feedback that names its assembly and the other parts, and it is told to return a spec for its own work, never a plan. Parts cannot be assemblies themselves, so assemblies do not nest.

## How an assembly closes

An assembly is never claimed by an agent. It stays at the station that planned it, in the state `assembly`.

When the last open part finishes, the assembly closes in the same step:

- it becomes `done` when at least one part is done, even if other parts were cancelled, and
- it becomes `cancelled` when every part was cancelled.

When an assembly has an integration branch, it does not close at that step. It moves to its landing station first. Its integration branch has one name and is in each repository its parts work in, and its landing covers the union of its done parts' repositories. See [integration branches](/docs/concepts/integration-branches/).

## Acting on an assembly

Pausing, cancelling and retrying an assembly apply to its parts, each as the same action would apply to one item, all in one step. A person can also add a part to an open assembly, from the assembly page or through the API. See [Act on an assembly](/docs/guides/act-on-an-item/#act-on-an-assembly).

## Dependencies between items

`board.flow.dependencies` in the factory definition names the output fields that set an item's dependencies:

- `after` is the output field that lists the open items (`<factory>#<number>`) the work must follow. Each becomes a linked edge from the item, or from the root parts of its plan.
- `paths` is the output field whose list of file globs replaces the item's `paths` when a station finishes with it. The overlap hold above reads these paths.

Both are empty by default, and then no output sets them. See the [definition reference](/docs/reference/definition/).

## Seeing what is in flight

A station with `context: [inflight]` gets an `## In flight` section in each run's feedback. It lists the factory's other open items and assemblies with their paths, those closest to the request first. Set it on a station in `board.stations[].context`.

`board.flow.assemblies.changedSince` is off by default. When it is on, a station with `context: [inflight]` records the default branch's head as the item's base commit when it finishes the item. Every later claim of the item then gets a `## Changed since your plan` section: the commits on the default branch since that base that changed files the item's paths match, oldest first.

## Keeping a branch current with the default branch

`board.flow.assemblies.freshBase` says how a run brings an existing item branch up to date with the default branch. `rebase` is the default and rebases the branch. `merge` merges the default branch into it. What happens when that conflicts is set by `board.flow.assemblies.onConflict` (see [rework](/docs/concepts/rework/)).

## Progress

An assembly shows how many of its parts are done, how many are in progress, how many wait, and at which stations they are.

The assembly page's Plan lists each part that isn't being worked with what holds it, worded as on its Board card: the parts it needs (`Waits for software#41`), a merge turn (`Waits for software#42 to land`), or CI on its commits (`CI · 9 of 14`, with its commits and checks behind an ⓘ). A part that waits for nothing shows its state, or Merged once it is done. `GET …/items/{item}/parts` gives each part this as `waiting`, the same object the item answers, or null.

## Related

- [Work items and work types](/docs/concepts/work-items/) explains the item a part is.
- [Rework](/docs/concepts/rework/) explains what a rejected part does.
- Every field is listed in the [definition reference](/docs/reference/definition/).
