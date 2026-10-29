---
title: Add an agent, skill or rule
description: Add an agent, a skill or a factory rule to a factory definition, and publish it so the next runs use it.
---

You end with a new agent, skill or rule that the factory's next runs use. Each one is a file in the definition folder and an entry in `factory.yaml` that names the file. Goal reads only what `factory.yaml` names, so an unnamed file does nothing.

Start from a folder you have pulled and validated. The steps for that are in [Change a factory definition](../change-a-factory-definition/).

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
2. Add the entry to `spec.agents` in `factory.yaml`. `name` is the name the stations use, and `prompt` must point into `agents/`.
3. Name the agent in a station's `agent` field on the board, or in a route entry for a lane. An agent that no station names runs nowhere.

In the web app, the factory's Agents page lists the agents, and an agent's page shows its prompt, model, skills and tools.

## Add a skill

A skill is a folder with a `SKILL.md` file, listed under `spec.skills`:

```yaml
spec:
  skills:
    - { name: acme-conventions, source: ./skills/acme-conventions }
```

1. Create `skills/acme-conventions/SKILL.md`. It holds the skill's name, a description and its instructions.
2. Add the entry to `spec.skills`. `source` must point to a folder that exists inside `skills/`.
3. Give the skill to each agent that should use it with that agent's `skills` list. A skill that no agent lists is not loaded.

A skill can also come from the skills.sh catalog: its `source` is written as `skills.sh:owner/repo@skill`, and the definition then names the skill the same way.

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
appliesTo:
  agents: [engineer]
---

Check that a variable is set without printing its value.
```

1. Create `rules/secrets.md`. The body after the frontmatter is the rule's text.
2. Add the entry to `spec.rules`. `source` must point to a file inside `rules/`.
3. Use `appliesTo` to limit the rule to some agents, stations, lanes or work types. Leave it out to apply the rule to every run.

On an item's trace, each run lists the rules it received, so you can check that a new rule reached the run.

## Validate and publish

Validate the folder, then push and publish it:

```sh
goal factory validate --offline ./acme-line
goal factory validate ./acme-line
goal factory push ./acme-line --publish --message "Add the reviewer agent"
```

Validation refuses a reference to a file that does not exist, a name that the definition does not declare, and a rule or skill outside its folder. The error names the entry in `factory.yaml`, so you can fix it there.

Field-by-field detail for `spec.agents`, `spec.skills`, `spec.rules` and the rule frontmatter is in the [factory definition reference](/docs/reference/). What agents, skills and rules are is in the [concepts](/docs/concepts/).
