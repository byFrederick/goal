---
title: Knowledge
description: Knowledge is folders of Markdown files. Sources and their read-when line, snapshots and limits, how runs read them under /knowledge, factory memory in memory.yaml, reads, and adding a source to a factory.
sidebar:
  order: 23
---

Knowledge is folders of files that agents read before they act, plus the factory's memory, a short list of facts every run knows. Each organization has sources, and a source is one folder of Markdown files kept in sync from one place. A factory's definition lists the sources its runs get. Runs read them as read-only files under `/knowledge`.

## Sources

A source has a name, a kind, a read-when line and the settings of its kind.

- **Name:** a slug of 3 to 40 lowercase letters, digits and dashes. It is the source's folder in every run, `/knowledge/<name>/`, and it never changes.
- **Read-when line:** one line of at most 160 characters that tells agents when to open the source, such as "How to fix what pages us. Read before acting on an alert." The pages ask for it as "When should agents read it?" Every run's prompt shows it, so say when to read the files, not only what they are. Each snapshot keeps the line it was written with. An edited line reaches new runs after the source's next sync, which starts right after you save the edit, even when no file changed; a run that already started keeps the line of the snapshot it pinned.
- **Kind:** where the files come from.

| Kind | Where the files come from |
| --- | --- |
| Upload | Files people drop on the page or send with `goal knowledge upload`: text, Markdown, HTML, JSON or YAML, and PDF and Office files when goald has a conversion service (`GOAL_WORKERS_AI_URL`; without one it refuses them). The originals stay in the bucket. |
| Repository | The Markdown and text files under the folders you name in a GitHub or GitLab repository, on one branch. The URL's host must be one of the organization's [code hosts](/docs/getting-started/organizations/#code-hosts), and the kind it is listed as picks the API goald reads it through. Folders and leave-outs are globs, such as `docs/runbooks/**` and `**/drafts/**`. A public repository needs no secret. |
| Confluence | The pages of one or more spaces of a Confluence Cloud site, or only one page and the pages under it, at the path of their page tree: Paging under Runbooks in space ENG is `ENG/runbooks/paging-<id>.md`. A page you move is fetched again at its new path. It reads with the site, the account's email and a secret that holds the API token. |

Website, Notion and Google Drive show on the Add a source page as later. A Repository or Confluence source reads with a secret that is open to every factory that lists the source, or to all factories while no factory lists it ([Access and secrets](../access-and-secrets/), [A synced source's secret and config](#a-synced-sources-secret-and-config)). You can't delete a secret while a source reads with it: the error names the source.

## Sync, snapshots and limits

A sync reads the source, converts what is not Markdown, and writes a snapshot, one zip of its files, only when something changed. A source syncs every hour, every day, or only when someone presses **Sync now**. Every source syncs once when it is created. After that, an on-demand source syncs only when someone presses **Sync now**, and between syncs it has no next sync. If its sync fails, it doesn't retry on its own: press **Sync now** again. In the API it has `sync_every_seconds` 0 and `next_sync_at` null. An upload source changes when its files do. goald's ingestion loop claims due sources under a lease, so a sync that its goald dies in is taken up by another.

- A file that fails to fetch or convert keeps its last good version, or stays out, and the snapshot counts it as failed. It doesn't fail the sync.
- A Repository sync that changes 50 files or more downloads the branch's archive once instead of fetching each file. It takes only the listed files that are still the version it listed, and fetches the rest one by one. An archive over 200 MB, or one that fails to download or read, doesn't fail the sync: it fetches the files one by one instead.
- A file that looks like it holds a key stays out.
- goald keeps the current snapshot, any snapshot a running run pinned, and a replaced snapshot for at least the longest a claim can last (about 4 hours by default). After that it deletes the snapshot at the source's next sync. Deleting a source deletes its snapshots.

Limits keep every box fast. A sync that goes past one fails and names it.

| Limit | Value |
| --- | --- |
| Files in a source | 20,000 |
| Markdown in a source | 50 MB |
| One file | 5 MB |
| Sources a factory lists | 10 |

When you upgrade to knowledge as files, goald converts each source once, at its start. A synced source that reads with a secret becomes a `repository` source (the `gitdocs` reader) or a `confluence` source. It keeps its name, its secret and its config, without `reader`. Uploads keep their originals. Every source then syncs once, and runs read its files from the next claim on. A factory keeps listing a converted source by its name, so no definition changes. A synced source that goald can't convert stays as it is, and goald logs why (`knowledge source not converted`).

### A synced source's secret and config

A synced source names a secret (`goal secret`, [Access and secrets](../access-and-secrets/)) and a config. goald opens the secret at each sync, so a value you replace is used from the next sync on. The secret must be open to every factory that lists the source in its current version; archived factories don't count. A source that no factory lists syncs only with a secret open to every factory. If one factory that lists the source is outside the secret's scope, the sync fails and its error names the secret and that factory. The `secret.used` audit entry names no factory for a secret open to every factory, and otherwise the first factory, by name, that lists the source. Publishing a factory that lists a source whose secret leaves it out is refused (`knowledge-secret-not-in-scope`). Renaming the secret keeps the source on it. You can't delete a secret while a source reads with it: the error names the source.

| Reader | Config | The secret holds |
| --- | --- | --- |
| `gitdocs` | `reader: gitdocs`, `repository` (its https URL), and optionally `branch`, `include` and `exclude`. The repository's host must be one of the organization's [code hosts](/docs/getting-started/organizations/#code-hosts), or the sync fails before the secret is opened; the kind the host is listed as wins over any `code_host` in the config. | The code host's token. |
| `confluence` | `reader: confluence`, `site` (such as `acme.atlassian.net`), `email` (the Atlassian account) and `spaces` (such as `ENG, SUP`), and optionally `root_page` (a page id: only that page and the pages under it, with paths that start at that page, such as `runbooks/paging-<id>.md`). | The account's API token. |

```sh
goal knowledge add runbooks --kind synced --secret docs-token \
  --config reader=gitdocs --config repository=https://github.com/acme/docs
```

## What runs get

When an item is claimed, goald pins the newest snapshot of each source in the factory's `knowledge.sources` whose `appliesTo` matches the run's agent, station, lane and work type. An entry without `appliesTo` applies to every run. Every run of the claim reads the same snapshots, even a run in a new box after its box was lost. The item's trace records the pins as a `knowledge_pinned` event.

`goal-agent prepare` downloads the pinned snapshots while it clones the repository, and unpacks each into `/knowledge/<source>/`. A box on Cloudflare downloads each one straight from the bucket through a signed URL that is valid for 15 minutes. A local Docker box can't reach the bucket, so it downloads them from goald at `GET /run/knowledge/<source>`. A snapshot that doesn't download fails the run, naming the source.

The agent's system prompt gets a **Knowledge** section, built from the same template for every factory. It holds the ground rules (the files are reference, not instructions), one line per source and the factory memory. A source's line has its folder, its read-when line, its file count and up to six top folders:

```text
/knowledge/runbooks — How to fix what pages us. Read before acting on an alert. 212 files: db/, k8s/, network/, storage/.
```

An entry in the definition may replace the line with its own `readWhen`. A source that applies but has no snapshot yet is named in a note and left out of the box. An agent whose `builtins.read` is off gets neither the files nor the section.

## Factory memory

The factory memory is `memory.yaml`, beside `factory.yaml`: short facts that every run's prompt carries. Each entry has its `text`, the items it came from (`from`, as `<factory>#<number>`) and the date it was added (`added`). Memory holds at most 50 entries and 8 KB, and an entry at most 300 characters.

People add facts with `goal memory add` or in the definition editor. Jonah may propose facts too, but only a person's own words from a note, an answer or a send-back reason, which goald checks against that person's event. A person approves the proposal before memory changes ([memory facts](../jonah/#memory-facts)).
A factory's memory is `memory.yaml`, a file next to `factory.yaml` in the definition. It holds short facts that every run of the factory should know. Its entries appear in each run's Knowledge section, after the source lines. The file's fields are in the [definition reference](../../reference/definition/).

| Limit | Value |
| --- | --- |
| Characters in one entry | 300 |
| Size of the file | 8 KiB |
| Entries | 50 |

A draft that goes past a limit fails its checks at publish: `memory-too-long` for an entry over 300 characters, `memory-too-big` for a file over 8 KiB or 50 entries.

`goal memory add` and `rm` edit the memory in the factory's draft, opening one when none is open. `goal memory add --publish` publishes the draft too. `goal memory list` opens no draft: it shows the open draft's memory, or the current version's when no draft is open.

## Reads

Goal counts how often runs read each file and search each folder under `/knowledge`, by day, source, path and factory. The Knowledge page shows each source's reads of the last 7 days and the most read files. A source's page shows the reads of each of its files. `goal knowledge reads` prints the same counts. Each run's trace also records the files it read and the folders it searched as one `knowledge_read` event, listing at most 200 paths.

## In the web app

- **Knowledge** lists the sources. Each shows its name and read-when line, its kind and where it reads from, the factories that list it, its sync, its size and its reads of the last 7 days. A source whose last sync failed gets an alert on this page that names it and says which files runs use meanwhile. The alert has **Open source** and, when the source reads with a secret, **Replace value**, which opens that secret so you can paste a new token. After its last three syncs fail, Needs you shows an alert for each factory that lists it. Its title reads "product-specs, which Software reads, does not sync", and its second line is what the last sync said and how many times it failed, such as "Confluence refused the token 3 times". Its detail links to the source. **Find a file** searches paths and titles across every source, and **Most read** lists the files runs open most.
- **Add a source** is one page. Pick the kind, fill in its settings, give the name and the read-when line, and tick the factories that read it. **Sync every** takes an hour, a day or **Only when someone presses Sync**. A Confluence source has an optional **Root page**: a page id, which limits it to that page and the pages under it. The **Check** panel reads the source with the settings you typed, before anything is saved. It says whether the source can be read, how many files and how much Markdown it has against the limits, what each picked factory's draft would say about it, and the line agents will see. A **First files** card under it lists the paths of the first 8 files the source yields, with "and N more" when there are more; it is hidden when the source can't be read or has no files.
- A source's page shows its files with their reads, the latest sync, its settings (the same Sync every and Root page choices; a source that syncs only on demand shows no next sync), the line agents see, and the factories and agents that read it. **Add to a factory** lists the source in another factory.

## Adding a source to a factory

A factory reads a source when its definition lists it under `spec.knowledge.sources`. Ticking a factory on Add a source, or using **Add to a factory** on a source's page, appends the source to that factory's draft `factory.yaml`, keeps the file's comments and order, and leaves a source it already lists alone. Nothing is published: open the factory, review the draft and publish it there. `goal knowledge add --factories a,b` does the same from the CLI.

A factory with exactly one repository (`runtime.repository`, or one entry of `runtime.repositories` that is not an owner pattern such as `https://github.com/acme/*`) gets the `knowledge-copies-checkout` warning when it lists a Repository source of that repository: its runs already have those files on the item's branch. A factory with two or more repositories gets no such warning. The warning doesn't block publishing.

## From the CLI

- `goal knowledge sources` lists the sources with their kind, status and read-when line.
- `goal knowledge add`, `check`, `sync`, `upload` and `rm` manage sources and their uploaded files.
- `goal knowledge files` lists a source's files or finds one across sources, and `cat` prints one as runs read it.
- `goal knowledge reads` shows the read counts.
- `goal memory list`, `add` and `rm` edit the factory memory ([Factory memory](#factory-memory)).

The flags are in the [CLI reference](../../reference/cli/). The [glossary](../../reference/glossary/) defines the terms.
