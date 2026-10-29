---

title: Create a factory
description: How to make a factory that can take work, in the web app's setup or from a folder with the CLI.
sidebar:
  order: 4
---

A factory runs from a published version. You can get one in two ways: the web app's setup creates a factory from a template, or the CLI publishes a folder you wrote.

## Create one in the web app

Setup starts after you create an organization and takes you through eight steps. Step 3 (Factory) makes the factory from a template, and step 5 (Budget) publishes it. The steps, in order:

1. **Organization** names the organization and asks what its factories will do. Goal suggests templates from the answer.
2. **Code** connects GitHub, the code the factory's agents work on.
3. **Factory** starts a factory from a template and lets you name what it works on.
4. **Line** shows the factory's stations.
5. **Budget** sets the spend and the targets. Continuing from this step publishes the factory.
6. **Intake** chooses where work comes from: the triggers the template ships with, off until you turn them on, plus **New item**, the CLI and the API.
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
