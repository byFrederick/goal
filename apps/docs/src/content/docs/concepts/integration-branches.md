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
- The assembly has its integration branch in each repository its parts work in, with the one name. When a part's run starts on the branch, the assembly takes the branch in each of the part's repositories, and a repository the assembly did not have joins it. The branch is set once in each repository, like the name.

## Landing

When the last part is done or cancelled, the assembly does not close. It moves to its landing station and waits there in the queue.

The landing covers the union of the repositories of its done parts. As the assembly moves to its landing station, it takes the integration branch in each of them, so the landing run clones all of them. A repository only a cancelled part worked in is left out. An item has at most 10 repositories, so a repository past that limit is left out, and its part's change does not land.

The landing run opens a pull request from the integration branch into the default branch, in each repository where the branch has changes. Its prompt names the command for the host of the assembly's first repository: `gh pr create --base <default branch> --head <branch>` on GitHub, and `glab mr create --target-branch <default branch> --source-branch <branch>` on GitLab. An agent that only reads is told to push nothing and open no pull request. Approval at the gate merges each pull request whose head is the integration branch in its repository, with the token of the entry that covers that repository (see [the repository and the model](/docs/concepts/access-and-secrets/#the-repository-and-the-model)). An assembly whose parts changed nothing can land with no pull request.

If the landing is rejected, the gate sends the assembly back to its `onReject` station. There, the work goes back on the integration branch and the pull request stays open.

## One merge per target at a time

An agent station with `action: merge` merges one item per merge target at a time. The merge target is the branch an item's pull request merges into:

- A part of an assembly with an integration branch merges into that branch.
- A standalone item, an assembly's landing and a part in `trunk` mode merge into the default branch. They share one turn per factory, even when they are in different repositories.

While one item of a target is at the station, the other items with that target wait in its queue for their turn. They are not blocked. The station's free slots still take items with other targets, so parts of different assemblies merge at the same time. When the item holding the turn is done, is sent back to an earlier station or is blocked, the next item in pull order takes the turn. An item whose merge is deferred back to the queue keeps its turn, and so does one that waits for a person to answer a question or approve a tool.

The station's `merges` setting controls this. `serialize`, the default, takes turns. `allow` merges several items into the same target at once. See [stations and the flow](/docs/concepts/stations-and-flow/#merge-stations-take-turns).

## Related

- [Assemblies and parts](/docs/concepts/assemblies-and-parts/) explains how the parts are made.
- [Gates and human touches](/docs/concepts/gates-and-human-touches/) explains the landing gate and what a person decides there.
