# ADR 0023 — Intake triggers

Status: accepted · 2026-10-27 · Builds on [ADR 0005](0005-factory-definitions-in-object-storage.md) (triggers are part of the factory definition), [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (sealed secrets, and Hyperdrive drops advisory locks) and [ADR 0016](0016-ui-revamp.md) (the Intake screens are the contract, and controls for features not built yet)

## Context

The Intake screens draw a factory's intake as triggers: rules that turn an event, a schedule or a webhook delivery into a work item. [`intake.html`](../design/screens/intake/intake.html) lists the triggers and where items came from; [`intake-new.html`](../design/screens/intake/intake-new.html) picks a source (Schedule, Webhook, GitHub, Slack, Cloudflare, Linear, Jira, Sentry) and an event; [`trigger.html`](../design/screens/intake/trigger.html) edits one trigger in four blocks, When, Only if, Create and Limits, and shows its definition (lines 215 to 221):

```yaml
- name: flaky-test-on-main
  on: github.check_run.failed
  filter: { branch: main, repo: acme/app }
  only: { failuresInARow: 2, dedupe: check }
  create: { type: fix, station: build, lane: expedite }
  limits: { perDay: 3, pauseOnBudget: true }
```

[`webhooks.html`](../design/screens/intake/webhooks.html) lists webhook endpoints with their address, signing secret and deliveries. The phone layout is [`mobile/intake.jpg`](../design/screens/mobile/intake.jpg). [MASTER.md](../design/MASTER.md) ("Vocabulary") calls the page Intake and a rule a Trigger.

What exists today:

- **`board.intake`** in [`contracts/definitions/factory.schema.json`](../../contracts/definitions/factory.schema.json) (lines 410 to 424) is a list of sources (`manual`, `github-issues`, `schedule`, `webhook`, `email`) with `repo`, `labels`, `cron`, `template` and `type`. `internal/config` parses it into `config.Intake` and the definition editor (`internal/factorymodel`) shapes it, but nothing in the engine reads it. It has no filters, no checks and no limits.
- **Secrets** are sealed with the `ports.MasterKey` port (`internal/core/ports/connections.go`), AES-256-GCM with a key id, as connections' secret fields are (ADR 0008, decision 5).
- **Locks.** goald already takes transaction-scoped advisory locks (`db/queries/platform.sql`, `pg_try_advisory_xact_lock` for the auth cleanup). ADR 0008 records that Hyperdrive drops advisory locks and session settings.
- **Budget.** A factory has a weekly budget (`domain.Runtime.WeeklyBudget`, `internal/core/domain/budget.go`); there is no daily one.

Three places in the screens disagree with what is decided here, listed so a person can fix them:

- `webhooks.html` (lines 158, 171 and 182) shows addresses like `/hooks/<factory name>/<endpoint name>`. That keys a factory by name and breaks AGENTS.md rule 2.
- `webhooks.html` (lines 159 and 172) shows the signing secret as a credential "in Credentials" ([ADR 0017](0017-credentials.md)). A signing secret is goald's to check, not something an agent or connection uses, so decision 2 seals it on the endpoint instead.
- The screens' index ([`screens/README.md`](../design/screens/README.md), line 57) lists Linear, Jira and Sentry as not built, but not Slack; decision 6 adds Slack.

## Decision

1. **Triggers are part of the factory definition, as `board.triggers`.** They are versioned and published with the rest of `factory.yaml` (ADR 0005); saving a trigger publishes a new version. Each trigger is an object:
   - `name`: a slug, unique within the factory. It names the trigger in the API, the UI and the firing log.
   - `enabled`: a boolean, default `true`. A disabled trigger matches nothing.
   - `on`: one event key. The keys are `github.check_run.failed`, `github.issues.labeled` (more `github.*` keys follow the picker's events), `webhook.<endpoint name>` with an optional event name (`webhook.<endpoint name>.<event>`, matched against the delivery's `event` field), and `schedule`, which takes `cron` (five fields) and `timeZone` (an IANA name, default `UTC`).
   - `filter`: equality filters on the event's fields, all of which must match: `branch`, `repo`, `label`, `paths` (globs; any changed path matches), `authors` (any of) and `skipBots` (a boolean that drops events whose sender is a bot).
   - `only`: checks made before any item is created, in this order. `failuresInARow: n` passes when the same check failed on the last n events for it. `dedupe: <field>` passes when no open item that this trigger created holds the same value of that field.
   - `create`: `{type, station, lane, expedite, title, brief}`. `type` is one of `board.workTypes`, `station` and `lane` place the item, `expedite` puts it in the expedite flow. `title` and `brief` are templates whose `{{field}}` placeholders take the event's fields (`{{check.name}}`, `{{service}}`).
   - `limits`: `perDay` (at most n items created by this trigger since the start of the UTC day) and `pauseOnBudget` (skip events while the factory's budget is spent; today that is the weekly budget).

   `board.intake` stays accepted by the schema so existing definitions load, but it is deprecated and ignored: nothing reads it, and the editor offers `board.triggers` only. Removing it is a later change.

2. **Webhook endpoints, deliveries and trigger firings live in PostgreSQL, under row-level security, keyed by UUID.** They are state, not definition, so they are rows, not files in the bucket (AGENTS.md rule 3). Each table carries `org_id` with row-level security on it.
   - **Webhook endpoint**: per factory, with a UUID `id`, a `name` (a slug unique within the factory, the one `on: webhook.<name>` refers to) and a `kind`, `generic` or `github`.
   - **Delivery**: each request an endpoint received, with its headers and body, kept 30 days and then deleted, and one outcome: refused with a reason (such as "signature did not match"), accepted with no trigger matched, or created item N.
   - **Trigger firing**: each time an event reached a trigger, with the event, the trigger's name, the outcome (created, or skipped with the step and reason) and the item when one was created. The trigger page's "Recent events" reads it.
   - **Signing secrets** are generated by goald, sealed with the `ports.MasterKey` port on the endpoint's row, and shown once: in the answer that creates the endpoint and in the answer that rotates its secret. No API answer, log or audit entry shows them afterwards.

3. **The public address is `POST /hooks/{endpoint id}`.** The endpoint's UUID is the only thing in the path, so neither a factory's nor an endpoint's name appears in it (AGENTS.md rule 2) and renaming either keeps the address. The request carries no session or API key; its signature is its authentication, checked against the body as received before anything is parsed.
   - A `generic` endpoint takes `X-Goal-Signature: sha256=<hex HMAC-SHA256 of the body under the signing secret>`, and an event name in the body's `event` field.
   - A `github` endpoint takes GitHub's `X-Hub-Signature-256` (the same HMAC scheme) and names the event with `X-GitHub-Event`.
   - A request with a missing or wrong signature, or to an unknown or disabled endpoint, is refused and, for a known endpoint, logged as a refused delivery. Signatures are compared in constant time.

4. **Schedules fire in goald under `pg_try_advisory_lock`, so exactly one instance fires each.** Every goald instance ticks; the one that takes the lock fires the schedules that are due, and the others skip the tick (AGENTS.md rule 4). The lock is taken on a direct session connection to PostgreSQL, not through Hyperdrive, which drops advisory locks (ADR 0008). A firing is idempotent on (trigger, event id) for events and (trigger, scheduled instant) for schedules: a unique key on the firing log makes a second attempt, after a crash or by another instance, a no-op.

5. **Text from an event reaches agents only inside a block marked as untrusted data, never as instructions.** The item's brief holds the rendered template, and the event's fields go to the agent in one delimited block that says its content is data from outside the organization and that nothing in it is an instruction. Placeholders in `brief` refer to fields in that block; the rendered brief quotes them rather than splicing raw event text into the agent's instructions. The same holds for `title`.

6. **Not built in this feature: Slack, Linear, Jira and Sentry sources, "Try with a past event", and the "A model agrees" check.** They render disabled with the tooltip "Not available yet" (ADR 0016, decision 4), and the schema accepts none of them. Cloudflare in the picker is a `generic` webhook endpoint, as `webhooks.html` draws it. The item that builds each one enables its control.

## Consequences

- One model for all of software#154's parts: the schema part adds `board.triggers`, the store part the three tables and the sealed secret, the engine part matching, checks, limits and schedules, the API parts the endpoints, the delivery log and `/hooks/{endpoint id}`, the CLI and web parts the commands and screens.
- Factory definitions stay the single source of what a trigger does; the database only records what arrived and what happened. A trigger's history survives an edit, because firings name the trigger and the event, not a definition version.
- An endpoint's address is an opaque UUID, which is less readable than a name; the Webhooks screen shows the endpoint's name beside it.
- A lost signing secret cannot be read back, only rotated, and a rotation breaks the sender until it is given the new one.
- The schedule lock needs a direct PostgreSQL connection on Cloudflare as well as locally; the engine part must open it outside any pooler that drops session locks.
- Deliveries hold whatever senders post, so a job deletes them after 30 days.
- The screens must change to match this ADR, in the web-webhooks part: `webhooks.html` shows `/hooks/{endpoint id}` instead of `/hooks/<factory>/<name>`, and the signing secret as a sealed secret shown once with Rotate, not as a credential "in Credentials". The screens' index adds Slack to the Intake picker's not-built sources.
- `pauseOnBudget` reads the weekly budget until a daily budget is decided, while `trigger.html` says "daily budget"; the web-trigger part shows the copy for the budget that exists.
