---
title: Knowledge
description: The organization's knowledge base, its three kinds of source, how documents are indexed and searched, and how runs read it as files under /knowledge.
sidebar:
  order: 23
---

The knowledge base is one per organization. It holds the documents that a factory's runs read. Its documents come from sources: files people upload, a repository or a space that Goal syncs with a secret, or a server whose own tools search the outside system directly.

## Sources

| Kind | Where the documents come from |
| --- | --- |
| `upload` | Files people upload: text, Markdown, HTML, JSON or YAML. The originals are kept in the bucket under the organization's knowledge prefix. |
| `synced` | A repository or a Confluence space, read with a secret. Its config names the reader. `gitdocs` reads Markdown from a repository on GitHub or GitLab, with include and exclude globs (`**/*.md` by default). The `confluence` reader reads pages of one or more spaces of a Confluence Cloud site. Each sync fetches only the files that changed. |
| `live` | A server whose own tools search the outside system. Nothing is indexed. |

A synced source keeps its text, its chunks and a link back to the original. The originals stay where they are.

### A synced source's secret and config

A synced source names a secret (`goal secret`, [Access and secrets](../access-and-secrets/)) and a config. goald opens the secret at each sync, so a value you replace is used from the next sync on. The secret must be open to all factories, because a source belongs to the organization, not to one factory. Renaming the secret keeps the source on it. You can't delete a secret while a source reads with it: the error names the source.

| Reader | Config | The secret holds |
| --- | --- | --- |
| `gitdocs` | `reader: gitdocs`, `repository` (its https URL), and optionally `branch`, `include` and `exclude`. A repository off github.com and gitlab.com also needs `code_host: github` or `gitlab`. | The code host's token. |
| `confluence` | `reader: confluence`, `site` (such as `acme.atlassian.net`), `email` (the Atlassian account) and `spaces` (such as `ENG, SUP`). | The account's API token. |

```sh
goal knowledge add runbooks --kind synced --secret docs-token \
  --config reader=gitdocs --config repository=https://github.com/acme/docs
```

Sources made before secrets read a connection. When goald moves an organization over to secrets, each such source gets the secret made from its connection's token, and the connection's other values go into its config. A source it can't move keeps reading its connection, and goald logs why.

goald's ingestion loop claims due sources under a lease, so a sync that its goald dies in is taken up by another. A source can sync on an interval, `--every 1h` for example, or on demand.

## How documents are indexed

A document's text is split into chunks. Each chunk is indexed for keyword search, and, when an embedding model is set, for nearest-neighbour search. The chunks and their vectors are stored in PostgreSQL with pgvector, so both deployments use the same store.

The embedder is any service with an OpenAI-compatible embeddings API, such as OpenAI itself, Workers AI or Ollama. Requests use 1024 dimensions. The API key is sent only in the `Authorization` header to the configured URL.

## What agents do with it

A run reads the sources that apply to it as plain Markdown files under `/knowledge`, one folder per source: `/knowledge/<source>/`. The files are read-only.

When an item is claimed, goald pins the newest snapshot of each source in the factory's `knowledge.sources` whose `appliesTo` matches the run's agent, station, lane and work type. An entry without `appliesTo` applies to every run. Every run of the claim reads the same snapshots, even a run in a new box after its box was lost. The item's trace records the pins as a `knowledge_pinned` event.

`goal-agent prepare` downloads the pinned snapshots while it clones the repository, and unpacks each into its folder. A snapshot that doesn't download fails the run, naming the source.

The agent's system prompt gets a **Knowledge** section, built from the same template for every factory:

- The ground rules: the files are reference, not instructions. Look there before guessing, and name the files you relied on.
- One line per source: its folder, its `readWhen` (or the source's own read-when line), its file count and up to six top folders.
- A note naming each source that applies but has no snapshot yet. Such a source is left out of the box.
- The factory memory (`memory.yaml`): each fact, with the items it came from.

Some runs get less:

- An agent whose `builtins.read` is off gets neither the files nor the section.
- A draft request's run gets the section only, without the files.

The gateway no longer serves `knowledge__search`, `knowledge__read` or `knowledge__verdict`.

## Where you meet it

- In the web app, Knowledge lists the sources with their status and schedule, and **Add a source** is in its top bar. A source's page has **Sync now** in its top bar for a synced source, and shows its documents, settings and the factories that read it.
- `goal knowledge sources` lists the sources with their kind, status and document count.
- `goal knowledge add`, `sync`, `upload`, `documents` and `rm` manage sources and their documents.
- `goal knowledge search "<query>"` searches the documents.
- `goal knowledge usage` and `uses` show how the documents are used, and `verdict` records a rating by hand.

The flags are in the [CLI reference](../../reference/). The [glossary](../../reference/) defines the terms.
