---
name: goal-adr
description: Record an architecture decision in docs/adr, or amend an existing one. Use when a change sets a rule others must follow, adds a port, a top-level directory, a database, a deployment target or a dependency on a new kind of service, or reverses an earlier ADR.
---

# Write an ADR

AGENTS.md says it: decisions that others must follow get an ADR in `docs/adr/`. A change that only follows an existing ADR needs none. A new top-level directory needs an amendment to [ADR 0013](../../../docs/adr/0013-repository-layout.md) (its decision 2).

1. **Check whether an ADR is needed.** Read the ADRs in `docs/adr/` that touch the area. If the change reverses or extends one of them, amend it (step 6) instead of writing a new one.
2. **Take the next number.** List `docs/adr/`. The new file is the highest number plus one, four digits, named `NNNN-short-title.md` (lowercase, hyphens). Numbers are never reused.
3. **Write the header.** Follow the shape of the latest ADRs, such as [0012](../../../docs/adr/0012-assemblies-and-parts.md) and [0013](../../../docs/adr/0013-repository-layout.md):
   ```markdown
   # ADR NNNN — Title: what was decided, in a few words

   Status: accepted · YYYY-MM-DD · Builds on [ADR MMMM](MMMM-title.md) (why it matters here)
   ```
   Leave out the `Builds on` part when the ADR builds on none. Use `Supersedes` or `Amends` when the ADR replaces or changes earlier decisions.
4. **Write the three sections.**
   - `## Context`: the problem and the forces behind it, as things stand today, with paths and numbers.
   - `## Decision`: numbered decisions, each opening with a bold one-line summary. Each must be specific enough that a reviewer can tell whether a change follows it.
   - `## Consequences`: what gets easier, what gets harder, what must change and who does it.
   Add a `## History` section only when the decision moves or renames things whose old names someone will look up.
5. **Link it.** Name the ADR in the docs, rules or AGENTS.md lines it changes. Do not copy its decisions into them.
6. **Amend instead of rewriting.** Leave an accepted ADR's decisions as they are. Add a section `## Amendment — <when>: <what>` at the end with its own `Status:` line naming the decisions it changes. Then extend the ADR's top `Status:` line with `Amended …` and a link to that section. An ADR that changes another one says so in both files.
7. **Check the links.** Every relative link in the new ADR must point to a file that exists. Use `ls docs/adr` and `ls` on the docs you linked.
8. **Deliver** with [goal-deliver](../goal-deliver/SKILL.md). The ADR goes in the same pull request as the change it decides when that change is small, or in a pull request of its own when the work spans several.
