@AGENTS.md

## Claude Code specifics

- Claude Code loads a rule from `.claude/rules` when a file you read or edit matches its `paths:` globs. Before editing a file, read it, so its rule is in context.
- Load a skill with the Skill tool by its folder name in `.claude/skills`; the rule for the area names which ones to load.
