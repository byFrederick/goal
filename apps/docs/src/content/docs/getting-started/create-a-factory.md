---

title: Create a factory
description: How to make a factory that can take work, in the web app's setup or from a folder with the CLI.
sidebar:
  order: 4
---

A factory runs from a published version. You can get one in two ways: the web app's setup creates a factory from a template, or the CLI publishes a folder you wrote.

## Create one in the web app

Setup starts after you create an organization and takes you through eight steps. Step 2 (Factory) makes the factory from a template, and step 5 (Budget) publishes it. The steps, in order:

1. **Organization** names the organization and asks what its factories will do. Goal suggests templates from the answer.
2. **Factory** starts a factory from a template and lets you title it. The **Factory name** field takes the factory's title, prefilled from the template (**New factory** for Start empty). Goal derives the factory's name from it: lowercase words joined by hyphens, with a number added when the organization already has that name. Goal ships three starter templates: **Software delivery** (Spec, Build and a human Review: requests become small pull requests a reviewer merges), **Incident response** (Triage, Investigate, Fix and Review: alerts become investigations, fixes and postmortems) and **Support replies** (Triage, Draft, a human Approve and Send: tickets get drafted answers a person approves). **Start empty** uses the catalog's blank template. When the catalog has none, it makes a factory with one agent station, Work, that runs one agent, `worker` (its prompt in `agents/worker.md`), and a done station, Done. That factory publishes as it is, so you can shape it in the steps that follow. An install offers the starter templates once its operator has pushed them into its catalog with `goal catalog templates push`.
3. **Code** asks how much of your code agents may touch, once the factory exists, so the token is open to that one factory. Choose **No code**, **Read only** or **Read and write**. The template's line decides which choices are open: a line that merges pull requests needs Read and write, a line that reads code can't go without code, and the ⓘ on a disabled choice says why. No code asks for nothing. The other two ask which repositories (a whole organization, or only some, one or more by https address) and a token. Goal saves the token as a secret open to this factory only and tests it on every address, and shows the host's answer for each. Continue stays off until the test passes. If the secret's name is taken, Goal never replaces its value: use the existing secret or pick a new name. When saving needs a fresh sign-in and you cancel it, the step says so and offers **Sign in again**, which signs you in and saves and tests the token again. The repositories go into the factory as `runtime.repositories` entries with the access you chose. There is no app to install: a fine-grained token with Contents (and, to write, Pull requests) is enough. See [Access and secrets](../../concepts/access-and-secrets/).
4. **Line** shows the factory's stations as cards, in the order work moves through them. Rename a station on its card. Press **Add a station here**, between two cards or after the last one, to add an agent station at that place. Use **Move earlier** and **Move later** to reorder a station (Done always stays last) and **Remove** to take one out. The **Model key** card asks for the key every agent runs on, once, for every template and for a factory started empty. Paste a Claude subscription token or an Anthropic API key and press **Save the key**. Goal saves it as a secret named `model-key`, open to this factory only, and sets it as the factory's model key. If the organization already has a secret with that name, Goal never replaces its value: choose **Use the existing secret**, or give the key a new name. **Change the key** replaces the factory's key later. **Continue** stays off while the line has no station that runs an agent, while the draft has a problem that would stop it from publishing, or while the factory has no model key; a sentence beside it says which. A station whose WIP limit isn't set shows **No limit** in its WIP field: type a number or press + to set one. A factory started empty begins with one station, Work, and one agent.
5. **Budget** sets the spend and the targets. The week is the budget: Goal starts no new work while the last 7 days' spend (a rolling window, with no reset at midnight) has reached it. The step shows the day as about the week ÷ 7, the figure Home shows as Spend today. "Most per run" caps one agent run, not a whole item. "When a budget runs out" chooses between finishing running items and stopping everything. Continuing from this step publishes the factory. If you come back to Line or Budget after that, they show the running version; your first change opens a draft, and Continue on Budget publishes a new version only when something changed, otherwise it moves on. When goald refuses the publish, the step stays and lists each problem in a plain sentence, with one link to the step that fixes it: **Factory** for the name and purpose, **Code** for the repositories, **Line** for stations and agents. Budget problems are fixed on Budget itself, so they have no link. A problem Goal can't place is shown as its message.
6. **Intake** chooses where work comes from: the triggers the template ships with, off until you turn them on, plus **New item**, the CLI and the API. Each trigger says in words when it fires ("Weekdays at 07:00 UTC"), with the cron expression behind its ⓘ, and the station it files at ("Creates a ticket at Inbox").
7. **People** invites the people who work in the factory.
8. **First item** submits the template's starter item and opens its item screen. See [The first work item](/docs/getting-started/first-work-item/).

## Create one from a folder

A factory definition is a folder with a `factory.yaml` at its root. The file names the factory with a UUID in `metadata.id`, and points to the files the factory uses: its agents' prompts, rules, skills and contracts, usually in `agents/`, `rules/`, `skills/` and `contracts/` next to it. The file starts with these keys:

```yaml
apiVersion: goal/v1
kind: Factory
metadata:
  id: 7d1c2e4a-9b3f-4e8a-8c21-5f0d6a7b9e10
  name: acme-line
spec:
  # the rest of the definition
```

The full set of keys is in the [factory definition reference](/docs/reference/). Its `starter.item` section, which the setup uses for the first item, is covered in the same reference.

Check the folder before you send it:

```sh
goal factory validate ./acme-line
```

Validation runs through the Goal server and reports each problem. Use `--offline` to check the schema and references without the server.

Then make the folder the factory's draft and publish it in one step:

```sh
goal factory push ./acme-line --publish --message "First version"
```

`goal factory push` creates the factory when its `metadata.id` is new. It refuses to replace an open draft that has changes the folder was not pulled from, and to overwrite a version published after you pulled. The error names the flag to use if you mean it: `--replace-draft` archives the open draft, and `--force` pushes over the newer version. Read the message before you use either.

If you want to publish later, push without `--publish`, then run:

```sh
goal factory publish acme-line
```

## Check that it is ready

```sh
goal factory list
```

The list shows each factory with its current version and any open draft. `goal factory versions <factory>` lists the published versions; the one marked `*` is the one that runs. To write a version back into a folder, use `goal factory pull`.

## Next

[The first work item](/docs/getting-started/first-work-item/) submits work to this factory and follows it.
