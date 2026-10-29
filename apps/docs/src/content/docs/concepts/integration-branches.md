---
title: Integration branches
description: How an assembly's parts can merge into one integration branch that lands on the default branch at a gate, and how that differs from the default trunk.
sidebar:
  order: 8
---

By default each part of an assembly merges into the default branch on its own. The default branch takes the feature one part at a time, so it holds part of a feature until the last part is done.

An integration branch changes that. The parts of an assembly merge into one branch, and that branch lands on the default branch once, at a gate, when the whole assembly is ready. The default branch sees the feature all at once, behind one review.

Integration branches are opt-in. The default is `trunk`, where each part merges into the default branch.

## Turning it on

The setting is `board.flow.assemblies.integration`:

- `board.flow.assemblies.integration.mode` is `trunk` (the default) or `branch`.
- `board.flow.assemblies.integration.minParts` is the fewest parts an assembly needs to get a branch. An assembly with fewer parts stays on trunk. The default is 0.
- `board.flow.assemblies.integration.refresh` says when the branch takes in the default branch. `onLand` is the default, which updates it when it lands. `eachPart` updates it before each part runs.
- `board.flow.assemblies.integration.landAt` names the station where the branch lands. When it is omitted, the branch lands at the first station after the planning station that merges.

A line in `branch` mode needs a station that merges, and `landAt` must name a station with a gate and `action: merge`. See the [definition reference](/docs/reference/definition/) for each field.

## How a branch is set up

- An assembly's integration branch is named `goal/a<root number>-<root title slug>`. The root is the assembly.
- The branch is set once, when the first part's run starts, if the assembly has at least `minParts` parts and no pull request yet. After that it does not change. An assembly that has no branch runs on trunk to the end, even if the definition changes later.
- Each part builds on the assembly's integration branch. A part whose pull request was already opened against another base keeps that base.

## Landing

When the last part is done or cancelled, the assembly does not close. It moves to its landing station and waits there in the queue.

The landing run opens a pull request from the integration branch into the default branch. Approval at the gate merges it, as it would any item. An assembly whose parts changed nothing can land with no pull request.

If the landing is rejected, the gate sends the assembly back to its `onReject` station. There, the work goes back on the integration branch and the pull request stays open.

## Related

- [Assemblies and parts](/docs/concepts/assemblies-and-parts/) explains how the parts are made.
- [Gates and human touches](/docs/concepts/gates-and-human-touches/) explains the landing gate and what a person decides there.
