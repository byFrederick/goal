---
title: Connect a webhook in the web app
description: Create a webhook on the Webhooks page, paste its address and secret into the sender, open it to factories and read what it delivered.
---

You end with a webhook that a sender such as GitHub posts events to, opened to the factories that use them, and you can read in the web app what each delivery led to. Owners and admins see Webhooks in the organization's sidebar, next to Secrets, and create and change webhooks (`webhook:read` and `webhook:manage`). The same steps are in the CLI: [Receive events with organization webhooks](../webhooks/).

## Create the webhook

1. Open **Webhooks** in the sidebar and choose **New webhook**.
2. Pick the sender: GitHub, GitLab, PagerDuty, Sentry, Standard Webhooks, or Token in a header. For Token in a header, give the header that carries the value, and optionally the body paths of the event name and of a delivery id.
3. Name it with lower-case letters, digits and single hyphens, 3 to 40 characters, such as `github-acme`. Triggers name the webhook by this name, for example `github-acme.check_run`. The names of the senders, `schedule` and `webhook` are reserved.
4. Choose **Generate one** to have Goal create the secret `webhook-<name>`, or **Use a secret** to pick one from Secrets. PagerDuty and Sentry issue their own secret: save it in Secrets first, then use it here.
5. Optionally tick the factories to open it to, each with the repositories it may receive (empty is all repositories). A new webhook is open to no factory until you open it.
6. Choose **Create webhook**. A generated secret appears once, with **Copy**. Copy it before you choose **Done**: it is never shown again, and the page opens the new webhook.

## Paste it into the sender

On the webhook's page, **Connection** shows the address, with **Copy**, and the secret by name. **Set it up in** the sender lists the steps there and the events to choose: the events the triggers of the factories it is open to use. When a trigger names the webhook with no event, the sender should send every event.

For GitHub, once you add the webhook, GitHub sends a ping. The **Status** line then reads "Received GitHub's ping on" with the date. Until then it reads "Waiting for GitHub's ping".

## Open it to factories

**Open to** lists the factories and the repositories each receives. **Edit** opens, closes, limits to repositories and renames in one save. A rename or a close that would leave a published trigger without the webhook is refused, and the dialog lists each trigger with its factory. Publish those triggers without the webhook first, then save again.

## Read the log

**Deliveries** lists what the webhook received, newest first, and **Show older** loads the next page. Each delivery shows its receipt:

- **Accepted**: the request was valid.
- **Duplicate**: Goal had already taken this delivery id, so it created nothing.
- **Refused**: with the reason, such as a signature that did not match.

Under a delivery is one line per factory whose triggers name it: **Created** with the item's ref, **Skipped** with the reason, or **Failed** with the error. Accepted deliveries that no factory names are not listed; the log counts them in one line.

**View request** on a delivery opens its headers and body. Only people with `webhook:manage` see it, because an organization webhook carries every repository's events. Goal keeps deliveries for 30 days.

To delete a webhook, choose **Delete** on its page. Its address stops accepting requests; its secret stays in Secrets.
