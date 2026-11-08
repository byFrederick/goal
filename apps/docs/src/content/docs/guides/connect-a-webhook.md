---
title: Connect a webhook in the web app
description: Create a webhook on the Webhooks page, paste its address and secret into the sender, open it to factories and read what it delivered.
---

You end with a webhook that a sender such as GitHub posts events to, opened to the factories that use them, and you can read in the web app what each delivery led to. Owners and admins see Webhooks in the organization's sidebar, next to Secrets. The rules (who may manage, secrets and rotation, openings) are in [Receive events with organization webhooks](/docs/guides/webhooks/), which also has the CLI steps: [Receive events with organization webhooks](/docs/guides/webhooks/).

## Create the webhook

1. Open **Webhooks** in the sidebar and choose **New webhook**.
2. Pick the sender: GitHub, GitLab, PagerDuty, Sentry, Standard Webhooks, or Token in a header. For Token in a header, give the header that carries the value, and optionally the body paths of the event name and of a delivery id.
3. Name it with lower-case letters, digits and single hyphens, 3 to 40 characters, such as `github-acme`. Triggers name the webhook by this name, for example `github-acme.check_run`. The names of the senders, `schedule` and `webhook` are reserved.
4. Choose **Generate one** to have Goal create the secret `webhook-<name>`, or **Use a secret** to pick one from Secrets. PagerDuty and Sentry issue their own secret: save it in Secrets first, then use it here.
5. Optionally tick the factories to open it to, each with the repositories it may receive (empty is all repositories). A new webhook is open to no factory until you open it.
6. Choose **Create webhook**. A generated secret appears once, with **Copy**. Copy it before you choose **Done**: it is never shown again, and the page opens the new webhook.

## Paste it into the sender

On the webhook's page, **Connection** shows the address, with **Copy**, and the secret by name, with **Rotate** for people with `secret:manage` ([rotation](/docs/guides/webhooks/#rotate-the-secret)). **Set it up in** the sender lists the steps there and the events to choose: the events the triggers of the factories it is open to use. When a trigger names the webhook with no event, the sender should send every event.

For GitHub, once you add the webhook, GitHub sends a ping. The **Status** line then reads "Received GitHub's ping on" with the date. Until then it reads "Waiting for GitHub's ping".

## Open it to factories

**Open to** lists the factories and the repositories each receives. **Edit** opens, closes, limits to repositories and renames in one save. If a published trigger still names the webhook, the save is refused and the dialog lists each trigger with its factory ([the rule](/docs/guides/webhooks/#open-it-to-a-factory)). Publish those triggers without the webhook first, then save again.

## Read the log

**Deliveries** lists what the webhook received, newest first, and **Show older** loads the next page. It shows only refusals and the deliveries a trigger names; GitHub's ping and deliveries that arrive before a trigger names the webhook never show. Each delivery shows its receipt, **Accepted** or **Refused** with the reason, such as a signature that did not match. A delivery Goal had already taken shows no row of its own: the first delivery reads "Sent N times".

Under a delivery is one line per factory whose triggers name it: **Created** with the item's ref, **Skipped** with the reason, or **Failed** with the error.

**View request** on a delivery opens its headers and body, for people with `webhook:manage`. [What is kept and for how long](/docs/guides/webhooks/#read-the-delivery-log) is on the webhooks page.

To delete a webhook, choose **Delete** on its page. While published triggers name it, Delete answers 409 `webhook-in-use` and lists those triggers. Otherwise its address stops accepting requests and its secret stays in Secrets.
