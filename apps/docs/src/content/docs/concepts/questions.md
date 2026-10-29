---
title: Questions and answers
description: How an agent asks a person a question, what the item does while it waits, and how the answer reaches the next run.
sidebar:
  order: 25
---

When an agent cannot decide something on its own, it asks a person a question. The run ends, and the item waits until someone answers. The answer is recorded on the item and the item goes back to its station's queue, so the next run starts with the answer in hand. Questions are the way an agent stops for a person; a tool that needs approval works the same way, as described in [skills and rules](../skills-and-rules/#tools-that-ask-first).

## Asking

Every run is served one tool for this, `question__ask`. It takes:

- `question`: the question in plain words, 1 to 500 characters. Required.
- `choices`: up to 4 quick answers, each 1 to 60 characters. The person can always write their own answer.

An item has at most one open question. A second question while one is open is refused, and the agent reads the reason. Once the question is recorded, the run ends and the item's lease ends with the outcome `asked`. The decision is ADR 0018 (`docs/adr/0018-agents-ask-questions.md` in the repository).

## Answering

A person answers with the CLI:

```sh
goal answer <item> "Use the staging database"
goal answer <item> --choice 2
```

The first form gives a free answer. The second picks one of the offered choices by its number. Either way the answer is recorded as `question_answered`, with the person who gave it, and the item returns to its station's queue.

## When a question is withdrawn

Moving, pausing, rejecting or cancelling an item withdraws its open question without an answer. The question is no longer open, and no answer is recorded.

## Where you meet it

- The item shows its open question while it waits.
- `goal answer` answers it from the command line.
- The question and its answer are events on the item, so they appear in its history.

The flags are in the [CLI reference](../../reference/).
