---
title: Gates and human touches
description: How a gate decides whether work moves on, what a person is asked to decide, and what counts as a human touch.
sidebar:
  order: 5
---

A gate is a check on an agent's work. A reviewer agent at a station reads the work and returns a verdict: approve, or reject with a reason. The verdict decides whether the item moves on or goes back. A human touch is any action a person takes on an item. Goal counts them to measure how much human help the line needed.

## Gates

A station has a gate when its `gate` block is set. The gate's agent returns a verdict as its output, in this form:

```json
{ "decision": "reject", "reason": "The test for the empty list is missing.", "category": "tests" }
```

- `decision` is `approve` or `reject`. Any other value is an error.
- A rejection needs a `reason`. The upstream agent reads it and acts on it.
- `category` is optional. It groups rejections in the agent statistics. Goal does not name any categories.

On approval the item moves on to the next station. On rejection the item goes back to the station named by `board.stations[].gate.onReject`, and its rework count goes up by one. See [rework](/docs/concepts/rework/).

At a station with `waitFor: ci`, an approval whose run pushed commits takes effect only once CI on those commits passes, and a merge on approval waits with it. When CI fails, the gate's agent runs again with the failing checks in its brief. A rejection does not wait for CI. [Wait for CI](/docs/concepts/stations-and-flow/#after-a-run-pushes) explains the hold and its rounds.

A gate's rejections are limited by `board.stations[].gate.maxRework`, from 0 to 5. When it is omitted, the limit is 0, so the first rejection goes to a person. The limit is compared with the item's total rework count, from every source, not only this gate's rejections. Once the count has reached it, the next rejection leaves the item waiting for a person at the gate's station. A person then approves it or rejects it.

The gate's `onReject` must name a station upstream of the gate on the same line. The factory check fails when it does not. See [rework](/docs/concepts/rework/) for where rejected work goes.

## Merge on approval

A station with `action: merge` merges the item's pull requests when the item is approved there. It works at a human station, and at an agent station with a gate. The merge reads the pull requests from the station outputs of the item, and takes them in the order the outputs name them, each in its own repository.

Each pull request has one of these outcomes when it can't be merged:

- A conflict with its base sends the item back to be built again. With `flow.assemblies.onConflict: human`, the conflict blocks the item instead.
- A pull request that isn't mergeable yet, such as one whose required checks haven't passed, keeps the item at the gate, which runs again and merges it later. After 5 deferrals in a row, the item is blocked instead.
- Any other refusal blocks the item, with the reason.

Pull requests merged before a refusal stay merged, and merging again skips them. An item with pull requests in several repositories can therefore be merged in some of them and not yet in the others.

goald merges a pull request only when its head is the item's branch in that repository, or, for an assembly's landing, the assembly's integration branch there. A pull request from a fork or from any other branch is refused with a reason that names it and its branch, because no station checked it. goald also merges only in a repository an entry with `access: write` covers (see [repositories and what agents may write](/docs/concepts/access-and-secrets/#repositories-and-what-agents-may-write)).

An approved item waits at the station for a person to merge it in two cases: the organization's policy does not let agents merge, or the item was made from a webhook delivery whose trigger is not marked `create.trusted`, so its text comes from outside the organization.

## Human touches

A human touch is one action a person takes on an item. Every event a person causes on an item counts as a touch, such as an approval, a rejection, a merge, a move, a pause, a retry, a note, an answer or a release. Two kinds of event do not count: filing the item (`created`, `part_created`) and `lease_taken`. Each touch has a reason:

| Reason | When it is used |
| --- | --- |
| `required_gate` | The line asks for this decision, such as an approval at a human station, a gate approval or a merge. It is the default for those. |
| `missing_knowledge` | A person had to supply something the agent lacked. |
| `wrong_decision` | A person had to change a decision the agent made. |
| `other` | Anything else. |

A person can label a touch with a reason. A touch the line required is counted apart from an unplanned one, which the line did not ask for.

Two measures use touches:

- The **touchless rate** is the share of finished items that no person touched. The `TOUCHLESS` column of `goal status` shows it.
- The touch counts per station and per reason show where people had to help.

## Related

- [Rework](/docs/concepts/rework/) explains what happens after a rejection.
- [Andon](/docs/concepts/andon/) explains how a person can stop the whole line.
- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations a gate sits on.
