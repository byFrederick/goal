---
title: Intake
description: See which items are waiting to start, where items came from, the follow-ups waiting for triage, the triggers and the webhooks open to a factory.
---

You end knowing what is about to enter a factory's line and through which door. Open Intake from the factory's sidebar, or go to `/o/<org>/f/<factory>/intake`. The page has five sections; each has an ⓘ that says what it holds.

## Coming up

Items that have not started, soonest first, up to 50. Each row shows its start date and time, or what it waits for ("Waiting for goal#523"), the item's title and where it came from: who filed it and through what, a trigger, an assembly, or the item whose follow-up filed it.

**Start now** clears the item's start time, so the first station releases it on the next tick. The row shows a check, then leaves the list. A row without a start time waits only for other items; it has no Start now, since those items still hold it.

## Where items came from

How many of the factory's items created in a window came from a person, an assembly's plan, follow-ups or triggers. Choose 7, 30 or 90 days; 30 is the default. People counts New item, the CLI and the API.

## Follow-ups to triage

Follow-ups that name no item and no time. When an item lands, its triage follow-ups join the factory's open triage item, which the architect turns into work or closes. The first row is that triage item, with how many follow-ups have joined it and Open. The rows under it are items whose triage follow-ups are waiting to join when those items land. Without either, the section says no follow-ups are waiting.

## Triggers

The triggers of the factory's current definition, with when each fires, what it creates, how many items it created in the last 7 days, and its switch. A trigger that runs a check shows **Check** and the agent that runs it, because it asks that agent about its source instead of filing an item each time it fires. A trigger that is off in the definition is a suggestion: turning it on publishes a new version. New item, the `goal` CLI and the API are always on.

A trigger whose webhook does not exist, or whose deliveries were refused, says so under its name.

## Webhooks open to the factory

The organization's webhooks an admin opened to this factory, with their state:

- **Receiving**: the webhook is on and an enabled trigger of the factory names it.
- **No trigger yet**: the webhook is on, but no enabled trigger names it.
- **Closed**: the webhook is turned off.

The state comes from the definition, not from recent deliveries. **Manage** opens the organization's webhooks page.
