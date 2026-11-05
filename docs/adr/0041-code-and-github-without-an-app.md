# ADR 0041 — Code and GitHub without an App: optional code access, the repositories an item opens, write access per agent, webhooks the organization connects once

Status: accepted · 2026-11-04

- **Amends:**
  - [ADR 0037](0037-secrets-and-environment-files.md), decisions 6–7: `runtime.repository` becomes a list of repositories with their access, on code hosts the organization lists (decisions 1–8). Its run start on secrets, which software#538 finishes, is what this builds on.
  - [ADR 0032](0032-self-serve-onboarding.md): onboarding asks for code after the factory, as a choice of no code, read only, or read and write (decision 9).
  - [ADR 0015](0015-overlapping-in-flight-work.md) and [ADR 0033](0033-assembly-integration-branches.md): paths overlap only within the same repository, and an assembly has an integration branch in each repository it changes (decision 5).
  - [ADR 0038](0038-knowledge-is-files.md): a repository source's secret opens only for the factories that list it (decision 16).
  - [ADR 0039](0039-jonah-learns-from-the-line.md): a try clones the repositories its run recorded (decision 17).
  - [ADR 0040](0040-one-door-for-every-source.md): an item records its repositories at filing, and a trigger, a check and a follow-up can name them (decisions 4 and 13); GitHub webhooks are connected once for the organization (decision 12, with ADR 0040's decisions 12–16).

## Context

The owner asked how GitHub works in Goal without a GitHub App, whether it scales beyond one factory, and whether a factory has to be tied to GitHub at all. An enterprise incident team may have read-only access to GitHub, or none, and an incidents factory may work across many repositories. The map of origin/main 684189c5 (with ADR 0037 built) showed this:

- **A factory without code already works, except in onboarding.** `runtime.repository` is optional in the schema, the engine, the editor and New factory. Onboarding's step 2 has no skip, and Continue stays disabled until the token can push.
- **Read-only isn't a mode.** Nothing in a definition says read or write. The repository test fails a read-only token in onboarding, in settings and in `goal repository test`.
- **One repository per factory,** from the schema to the box's `/workspace`: the definition, the line, the run request, the bundle, prepare's single clone, the item's base commit, integration branch and paths, and the run's heads.
- **The code host port takes a connection in every method,** and on secrets nothing reads the repository token at all. software#538, filed on 2026-11-03, fixes that for today's single repository.
- **Webhooks are tied to one factory** in the tables, routes, permission and CLI.
- **Hosts:** the repository test sends tokens only to github.com and gitlab.com, so GitHub Enterprise and self-managed GitLab can't get through onboarding. The adapters already derive Enterprise API bases.
- **The screens show things the code doesn't have:** the settings board's "GitHub label" and "Handle", and the Floor's "Label an issue" row. The Floor's setup also counts a `github` connection, and the Board's done column says "Merged pull requests" for every factory.

Without an App, GitHub offers organization webhooks (one per GitHub organization, added by an owner, covering every repository) and repository webhooks (one per repository), both signed with `X-Hub-Signature-256`. Fine-grained tokens can be read-only.

An adversarial review of the first draft against the code found 30 problems across this ADR and ADR 0040's webhooks, 4 of them breaking. All are folded in below.

The owner then reviewed the draft (2026-11-03). An incident may need several repositories, and its outcome is often not a pull request. The investigating agent needs all the tools and code access to find the cause. Within one factory, the investigator should only read, while a lane whose agent can write makes the fix: "It doesn't necessarily need another factory, just a lane with an agent that knows how to write." So an item opens the repositories it needs, and write access is per agent. A second adversarial review of this revision found 23 problems across both ADRs, 1 of them breaking; all are folded in.

## Decision

### Code hosts and repositories

1. **The organization lists its code hosts.**
   - **Built in:** github.com and gitlab.com.
   - **Added hosts:** owners add a GitHub Enterprise or self-managed GitLab host, with its kind, in a fresh session: `GET` and `PUT /v1/orgs/{org}/code-hosts`.
   - **Where tokens go:** goald sends repository tokens and knowledge secrets only to listed hosts, so no one can point a secret at a host of their choosing.
   - **API bases** are derived as the adapters already do, including GitHub Enterprise Cloud's `api.<name>.ghe.com`.
   - **Reachability:** a host must be reachable from the internet, because goald dials public addresses only (ADR 0009).

2. **A factory's code is a list of repositories, and it may be empty.**

   ```yaml
   runtime:
     repositories:
       - name: goal
         url: https://github.com/byFrederick/goal
         token: "{{github-byfrederick}}"
         access: write
       - name: services                      # an entry may be one repository or a whole organization
         url: https://github.com/byFrederick/*
         token: "{{github-byfrederick-read}}"
         access: read
   ```

   - **Entries:**
     - `name` is a slug, unique in the factory.
     - `url` is one repository on a listed host, or a whole organization (an owner pattern ending in `/*`), for either access. One factory can mix them: one repository, several, or every repository of an organization.
     - `token` is a `{{secret}}`, required for `write`.
     - `access` is `read` or `write`.
     - `default: true` marks the entry an item uses when it names none. It must name one repository, and at most one entry has it. The shorthand's entry is the default, and so is the only entry when it names one repository.

     There is no `forge`, because the host list gives it. There is no `base` either: the host's default branch is used, because prepare, base commits, the landing note and `gh pr create` all read it.
   - **No repositories** is a factory with no code.
   - **The shorthand:**
     - `runtime.repository` stays a shorthand for one `write` entry, named with a slug of the URL's last segment, or `repo` when that isn't a valid slug. It keeps every published version working.
     - A definition with both forms is refused, and so is `write` without a token.
   - **Where secrets may appear:** repository tokens become one of the places a `{{secret}}` may appear (ADR 0037 decision 6). They count as the secret's uses, so it can't be deleted while used, and they show on Access.

3. **Write access is per agent, and goald never writes where it isn't allowed.**
   - **Per agent:** an agent's `code` is `write` or `read`, next to its `builtins`. It defaults to `write` when the factory has a `write` entry, so today's definitions keep working, and to `read` otherwise.
   - **A lane that writes:** the investigator's output sets `lane`, as outputs already can, and the next station's `route` sends that lane to an agent that writes. So the fix happens in the same factory and the same item.
   - **Never writing:** goald never pushes, creates branches, opens pull requests or merges in a `read` entry's repositories, or for an agent that only reads. A check ([ADR 0040](0040-one-door-for-every-source.md), decision 19) always only reads, whatever its agent's `code`.
   - **Which token:** entries may cover the same repositories with different access. For each repository, a run uses the most specific entry that matches, one repository before an owner pattern. An agent that only reads takes a `read` entry's token when one matches, so it never holds a write token where a read-only one exists.
   - **The guard:** agents push with their own git, as today. Prepare sets git's `url.<prefix>.pushInsteadOf` to an address that refuses pushes, for each entry's prefix the run may not write. A push fails with a clear message, even from a repository the agent opened later. The agent's git keeps the token for fetches, which the partial clone needs.
   - **A reader with a write token:** when an agent that only reads would get a `write` entry's token, because no `read` entry matches, publish warns: "reviewer only reads, but the only token for services can write."
   - **Tokens an environment names,** such as `GH_TOKEN`, are the environment's. Goal can't limit them, and decision 14's warning flags GitHub tools that aren't read-only.
   - **The real limit** is each token's scope: an agent can push wherever its token can. A fine-grained token per entry keeps writes where they belong.
   - **The workspace note** says, for each repository, whether this agent may write.
   - **The read test** warns when a `read` entry's token can push: "Goal won't push, but agents could. Use a read-only token."
   - **At publish:** a line that merges (a merge station or integration branches) needs a `write` entry and an agent that writes.

4. **An item records the repositories it uses, and the list grows as its agents open more.** `work_item_repositories` holds one row per repository, by normalized `host/owner/name` in lower case, with how it joined and when. Renaming or reordering entries never moves an item.
   - **At filing:**
     - **a person** picks any number in New item, or with `goal run --repo`, repeated;
     - **a trigger** names them with `create.repositories` (decision 13), and **a check** with each item's `repositories` ([ADR 0040](0040-one-door-for-every-source.md), decision 19);
     - **a part** takes the ones its plan names, or else its assembly's;
     - **a follow-up's item** takes the follow-up's `start.repositories`, or else its source item's.
   - **When nothing names any:** the default entry (decision 2). Without one, the item starts with none, and its agents open what they need (decision 5).
   - **Old items:** an item with no rows uses the default entry at run start, for good. Items filed before the migration, or by an old instance during a deploy, keep their repository. Repository URLs live in the definitions in the bucket, so no SQL migration could write them.
   - **Growing:** a repository joins the item when a run pushes to it, or when a station's output names it in the field the definition maps (`board.flow.outputs.repositories`, next to ADR 0040's `followUps`). One the run only read stays on the run (decision 5). Rows are never removed.
   - **At most 10** per item. Past that, the output fails its check with the reason.
   - **The first** is the first named at filing, else the default entry, else the first that joined.
   - **A triage item** (ADR 0040) gets its sources' repositories, up to the cap.
   - **Removing an entry** that open items use is refused at publish.

