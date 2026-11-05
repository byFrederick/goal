---
title: Let agents leave follow-ups
description: Let an agent leave a note for another item or schedule later work, and see where each follow-up went, file one by hand or drop it.
---

You end with agents that can say where work they noticed should go: a note on an open item, an item of its own that starts later, or a place in the factory's triage item. A follow-up never blocks the run that wrote it.

## Turn follow-ups on

A factory reads follow-ups from the output field its definition names:

```yaml
board:
  flow:
    outputs:
      followUps: followups
    followUpsFrom: [00000000-0000-4000-8000-000000000020]
```

Add the field to the output contract of each station whose agent may leave them, as a plain array of strings or objects: no `$ref`, no `maxItems`, no `required` inside the items. Goald refuses any other declaration, because a strict shape could fail a run over a note. Without `outputs.followUps`, goald reads none.

Goald reads the first 10 entries of each accepted output, keeps each once per item, and never fails a run over one. An entry is a string or an object:

```json
{ "followups": [
  "Add an index on agent_runs.item_id",
  { "text": "The API needs this field too", "for": "acme#647" },
  { "text": "Rotate the certificates", "start": { "in": "7d", "title": "Rotate staging certificates" } }
] }
```

| Entry | Where it goes |
| --- | --- |
| A string, or an object with only `text` | The factory's triage item. |
| `for: "<factory>#<number>"` | A note on that open item. |
| `start: {title, at, in, after, type, lane, repositories, factory}` | An item of its own, filed when the work lands. `title` is required. `at`, `in` and `after` are an item's start (see [Start an item later](../submit-and-follow-items/#start-an-item-later)); `in` counts from when the work landed. |

With both `for` and `start`, `start` wins. The field names are in the [definition reference](/docs/reference/definition/).

## Follow-ups for other factories

A follow-up can name another factory, by `for` or by `start.factory`. The receiving factory takes it only when its own `board.flow.followUpsFrom` lists the sender's factory by `metadata.id`; the list is empty by default. In another factory, `after` items must be that factory's own. A factory takes at most 50 notes a day from other factories and files at most 20 follow-up items a day, counting every sender.

## What goald does with them

Each tick, goald delivers waiting notes as `item_note` events and, when an item reaches done, files its waiting follow-ups that have a `start`. [Work items](/docs/concepts/work-items/#follow-ups-filed-when-work-lands) lists the rules: the filed item's priority and generation, the caps, joining an open item with the same title, the generation limit of 3 and what happens on cancel.

## Triage

Follow-ups with no `for` or `start`, and those that could not go where they said, wait for the factory's triage item. It collects for 24 hours or until an agent claims it, and its title names its sources, as in "Follow-ups from software#513 and 4 more". Its brief quotes each follow-up with the station and agent that wrote it, and says they are notes from work in progress and not requests from people. The factory's first agent plans parts of at most 3, writes one spec or closes the item.

The Intake page, `goal intake <factory>` and the intake API show the open triage item and the items whose follow-ups will join it. See [The triage item](/docs/concepts/work-items/#the-triage-item).

## Close an item with nothing left to do

An item that a trigger or a follow-up filed, such as a triage item, can be closed by the agent when nothing remains. Name the output field in `board.flow.outputs.close`, and the agent writes 10 to 500 characters of reason in it. Goald cancels the item with that reason, and the item shows "Closed by the architect". A person's item and a part are never closed this way: the output fails its check and the agent asks a person.

## File or drop a follow-up

On an item, each follow-up shows where it went or will go. Through the API, a person with the matching permission can:

- drop a waiting follow-up: `POST /v1/orgs/{org}/items/{item}/follow-ups/{id}/drop`. It is never delivered or filed.
- file one from a dropped follow-up, or from one in a triage item that is done or cancelled, including one closed by its agent: `POST /v1/orgs/{org}/items/{item}/follow-ups/{id}/file`, with `{title, body, type, lane, start}` where only `title` is required. The new item has origin `follow-up` and you as its filer.

[Act on an item](../act-on-an-item/#drop-or-file-a-follow-up) has the permissions and the errors. Follow-ups of an item made from a webhook delivery are never delivered or filed on their own; a person drops one and then files it, or leaves it.

## Next

To see what is waiting to be filed, read [Review what enters a factory](../intake/).
