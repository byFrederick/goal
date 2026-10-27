# ADR 0023 — Self-serve onboarding: anyone signed in creates an organization, and progress comes from existing data

Status: accepted · 2026-10-27 · Builds on [ADR 0016](0016-ui-revamp.md) (decision 6 orders onboarding among the new features built end to end) · Amends [ADR 0007](0007-identity-better-auth.md) decision 5 (only platform admins created organizations)

## Context

The revamp's onboarding screens are in [get-started/](../design/screens/get-started/) ([index](../design/screens/README.md)). A new person signs up, then: 1 creates an organization, 2 connects GitHub and picks repositories, 3 picks a template, 4 shapes the line, 5 sets budgets and targets, 6 turns on starter triggers, 7 invites people, 8 watches a ready-made first item, and lands on the new factory's Floor with a setup checklist ([ready.html](../design/screens/get-started/ready.html)).

What the code offers today:

- Only platform admins may create an organization. [permissions.yaml](../../contracts/api/permissions.yaml) declared `org:create` as `{level: install, platform_admin: true}`, as [ADR 0007](0007-identity-better-auth.md) decision 5 decided. A person who signs up therefore lands in no organization and can do nothing.
- An install permission with no `platform_admin`, `keys` or `pat` flag is held by every principal signed in with a session and by no API key or personal token; `device:read` works that way. goald's Authorizer does not tell a browser session from a session the CLI got through the device sign-in: both are sessions.
- Each step already has data behind it: organizations, connections (the `github` and `slack` kinds), factories and their definitions with budgets, invitations, items, and knowledge sources. A factory's definition has a draft and published versions, and the new-factory wizard (`/o/$org/new/$step`) creates one from a template.
- `submitItem` submits work to a factory; Intake triggers, which start work from outside, are their own feature, built before onboarding (ADR 0016, decision 6).

The work is split into the parts of one assembly (the permission, repositories, the starter item, and one part per screen). This ADR is the decision they all follow, so the names below are exact.

## Decision

1. **Any person signed in with a session may create an organization, and becomes its owner.** "A session" is a browser sign-in or the CLI's device sign-in (`goal org create`); goald's permissions do not tell them apart, and this ADR adds no flag that would. Platform admins keep the right. API keys and personal tokens may not, even when their owner may. `org:create` is `{level: install}` in `contracts/api/permissions.yaml`. Under `GOAL_AUTH=none` nothing changes: every request is the local user, as today. This ADR sets no limit on how many organizations a person may own; a quota is a later decision.
2. **Onboarding progress comes from existing data, with no new table.** The steps are done when, in order: the organization exists, a GitHub connection exists, a factory exists, its budgets and targets are set, it has triggers, it has invitations, its first item is done, a Slack connection exists, and a knowledge source exists. The last two are only on the Floor's setup checklist. The current step is in the URL: `/get-started` for step 1, then `/o/$org/get-started/$step` with the step keys `code`, `factory`, `line`, `budget`, `intake`, `people` and `first-item`, and `?factory=<id>` once the factory exists. Dismissing the Floor's setup checklist is remembered per browser, not on the server.
3. **The first item comes from an optional `starter.item` section of `factory.yaml`.** It has `title`, `type` and `body`, and a template carries it. It is submitted once, through the existing `submitItem`, when the factory has no items yet; a factory with items gets none.
4. **The factory is created from the template at step 3, and published when the person continues from step 5.** Steps 4 and 5 edit its draft. Publishing at the end of step 5 means triggers and the first item run on a published version.
5. **The starter triggers are the template's triggers, off until the person turns them on.** They are triggers as the Intake triggers feature defines them; step 6 only shows them and turns the chosen ones on.

## Consequences

- A person who signs up can start on their own: step 1 needs no platform admin. Who may sign in still limits who reaches it (the bootstrap admin and invitations, [ADR 0007](0007-identity-better-auth.md)).
- Anyone who can sign in may create as many organizations as they like. Limits are left for later, on purpose.
- Progress needs no migration and cannot drift from the data it describes; each screen reads what it already reads. The cost is that "done" for a step is a rule over several queries, which the UI parts compute.
- `factory.yaml` gains an optional section that the definition's schema and loader must accept (the `starter-item` part).
- The web UI and its mock still offer Create organization only to platform admins; the onboarding UI parts change that. The CLI's help still says "platform admins only".
- [ADR 0007](0007-identity-better-auth.md) decision 5's last sentence no longer holds; its amendment points here.
