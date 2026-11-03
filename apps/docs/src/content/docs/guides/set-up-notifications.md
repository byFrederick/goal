---
title: Set up notifications
description: Choose where you are told about approvals, questions, andon pulls and budgets, read the daily digest, and connect Slack to your organization with a secret.
---

You end with each person choosing their own channels, and Slack direct messages working through one secret that an organization admin picks.

## Choose your channels

Open your account page and find Notifications. The table has one row per event and one column per channel. The events are:

- An agent needs your OK for a write.
- An agent asks you a question.
- A factory pulls the andon.
- A budget passes 80%.
- Daily digest.

The channels are In Goal, Email and Slack. The digest has no In Goal column.

Each box saves when you change it. Notifications are personal: they follow you into every organization you belong to. With sign-in off there is no person to notify, so the boxes are unavailable.

The Slack column works only after an admin has set up Slack for the organization (below).

## The daily digest

The digest is one message a day at 08:00 in your time zone, listing what needs you. It has no In Goal channel, because the app already shows it. Goal saves your browser's time zone the first time you change a box; until then the digest uses UTC.

## Set up Slack

You need the `org:manage` permission.

1. In Slack, create an app for your workspace and add a bot token with the scopes `users:read.email`, `im:write` and `chat:write`. Goal looks up each person by their email address, opens a direct message with them and posts to it.
2. Install the app to the workspace and copy the bot token.
3. In Goal, open Secrets and choose New secret. Name it `slack-bot`, paste the token as the value, and leave it available to all factories. A secret limited to some factories is refused.
4. Open Organization settings, then Notifications. The Slack row preselects `slack-bot` when no secret is saved and a secret of that name exists. Pick another secret if you used a different name.
5. Press Test. Goal asks Slack about the token and shows the result next to the buttons:
   - `Works with <workspace>`: Slack accepted the token.
   - `Slack refused this token`: the token is wrong or revoked. Fix the secret's value.
   - `Slack could not be reached. Try again.`: Slack did not answer. Test again later.
   - `Pick a secret first`: no secret is picked.
6. Press Save. Slack direct messages go out to people who ticked Slack on their account page.

Turn off removes the setting; Slack messages stop. Members without `org:manage` see the saved secret name but no controls.

## Change or delete the secret

To rotate the token, change the secret's value; the setting keeps pointing at its name. A secret that notifications use cannot be deleted: Goal answers 409 `secret-in-use` and lists the use. Turn Slack off, or pick another secret, and then delete it.
