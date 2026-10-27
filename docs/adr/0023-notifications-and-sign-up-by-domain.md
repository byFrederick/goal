# ADR 0023 — Notifications by email and Slack, the daily digest and sign-up by domain

Status: accepted · 2026-10-27 · Builds on [ADR 0016](0016-ui-revamp.md) (the screens are the contract, controls for features not built yet), [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (outside systems are connections of a connection kind) and [ADR 0018](0018-agents-ask-questions.md) (questions, one of the events)

## Context

UI revamp wave 4 turns on two controls the screens draw:

- The **Notifications** table on the account screen ([account.html](../design/screens/account/account.html), lines 185-194). Its columns are When, In Goal, Email and Slack. Its rows are "An agent needs your OK for a write", "An agent asks you a question", "A factory pulls the andon", "A budget passes 80%" and "Daily digest at 08:00", and the digest's In Goal cell is "None". The profile above it has the field "Email for notifications" (line 172).
- The switch **"Only emails at a domain"** on the members screen ([members.html](../design/screens/organization/members.html), line 218), with the sub-line "Anyone at &lt;domain&gt; joins as a Viewer" (the screen shows a sample domain).

Assembly software#274 builds these in eight parts: preferences, the outbox, email, Slack, the digest, the account screen, the sign-up domain API and the members screen. They need one set of names and rules to build against. This is what the code offers and where it differs from the brief:

- **Roles.** [identity.go](../../internal/core/domain/identity.go) has the organization roles `owner`, `admin` and `member`, and the factory roles `viewer`, `approver` and `editor`, a ladder where each includes the one before. "Viewer" and "editor" are factory roles, not organization roles. An invitation already carries factory roles, and `goal_apply_invitation_roles` grants them on accept ([00044_auth_service_functions.sql](../../db/migrations/00044_auth_service_functions.sql)). Deciding on an item (`item:decide`) and managing the andon (`andon:manage`) both need `approver` ([permissions.yaml](../../contracts/api/permissions.yaml)).
- **The events.** An item that waits for a person's OK is in state `waiting_human` ([item.go](../../internal/core/domain/item.go)). An andon pull is `AndonPull` ([attention.go](../../internal/core/domain/attention.go)). A weekly budget covers the last 7×24 hours, rolling, not a calendar week (`BudgetWindow` in [budget.go](../../internal/core/domain/budget.go)). Questions are designed in ADR 0018 but not built: no code on main names `question__ask`.
- **The event stream.** `ItemNotices` ([ports.go](../../internal/core/ports/ports.go), lines 655-666) is PostgreSQL LISTEN/NOTIFY. After a lost connection it delivers a Resync notice and misses the events committed meanwhile.
- **Mail.** [contracts/env.yaml](../../contracts/env.yaml) (lines 412-426) lists only the auth service, the control Worker and compose as readers of `GOAL_SMTP_URL`, `GOAL_SMTP_FROM` and `GOAL_SMTP_PLAINTEXT`. goald reads none of them today. `GOAL_PUBLIC_URL` is the one origin people open goald at, and goald already reads it.
- **Sign-up.** The single sign-up gate is `SignIn.allowOnlyInvitedOrBootstrap` in [signin.ts](../../apps/auth/src/signin.ts). It admits a verified email with a pending invitation, or the bootstrap owner's email while no user exists, and refuses and logs everyone else.
- **Constraints.** PostgreSQL is the only database and processes are stateless ([AGENTS.md](../../AGENTS.md) rules 3 and 4), so there is no queue, no cache and no in-memory schedule. Outside systems are connection kinds, which are data in `internal/connkind/kinds/` ([ADR 0008](0008-portable-runs-connections-and-knowledge.md)). No secret goes into logs (rule 7).

## Decision

1. **Preferences are per person: one boolean per event and channel, in PostgreSQL.** The events are `approval`, `question`, `andon`, `budget` and `digest`. The channels are `in_goal`, `email` and `slack`. They live in a table `notification_preferences`, keyed by user id and event, with the boolean columns `in_goal`, `email` and `slack`. A missing row means the defaults. The preferences hold for every organization the person belongs to. The defaults are the boxes account.html checks:

   | Event | In Goal | Email | Slack |
   | --- | --- | --- | --- |
   | `approval`: an agent needs your OK for a write | on | off | on |
   | `question`: an agent asks you a question | on | off | on |
   | `andon`: a factory pulls the andon | on | on | on |
   | `budget`: a budget passes 80% | on | on | off |
   | `digest`: daily digest at 08:00 | none (always off) | on | off |

   A table `notification_settings`, keyed by user id, holds two more values. `email` is the optional notification email: blank means the sign-in email. Goal sends to a different address only after the person confirms it through a link mailed to it. `time_zone` is an IANA name, such as `Europe/Berlin`, that both Go's `time.LoadLocation` and PostgreSQL accept. It defaults to `UTC` until the account screen sends the browser's zone. Both are served under `/v1/me/notifications`. In Goal is the app's own alert: the badge and the toast the stream already drives. It needs no outbox row. Needs you lists the items either way.

2. **Each event has fixed recipients, worked out from the roles when the event is committed.** Being an organization owner or admin counts as every factory role.
   - `approval` (an item enters `waiting_human`): every member who may decide on it, which means factory role `approver` or `editor` on that factory, plus the organization's owners and admins.
   - `andon` (an `AndonPull` is opened): members with factory role `editor` on that factory, plus the owners and admins.
   - `budget`: the organization's owners and admins, when a factory's spend in its rolling weekly window first passes 80% of `WeeklyUSD`. The event is raised at most once per factory per 7×24 hours, the budget's own window. A factory with no weekly budget (`WeeklyUSD` 0) raises none.
   - `question` (ADR 0018): whoever may answer it, which means `approver` or higher on the factory, plus the owners and admins. It starts only once ADR 0018's questions exist on main. Until then the account screen's question row stays as drawn, with no event behind it.
   - `digest`: every person with digest email or digest Slack on (decision 6).

   The person who caused the event (the one who pulled the andon, for example) gets no notification of it.

3. **Delivery goes through a PostgreSQL outbox.** Each recipient and each channel that is on (email or Slack) gets one row in the table `notification_outbox`, under row-level security by organization. The columns are `org_id`, `event_id`, `event` (the kind), `user_id`, `channel`, `payload`, `state`, `attempts`, `next_attempt_at`, `lease_until`, `last_error`, `created_at` and `sent_at`. `payload` holds only ids and the event's facts, never an address or a secret. The states are `pending`, `sent`, `failed` and `skipped`. `(event_id, user_id, channel)` is unique, and every insert is `ON CONFLICT DO NOTHING`, so writing an event twice queues it once.
   - **Where rows are written.** The rows are written in the same transaction as the event that causes them: the store method that records the `waiting_human` event, the andon pull or the cost that passes 80%. They are not written from the `ItemNotices` listener, because a listener misses the events committed while it reconnects. `event_id` is the id of the recorded event or andon pull. A budget event's id is derived from the factory and the time of the crossing.
   - **The sender job.** Every goald instance runs one sender job. It claims at most 50 due rows (`pending`, `next_attempt_at <= now()`, no live lease) with `FOR UPDATE SKIP LOCKED`, sets `lease_until` to now plus 2 minutes, and increases `attempts`. Then it sends each row. A row whose lease ran out is claimed again by any instance.
   - **Retries.** Before sending, the job checks again that the recipient is still a member who would receive this event and that the channel is still on. If not, the row becomes `skipped`. A sent message becomes `sent`. A temporary failure goes back to `pending` with a backoff of 1, 2, 4, … minutes, capped at 1 hour. After 8 attempts, or on a permanent failure (an address refused, a Slack user not found), the row becomes `failed` with a short reason in `last_error` that names no address.
   - Nothing is held in memory or in another store (AGENTS.md rules 3 and 4). Rows that are `sent`, `failed` or `skipped` are deleted after 30 days.

4. **Email is sent by goald through a new `Mailer` port, with an SMTP adapter.**
   - **The port.** `ports.Mailer` has one method, `Send(ctx, Message) error`. A `Message` holds the recipient, the subject, a plain-text body and a minimal HTML body. The error says whether the failure is permanent. The in-memory fake goes in `internal/core/ports/porttest`, and the adapter in `internal/adapters/mailer/smtp`, wired in `cmd/goald/main.go`.
   - **The settings.** goald becomes a reader of `GOAL_SMTP_URL`, `GOAL_SMTP_FROM` and `GOAL_SMTP_PLAINTEXT`, with the auth service's meanings: plaintext only to compose's Mailpit, and STARTTLS off loopback. The email part adds `goald` to their `readers` in `contracts/env.yaml` and reads them in `cmd/goald/settings.go`.
   - **Email off.** While `GOAL_SMTP_URL` is unset, goald has no mailer and the account screen's Email column is off.
   - **The message.** It is plain text with a minimal HTML copy: no images, no tracking and no remote content. Every link is built from `GOAL_PUBLIC_URL` and never from request data.
   - **Logs.** Events are logged as `notify.sent` and `notify.failed` with the outbox row id, the event and the channel. No address, token or message body is logged.

5. **Slack is a connection kind, `internal/connkind/kinds/slack.yaml`, not code in the engine.**
   - **The kind.** Its one secret field is a bot token (`xoxb-…`), and the bot needs the scopes `chat:write`, `im:write` and `users:read.email`. Its test calls `auth.test`.
   - **The adapter.** The Slack sender is an adapter beside the Mailer, called by the same sender job. It finds the recipient with `users.lookupByEmail`, using the notification email from decision 1, opens a direct message with `conversations.open` and posts with `chat.postMessage`. It sends only direct messages to that person, never to a channel. A person Slack does not find makes the row `failed` with `slack-user-not-found`, and it is not retried.
   - **Which connection.** When an organization has no Slack connection, the Slack column on the account screen is off for that organization, and no Slack rows are queued. When it has more than one, notifications use the oldest.

6. **The daily digest goes out at 08:00 in each person's time zone.**
   - **The job.** It runs every 5 minutes in every goald instance, under `pg_try_advisory_lock` on a fixed key. An instance that does not get the lock skips that round.
   - **Who is due.** A person is due once 08:00 has passed on their local date and no digest exists yet for that date. A table `digests`, keyed by user id and local date, records each one, so the job sends at most one digest per person per local date, even across restarts. Its rows are claimed and retried like outbox rows (decision 3).
   - **What it says.** It covers the time since the person's last digest, or 24 hours for the first one, across all the person's organizations: what Needs you lists for them now, items done, andons pulled and spend by factory.
   - **How it goes out.** The email digest is one message across all organizations. The Slack digest is one direct message per organization that has a Slack connection, covering that organization only, since a Slack workspace belongs to one organization. A person with nothing to report on any line gets no digest that day.

7. **Sign-up by domain is an organization setting that only an owner sets.**
   - **The setting.** It is one row of `org_signup_domains`, under row-level security by organization, with the columns `org_id`, `domain` (lower case, matched exactly, subdomains not included), `join_role`, `set_by` and `set_at`. `join_role` is a factory role (`viewer`, `approver` or `editor`) and defaults to `viewer`. A domain belongs to at most one organization. A second claim is answered 409 with the problem type `signup-domain-taken`.
   - **Setting it.** goald's API sets and clears it with the new permission `org:signup-domain` in `contracts/api/permissions.yaml`, held only by `owner` and needing a recent sign-in (`fresh: true`), like `org:transfer`. One `SECURITY DEFINER` function, `goal_set_signup_domain`, writes the row. It refuses a domain unless the acting owner's own sign-in email is at it. It also refuses a well-known public webmail domain.
   - **The webmail list.** The list of those domains is data in one place: the table `public_email_domains`, filled by a migration. Neither Go nor TypeScript holds a copy, and changing the list takes a new migration.
   - **The gate.** `allowOnlyInvitedOrBootstrap` in `apps/auth/src/signin.ts` gains a third case. It admits a verified email whose domain is some organization's sign-up domain and is not in `public_email_domains`, which it reads through a definer function. An invitation still comes first.
   - **Joining.** The membership is added when the user is created, in the user-create `after` hook, through a definer function like `goal_apply_invitation_roles`. The person joins as an organization `member`, with `join_role` on every factory that exists then. Factories created later give them no role until an admin grants one, as for any other member. The join is recorded in the audit log as the existing `member.added` event, with `via: signup-domain` in its data.

8. **Send to Jonah waits for the Jonah wave.** Jonah has no code yet ([ADR 0002](0002-jonah-evals.md)). Send to Jonah on the Runs screen ([runs.html](../design/screens/daily-work/runs.html)) stays disabled with "Not available yet" (ADR 0016, decision 4) until the Jonah wave builds it.

## Consequences

- People hear about approvals, andons and budgets outside the app without anyone watching it. Each event is queued once, in the transaction that caused it, and survives any instance's crash.
- goald now sends mail, so it needs the SMTP settings that only the auth service needed before. Deployments that set `GOAL_SMTP_URL` for auth set it for goald too, and the email part updates `contracts/env.yaml`, `cmd/goald/settings.go` and the compose and Cloudflare wiring.
- Slack follows the connection model: an admin adds a Slack connection with a bot token. No Slack code goes into the engine, and the same sender job serves both channels.
- Recipients are worked out at commit time, and the sender checks them again before sending. A person who loses access between the two gets nothing.
- Joining by domain grants roles only on the factories that exist at the time. An owner who wants domain joiners on a new factory grants the role, the same step as for invited members.
- The webmail list lives in the database, so keeping it current takes a migration, and Go and TypeScript can never disagree about it.
- Question notifications and Send to Jonah are named here but wait for ADR 0018's questions and for the Jonah wave.
- The parts of assembly software#274 build against these names: the preferences (decision 1), the outbox (2, 3), email (4), Slack (5), the digest (6), the account screen (1, 4, 5), the sign-up domain API and gate (7) and the members screen (7).
