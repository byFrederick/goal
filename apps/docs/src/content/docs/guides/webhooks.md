---
title: Receive events with organization webhooks
description: Connect a sender such as GitHub once for the organization, open it to the factories that may use it, and read what it delivered.
---

You end with a webhook that a sender posts events to, opened to the factories that use them, and a delivery log that says what happened to each delivery. A webhook belongs to the organization and is open to no factory until you open it. Managing webhooks needs `webhook:manage`, which owners and admins have.

## Create a webhook

```sh
goal webhooks create github-acme --sender github
```

`--sender` is one of `github`, `gitlab`, `standard` (the default), `pagerduty`, `sentry` and `token`. The name is unique in the organization, and a sender's name, `schedule` and `webhook` are reserved. The command makes a secret named `webhook-github-acme`, tells you the address senders post to, `POST /hooks/<webhook id>` on goald's public origin, and prints the secret's value once, alone on standard output (the rest goes to standard error, so you can capture the value). Store it in the sender at once; Goal does not show it again.

For a sender that issues its own value, such as PagerDuty or Sentry, sign with a secret that exists:

```sh
goal webhooks create incidents --sender pagerduty --secret pagerduty-signing
```

How each sender signs, and what goald answers to a delivery, is in [Receiving deliveries](/docs/concepts/access-and-secrets/#receiving-deliveries). The secret is a secret like any other, open to no factory, and no run can hold it: see [Webhook secrets](/docs/concepts/access-and-secrets/#webhook-secrets). Replacing its value keeps the previous one for 24 hours, so deliveries signed with either pass while you update the sender.

## Open it to a factory

```sh
goal webhooks open github-acme acme-line --repos acme/api,acme/web
goal webhooks close github-acme acme-line
```

An opening lets one factory receive the webhook's deliveries. With `--repos`, only deliveries from those repositories reach the factory; without it, every repository's do. Running `open` again replaces the repositories. `close` stops the factory's deliveries. A webhook that published triggers name (`github-acme.check_run`, `github-acme`) cannot be closed, renamed or deleted until they change, and goald answers 409 with their names. A trigger that names only the sender (`github.check_run`) does not hold a webhook: it still matches the sender's other webhooks.

Only a factory the webhook is open to can have a trigger on it. A trigger's `on` names the sender and event (`github.check_run`), one webhook's event (`github-acme.check_run`) or all of one webhook's events (`github-acme`). See [Review what enters a factory](../intake/).

## Connect GitHub without an app

One organization webhook, added by an owner of the GitHub organization, covers every repository, including ones created later; [Connect GitHub](../connect-github/) has the steps, and the repository webhooks for a personal account. A repository webhook posts to the same address for one repository. In GitHub, set the payload URL to the webhook's address and the secret to the value `create` printed. Send only the events your triggers use. GitHub's ping, sent when you add the webhook, is accepted and shown as "Received GitHub's ping".

## Read the delivery log

```sh
goal webhooks list
goal webhooks deliveries github-acme --limit 20
```

`list` shows each webhook's sender, address, signing secret's name, open factories, the triggers that use it and its deliveries in 7 days; never a secret's value. The 7-day count of delivered deliveries includes those no enabled trigger names, such as GitHub's ping or one that arrives before a trigger names the webhook, which goald only counts. The triggers are those of its open factories that name the webhook, enabled or not, and the enabled ones that name its sender with an event, by factory name, then trigger. `deliveries` lists what it received, newest first, in the columns RECEIVED, OUTCOME, RECEIPT, REASON, ITEM and EVALUATIONS. EVALUATIONS is `<factory>: <outcome>` for each factory, by factory name, or the evaluation's state (`pending`) while it has no outcome; a column with nothing to show prints `-`. With `--json` it prints the API's answer, where each delivery has its `event` and these fields:

- **`receipt`:** `accepted`, `duplicate` or `refused`, with the `reason` when refused. A duplicate keeps no row of its own: the webhook already took this delivery id, nothing is created, and the first delivery's `duplicates` counts it.
- **`evaluations`, per factory, by factory name:** `created` with the item and its ref (`acme#88`), `skipped` with the reason, or `failed` with the error.

The answer also carries `unnamed_days`: the webhook's counts of accepted deliveries no enabled trigger names, which the log does not list, as `{day, count}` per UTC day over the log's 30 days, oldest first. A day without one has no entry, and every page carries the same counts.

The web delivery log shows the same counts as one line under its rows, or under "No deliveries to show yet.": "{n} more deliveries no trigger names in the last 30 days", where n is the sum of `unnamed_days`. The line is hidden when n is 0.

`goal webhooks list --json` also gives each webhook's `pinged_at`, when goald last accepted GitHub's ping, and its `setup_events`: the events its enabled triggers use, the ones to choose in GitHub. `setup_all_events` is true when a trigger names the webhook with no event.

Goald keeps a delivery's headers and body for 30 days when an enabled trigger names it, and its metadata only when refused. Every accepted id stays in the webhook's ledger for a year, so a replay is a duplicate even after its row is gone. A signature header is never stored. A delivery's body and headers can be read only with `webhook:manage`, because an organization webhook carries every repository's events; a factory's Intake shows only its own evaluations.

`goal webhooks delete <webhook>` deletes the webhook and its deliveries. Its secret stays.

## Webhooks made before organizations

Before organization webhooks, a factory had its own webhook endpoints. The one-time conversion from connections made each one an organization webhook open to its factory for every repository; a renamed one was logged as `webhook renamed`, and a trigger that named the old name needs its `on` changed. The factory webhook routes, `/v1/orgs/{org}/factories/{factory}/webhooks` and the routes under it, are gone and answer 404. [Webhook secrets](/docs/concepts/access-and-secrets/#webhook-secrets) has the rules.

## Next

To turn deliveries into items, read [Review what enters a factory](../intake/).
