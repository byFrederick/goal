---
title: Change a factory definition
description: Pull a factory's files into a folder, edit them, validate and publish a new version with the goal CLI.
---

You end with a new published version of a factory, and the work that runs from then on follows it. The folder you edit is a working copy: Goal keeps the definition itself, and `goal factory push` makes your folder the factory's draft.

Use the web app's Definition page to edit the same definition by hand. In its Agents section, an agent's prompt has a Reset to default button that restores the prompt file from the running version; the [Add an agent, skill or rule](../add-an-agent-skill-or-rule/) guide describes when it is off. Use the steps below when you want the files in your editor and in version control.

## Edit in the YAML view

The Definition page's YAML view shows the draft's files. Pick a file in the file tree, edit it, and use Format to rewrite it in Goal's own layout. The Problems panel under the editor lists each validation problem of the draft with its file and line; select one to jump there. It says "No problems." when the draft is valid. The bar above the editor, "Ask the factory to change it", is not available yet: its input is disabled.

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

## Review in the editor

In the Definition editor, Review and publish shows, beside the changes of the open draft, the checks of the draft, or of the version that runs when no draft is open, and the four newest versions with a link to all of them. Evals before publish is not available yet. Budgets are not part of this page; set them in Factory settings under Budgets and targets.

## Edit the line in the app

You can also change the line in the draft's form: open the factory's Definition and choose Line. The stations are one table, in the order work moves through them. Each row has its number, its name with its id under it, its kind (Queue, Agent, Human or Done), its WIP limit, who works there, and the Order buttons. On a narrow screen the table scrolls sideways inside its own box.

- Change a station's name, kind or WIP limit in its row. A kind change that drops fields, such as an agent's checks, says what it removed and offers Undo.
- Move a station with its up and down buttons, or focus its row and press Alt with the up or down arrow, or drag its number. The More menu inserts a station above or below, duplicates it or deletes it.
- Open Rules under a row to set its checks, rework rule, merge step and note. A row that has a problem opens its rules for you.
- Checks on this line, under the table, lists what passed, what to know and what must be fixed before you can publish.

## Check the result

```sh
goal factory versions acme-line
```

The published version that runs is marked with `*`. To run an earlier version again, use `goal factory restore acme-line N`; it makes version N the one that runs again and keeps every version number.

## Related

- [Add an agent, skill or rule](../add-an-agent-skill-or-rule/)
- [Factory definition reference](/docs/reference/)
