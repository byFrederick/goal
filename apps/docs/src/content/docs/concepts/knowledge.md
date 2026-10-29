---
title: Knowledge
description: The organization's knowledge base, its three kinds of source, how documents are indexed and searched, and the tools agents use.
sidebar:
  order: 23
---

The knowledge base is one per organization. It holds the documents that agents may search, read and rate through the gateway. Its documents come from sources: files people upload, a repository or a space that a connection syncs, or a connection whose own tools search the outside system directly.

## Sources

| Kind | Where the documents come from |
| --- | --- |
| `upload` | Files people upload: text, Markdown, HTML, JSON or YAML. The originals are kept in the bucket under the organization's knowledge prefix. |
| `synced` | A connection whose kind can sync. `gitdocs` reads Markdown from a repository on GitHub or GitLab, with include and exclude globs (`**/*.md` by default). The `confluence` reader reads pages of one or more spaces of a Confluence Cloud site. Each sync fetches only the files that changed. |
| `live` | A connection whose own tools search the outside system. Nothing is indexed. |

A synced source keeps its text, its chunks and a link back to the original. The originals stay where they are.

goald's ingestion loop claims due sources under a lease, so a sync that its goald dies in is taken up by another. A source can sync on an interval, `--every 1h` for example, or on demand.

## How documents are indexed

A document's text is split into chunks. Each chunk is indexed for keyword search, and, when an embedding model is set, for nearest-neighbour search. The chunks and their vectors are stored in PostgreSQL with pgvector, so both deployments use the same store.

The embedder is any service with an OpenAI-compatible embeddings API, such as OpenAI itself, Workers AI or Ollama. Requests use 1024 dimensions. The API key is sent only in the `Authorization` header to the configured URL.

## What agents do with it

Each run can reach the knowledge base through three of Goal's own tools:

- `knowledge__search` returns the best passages of the run's sources, or of the source it names.
- `knowledge__read` returns a whole document by the id that a search gave, up to 200 KiB of its text.
- `knowledge__verdict` records whether a document `helped` or `misled` the run, with an optional note.

The gateway records one knowledge use per run, document and kind (search or read), so `goal knowledge uses <document>` lists the runs that used a document, newest first.

## Where you meet it

- `goal knowledge sources` lists the sources with their kind, status and document count.
- `goal knowledge add`, `sync`, `upload`, `documents` and `rm` manage sources and their documents.
- `goal knowledge search "<query>"` searches as agents do, so a person can check what an agent would find.
- `goal knowledge usage` and `uses` show how the documents are used, and `verdict` records a rating by hand.

The flags are in the [CLI reference](../../reference/). The [glossary](../../reference/) defines the terms.
