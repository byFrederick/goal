---
title: Change a factory definition
description: Pull a factory's files into a folder, edit them, validate and publish a new version with the goal CLI.
---

You end with a new published version of a factory, and the work that runs from then on follows it. The folder you edit is a working copy: Goal keeps the definition itself, and `goal factory push` makes your folder the factory's draft.

Use the web app's Definition page to edit the same definition by hand. In its Agents section, an agent's prompt has a Reset to default button that restores the prompt file from the running version; the [Add an agent, skill or rule](../add-an-agent-skill-or-rule/) guide describes when it is off. The Environments section edits `environments/<name>.yaml`, which says what an agent's box installs and which secrets and servers it reaches; Add a tool puts an MCP server, a command-line tool or an API into it (see [Access and secrets](../../concepts/access-and-secrets/)). On an agent's page, the Environment select picks which file the agent gets. Each entry in the page's section nav shows a summary of its section, such as the number of stations on the Line or the agents and models in Agents. A count of changes and problems appears beside the section name when there are some. Use the steps below when you want the files in your editor and in version control.

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

`goal validate ./acme-line` runs the same check; without `--offline`, `goal factory validate` also asks the server to check the organization's secrets and knowledge sources: a `{{secret}}` that does not exist or is not open to the factory is reported at its key. Validation reports each problem with the file and the line it is in. Fix them and run the command again until it passes.

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

In the Definition editor, Review and publish shows, beside the changes of the open draft, the checks of the draft, or of the version that runs when no draft is open, and the four newest versions with a link to all of them. Before you publish, the Publish card lists the kept tests of the stations the draft reaches and shows each result as it arrives. A failing test, or one that could not run, warns with its reason and never blocks Publish. Each version in the list shows the watch on its change: the metric, its number before and after, and the verdict, or Watching while the watch is open. Budgets are not part of this page; set them in Factory settings under Budgets and targets.

## Edit the line in the app

You can also change the line in the draft's form: open the factory's Definition and choose Line. The stations are one table, in the order work moves through them. Each row has its number, its name with its id under it, its kind (Queue, Agent, Human or Done), its WIP limit, who works there, and the Order buttons. On a narrow screen the table scrolls sideways inside its own box.

- Change a station's name, kind or WIP limit in its row. A kind change that drops fields, such as an agent's checks, says what it removed and offers Undo.
- Move a station with its up and down buttons, or focus its row and press Alt with the up or down arrow, or drag its number. The More menu inserts a station above or below, duplicates it or deletes it.
- Open Rules under a row to set its checks, rework rule, merge step and note. A row that has a problem opens its rules for you.
- Checks on this line, under the table, lists what passed, what to know and what must be fixed before you can publish.

## Edit knowledge and factory memory in the app

Open the factory's Definition and choose Knowledge. It has three cards.

- **Sources** lists the sources the factory reads, each with who reads it (every agent, or the agents, stations, lanes and work types its `appliesTo` names), the line that tells agents when to open it, and its size. **Add source** lists another of the organization's sources in the draft, and the remove button on a row takes one out of the draft. Use the YAML view to give an entry its own `appliesTo` or `readWhen`.
- **Factory memory** lists the short facts every run of the factory starts with, each with the date it was added and the items it came from. Add a fact in the field under the list, change a fact by editing it and leaving the field, and remove one with its button. A fact is at most 300 characters, and the list is at most 50 facts and 8 KB. The facts are `memory.yaml`, which the YAML view shows too. A version whose only change is `memory.yaml` publishes without evals.
- **What agents see** shows the Knowledge section of the prompt as the agent you pick gets it, built from the draft's sources and facts. It reads what the draft has saved, so it follows your edits a moment after you make them.

Knowledge edits go into the draft like any other; publish from Review and publish.

## Check the result

```sh
goal factory versions acme-line
```

The published version that runs is marked with `*`. To run an earlier version again, use `goal factory restore acme-line N`; it makes version N the one that runs again and keeps every version number.

## Related

- [Add an agent, skill or rule](../add-an-agent-skill-or-rule/)
- [Factory definition reference](/docs/reference/)
