---
title: Act on an item
description: Answer a question, approve, deny, send back, merge, retry, move, note, pause, cancel or re-lane an item, from the CLI or the item page.
---

You end with an item moved on, held, or closed as you meant, and a record of who did it. Each action has a CLI command and, where the item page shows the button, a control in the web app. The CLI form is shown first.

Items are named `<factory>#<number>`, as in [Submit and follow items](../submit-and-follow-items/#read-an-item). A person needs the matching permission for each action; without it, the web app disables the control and says who decides.

## Answer a question

An agent that needs an answer asks it, and its run waits until someone answers.

```sh
goal answer acme-line#3 "Use the existing config package"
goal answer acme-line#3 --choice 2
```

Use the text form for your own words, or `--choice` with the number of one of the question's choices, counted from 1. On the item page, the question card shows the choices and a Write an answer field.

## Approve, or deny a tool call

At a station that waits for a person, approve the item to let it move on:

```sh
goal approve acme-line#3
```

An agent can also wait for permission to use a tool. Approving it lets that call go through. `--always-allow` does the same and lets that agent role make that call without asking from now on; the CLI needs the `connection:manage` permission for it. Deny the call with a note, which the item's next run reads:

```sh
goal deny acme-line#3 --note "Use the staging connection"
```

On the item page and in Needs you, the Approve button approves, the Deny button denies, and an Always allow checkbox appears when you may set it.

## Send back

Send an item back to rework when its output is not good enough. A reason is required, and the next run reads it.

```sh
goal reject acme-line#3 --reason "The spec leaves out the error cases"
```

On the item page, Send back to the previous station asks for the reason in a dialog.

## Retry a blocked item

A blocked item goes back to its station's queue:

```sh
goal retry acme-line#3
```

The same command resumes an item you paused. On the item page, the Retry button does this.

## Merge

At a station whose action is `merge`, approving the item merges its pull request first:

```sh
goal merge acme-line#3
```

The Merge button on the item page does the same. Goal merges the pull request, then approves the item.

## Note

A note is text the item's next agent run reads. Leave one when you want the agents to know something the item does not say:

```sh
goal note acme-line#3 "The API version is pinned in go.mod"
```

On the item page, the note composer's Send note button leaves the note. A note counts as a human touch on the item.

## Move to another station

Put the item at another station of the same line:

```sh
goal move acme-line#3 review --reason "Needs a second look"
```

A run in progress is stopped first. Moving an item to an earlier station needs a reason, and counts as rework. On the item page, use More, then Move.

## Pause and resume

Pause stops the item where it is. A run in progress is stopped with it.

```sh
goal pause acme-line#3 --reason "Waiting for the vendor"
goal retry acme-line#3
```

Retrying a paused item resumes it. On the item page, Pause and Resume do the same.

## Cancel

Cancel closes the item for good, where it is. Its pull request stays open.

```sh
goal cancel acme-line#3 --reason "Superseded by acme-line#4"
```

On the item page, Cancel does the same. Cancelling an assembly also cancels its open parts.

## Set the lane

Where a station routes work by lane, a lane decides which agent takes the item. Set it on an open item that is not in progress:

```sh
goal lane acme-line#3 backend
```

On the Board and in Items, Set lane changes it from the item's menu.

## Related

- [Read the Board, the item Timeline and Runs](../read-the-board-timeline-and-runs/) to see where an item waits.
- [Concepts](/docs/concepts/) for stations, gates, leases and human touches.
- [CLI reference](/docs/reference/) for every flag.
