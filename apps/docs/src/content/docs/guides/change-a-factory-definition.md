---
title: Change a factory definition
description: Pull a factory's files into a folder, edit them, validate and publish a new version with the goal CLI.
---

You end with a new published version of a factory, and the work that runs from then on follows it. The folder you edit is a working copy: Goal keeps the definition itself, and `goal factory push` makes your folder the factory's draft.

Use the web app's Definition page to edit the same definition by hand. Use the steps below when you want the files in your editor and in version control.

## Before you start

- A signed-in `goal` CLI that can reach your Goal server ([Self-host with Docker Compose](../self-host-with-docker-compose/) shows how to start one on your machine).
- The factory's name. `goal factory list` prints each factory with its current version and its open draft, if it has one.

## 1. Pull the factory into a folder

```sh
goal factory pull acme-line ./acme-line
```

This writes the current version's files into `./acme-line`, which must be a new or empty folder, and records where they came from in `.goal/pulled.json`. To start from an older version, pass `--version N`. To start from the open draft, pass `--version draft`.

## 2. Edit the files

The folder holds `factory.yaml` at its root, and the files it points to:

- `agents/` for each agent's prompt,
- `skills/` and `rules/` for skills and factory rules,
- `contracts/` for output contracts and `evals/` for judges and eval cases.

What each field means is in the [factory definition reference](/docs/reference/). To add an agent, a skill or a rule, follow [Add an agent, skill or rule](../add-an-agent-skill-or-rule/).

## 3. Validate the folder

Check the schema and every reference inside the folder, without a server:

```sh
goal factory validate --offline ./acme-line
```

`goal validate ./acme-line` runs the same check; without `--offline`, `goal factory validate` also asks the server to check the organization's connections and knowledge sources. Validation reports each problem with the file and the line it is in. Fix them and run the command again until it passes.

Run the server check before you publish:

```sh
goal factory validate ./acme-line
```

## 4. Push the folder as the draft

```sh
goal factory push ./acme-line
```

The folder becomes the factory's open draft. You can review it on the factory's Definition page under Review and publish, which lists the changes from the running version. The draft does not change the running work.

If a draft already exists and it has changes that did not come from your pull, `goal factory push` refuses. Look at the draft first; if you want to replace it, pass `--replace-draft`, which archives the old draft.

If someone published a version after you pulled, `goal factory push` refuses again. Pull the factory again to get the new version, or pass `--force` to push over it.

## 5. Publish

Publish in the same step as the push, or publish the open draft later:

```sh
goal factory push ./acme-line --publish --message "Engineer uses the Go test skill"
goal factory publish acme-line --message "Engineer uses the Go test skill"
```

Publishing runs the validation again, and the new version runs from then on. If the new version drops a station that has open items, name where those items go with `--move S=T`, once for each station:

```sh
goal factory push ./acme-line --publish --move build-old=build
```

## Check the result

```sh
goal factory versions acme-line
```

The published version that runs is marked with `*`. To run an earlier version again, use `goal factory restore acme-line N`; it publishes version N as a new version, so the history keeps every step.

## Related

- [Add an agent, skill or rule](../add-an-agent-skill-or-rule/)
- [Factory definition reference](/docs/reference/)
