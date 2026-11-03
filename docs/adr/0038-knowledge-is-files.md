# ADR 0038 — Knowledge is files: synced folders, factory memory, an index in every prompt

Status: proposed · 2026-11-03

Amended during the build: the migration that drops the old tables (00099, with 00098 for the embeddings) does not drop the `vector` extension, because the administrator owns it and the migration role may not drop it. `goal db init` creates it and the administrator drops it, once no database needs 00054's `vector` columns on a fresh install; row 8 of the build plan reads that way. Uploads keep the original in the bucket and their name and content hash in `knowledge_uploads` (decision 2).

- **Replaces:**
  - Decision 6 of [ADR 0008](0008-portable-runs-connections-and-knowledge.md): the knowledge base of chunks and vectors, searched through gateway tools.
  - [ADR 0027](0027-knowledge-use-and-verdicts.md), knowledge uses and verdicts. Uses become reads (decision 10), and verdicts go.
  - The built-in Learned source of [spec 5](../history/specs/5-continuous-learning.md). Factory memory (decision 6) takes its place.
- **Amends:**
  - [ADR 0025](0025-judges-and-evals-as-the-screens-show-them.md), decision 4: a version whose only change is `memory.yaml` publishes without the eval gate (decision 6).
  - [ADR 0014](0014-factory-rules.md): factory memory joins the kinds of guidance.
  - The tool list in ADR 0008's phase 6 amendment: the gateway's knowledge tools go.
- **Builds on:**
  - [ADR 0037](0037-secrets-and-environment-files.md): secrets, and MCP servers in environment files.
  - [ADR 0005](0005-factory-definitions-in-object-storage.md): definitions are versioned files.
  - [ADR 0019](0019-item-watch-live-trace-teardown.md): the trace of tool calls.
- **Screens:** the canvas pages "Access, secrets and knowledge", "Build a factory", "Daily work", "Get started" and "Mobile", exported to [docs/design/screens/](../design/screens/README.md). They are binding ([ADR 0016](0016-ui-revamp.md), [ADR 0036](0036-design-system-v3-plain-and-motion-binding.md)).

## Context

Each organization has one knowledge base in PostgreSQL (ADR 0008, decision 6):
- **Ingestion:** an ingestion loop claims due sources under leases, chunks their documents and embeds each chunk through `ports.Embedder` (Workers AI `bge-m3` in production).
- **Storage:** vectors go into `halfvec(1024)` with an HNSW index, beside a `tsvector`.
- **Agents:** they reach it through three gateway tools, `knowledge__search`, `knowledge__read` and `knowledge__verdict`.
- **Factories:** a factory lists its sources in `spec.knowledge.sources`.

The production organization's numbers, measured on 2026-11-03:
- **One source, which copies the checkout.** `goal-docs` holds 135 files of this repository, synced hourly from `main`. Every run already has them, on the item's branch.
- **Almost no use.** In 90 days, 4 runs called `knowledge__search`. None read a whole document or gave a verdict. The factory finished 511 items in the last 30 days alone.
- **A lot of code.** About 10,500 lines of code and 6,300 lines of tests cover the store, the loop, the chunker, the embedder, the gateway tools, the API, the CLI and three pages.

Four problems follow:

