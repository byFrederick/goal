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

A gate's rejections are limited by `board.stations[].gate.maxRework`, from 0 to 5. When it is omitted, the limit is 0, so the first rejection goes to a person. Once an item has used up the gate's rework allowance, its next rejection leaves the item waiting for a person at the gate's station. A person then approves it or rejects it.

The gate's `onReject` names a station on the same line. The engine refuses to send work to a station that is not on the line.

## Merge on approval

A station with `action: merge` merges the item's pull requests when the item is approved there. It works at a human station, and at an agent station with a gate. The merge reads the pull requests from the station outputs of the item.

When the organization's policy does not let agents merge, an approved item waits at the station for a person to merge it.

## Human touches

A human touch is one action a person takes on an item. The actions that count are approvals, rejections, merges, moves, pauses, retries, notes and releases. Each touch has a reason:

| Reason | When it is used |
| --- | --- |
| `required_gate` | The line asks for this decision, such as an approval at a human station, a gate approval or a merge. It is the default for those. |
| `missing_knowledge` | A person had to supply something the agent lacked. |
| `wrong_decision` | A person had to change a decision the agent made. |
| `other` | Anything else. |

A person can label a touch with a reason. A touch the line required is counted apart from an unplanned one, which the line did not ask for.

Two measures use touches:

- The **touchless rate** is the share of finished items that no person touched. The **Floor** screen shows it.
- The touch counts per station and per reason show where people had to help.

## Related

- [Rework](/docs/concepts/rework/) explains what happens after a rejection.
- [Andon](/docs/concepts/andon/) explains how a person can stop the whole line.
- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the stations a gate sits on.
