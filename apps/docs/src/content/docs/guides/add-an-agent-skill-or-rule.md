---
title: Add an agent, skill or rule
description: Add an agent, a skill or a factory rule to a factory definition, and publish it so the next runs use it.
---

You end with a new agent, skill or rule that the factory's next runs use. Each one is a file in the definition folder and an entry in `factory.yaml` that names the file. Goal reads only what `factory.yaml` names, so an unnamed file does nothing.

Start from a folder you have pulled and validated. The steps for that are in [Change a factory definition](/docs/guides/change-a-factory-definition/). Only rules must sit in a fixed folder (`rules/`). Keeping prompts in `agents/` and skills in `skills/` is a convention that the examples follow.

## Add an agent

An agent is a prompt file and an entry under `spec.agents`:

```yaml
spec:
  agents:
    - name: reviewer
      prompt: ./agents/reviewer.md
      model: claude-sonnet-5-5
      skills: [acme-conventions]
      tools: { allow: [Read, Grep] }
```

1. Write the prompt in `agents/reviewer.md`. It is the agent's instructions for every run.
2. Add the entry to `spec.agents` in `factory.yaml`. `name` is the name the stations use, and `prompt` must point to a file that exists. By convention it is in `agents/`.
3. Name the agent in a station's `agent` field on the board, or in a route entry for a lane. An agent that no station names runs nowhere.

In the web app, the factory's Agents page lists the agents, and an agent's page shows its prompt, model, skills and tools. The page has four tabs: Overview, Prompt, Tools and access, and Runs, which shows how many tasks the agent finished this week. Its actions sit in the top bar: Try another model, which needs the `definition:edit` permission, Open prompt file and, last, Save changes. Save changes sends the changes the page still holds in your draft, such as a reset. Resets save as you make them, so Save changes is usually off, and it is off for a role without definition editing.

To change a prompt in the editor, open the factory's Definition, choose Agents and edit the prompt there. Reset to default restores the prompt file of the running version into your draft, so a change can be undone to what runs now. It is off when the draft already matches the running version, and when the running version has no such file. It does not restore a built-in template's prompt: Goal keeps no built-in prompt text that the editor can read. The agent's page has the same Reset to default beside its Prompt header, next to Edit prompt. Under Settings, an agent that sets its own model also offers Reset to default with the factory's model, which removes its own model from the draft.

## Add a skill

A skill is a folder with a `SKILL.md` file, listed under `spec.skills`:

```yaml
spec:
  skills:
    - { name: acme-conventions, source: ./skills/acme-conventions }
```

1. Create `skills/acme-conventions/SKILL.md`. It holds the skill's name, a description and its instructions.
2. Add the entry to `spec.skills`. `source` must point to a folder that holds a `SKILL.md`. By convention it is in `skills/`.
3. Give the skill to each agent that should use it with that agent's `skills` list. A skill that no agent lists is not loaded.

To start from a catalog skill, use the skill picker on the agent's page in the factory editor. It copies the catalog skill into `skills/<name>/` and records where it came from in the entry's `origin` field. The entry's `source` stays the local folder. Operators fill the catalog with `goal catalog skills sync`.

A `source` written as `skills.sh:owner/repo@skill` is not shipped to runs. An agent gets such a skill only when the checked-out repository vendors it in `.claude/skills`.

## Add a factory rule

A rule is guidance that applies to the factory's agents on every run it targets, whatever repository the work is in. It is a Markdown file with frontmatter, listed under `spec.rules`:

```yaml
spec:
  rules:
    - { name: secrets, source: ./rules/secrets.md }
```

```markdown
---
description: Never print or commit a secret.
paths: ["**/*.env"]
appliesTo:
  agents: [engineer]
---

Check that a variable is set without printing its value.
```

1. Create `rules/secrets.md`. The body after the frontmatter is the rule's text.
2. Add the entry to `spec.rules`. `source` must be a `.md` file inside `rules/`.
3. Fill in the frontmatter (below).
4. Use `appliesTo` to limit the rule to some agents, stations, lanes or work types. Leave it out to apply the rule to every run.

The frontmatter takes these keys, and Goal refuses any other:

- `description` is required and must be one line.
- `paths` is a list of globs that scope the rule to the files an agent works on. An empty glob is refused.
- `appliesTo` limits the rule by `agents`, `stations`, `lanes` and `workTypes`.

On an item's trace, each run lists the rules it received, so you can check that a new rule reached the run.

## Validate and publish

Validate the folder, then push and publish it:

```sh
goal factory validate --offline ./acme-line
goal factory validate ./acme-line
goal factory push ./acme-line --publish --message "Add the reviewer agent"
```

Validation refuses a reference to a file that does not exist, a name that the definition does not declare, a rule that is not a `.md` file in `rules/`, and a rule file whose frontmatter has no description, a multi-line description, an empty glob in `paths` or an unknown key. The error names the entry in `factory.yaml`, so you can fix it there.

Field-by-field detail for `spec.agents`, `spec.skills`, `spec.rules` and the rule frontmatter is in the [factory definition reference](/docs/reference/definition/). What agents, skills and rules are is in the [concepts](/docs/concepts/).
