---
title: Expedite and priority
description: How an item's class of service and priority decide which waiting item an agent slot pulls next.
sidebar:
  order: 9
---

When an agent slot is free, the engine pulls the next waiting item at that station. Two things decide which one: the item's class of service, and then its priority. Items of the same class and priority are pulled oldest first.

## Class of service

Every item has one class of service, `standard` or `expedite`. At each agent station, an expedite item is pulled before every standard item.

An item becomes expedite in one of two ways:

- its work type is listed in `board.flow.expedite.types`, or
- a trigger that creates it sets `board.triggers[].create.expedite`.

Assemblies pass their class to their parts.

## Priority

Priority is a whole number that a person sets when an item is submitted through the API. It defaults to 0. Within a class, a higher number is pulled first. A part gets its assembly's priority.

No field of the factory definition sets priority.

## What the engine enforces

The pull order above is enforced. Two expedite settings are not enforced yet:

- `board.flow.expedite.wip`, the cap on expedite items in progress, is accepted by the schema, but the engine does not apply it. Expedite items are not limited by it today.
- `board.flow.targets.expediteLeadTimeMinutes` is a target only. The flow metrics show each expedite item's lead time against it. The engine does not act on it.

## Where expedite shows up

- The **Inbox** lists open expedite items, so a person sees them even when they need no decision.
- The flow metrics show expedite lead time against its target.

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the queues an item is pulled from.
- [WIP and budget](/docs/concepts/wip-and-budget/) explains the caps that do limit work.
- [Work items and work types](/docs/concepts/work-items/) explains the work types that can be expedite.
