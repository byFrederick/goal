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

An agent can also wait for permission to use a tool. Approving it lets that call go through. `--always-allow` does the same and lets that agent role make that call without asking from now on; the CLI needs the permission to manage secrets and access for it (`connection:manage` in the API today). Deny the call with a note, which the item's next run reads:

```sh
goal deny acme-line#3 --note "Use the staging server"
```

On the item page and in Needs you, the Approve button approves, the Deny button denies, and an Always allow checkbox appears when you may set it.

On a phone, Needs you shows the list first. Choosing an entry opens only that entry, with Back in the top bar to return to the list where you left it.

## Fix an alert about a source or a conversion

Two alerts on the Alerts tab of Needs you are about a factory, not an item.

- **A source does not sync.** The title reads "product-specs, which Software reads, does not sync", with the factory's title or else its name. The second line is the source's last error and the number of failed syncs, such as "Confluence refused the token 3 times". The time on the row is how long ago the source last changed, not the time of its last good sync. The detail has **Open product-specs**, which opens the source's page.
- **A factory could not move to secrets.** The title reads "Software could not move to secrets", and the second line says why. The detail lists each problem to fix, one per line, and says the factory keeps running on its current version until a version on secrets is published. **Open the definition** opens the factory's definition editor.

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

## Keep an item as a test

On the item's Teardown tab, **Keep as a test** at a station the item passed the first time keeps the item as a test of that station. Jonah and Publish run kept tests against later changes. It needs `definition:edit`, and Goal refuses a station the item did not pass the first time. A toast says **Kept**.

To try another model for an agent, open the agent's page and choose **Try another model**. Pick a model your policy allows, read the estimate and confirm; Jonah tries it on what the agent finished lately and makes a proposal, and a toast says **Proposal made**.

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
