---
title: Rework
description: What rework is, what sends an item back, where it goes, and how the rework count and the gate limit work.
sidebar:
  order: 6
---

Rework is a round in which an item goes back to an earlier station to be tried again. The reason it went back is recorded on the item, and the next run at the earlier station works from that reason.

Three things send an item back.

| What sends it back | Where it goes | Counts toward the gate's limit |
| --- | --- | --- |
| A gate rejects the item | The station named by the gate's `board.stations[].gate.onReject` | Yes |
| A person rejects the item | The gate's `onReject`, or else the nearest agent station upstream | Yes |
| A merge conflict, when `board.flow.assemblies.onConflict` is `rework` (the default) | The same station as a rejection | No |

## What the item carries

Each rework has a reason. A gate's reason comes from its verdict. A person must give a reason when rejecting. A merge conflict's reason is the conflict itself. The reason is what the agent acts on, so it is required for a rejection. The engine refuses a verdict that rejects without one.

Each rejection adds one to the item's rework count. The flow statistics record each round and the station that sent the item back.

## Limits

A gate's `board.stations[].gate.maxRework` caps how many times the gate can send an item back. It is 0 when omitted, so the first rejection leaves the item waiting for a person. Once an item has used the allowance, the next rejection at that gate is not sent back. The item waits at the gate's station for a person.

A person's rejection is not capped. It is always a rework round, and it does not check the gate's allowance.

A merge conflict under `onConflict: rework` sends the item back, but it does not add to the rework count, and it does not use up the gate's allowance.

## Related

- [Gates and human touches](/docs/concepts/gates-and-human-touches/) explains the verdicts and the limit.
- [Assemblies and parts](/docs/concepts/assemblies-and-parts/) explains conflicts between parts.
- Every field is listed in the [definition reference](/docs/reference/definition/).