5. **A run starts with the item's repositories, and its agent opens others as it needs them.**
   - **At start:** prepare clones the item's repositories, partially (`--filter=blob:none`):
     - the first, by the order they joined, at `/workspace`, as today, where the environment's `setup` and the repository's `mise.toml` run;
     - each other at `/repos/<owner>/<name>`, on the host's default branch, or on the item's branch where a run already pushed one;
     - none, for an item with none: `/workspace` starts empty.
   - **Opening more:** the agent clones any repository the factory's entries cover with plain git, into `/repos/<owner>/<name>`, and the workspace note says how.
   - **Credentials:** git asks for each repository's path (`credential.useHttpPath`), so one host can carry several tokens. The box's credential helper replaces today's one stored login per host. It answers only for URLs that match an entry, with the token decision 3 picks, and records each repository it answers for. A repository no entry covers gets no token.
   - **Recording:** `agent_run_repositories` keeps, per run and repository the run fetched or pushed, the base commit, the heads and whether it wrote. It replaces the run records' single `branch_head`, `base_head` and `pushed_head` (ADR 0039). Only pushes and output names join the item (decision 4).
   - **Writing:** an agent that writes pushes the item's branch (decision 6) in each `write` repository it changed, and opens a pull request there. It names them in `pull_requests`, which outputs already accept.
   - **Merging:** a merge takes the item's pull requests in the order they were opened, each in its own repository with its entry's token, and keeps today's three outcomes for each. A conflict sends the item to rework. A pull request not yet mergeable waits at the gate. Anything else blocks the item, with the reason.
   - **The item's pull requests:** a pull request is the item's when its head is the item's branch, or its assembly's integration branch, in that repository. Any other is refused, because no station checked it.
   - **Per repository:**
     - a `paths` entry may start with `owner/name:`; without one, it's in the item's first repository;
     - overlapping paths compare repository and path (ADR 0015);
     - each repository's row on the item holds its `base_commit` and, for an assembly, its `integration_branch`, so an assembly has an integration branch in each repository it changes (ADR 0033);
     - an assembly's landing takes the union of its parts' repositories.
   - **The workspace note** lists the item's repositories with this agent's access, and says how to open another: any repository the factory's entries cover, with git.
   - **Not every item writes code.** A line that doesn't merge never needs a `write` entry. An item can end in findings, actions through tools, a postmortem or follow-ups.

