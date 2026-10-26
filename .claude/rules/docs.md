---
paths:
  - "docs/**"
  - "**/*.md"
---

# Docs

Docs describe the code as built. How the repository lays out its docs: [ADR 0013](../../docs/adr/0013-repository-layout.md). How a phase spec is written: [docs/specs/README.md](../../docs/specs/README.md).

## Keeping docs true

- Update the spec, the architecture page and the README that describe a behavior in the same pull request as the behavior. A stale page sends the next agent the wrong way.
- Link the longer doc instead of repeating it. A sentence copied into two places drifts apart, so one place keeps each rule.
- Check that relative links still resolve after a file moves. A move that breaks a link hides the page from readers.

## Decisions

- Write an ADR in `docs/adr/` for any decision others must follow, such as a layout, a dependency's role or a security model. A pull request description disappears; an ADR stays.
- Change an accepted ADR only by a dated amendment. Readers rely on the record of what was decided when.
- Write a phase spec in `docs/specs/` before the code, and follow the README there. Reviewers check the design before it is built.

## Writing

- Write short sentences in plain words, one term per concept. The people reading did not write the docs and need them the first time.
- Commit no process output: no test logs, command transcripts or run captures. The docs keep what a reader needs to act on ([ADR 0013](../../docs/adr/0013-repository-layout.md)).
- Use `acme` and `example.com` in every sample. Employer and client names never appear in the repository.

## Skills

Vendored skills for this area: architecture-decision-records.
