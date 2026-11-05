---
title: Ask an agent what to file
description: Add a check to a schedule trigger so an agent looks at a source that cannot send webhooks and answers what is worth an item.
---

You end with a trigger that, on a schedule, runs an agent to read a source (an incident tool, a mailbox, a status page) and file only what is new and worth doing. Use a check when the source cannot send webhooks, or when nobody in your organization may add one.

## Add a check to a trigger

A check is the `check` block of a trigger whose `on` is `schedule`. Any other trigger refuses it.

```yaml
board:
  triggers:
    - name: new-incidents
      on: schedule
      cron: "*/5 * * * *"
      check:
        agent: watcher
        brief: ./triggers/new-incidents.md
        maxUsd: 0.10
        timeout: 5m
      create: { type: incident, station: triage }
      limits: { perCheck: 5, perDay: 20, pauseOnBudget: true }
```

- `agent` is any agent of the factory. The check runs with its environment: its tools, secrets and model. Give it read-only tools; publishing warns when the agent may use tools that are not read-only. For GitHub's MCP server, the warning names the tools the environment's last test found not read-only, or says their access is unknown when the server hasn't passed a test ([Access and secrets](/docs/concepts/access-and-secrets/)). A check only reads, whatever the agent's `code` setting, and it clones nothing.
- `brief` says what to look for and what is worth an item. It can name a Markdown file of the definition (`./triggers/<name>.md`).
- `maxUsd` is the most one check may spend (default 0.10), and `timeout` its time limit (default `5m`, at most `1h`).
- `create` holds the defaults for the items the check files. `create.trusted` applies to them as it does to a delivery's items.
- A `cron` that fires more often than every 5 minutes is refused at publish.

The agent's model is the definition's choice, so a cheap one fits. A check spends from the factory's budget like any run, and `pauseOnBudget` stops checks too.

## What a check is given and answers

The agent gets the brief, the open items this trigger filed (at most 50, with their key, reference, title, state and last note), the keys whose items closed in the last 7 days with how and why, the last check's decisions, and the `state` it saved last time (at most 4 KiB). No person is in the loop: it cannot ask a question, and tools set to `ask` are off.

It answers in a fixed shape:

- `file`: items to file, each with a `key` (the source's own id, such as the incident's), a `title` and a `brief`, and optionally `type`, `lane`, `priority`, `repositories`, `start` and `reopen` (a reason).
- `note`: `{key, text}` for an open item this trigger filed. It reaches the item as a note.
- `skip`: `{key, reason}` for what it looked at and left.
- `state`: what the next check gets.

## What goald enforces

- **Keys.** A `file` whose key already has an open item becomes a note on it. A key whose item closed is filed again only with `reopen`, linked to the old item; otherwise it is skipped with "closed on <date>: <reason>", so a source that stays open is not filed and closed on every check. A key takes at most one note an hour, and a repeated note is dropped.
- **Limits.** `limits.perCheck` (default 5) is the most items one check files. `limits.perDay` counts the items the trigger filed, not its firings. What is over either is recorded as skipped, "over the limit", and Needs you shows that once a day.
- **Repositories.** A `file` entry's `repositories` are matched against the factory's `runtime.repositories` as a trigger's `create.repositories` are ([Repositories of an item](../intake/#repositories-of-an-item)). An entry that names none gets the default entry. An entry that names a repository the factory doesn't have is skipped with "can't file it: acme/legacy is not one of SRE's repositories", and the other entries are still filed.
- **The door.** Each item is filed with origin `trigger`, the check's run as its filer, and untrusted unless the trigger has `create.trusted`: what the agent read is outside text, so a person merges what its items build.
- **One at a time.** A firing while the trigger's check is queued or running is skipped, "still checking". No check is queued while the factory's line is paused.
- **Failures.** An error, a timeout or an answer that breaks the fixed shape is recorded with its reason and files nothing. Three in a row show in Needs you.

## The check's page

A trigger with `check` opens a page with When (the schedule), Check, Create and Limits, and a log of checks beside them. Check sets the agent, shows the tools that agent has on, and holds the brief. Remembers shows how many of the trigger's items are open, how many closed this week, how far the last check got and Clear, which forgets the saved state; Clear is refused while a check is queued or running. Limits sets `perCheck`, `perDay`, what one check may spend and whether to pause when the weekly budget is spent. The title line shows today's checks, items and cost.

The log lists each check with its time, how it ended (Filed, Noted, Nothing new, Failed) and what it cost, and under it each key it filed, noted or skipped with the reason and a link to the item. Try a check queues a try; its row is marked Try and lists what it would do.

## Read the checks

From the CLI, or with `GET` on the same API path:

```sh
goal triggers checks acme-line new-incidents --limit 20
```

Each check shows when it ran, what it filed, noted and skipped with the reasons, and what it cost. `--limit` is 1 to 100.

## Try a check

Try runs the check now and shows what it would do. It spends like any check, and files nothing, writes no note, keeps no key, saves no state and counts toward no limit.

```sh
goal triggers try acme-line new-incidents --wait 6m
```

The command waits at most `--wait` for the answer and prints what it would file, note and skip. The API (`POST /v1/orgs/{org}/factories/{factory}/intake/triggers/{trigger}/checks`) queues the try and answers 202; it answers 409 `still-checking` while the trigger's check is queued or running, and 422 `no-check` for a trigger without a `check`. For a trigger without a check, Try runs on one of its [past events](../intake/#try-a-trigger-on-a-past-event) instead.

## Next

For the keys, read the [definition reference](/docs/reference/definition/). For how agents and tools are set, read [Add an agent, skill or rule](../add-an-agent-skill-or-rule/).
