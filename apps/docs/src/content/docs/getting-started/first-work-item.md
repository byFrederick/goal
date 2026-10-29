---

title: The first work item
description: Submit one work item to a factory, follow it through the line, and answer the approval or question it stops at.
sidebar:
  order: 5
---

This page takes one work item from submission to an approval. You need a factory with a published version, as [Create a factory](/docs/getting-started/create-a-factory/) describes, and a sign-in with access to it.

In the examples, `<item>` is the item's reference, which `goal run` prints when it submits the item, or its id.

## 1. Submit the item

Give the item a title and submit it to the factory:

```sh
goal run <factory> "Add a health check endpoint"
```

The first line of the brief is the title. Anything after the first line becomes the body, and `--body` adds more text to it. `goal run` follows the item on the terminal until it reaches a human station, asks a question, or stops. Pass `--detach` to submit without following. Other flags: `--type` picks one of the factory's work types, and `--priority` orders the item ahead of others (higher first).

```sh
goal run <factory> "Add a health check endpoint" --body "Return 200 from /healthz when the database answers."
```

The web app can submit an item too. Setup submits the template's starter item at its last step, the First item step.

## 2. Watch it move

The item moves through the factory's line: its stations, the agents at each one, and the order they run in all come from the factory's definition. The screens show an example line of Intake, Spec, Build, Review, Done. A factory's own stations may have other names. [Concepts](/docs/concepts/) explains the flow.

To follow an item again after the terminal stopped following it:

```sh
goal follow <item>
```

To read an item's outputs as JSON, use `goal item`. It also lists, on the error output, each human touch on the item: who acted, where, and why.

```sh
goal item <item>
```

In the web app, the **Board** shows the factory's items by station. The item screen shows one item with its outputs. **Needs you** lists the items that wait for you, so start there when you come back to a factory.

## 3. Approve a step that needs a person

An item at a human station waits for an approval. Read its outputs with `goal item`, then approve it:

```sh
goal approve <item>
```

The same command also approves a tool call that the item's agent is waiting on. Pass `--always-allow` only if that agent role should call that tool without asking from now on. That is a standing rule, and it needs the `connection:manage` permission.

To refuse a tool call instead, deny it with a note. The agent's next run receives the denial and the note:

```sh
goal deny <item> --note "Use the staging connection, not production."
```

At a station that merges the item's pull request, `goal merge` merges it and then approves the item:

```sh
goal merge <item>
```

## 4. Answer a question

An agent can stop to ask a question. Answer it in your own words:

```sh
goal answer <item> "Use the existing database pool."
```

When the question offers choices, give the number of the choice instead. Numbers start at 1:

```sh
goal answer <item> --choice 2
```

## 5. Send it back for rework

If an output is wrong, send the item back for rework. A reason is required:

```sh
goal reject <item> --reason "The health check must not touch the database."
```

To leave a note for the agents without sending the item back, use `goal note <item> "<text>"`. The item's next run reads it.

To stop an item where it is, use `goal pause <item>`. `goal retry <item>` resumes it.

## Stop an item for good

`goal cancel <item>` closes the item where it stands. Its pull request stays open, so close that yourself if you no longer need it.

## Related

- [Concepts](/docs/concepts/): the factory, its flow, agents and questions.
- [Reference](/docs/reference/): every command and its flags.
