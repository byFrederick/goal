---
title: Assemblies and parts
description: How an item becomes an assembly of parts, how parts wait for each other, and how an assembly closes.
sidebar:
  order: 7
---

An assembly is an item that has been split into parts. Each part is worked as a separate item, and the parts ship together. The assembly is the parent that holds them. A part is one of those items, counted in its assembly's progress.

## How an item becomes an assembly

A station's output can carry a `plan`: a list of parts, each with a `key`, `title`, `brief` and optionally `lane`, `paths` and `needs`. When the station finishes the item with a non-empty plan, and `board.flow.assemblies.materialize` is on, the item becomes an assembly. Its parts are queued at the station that planned them, and each part gets its brief.

`materialize` is on by default. When it is off, the plan is ignored and the item moves on to the next station as usual.

A gate that rejects work ignores the plan. An approving gate creates the assembly without merging anything, because the parts make the pull requests.

## How parts wait

A part can list the parts it `needs` in its plan. A part is not claimed while a part it needs is neither done nor cancelled. The item's waiting reason names the parts it waits for.

Parts can also wait for each other on files. When two items' `paths` overlap, the item that is further back on the line waits for the other one to finish. This is controlled by `board.flow.assemblies.overlap`, which defaults to `serialize`. See [integration branches](/docs/concepts/integration-branches/) for how parts share a branch.

A part gets a note in its run's feedback that names its assembly and the other parts, and it is told to return a spec for its own work, never a plan. Parts cannot be assemblies themselves, so assemblies do not nest.

## How an assembly closes

An assembly is never claimed by an agent. It stays at the station that planned it, in the state `assembly`.

When the last open part finishes, the assembly closes in the same step:

- it becomes `done` when its parts are done, and
- it becomes `cancelled` when every part was cancelled.

When an assembly has an integration branch, it does not close at that step. It moves to its landing station first. See [integration branches](/docs/concepts/integration-branches/).

## Acting on an assembly

Pausing, cancelling and retrying an assembly apply to its parts, each as the same action would apply to one item, all in one step. A person can also add a part to an open assembly, through the API.

## Progress

An assembly shows how many of its parts are done, how many are in progress, how many wait, and at which stations they are.

## Related

- [Work items and work types](/docs/concepts/work-items/) explains the item a part is.
- [Rework](/docs/concepts/rework/) explains what a rejected part does.
- Every field is listed in the [definition reference](/docs/reference/definition/).
