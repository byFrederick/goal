---
title: Factory
description: What a factory is, what its definition holds, and where a factory shows up on screen.
sidebar:
  order: 1
---

A factory is one organization's configured production line. It has stations, agents and work types. The engine runs the line: it picks up work items, runs agents on them and moves each item to the next station.

A factory is keyed by its `metadata.id`, a UUID. Its `metadata.name` is a slug shown in the screens and the CLI, and it can change without changing the factory. The id keys the factory's database rows and bucket paths. The API, the CLI and item refs such as `acme-line#12` also take the name, so a rename changes what people type but not where the factory's data lives.

## What a definition holds

The definition is the set of files that describe a factory. Goal keeps each file as a content-addressed blob, and each saved state of the definition is a version.

- `factory.yaml` is the factory's manifest. Its top-level keys are `apiVersion` (always `goal/v1`), `kind` (always `Factory`), `metadata`, `spec` and an optional `starter`, the first item that onboarding submits to a new factory.
- The `spec` holds `runtime`, `agents` and `board`, and also `connections`, `knowledge`, `knowledgeBase`, `secrets`, `skills`, `rules`, `contracts` and `learning`.
- `environments/default.yaml` is required. An agent that names no environment gets it. More environments sit beside it in `environments/`.
- `memory.yaml` holds the short facts the factory's runs should know. It is optional.
- `agents/`, `rules/` and `skills/` hold the files that `spec` refers to by name.

A version is either a draft or published. Drafts can be edited and are never run. The engine runs the line of the factory's current version, which is the latest published one unless someone restored an older version. A run that started on an older version finishes on it, and the next step of its item follows the new line.

For each field of the manifest, see the [definition reference](/docs/reference/definition/). This page does not repeat them.

## Where a factory shows up

- The **Floor** screen shows the line live: its stations, how busy each one is and what needs people.
- The **Board** screen shows the factory's work items in columns, one column per station.
- The **Definition** screen edits the draft and publishes a version.

Each factory also has its own [andon cord](/docs/concepts/andon/), its own weekly [budget](/docs/concepts/wip-and-budget/) and its own [work items](/docs/concepts/work-items/).

## Related

- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the line a factory is made of.
- [Work items and work types](/docs/concepts/work-items/) explains what moves along it.