6. **Branch names are stored, and new ones include the factory.** `work_items.branch` is set at filing, `goal/<factory>/<number>-<slug>`, so two factories working in one repository never share a branch. The migration backfills today's names, so renaming an item or its factory never moves a branch and orphans a pull request. Integration branches are stored the same way, on the item's repository rows.

7. **The code host port takes a repository and its token** (it replaces software#538's single-repository form).
   - **Hosts** come from the organization's list.
   - **GitLab** gains base commits and changed-since. A repository list isn't needed, since nothing calls it.
   - **The landing note** names `gh pr create` or `glab mr create`, and `glab` joins the agent image.

8. **Repository tests are per factory and kept.**
   - **The route:** `POST /v1/orgs/{org}/factories/{factory}/repositories/test` needs `definition:edit` and opens the secret for that factory, as the environment test does.
   - **The answers:**
     - "Can read", with the warning of decision 3 when the token can also push; for an owner pattern, how many repositories the token can read ("Can read 48 repositories in byFrederick");
     - "Can read and push". The host reports no pull-request permission, so Goal can't check it.
   - **Kept results:** each result is kept per factory and repository, and reruns on Test or when the secret's value changes. Pages don't test on every view.
   - **What goes:** the organization-level test route. Onboarding, factory settings and `goal repository test` call it today, so it goes in row 8, once they call the factory route.

### Onboarding and settings

9. **Onboarding asks for code after the factory.**
   - **The order** becomes Organization, Factory, Code, Line, Budget, Intake, People, First item. The template and the factory then exist when the code is chosen.
   - **Template needs:** each template's `template.yaml` says `code: none` or `code: read`, and `write` follows from a line that merges. This is exposed on `TemplateSummary`.
   - **The Code step** offers No code, Read only, and Read and write, and disables the choices the template's line can't use, with the reason.
   - **One form for every case.**
     - **No code** asks for nothing, so onboarding never needs a repository.
     - **Read only, and Read and write,** ask the same two things: which repositories (a whole organization, or only some, one or more) and a token.
     - So an incidents factory reading an organization, a software factory writing to one repository, and one writing to many all fill in the same form.
     - Each item starts with the repositories it names, and its agents open others as they need them.
   - **The token** is saved as a secret open to the new factory and tested for it. On a name clash, the step offers to use the existing secret or pick a new name, and never replaces a value.

10. **Factory settings' Repository section becomes Code.**
    - **Each repository** shows its access, its kept test result with Test, and its token by name.
    - **Below the list:** the agents that write, from each agent's `code`, with a link to Agents; Add a repository; the branch names; and the host's default branch, read only.
    - **Who to ask:** creating a secret or opening a webhook needs an owner or admin, and the section says so.
    - **No code:** a factory with no repositories says "No code. Agents work through their tools."
    - **The organization's settings** gain Code hosts (decision 1).

11. **Copy stops assuming code.**
    - **The Floor's setup** counts the factory's code choice, and its ways-in card loses "Label an issue", which nothing implements.
    - **The Board's done column** says "Merged pull requests" only when the line merges.
    - **Onboarding's first-item copy** names no station (AGENTS.md rule 1).
    - **Factories with several repositories** show an item's repositories on the Board, the Items list and the item page.
    - **The settings board** loses the "GitHub label" and "Handle" it drew; the code never had them.

### GitHub events

12. **GitHub webhooks are connected once for the organization, and opened to factories** (ADR 0040, decisions 12–16).
    - **Who adds it:** an owner of the GitHub organization adds one organization webhook, which covers every repository, including later ones. A personal account adds repository webhooks with the same address.
    - **In Goal:**
      - an organization admin opens the webhook to factories, each opening optionally limited to some repositories;
      - a factory editor never sees a delivery's body or headers;
      - a team with read-only or no GitHub access sets up nothing.

13. **A trigger can name the item's repositories from the event.**
    - **How it matches:** `create.repositories: ["{{repo}}"]` matches each value against the factory's entries, owner patterns included, ignoring case. `{{repo}}` is `owner/name`, and the host comes from the sender's webhook.
    - **Results:**
      - **no match:** the evaluation is Skipped, with "byFrederick/legacy is not one of SRE's repositories";
      - **an empty value**, as PagerDuty's and Datadog's events have: none is added;
      - **a fixed name** works too: `create.repositories: [goal]`.
    - **As a filter:** evaluation skips, before any firing, an event from a repository the factory doesn't have (ADR 0040, decision 13).

### Agents' tools

14. **Two GitHub templates in ADR 0037's catalog: `github-read` and `github`.**
    - **`github-read`** runs the GitHub MCP server in its read-only mode and suggests a read-only token.
    - **`github`** keeps today's modes, with merges asking first.
    - **Enterprise:** templates gain non-secret parameters, so an Enterprise host gets `GH_HOST`, `GH_ENTERPRISE_TOKEN`, the local `github-mcp-server` with `GITHUB_HOST`, and a test against its API.
    - **The publish warning:** it fires when an agent that only reads has an environment that allows GitHub tools that aren't read-only. It judges them by the annotations from the server's `tools/list`, not by tool-name globs. Environment tests keep each tool's `readOnlyHint`, which they don't today, and test stdio servers too, such as an Enterprise host's local `github-mcp-server`.

15. **Tools stay for what git doesn't do.** Issues, checks, pull request comments and code search across repositories go through `gh`, `glab` or an MCP server with a token in the environment. ADR 0037 keeps a repository token out of the environment unless an environment file names it. Documents belong in knowledge sources (ADR 0038).

### Knowledge and Jonah

16. **Knowledge repository sources follow the same rules.**
    - **Opening the secret:** sync opens a source's secret only when every factory listing the source is in the secret's scope. A source no factory lists can use only a secret open to every factory.
    - **Hosts** come from the organization's list.
    - **The copy warning** (a source that copies the checkout) applies only to a factory with exactly one repository.

17. **A Jonah try clones the repositories its run recorded,** each at its recorded commit, read only (ADR 0039, decision 9).

### API, CLI and data

18. **API, CLI and data.**
    - **API:**
      - `GET` and `PUT …/code-hosts`;
      - definitions accept `runtime.repositories` with `default`, an agent's `code` and `board.flow.outputs.repositories`;
      - `POST …/items` takes `repositories`, and items answer with them and how each joined;
      - `POST …/factories/{factory}/repositories/test` replaces the organization route;
      - `TemplateSummary` gains `code`;
      - `create.repositories` becomes a template place, so `{{repo}}` passes the placement check.
    - **CLI:**
      - `goal run --repo`, repeated;
      - `goal repository test --factory`;
      - `goal factory show`, which lists the repositories with their access and kept tests.
    - **Data:**
      - `work_item_repositories` (with each repository's `base_commit` and `integration_branch`), `agent_run_repositories` and `work_items.branch`;
      - `repository_tests`, the kept results;
      - `code_hosts`, per organization, with row-level security.

### Moving over

19. **Nothing breaks for today's factories.**
    - **Old definitions:** a published `runtime.repository` reads as one `write` repository, and every agent keeps writing, because `code` defaults to `write`.
    - **Old items** use the default entry until something names a repository (decision 4), and keep their branch names (decision 6).
    - **Hosts:** goald seeds `code_hosts` with each published version's repository host that isn't built in, so no token stops being sent.
    - **Order:**
      - software#538 and the conversion of ADR 0037 decision 19 run first;
      - row 1 refuses a second repository, `read` access and `code: read` until row 4 has the run start and merges to match;
      - onboarding's new order ships with row 8.

## Rejected alternatives

- **A GitHub App.** The owner ruled it out, and organization webhooks plus tokens cover events and code.
- **A connection object per GitHub organization.** ADR 0037 removed connections. Tokens are secrets, webhooks are their own organization objects, and a factory lists its repositories.
- **Cloning every repository the factory covers into each box.** It is slow and large for an organization's worth of repositories. A run starts with the item's, and its agent opens the rest as it needs them.
- **One repository per item.** The first draft. An investigation may need several, and the owner asked for agents to open what they need.
- **Write access per factory only.** The investigator and the fixer would need two factories. The owner asked for a lane whose agent writes instead.
- **Agents that only read by default.** Safer, but every published definition whose agents push today would stop until it's republished. `code: read` is one line where it matters.
- **"No repository" meaning the factory's first, decided at run time.** Reordering or renaming the list would move open items mid-flight. Items record their repositories' URLs.
- **A per-entry `forge`.** It would let anyone send a token to any host. The organization lists its hosts instead.
- **A per-entry `base` branch.** Five readers use the host's default branch, and none of them would follow it.
- **A repository per environment.** Environments say what agents can reach; the item decides where the work happens.
- **Polling GitHub in goald, for organizations where nobody can add a webhook.** A check does it with the GitHub tool, like any other source (ADR 0040, decision 19).

## Consequences

- Any factory can have no code, one repository, several, or every repository of an organization, with read or write access. An incidents factory reads a whole organization; a software factory can write to one repository or many. Each item starts with the repositories it names, and its agents open others as they need them. Only agents that write push, only where an entry allows it, and a read-only token makes sure the others can't.
- An incident can be investigated across many repositories and fixed by a lane whose agent writes, in one factory and one item.
- Onboarding works for teams with read-only or no GitHub access, and for GitHub Enterprise and self-managed GitLab hosts that owners list.
- One GitHub organization webhook serves every factory it's opened to. Adding a factory or a repository needs nothing new in GitHub.
- Branch names change for new items, to include the factory.
- Two fields and a Floor row that never worked go from the screens.

## The build plan

- **One assembly** of 10 rows, filed after ADR 0040's (intake) assembly, which has `domain.File`, the organization's webhooks and the trigger door these rows extend. It also comes after software#538, which moves the code host port onto the repository token.
- **The design docs** are merged by ADR 0040's row 1, because both ADRs and their screens are on `design/intake`.
- **Until row 4,** row 1's publish check refuses a second repository, `access: read` and `code: read`, so nothing deploys half-built.
- **UI rows** follow [MASTER.md](../design/MASTER.md) v3 and [MOTION.md](../design/MOTION.md). They match their screens at 1440, 1280, 1024, 768 and 390 pixels, in light and dark:
  - nothing past the viewport edge;
  - no clipped or spilling text;
  - 44 px tap targets on phones;
  - explanations behind an ⓘ tooltip.

| # | Pull request | Lane | What it changes | Merges after |
|---|---|---|---|---|
| 1 | **Hosts and repositories in the definition.** Decisions 1, 2, 18 and 19. | backend | `code_hosts` and its routes, seeded from published versions' hosts; `runtime.repositories` with `name`, `url` (one repository or an owner pattern), `token`, `access` and `default`; an agent's `code` and its default; the shorthand; the publish rules, including the warning for a reader with a write token and the temporary refusal of a second repository, `read` and `code: read`; repository tokens as secret places and uses; `create.repositories` as a template place; `code` in `template.yaml` and `TemplateSummary` | — |
| 2 | **The item's repositories.** Decisions 4, 6 and 13. | backend | `work_item_repositories`; filing by a person, a part, a follow-up's `start.repositories`, `create.repositories` and a check's items, with the matching and the Skipped evaluation; the default entry, for items with none too; `board.flow.outputs.repositories` and the cap of 10; `work_items.branch` with its backfill; the publish refusal for a used entry; `goal run --repo`, repeated | 1 |
| 3 | **Run start per item.** Decisions 3, 5 and 6. | backend | The bundle and prepare clone the item's repositories (the first at `/workspace`); the box's credential helper per repository path, with the token each agent's access picks, recording what each run fetched or pushed; `pushInsteadOf` for every prefix the run may not write; checks only read; `agent_run_repositories` with base commits, heads and changed-since; pushes joining the item at finish; `paths` with repository prefixes; base commits and integration branches per repository row; the workspace note; stored branch names | 2 |
| 4 | **Merges and hosts.** Decisions 5 and 7. | backend | The code host port by repository and token, for the organization's hosts; merging the item's pull requests, each in its repository, in order, with today's three outcomes for each, and refusing any whose head isn't the item's or its assembly's branch; GitLab base commits and changed-since; `glab` in the image and the landing note; removes row 1's temporary refusal | 3 |
| 5 | **Repository tests.** Decision 8. | backend | The factory route with the secret opened for the factory, the answers and the push warning, kept results and their reruns | 1 |
| 6 | **Agents' GitHub templates.** Decisions 14 and 15. | backend | `github-read` and `github`, the templates' non-secret parameters for Enterprise, the publish warning per agent and per check from tool annotations; environment tests keeping each tool's `readOnlyHint` and testing stdio servers | 1 |
| 7 | **Knowledge and Jonah ties.** Decisions 16 and 17. | backend | Sync's scope rule, hosts from the list and the copy warning; tries clone the run's repositories | 3 |
| 8 | **Code pages.** Decisions 9–11. | frontend | Onboarding's new order and Code step; factory settings' Code section; the organization's Code hosts; Basics' repositories; an agent's `code` on Agents; the agents that write in the Code section; New item's repositories; an item's repositories on the Board, Items and the item page; the Floor's setup and ways-in card; the done column's copy; the first item's copy; onboarding, settings and `goal repository test` on the factory route, and the organization route removed | 2, 4, 5 |
| 9 | **Tablet and phone.** | frontend | The tablet and phone layouts of the Code step, the Code section and New item | 8 |
| 10 | **Docs.** | docs | The docs site's guides to connecting code and GitHub (code hosts, an organization webhook, repository webhooks, read-only teams), the glossary, `factory-definition.md` and `architecture.md` | 9 |
