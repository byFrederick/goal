# ADR 0041 — Code and GitHub without an App: optional code access, a repository per item, webhooks the organization connects once

Status: proposed · 2026-11-03

- **Amends:**
  - [ADR 0037](0037-secrets-and-environment-files.md), decisions 6–7: `runtime.repository` becomes a list of repositories with their access, on code hosts the organization lists (decisions 1–8). Its run start on secrets, which software#538 finishes, is what this builds on.
  - [ADR 0032](0032-self-serve-onboarding.md): onboarding asks for code after the factory, as a choice of no code, read only, or read and write (decision 9).
  - [ADR 0015](0015-overlapping-in-flight-work.md) and [ADR 0033](0033-assembly-integration-branches.md): paths overlap, and integration branches live, only within one repository (decision 5).
  - [ADR 0038](0038-knowledge-is-files.md): a repository source's secret opens only for the factories that list it (decision 16).
  - [ADR 0039](0039-jonah-learns-from-the-line.md): a try clones the repository its run recorded (decision 17).
  - [ADR 0040](0040-one-door-for-every-source.md): an item records its repository at filing; a trigger and a follow-up can set it (decisions 4 and 13); GitHub webhooks are connected once for the organization (decision 12, with ADR 0040's decisions 12–16).

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

     There is no `forge`, because the host list gives it. There is no `base` either: the host's default branch is used, because prepare, base commits, the landing note and `gh pr create` all read it.
   - **No repositories** is a factory with no code.
   - **The shorthand:**
     - `runtime.repository` stays a shorthand for one `write` entry, named with a slug of the URL's last segment, or `repo` when that isn't a valid slug. It keeps every published version working.
     - A definition with both forms is refused, and so is `write` without a token.
   - **Where secrets may appear:** repository tokens become one of the places a `{{secret}}` may appear (ADR 0037 decision 6). They count as the secret's uses, so it can't be deleted while used, and they show on Access.

3. **goald never writes to a `read` repository; the token's scope is the real limit.**
   - **Never writing:** goald never pushes, creates branches or merges in a `read` repository.
   - **The guard:** prepare disables the remote's push URL, so a push fails with a clear message. The agent's git keeps the token for fetches, which the partial clone needs.
   - **The workspace note** says the repository is read only.
   - **The read test** warns when the token can push: "Goal won't push, but agents could. Use a read-only token."
   - **At publish:** a line that merges (a merge station or integration branches) needs at least one `write` repository.
   - **At filing:** an item filed into a `read` repository of such a line is refused, with the reason.

4. **Each item records its repository when it is filed, by URL.** `work_items.repository_url` holds the normalized `host/owner/name`, in lower case, so renaming or reordering entries never moves an item.
   - **How it's set:**
     - **a person** picks one in New item or with `goal run --repo`;
     - **a trigger** sets it with `create.repository` (decision 13);
     - **a part** takes its assembly's;
     - **a follow-up's item** takes the follow-up's `start.repository`, one of the factory's, or else its source item's.
   - **When nothing sets it:**
     - **one entry naming one repository:** that one;
     - **otherwise** (several entries, or a whole organization): none, so the item starts with an empty workspace. A station can then set the repository once, through the output field the definition names (`board.flow.outputs.repository`, next to ADR 0040's `followUps` and `close`).
   - **Setting it later** is refused on parts, into a `read` repository on a merging line, and after any run of the item cloned a repository. Runs record their repository (`agent_runs.repository_url`).
   - **A triage item** (ADR 0040) whose follow-ups come from several repositories gets none.
   - **Removing an entry** that open items use is refused at publish.

5. **Everything that used the factory's repository uses the item's:**
   - the run's clone, branch, heads, base commit and changed-since;
   - overlapping paths, compared only between items in the same repository (ADR 0015);
   - integration branches, per assembly in its repository (ADR 0033);
   - merges, which take only pull requests in the item's own repository, with its token. A pull request elsewhere is refused with the reason, because no station checked it;
   - the workspace note: the item's repository and its access, and at most ten of the factory's other repositories, with each host's CLI (`gh` or `glab`).

   Nothing else is cloned.

6. **New items' branches include the factory:** `goal/<factory>/<number>-<slug>`. Two factories working in one repository can then never share a branch. Items filed before keep today's names.

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
   - **What goes:** the organization-level test route, which only onboarding used before the factory existed.

### Onboarding and settings

9. **Onboarding asks for code after the factory.**
   - **The order** becomes Organization, Factory, Code, Line, Budget, Intake, People, First item. The template and the factory then exist when the code is chosen.
   - **Template needs:** each template's `template.yaml` says `code: none` or `code: read`, and `write` follows from a line that merges. This is exposed on `TemplateSummary`.
   - **The Code step** offers No code, Read only, and Read and write, and disables the choices the template's line can't use, with the reason.
   - **One form for every case.**
     - **No code** asks for nothing, so onboarding never needs a repository.
     - **Read only, and Read and write,** ask the same two things: which repositories (a whole organization, or only some, one or more) and a token.
     - So an incidents factory reading an organization, a software factory writing to one repository, and one writing to many all fill in the same form.
     - Each item clones the one repository it's about.
   - **The token** is saved as a secret open to the new factory and tested for it. On a name clash, the step offers to use the existing secret or pick a new name, and never replaces a value.

10. **Factory settings' Repository section becomes Code.**
    - **Each repository** shows its access, its kept test result with Test, and its token by name.
    - **Below the list:** Add a repository, the branch names, and the host's default branch, read only.
    - **Who to ask:** creating a secret or opening a webhook needs an owner or admin, and the section says so.
    - **No code:** a factory with no repositories says "No code. Agents work through their tools."
    - **The organization's settings** gain Code hosts (decision 1).

11. **Copy stops assuming code.**
    - **The Floor's setup** counts the factory's code choice, and its ways-in card loses "Label an issue", which nothing implements.
    - **The Board's done column** says "Merged pull requests" only when the line merges.
    - **Onboarding's first-item copy** names no station (AGENTS.md rule 1).
    - **Factories with several repositories** show the item's repository on the Board, the Items list and the item page.
    - **The settings board** loses the "GitHub label" and "Handle" it drew; the code never had them.

### GitHub events

12. **GitHub webhooks are connected once for the organization, and opened to factories** (ADR 0040, decisions 12–16).
    - **Who adds it:** an owner of the GitHub organization adds one organization webhook, which covers every repository, including later ones. A personal account adds repository webhooks with the same address.
    - **In Goal:**
      - an organization admin opens the webhook to factories, each opening optionally limited to some repositories;
      - a factory editor never sees a delivery's body or headers;
      - a team with read-only or no GitHub access sets up nothing.

13. **A trigger can set the item's repository from the event.**
    - **How it matches:** `create.repository: "{{repo}}"` matches the event's full repository URL (host and path, ignoring case) against the factory's entries, owner patterns included.
    - **Results:**
      - **no match:** the event is skipped with "byFrederick/legacy is not one of SRE's repositories";
      - **an empty value**, as PagerDuty's and Datadog's events have: no repository;
      - **a fixed name** works too: `create.repository: goal`.
    - **As a filter:** evaluation skips, before any firing, an event from a repository the factory doesn't have (ADR 0040, decision 13).

### Agents' tools

14. **Two GitHub templates in ADR 0037's catalog: `github-read` and `github`.**
    - **`github-read`** runs the GitHub MCP server in its read-only mode and suggests a read-only token.
    - **`github`** keeps today's modes, with merges asking first.
    - **Enterprise:** templates gain non-secret parameters, so an Enterprise host gets `GH_HOST`, `GH_ENTERPRISE_TOKEN`, the local `github-mcp-server` with `GITHUB_HOST`, and a test against its API.
    - **The publish warning:** it fires when every repository of a factory is `read` but an environment allows GitHub tools that aren't read-only. It judges them by the annotations kept from the server's `tools/list`, not by tool-name globs.

15. **Agents reach repositories other than their item's through tools, not clones.** `gh`, `glab` or an MCP server with a token in the environment reads what that token can see. Documents belong in knowledge sources (ADR 0038).

### Knowledge and Jonah

16. **Knowledge repository sources follow the same rules.**
    - **Opening the secret:** sync opens a source's secret only when every factory listing the source is in the secret's scope. A source no factory lists can use only a secret open to every factory.
    - **Hosts** come from the organization's list.
    - **The copy warning** (a source that copies the checkout) applies only to a factory with exactly one repository.

17. **A Jonah try clones the repository its run recorded,** at the recorded commit, read only (ADR 0039, decision 9).

### API, CLI and data

18. **API, CLI and data.**
    - **API:**
      - `GET` and `PUT …/code-hosts`;
      - definitions accept `runtime.repositories`;
      - `POST …/items` takes `repository`, and items answer with it;
      - `POST …/factories/{factory}/repositories/test` replaces the organization route;
      - `TemplateSummary` gains `code`;
      - `create.repository` becomes a template place, so `{{repo}}` passes the placement check.
    - **CLI:**
      - `goal run --repo`;
      - `goal repository test --factory`;
      - `goal factory show`, which lists the repositories with their access and kept tests.
    - **Data:**
      - `work_items.repository_url` and `agent_runs.repository_url`;
      - `repository_tests`, the kept results;
      - `code_hosts`, per organization, with row-level security.

### Moving over

19. **Nothing breaks for today's factories.**
    - **Old definitions:** a published `runtime.repository` reads as one `write` repository.
    - **Old items:** the migration writes each open item's repository URL, and items filed before keep their branch names.
    - **Order:**
      - software#538 and the conversion of ADR 0037 decision 19 run first;
      - row 1 refuses a second repository and `read` access until row 4 has the run start and merges to match;
      - onboarding's new order ships with row 8.

## Rejected alternatives

- **A GitHub App.** The owner ruled it out, and organization webhooks plus tokens cover events and code.
- **A connection object per GitHub organization.** ADR 0037 removed connections. Tokens are secrets, webhooks are their own organization objects, and a factory lists its repositories.
- **Cloning every repository into each box.** It is slow and large for an incidents factory over many repositories, so only the item's repository is cloned.
- **"No repository" meaning the factory's first, decided at run time.** Reordering or renaming the list would move open items mid-flight. Items record their repository's URL when they are filed.
- **A per-entry `forge`.** It would let anyone send a token to any host. The organization lists its hosts instead.
- **A per-entry `base` branch.** Five readers use the host's default branch, and none of them would follow it.
- **A repository per environment.** Environments say what agents can reach; the item decides where the work happens.
- **Polling GitHub with a read-only token, for organizations where nobody can add a webhook.** It's possible later, as another sender.

## Consequences

- Any factory can have no code, one repository, several, or every repository of an organization, with read or write access. An incidents factory reads a whole organization; a software factory can write to one repository or many. Each item clones only the repository it's about. Goal never pushes to them, and a read-only token makes sure agents can't either.
- Onboarding works for teams with read-only or no GitHub access, and for GitHub Enterprise and self-managed GitLab hosts that owners list.
- One GitHub organization webhook serves every factory it's opened to. Adding a factory or a repository needs nothing new in GitHub.
- Branch names change for new items, to include the factory.
- Two fields and a Floor row that never worked go from the screens.

## The build plan

- **One assembly** of 10 rows, filed after ADR 0040's (intake) assembly, which has `domain.File`, the organization's webhooks and the trigger door these rows extend. It also comes after software#538, which moves the code host port onto the repository token.
- **The design docs** are merged by ADR 0040's row 1, because both ADRs and their screens are on `design/intake`.
- **Until row 4,** row 1's publish check refuses a second repository and `access: read`, so nothing deploys half-built.
- **UI rows** follow [MASTER.md](../design/MASTER.md) v3 and [MOTION.md](../design/MOTION.md). They match their screens at 1440, 1280, 1024, 768 and 390 pixels, in light and dark:
  - nothing past the viewport edge;
  - no clipped or spilling text;
  - 44 px tap targets on phones;
  - explanations behind an ⓘ tooltip.

| # | Pull request | Lane | What it changes | Merges after |
|---|---|---|---|---|
| 1 | **Hosts and repositories in the definition.** Decisions 1, 2 and 18. | backend | `code_hosts` and its routes; `runtime.repositories` with `name`, `url` (one repository or an owner pattern), `token` and `access`; the shorthand; the publish rules, including the temporary refusal of a second repository and `read`; repository tokens as secret places and uses; `create.repository` as a template place; `code` in `template.yaml` and `TemplateSummary` | — |
| 2 | **The item's repository.** Decisions 4 and 13. | backend | `work_items.repository_url` with its backfill; filing by a person, a part, a follow-up's `start.repository` and `create.repository`, with its matching and the skip; the defaults; `board.flow.outputs.repository` with its refusals; `agent_runs.repository_url`; the publish refusal for a used entry; `goal run --repo` | 1 |
| 3 | **Run start per item.** Decisions 3, 5 and 6. | backend | The bundle and prepare clone the item's repository; the fetch token for the agent's git, and the push guard for `read`; heads, base commit, changed-since and paths per repository; integration branches in the assembly's repository; the workspace note; branch names with the factory | 2 |
| 4 | **Merges and hosts.** Decisions 5 and 7. | backend | The code host port by repository and token, for the organization's hosts; merging only the item's repository's pull requests; GitLab base commits and changed-since; `glab` in the image and the landing note; removes row 1's temporary refusal | 3 |
| 5 | **Repository tests.** Decision 8. | backend | The factory route with the secret opened for the factory, the answers and the push warning, kept results and their reruns; the organization route goes | 1 |
| 6 | **Agents' GitHub templates.** Decisions 14 and 15. | backend | `github-read` and `github`, the templates' non-secret parameters for Enterprise, and the publish warning from tool annotations | 1 |
| 7 | **Knowledge and Jonah ties.** Decisions 16 and 17. | backend | Sync's scope rule, hosts from the list and the copy warning; tries clone the run's repository | 3 |
| 8 | **Code pages.** Decisions 9–11. | frontend | Onboarding's new order and Code step; factory settings' Code section; the organization's Code hosts; Basics' repositories; New item's repository; the repository on the Board, Items and the item page; the Floor's setup and ways-in card; the done column's copy; the first item's copy | 2, 5 |
| 9 | **Tablet and phone.** | frontend | The tablet and phone layouts of the Code step, the Code section and New item | 8 |
| 10 | **Docs.** | docs | The docs site's guides to connecting code and GitHub (code hosts, an organization webhook, repository webhooks, read-only teams), the glossary, `factory-definition.md` and `architecture.md` | 9 |
