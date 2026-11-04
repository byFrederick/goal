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
| Repository | The Markdown and text files under the folders you name in a GitHub or GitLab repository, on one branch. The URL's host picks the code host. Folders and leave-outs are globs, such as `docs/runbooks/**` and `**/drafts/**`. A public repository needs no secret. |
| Confluence | The pages of one or more spaces of a Confluence Cloud site, at the path of their page tree. It reads with the site, the account's email and a secret that holds the API token. |

Website, Notion and Google Drive show on the Add a source page as later. A Repository or Confluence source reads with a secret that is open to all factories, because a source belongs to the organization, not to one factory ([Access and secrets](../access-and-secrets/)). You can't delete a secret while a source reads with it: the error names the source.

## Sync, snapshots and limits

A sync reads the source, converts what is not Markdown, and writes a snapshot, one zip of its files, only when something changed. A source syncs every hour or every day, or when someone presses **Sync now**. An upload source changes when its files do. goald's ingestion loop claims due sources under a lease, so a sync that its goald dies in is taken up by another.

- A file that fails to fetch or convert keeps its last good version, or stays out, and the snapshot counts it as failed. It doesn't fail the sync.
- A file that looks like it holds a key stays out.
- goald keeps the current snapshot, any snapshot a running run pinned, and a replaced snapshot for at least the longest a claim can last (about 4 hours by default). After that it deletes the snapshot at the source's next sync. Deleting a source deletes its snapshots.

Limits keep every box fast. A sync that goes past one fails and names it.

| Limit | Value |
| --- | --- |
| Files in a source | 20,000 |
| Markdown in a source | 50 MB |
| One file | 5 MB |
| Sources a factory lists | 10 |

## What runs get

When an item is claimed, goald pins the newest snapshot of each source in the factory's `knowledge.sources` whose `appliesTo` matches the run's agent, station, lane and work type. An entry without `appliesTo` applies to every run. Every run of the claim reads the same snapshots, even a run in a new box after its box was lost. The item's trace records the pins as a `knowledge_pinned` event.

`goal-agent prepare` downloads the pinned snapshots while it clones the repository, and unpacks each into `/knowledge/<source>/`. A box on Cloudflare downloads each one straight from the bucket through a signed URL that is valid for 15 minutes. A local Docker box can't reach the bucket, so it downloads them from goald at `GET /run/knowledge/<source>`. A snapshot that doesn't download fails the run, naming the source.

The agent's system prompt gets a **Knowledge** section, built from the same template for every factory. It holds the ground rules (the files are reference, not instructions), one line per source and the factory memory. A source's line has its folder, its read-when line, its file count and up to six top folders:

```text
/knowledge/runbooks — How to fix what pages us. Read before acting on an alert. 212 files: db/, k8s/, network/, storage/.
```

An entry in the definition may replace the line with its own `readWhen`. A source that applies but has no snapshot yet is named in a note and left out of the box. An agent whose `builtins.read` is off gets neither the files nor the section.

## Factory memory

A factory's memory is `memory.yaml`, a file next to `factory.yaml` in the definition. It holds short facts that every run of the factory should know. Its entries appear in each run's Knowledge section, after the source lines. The file's fields are in the [definition reference](../../reference/definition/).

| Limit | Value |
| --- | --- |
| Characters in one entry | 300 |
| Size of the file | 8 KiB |
| Entries | 50 |

A draft that goes past a limit fails its checks at publish: `memory-too-long` for an entry over 300 characters, `memory-too-big` for a file over 8 KiB or 50 entries. A version whose only change is `memory.yaml` publishes without the eval gate.

`goal memory list`, `add` and `rm` edit the memory in the factory's draft. `goal memory add --publish` publishes the draft too.

## Reads

Goal counts how often runs read each file and search each folder under `/knowledge`, by day, source, path and factory. The Knowledge page shows each source's reads of the last 7 days and the most read files. A source's page shows the reads of each of its files. `goal knowledge reads` prints the same counts. Each run's trace also records the files it read and the folders it searched as one `knowledge_read` event, listing at most 200 paths.

## In the web app

- **Knowledge** lists the sources. Each shows its name and read-when line, its kind and where it reads from, the factories that list it, its sync, its size and its reads of the last 7 days. A source whose last sync failed gets an alert on this page that names it and says which files runs use meanwhile. After its last three syncs fail, Needs you shows an alert under each factory that lists it, with what the last sync said. **Find a file** searches paths and titles across every source, and **Most read** lists the files runs open most.
- **Add a source** is one page. Pick the kind, fill in its settings, give the name and the read-when line, and tick the factories that read it. The **Check** panel reads the source with the settings you typed, before anything is saved. It says whether the source can be read, how many files and how much Markdown it has against the limits, what each picked factory's draft would say about it, and the line agents will see.
- A source's page shows its files with their reads, the latest sync, its settings, the line agents see, and the factories and agents that read it. **Add to a factory** lists the source in another factory.

## Adding a source to a factory

A factory reads a source when its definition lists it under `spec.knowledge.sources`. Ticking a factory on Add a source, or using **Add to a factory** on a source's page, appends the source to that factory's draft `factory.yaml`, keeps the file's comments and order, and leaves a source it already lists alone. Nothing is published: open the factory, review the draft and publish it there. `goal knowledge add --factories a,b` does the same from the CLI.

## From the CLI

- `goal knowledge sources` lists the sources with their kind, status and read-when line.
- `goal knowledge add`, `check`, `sync`, `upload` and `rm` manage sources and their uploaded files.
- `goal knowledge files` lists a source's files or finds one across sources, and `cat` prints one as runs read it.
- `goal knowledge reads` shows the read counts.
- `goal memory list`, `add` and `rm` edit the factory memory ([Factory memory](#factory-memory)).

The flags are in the [CLI reference](../../reference/cli/). The [glossary](../../reference/glossary/) defines the terms.