1. **Agents aren't told when to look.** The only hint is a tool's description. Claude Code reaches for Grep and Read, which can't see an index in PostgreSQL. In [Vercel's evals](https://vercel.com/blog/agents-md-outperforms-skills-in-our-agent-evals), a docs skill went unused in 56% of tasks, while an 8 KB index in AGENTS.md got every task right.
2. **There is a second way to read docs.** Passages cut from their files sit beside the checkout the agent already greps, an hour behind and on another branch.
3. **What a factory learns has no home.** The Learned source of spec 5 was never built, and ADR 0034 left learned facts for later.
4. **Some pieces have nothing to do.** Live sources store nothing, and ADR 0037 moved MCP servers into environment files. Verdicts have no rows.

The products that do this well have settled on the same pattern:
- **Files first.** Claude Code replaced an early RAG index with Grep and Read ([memory](https://code.claude.com/docs/en/memory), [skills](https://code.claude.com/docs/en/skills)). Sourcegraph dropped Cody's embeddings ([FAQ](https://sourcegraph.com/docs/cody/faq)), and Devin is folding Knowledge into Skills ([docs](https://docs.devin.ai/product-guides/knowledge)).
- **Always loaded beats "look it up".** A short index sits in the prompt, and the agent opens files on demand.
- **Memory is small, says where it came from, and is reviewed.** Managed Agents mounts memory as files and warns that writable memory lets prompt injection poison later sessions ([docs](https://platform.claude.com/docs/en/managed-agents/memory)). Copilot Memory checks each fact's citation ([docs](https://docs.github.com/en/copilot/concepts/prompting-and-context/copilot-memory)).
- **Search is for what can't be files.** Very large or messy corpora get hybrid search, now a managed service such as Cloudflare AI Search ([Contextual Retrieval](https://www.anthropic.com/engineering/contextual-retrieval)).

The owner's rules for this design:
- **Simple.**
- **Scalable.**
- **Generic.**
  - Nothing is built around this organization's software factory.
  - Any factory sets up the knowledge that fits it, in its own definition as it does everything else. That holds whatever its stations, agents, work types or way of working, with a repository or without.
  - Nothing in Go, SQL or TypeScript names a factory, station, agent or work type (AGENTS.md rule 1).

An independent review of the first draft against the code changed nine things, and they are folded in below:
- delivery and pinning;
- the memory file and the eval gate;
- the key name;
- the limits and reads;
- security, and what waits for later.

## Decision

### Sources

1. **A source is an organization's folder of Markdown files, kept in sync from one place.** Like a secret, it is an organization row, and definitions name it.
   - **Name:** a slug that is also its folder in every run, `/knowledge/<name>/`. It never changes.
   - **Read when:** one line, at most 160 characters, that tells agents when to open the source, such as "How to fix what pages us. Read before acting on an alert." The page asks for it as "When should agents read it?" Each snapshot stores the line, so a run's prompt comes from what the run pinned.
   - **Kind and settings:** decision 2.
   - **Secret:** the ADR 0037 secret the source reads with. goald opens it to sync, and it never reaches a box. A factory that lists the source must be in the secret's factory scope (decision 5).
   - **Sync:** every hour, every day, or only when someone presses Sync. An upload source changes when its files do.
   - **Live sources go.** A system an agent should query live is an MCP server in an environment file (ADR 0037).

2. **Every kind is one reader, and every reader writes Markdown.**
   - **The reader:** it implements `ports.SourceReader`. `List` returns every item with a version, and `Fetch` returns one.
   - **Conversion:** what isn't Markdown or text goes through a new `ports.Converter`. The built-in converter handles HTML. The Workers AI `toMarkdown` adapter handles PDF and Office files, and without it those files are refused, as today.
   - **Citations:** each file starts with a frontmatter line, `source: <url>`, so an agent can cite a link a person can open.

   The kinds now:

   | Kind | Asks for | What becomes a file | Version |
   |---|---|---|---|
   | Upload | Files or folders dropped on the page or sent with the CLI | Each file, at its path. Originals stay in the bucket under `orgs/<org>/knowledge/uploads/`. | Content hash |
   | Repository | A URL, a branch, folders to keep and to leave out, a secret | The Markdown and text files under the folders, read through the code-host adapters. The URL's host picks GitHub or GitLab, as in ADR 0037. A sync that changes many files reads the repository's archive instead of file by file. | Blob id |
   | Confluence | The site, the account email, a secret with the API token, one or more spaces, and optionally only the pages under one page | Each page, at the path of its page tree | Page version and ancestors |

   - **Later kinds**, each one more reader: Website (third-party text, crawled from goald), Notion, Google Drive, and Confluence attachments.
   - **A Command kind**, a command in a box that writes Markdown, is the planned way to cover the rest without Go code. It needs its own ADR.

3. **A sync writes a snapshot only when something changed.** goald's loop claims due sources under leases, as today.
   - **What it fetches:** the reader lists everything. goald fetches and converts only the items whose version changed.
   - **The snapshot:** goald writes one zip on local disk and stores it at `orgs/<org>/knowledge/<source>/<snapshot>.zip`. Unchanged entries are copied from the previous zip without recompressing them.
   - **The object store port** gains a ranged read and a streamed write. Today `Get` and `Put` move whole objects in memory, and nothing holds a whole zip.
   - **The database:** PostgreSQL keeps the source, its snapshots (counts, size, a summary of what changed) and its current files (path, title, size, version).
   - **Failures:**
     - A file that fails to fetch or convert keeps its last good version, or stays out, and the snapshot lists it. It doesn't fail the sync.
     - A file that looks like it holds a key, by ADR 0037's `plain-key` patterns, stays out and is flagged.
     - A source whose last three syncs failed shows in Needs you, once for each factory that lists it.
   - **Retention:** goald keeps the current snapshot and any snapshot an open claim pinned, and deletes the rest.

4. **Limits keep every box fast.**
   - **A source** holds at most 50 MB of Markdown, 20,000 files and 5 MB per file.
   - **A factory** lists at most 10 sources, so a box gets at most 500 MB. The usual few MB arrive in seconds.
   - **Past a limit,** a sync fails and says which limit.
   - **A bigger source** is the case for search by meaning: an index over the same snapshot files, such as Cloudflare AI Search, behind one tool. It needs its own ADR.

### In the definition

5. **`knowledge.sources` in `factory.yaml` lists the sources a factory's runs read.** The key and its check `knowledge-source-unknown` stay as they are. An entry may now also be an object that says who reads the source and why:

   ```yaml
   knowledge:
     sources:
       - handbook
       - name: runbooks
         appliesTo: { stations: [investigate, fix] }
         readWhen: Before you touch production, find the runbook for the alert.
   ```

   - **`appliesTo`:** ADR 0014's rule scope (agents, stations, lanes, work types), matched to each run in the same way. Without it, every run of the factory gets the source.
   - **`readWhen`:** replaces the source's line in this factory's prompt, in its own words.

   New checks:
   - `knowledge-too-many`: more than 10 sources. This is an error.
   - `knowledge-secret-not-in-scope`: the factory isn't in the source's secret's scope, so listing the source would hand that token's reach to it. This is an error.
   - `knowledge-copies-checkout`: a Repository source of the factory's own repository. This is a warning: its runs already have those files, on the item's branch.

   Adding a source to a factory is one line in its draft. The Add a source page and the source page write it, and they never publish. Publishing stays the person's step, with its eval gate.

6. **`memory.yaml` is the factory's memory: short facts its runs should know.**

   ```yaml
   - text: staging-db disk alerts below 85% are noise; the nightly vacuum clears them.
     from: [sre#225, sre#231]
     added: 2026-11-01
   ```

   - **An entry** is one fact of at most 300 characters. `from` names the items it came from, and `added` is the date.
   - **Limits:** the file holds at most 8 KB and 50 entries. The checks are `memory-too-long` and `memory-too-big`.
   - **Who writes it:**
     - people, in the Form view, the YAML view, or with `goal memory add`;
     - a definition folder pushed with `goal factory push` (a local agent that sets a factory up adds facts this way, as the person it acts for);
     - Jonah, once its redesign decides how (decision 11).
   - **Why one file:** add, merge and remove are edits to one file, easy to review as a diff. It is also what today's proposals can express: a `ProposalFile` writes whole files and never deletes one.
   - **Publishing:** a version whose only change is `memory.yaml` publishes without the eval gate (ADR 0025, decision 4). A fact is reviewed as text, and eval runs pick it up with the next change that runs the gate.
   - **Facts for the whole organization** belong in a source that every factory lists, not in memory.
   - **The name:** the UI calls it "factory memory" wherever "memory" alone could mean a box's memory or Claude Code's.

   Factory memory joins ADR 0014's kinds of guidance: rules say how to act, skills say how to do a task, and memory says what is true here.

### What a run gets

7. **The sources that apply, as files, under `/knowledge`.**
   - **Pinning:** when an item is claimed, goald decides the snapshots of its sources that apply. It does this next to the model and the deny list, so the snapshots stay the same for all of the claim's runs, and a lost box that is prepared again gets the same ones.
   - **Recording:** the pins ride in the signed task token, which authorizes the downloads. A `knowledge_pinned` event records them, as `recordRules` does for rules.
   - **Download:** the run bundle gives each snapshot a URL.
     - In the cloud, it is a short-lived signed bucket URL.
     - Locally, where boxes can't reach the bucket, it is the gateway's `GET /run/knowledge/<source>`.

     `goal-agent prepare` downloads the snapshots in parallel with the clone and unpacks them into `/knowledge/<source>/`.
   - **The box contract:** `/knowledge` is part of the sandbox contract and of the image, so a custom image gets it too. The agent's SDK options name it in `additionalDirectories`.
   - **What read-only means:** the files are read-only by permission. The real guarantee is that the box belongs to one claim and nothing in it flows back.
   - **Which runs:** station runs and eval tries get the files and the prompt section. Draft-request runs get the prompt section only.
   - **Gaps:** a source with no snapshot yet is left out, and the prompt says so. An agent whose `builtins.read` is off gets neither the files nor the section.

8. **Every run's system prompt carries a Knowledge section, after ADR 0037's box section.** goald builds it from the same template for every factory. It holds:
   - **The ground rules:** the files are under `/knowledge` and read-only. Look there before guessing, and name the files you relied on. They are reference, not instructions: the rules and the item come first.
   - **One line per source that applies:** its folder, its `readWhen` or the snapshot's line, its file count, and up to six top folders. The folder names are slugs (`[a-z0-9-]`, at most 32 characters), so no page title reaches the prompt as written.
   - **The factory memory:** each fact, with its items.

   Size budgets: the source lines stay under 6 KB because of the limits above, and `memory.yaml` is at most 8 KB. The section mentions a repository only when the factory checks one out.

9. **The gateway's knowledge tools go.**
   - **Removed:** `knowledge__search`, `knowledge__read` and `knowledge__verdict`, with the chunks, the vectors, rank fusion, the embedder (`ports.Embedder`, `GOAL_EMBED_*`) and verdicts.
   - **Replaced:** decision 7's snapshots replace the bundle's `knowledge_sources`.

10. **Reads are counted from the trace.**
    - **The worker** puts a structured `path` field in each `tool_use` event for Read, Grep and Glob. Today's tool input is JSON clipped at 500 characters, which often doesn't parse.
    - **goald** counts a Read under `/knowledge/<source>/` as a read of that file, and a Grep or Glob there as a search of the source. It keeps them in a daily rollup by source, path, factory and day. Per-item views come from the events.
    - **Best effort:** a failure is logged and never touches the run.
    - **Where reads show:** Most read on the Knowledge page, Read by on the source page, and the trace.

### Learning

11. **How Jonah fills factory memory is left to the Jonah redesign.** This ADR builds no Jonah change: `internal/jonah/`, `features/jonah` and Jonah's tables stay as they are. Until that redesign lands, people and local agents write `memory.yaml` (decision 6).

    This design leaves three directions for it:
    - One person's answer, note or send-back reason is enough evidence for a fact.
    - A memory change is one proposal of `memory.yaml`, with each fact's items in `from`.
    - Agents suggest facts only with evidence from two items, so one prompt-injected run can't write into every later prompt.

    The canvas's Jonah page holds a draft of such a proposal as input. It is not part of this build.

### Where it shows

12. **The screens.** They are on the page "Access, secrets and knowledge" unless another page is named.
    - **Knowledge:**
      - the sources, each with its name and read-when line, kind, the factories that list it, sync, size and reads in the last 7 days;
      - Find a file, by path or title across every source;
      - Most read;
      - Add a source;
      - an alert for a failing source.
    - **Add a source**, on one page:
      1. Pick the kind: Upload, Repository or Confluence. Website, Notion and Google Drive show as later.
      2. Fill in the kind's settings.
      3. Give the name and read-when line.
      4. Pick the factories that read it. Each is a checkbox that adds the line to that factory's draft.

      A Check panel:
      - tests access;
      - counts files and size against the limits;
      - warns when a repository is the checkout of a picked factory, or when a picked factory is outside the secret's scope;
      - shows the line agents will see.
    - **A source's page:** its files with their reads, the latest sync, its settings, the line agents see, and the factories and agents that read it, with Add to a factory.
    - **Definition, Form view, Knowledge section** (page "Build a factory"):
      - **Sources:** the factory's sources, each with who reads it (every agent, or its `appliesTo`), its own `readWhen` if it has one, and its size. People can add and remove them.
      - **Factory memory:** its entries, with add, edit and remove into the draft.
      - **What agents see:** the prompt section as one agent gets it.

      The YAML view shows `knowledge:` and `memory.yaml`.
    - **The trace:** reads and searches under `/knowledge`, with a knowledge tag.
    - **Needs you:** a source that keeps failing, shown under each factory that reads it.
    - **Every screen works at 1440, 1280, 1024, 768 and 390 pixels.** The tablet and phone versions are on the canvas.

### API, CLI and data

13. **The API** (`contracts/api/openapi.yaml`), under `/v1/orgs/{org}/knowledge`:
    - `sources`: list and create;
    - `sources/{name}`: get, patch and delete. Delete is refused with 409 while a published version lists the source, and the answer names the factories;
    - `sources/{name}/sync`;
    - `sources/{name}/files`, filtered by path or title, and `sources/{name}/files/{path}` for one file's Markdown, read by range from the zip;
    - `sources/{name}/uploads`: post and delete;
    - `check`: unsaved settings in, the Check panel's answers out;
    - `files`: find a file across sources;
    - `reads`: the rollup, by source and window.

    Factory memory is definition files and needs no endpoint of its own.

    Permissions:
    - `knowledge:read`: owner, admin and member, with keys and personal tokens.
    - `knowledge:manage`: owner and admin, with keys.
    - `knowledge:verdict` is removed.

14. **The CLI.**
    - **`goal knowledge`:**
      - `sources`;
      - `add <name> --kind … --read-when … [--factories a,b]`, which writes each factory's draft;
      - `check`, with the same flags as a dry run;
      - `sync`, `files [--find …]`, `cat <source> <path>`, `upload <source> <file|dir>…`, `rm` and `reads`.
    - **`goal memory`:** `list <factory>`, `add <factory> "<text>" [--from <item>] [--publish]` and `rm <factory> <n>`. They write the draft, and `--publish` publishes it.
    - **Removed:** `goal knowledge search`, `usage`, `uses`, `documents` and `verdict`.
    - **Goal's MCP server (spec 6):** when it lands for outside clients, these operations become its tools. Until then, a local agent uses the CLI.

15. **The data.**

    | Table | What a row is |
    |---|---|
    | `knowledge_sources` | Gains `read_when`, the kind's `config`, `secret` and the current snapshot. The kinds become upload, repository and confluence. |
    | `knowledge_snapshots` | One per snapshot: counts, size, changes, the read-when line, the zip's key |
    | `knowledge_files` | One per current file: path, title, size, version |
    | `knowledge_reads` | One per source, path, factory and day: reads and searches |

    - **Row-level security:** every table is keyed by `org_id`.
    - **Migration order:** the migration adds before it drops. `knowledge_documents`, `knowledge_chunks`, `knowledge_uses`, `knowledge_verdicts` and the `vector` extension go only after the cut-over (decision 16).

### Moving over

16. **A one-time, idempotent conversion of sources.** Definitions need none, since `knowledge.sources` keeps its meaning.
    - **Repository:** a source that read a gitdocs connection becomes a Repository source with the same repository, folders and secret. ADR 0037 already made its token a secret.
    - **Confluence:** a Confluence source keeps its spaces.
    - **Upload:** uploads keep their originals.
    - **Live:** live sources are deleted. They held nothing, and their MCP servers are in environment files.
    - **First snapshots:** every source syncs once, and runs get files from the next claim on.
    - **goal-docs:** its factory's definition shows the `knowledge-copies-checkout` warning. The owner removes the line and deletes the source.

## Rejected alternatives

- **Keep the index and make it better** (contextual chunks, reranking, passages in every brief). It handles the largest corpora, at a model call per chunk at each sync and a reranker per search. It is still a second way to read docs, and its passages arrive out of context. It waits behind decision 4, as an index over the same files.
- **No knowledge base:** repositories cloned into the box, and SaaS docs through their MCP servers. It is the least code. But support and ops people can't upload a PDF, nobody can see what agents know or read, and every lookup is a vendor's tool call.
- **Sources that pick their factories ("All factories").** It is one click. But what a factory's agents read would change without a version of that factory, and a factory couldn't scope or word a source for its own stations. The Add a source page gets the same single step by writing each factory's draft.
- **Knowledge in environment files.** An environment says what the box installs and reaches. A factory often has several, so the lines would repeat. `appliesTo` on an entry narrows a source the way rules are narrowed, which covers per-agent needs without a list on every agent.
- **A folder of memory files, or memory in its own tables.** Proposals can't delete files, and one file is what they edit well. Tables would mean a new review flow and a new CLI, while the definition already has versions, Jonah's proposals, Approve and Restore.
- **Memory opened on demand, or written by agents directly.** Opened on demand, it misses what Vercel measured: what must be known has to be in the prompt, and 8 KB of facts fits. Written directly by agents, it becomes a poisoning path into every later prompt.
- **Delivering snapshots through goald.** One goald would stream up to 500 MB per claim. Signed bucket URLs send boxes to the bucket, and the gateway serves only the local case.
- **People's full-text search with snippets.** It costs a `tsvector` per file, capped at 1 MB, and a ranged read per snippet. Finding a file by path or title covers what people do on the page, and agents grep.
- **Syncing every kind in a box now.** It would support any git host and any converter, at a box start per sync. goald's readers exist and are incremental, and the Command kind brings box syncs when a kind needs them.
- **Per-file blobs beside the zips.** Building each snapshot would need a fetch per file. One zip per snapshot is one object to write, serve and delete.
- **Mounting the bucket in the box (FUSE).** It is slower than unpacking one archive, and it needs a driver in every image.

## Consequences

- **Agents read knowledge the way they read code,** led by an index that is always in the prompt. What they read shows in the trace, and what people add to factory memory reaches every run.
- **One model for every factory.** A Software, SRE or Support factory, or one shaped like none of them, gets the same `/knowledge` layout and the same prompt section, whether or not it checks out a repository. `appliesTo` and `readWhen` fit the sources to how that factory works.
- **In the definition, like everything else.** A factory's sources, their scope and wording, and its memory are versioned, reviewed, published and restored.
- **Much less to run.**
  - **Removed:** the embedder, vectors, chunks, rank fusion, three tools and verdicts, with the `vector` extension and the embedding use of the Workers AI token.
  - **Added:** snapshots, the object store's ranged and streamed calls, a converter port, the Check, the daily reads rollup and the prompt section.
- **A box downloads its sources once per claim, straight from the bucket.** Typical sources are a few MB, and the limits cap the worst case at 500 MB.
- **Other ADRs:**
  - ADR 0014 gains factory memory as a kind of guidance.
  - ADR 0025's eval gate lets memory-only versions through.
  - ADR 0008's tool list and the docs site's Knowledge page are rewritten.
  - ADR 0034 is unchanged: the Jonah redesign decides how Jonah writes memory.
- **Accepted risk:** source files are reference text, and a prompt injection could hide in them. Boxes have open internet and hold their environment's secrets (ADR 0009).
  - The prompt says source files are reference, not instructions.
  - Folder names are slugs.
  - Only owners and admins add sources.
  - Agents can't write factory memory at all; people do.

## The build plan

- **One assembly:** each row is one pull request, in merge order.
- **The design branch:** `design/knowledge` holds this ADR, the exported screens and the screens index, and is merged first.
- **When it starts:** after ADR 0037's build (software#513), because the Repository and Confluence kinds read with its secrets.
- **UI rows** follow [MASTER.md](../design/MASTER.md) v3 and [MOTION.md](../design/MOTION.md). Each must match its screens at 1440, 1280, 1024, 768 and 390 pixels, in light and dark:
  - nothing past the viewport edge;
  - no clipped or spilling text;
  - 44 px tap targets on phones.

| # | Pull request | Lane | What it changes | Merges after |
|---|---|---|---|---|
| 1 | **Design docs.** Merge `design/knowledge`. | docs | This ADR, the ADR index, the status lines of ADRs 0027, 0008, 0014 and 0025, and `docs/design/screens/**` with its README | — |
| 2 | **Store, additive.** Decisions 1, 3 and 15. | backend | A migration that adds the new columns and the `knowledge_snapshots`, `knowledge_files` and `knowledge_reads` tables with row-level security, beside today's tables. The domain types, the reshaped `ports.Knowledge`, `ports.Converter`, the object store's ranged read and streamed write with their adapters, and conformance tests. | 1 |
| 3 | **Sync.** Decisions 2–4. | backend | The loop writes zip snapshots on local disk and copies unchanged entries. Per-file failures, the key scan, retention by open claims, and a Needs you entry per listing factory. Readers: Upload; Repository (generalized from `gitdocs`, with the archive path for big changes); Confluence by page tree. The HTML converter and the Workers AI `toMarkdown` adapter. The Workers AI token's rename from the embedder's is in this row's Terraform, applied through the coordinator with the owner's authorization. | 2 |
| 4 | **Definition.** Decisions 5 and 6. | backend | Entry objects in `knowledge.sources` (`appliesTo`, `readWhen`) and `memory.yaml` in `contracts/definitions/`. Config and factorymodel. The checks `knowledge-too-many`, `knowledge-secret-not-in-scope`, `knowledge-copies-checkout`, `memory-too-long` and `memory-too-big`. A version whose only change is `memory.yaml` passes the eval gate (`internal/authoring/evalgate.go`). | 2 |
| 5 | **Run start.** Decisions 7–9. | backend | Pins on the claim, in the task token and the `knowledge_pinned` event. The bundle's snapshot URLs, and the gateway's `GET /run/knowledge/<source>` for local runs. The prompt section, built per run from `appliesTo`. The three knowledge tools are removed. In `apps/worker`: `prepare` downloads in parallel with the clone, `/knowledge` goes into the image and the sandbox contract, and `agent` adds `additionalDirectories` and the section. | 3, 4 |
| 6 | **Reads.** Decision 10. | backend | The worker's structured `path` for Read, Grep and Glob. goald counts reads and searches into the daily rollup. | 5 |
| 7 | **API and CLI.** Decisions 13 and 14. | backend | The endpoints and permissions in `contracts/api/`, the handlers, `goal knowledge` and `goal memory`. | 3, 6 |
| 8 | **Moving over.** Decision 16. | backend | The conversion of sources. Then a migration that drops the old tables and the `vector` extension, and the removal of `ports.Embedder`, the OpenAI embedder adapter and `GOAL_EMBED_*`. | 5, 7 |
| 9 | **Knowledge pages.** | frontend | `features/knowledge`: the list with Find a file and Most read; Add a source with its kinds, the Check panel and the factory checkboxes; and the source page. | 7 |
| 10 | **Knowledge in the definition editor.** | frontend | `features/factory-editor`: the Form view's Knowledge section (sources with `appliesTo` and `readWhen`, factory memory, the prompt preview) and `memory.yaml` in the YAML view. | 4, 7 |
| 11 | **Everything else that showed knowledge.** | frontend | As the exported screens show them: the trace's knowledge reads, Needs you's failing source per factory, the secret page's uses, onboarding's Add knowledge card, and the phone screens. | 9, 10 |
| 12 | **Docs.** | docs | Rewrite `apps/docs/src/content/docs/concepts/knowledge.md`. Add Knowledge and factory memory sections to `docs/engineering/factory-definition.md`. Update the glossary, the CLI reference, and the change-map entries for sources, snapshots, reads and memory. | 11 |
