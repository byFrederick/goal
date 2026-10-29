---
paths:
  - "docs/**"
  - "apps/docs/**"
  - "**/*.md"
---

# Docs

The product docs site is `apps/docs/` (Astro Starlight, served at `/docs`; its tasks are in `apps/docs/Taskfile.yml`). It documents Goal as it works on `main`.

Docs describe the code as built. How the repository lays out its docs: [ADR 0013](../../docs/adr/0013-repository-layout.md). The plans earlier phases were built from are in `docs/history/`; the living docs are `docs/engineering/` and `docs/reference/`.

## Keeping docs true

- Update the spec, the architecture page and the README that describe a behavior in the same pull request as the behavior. A stale page sends the next agent the wrong way.
- Link the longer doc instead of repeating it. A sentence copied into two places drifts apart, so one place keeps each rule.
- Check that relative links still resolve after a file moves. A move that breaks a link hides the page from readers.
- Add or change a change-map row as its entry file in `docs/engineering/change-map/` (`NNN-<slug>.md`), and an ADR as its own file in `docs/adr/` (`NNNN-<slug>.md`), then run `task docs:gen` and commit the regenerated `docs/engineering/change-map.md` and `docs/adr/README.md`. Never edit those two by hand: the next `task docs:gen` overwrites the edit. Resolve a conflict on either, after merging or rebasing onto the base, by running `task docs:gen`. `task docs:check` fails on a malformed entry or ADR, and on a stale or missing change map or ADR index, naming the file and `task docs:gen`. A reviewer never rejects a pull request because its generated files differ from main's.
- Never edit the docs site's generated reference pages (`apps/docs/src/content/docs/reference/cli.md`, `definition.md`, `environment.md`, `glossary.md`). Change the source (`internal/cli`'s usage text, `contracts/definitions/factory.schema.json`, `docs/reference/environment.md` or `glossary.md`) and run `task docs:ref:gen`, which writes only those pages. `task docs:check` fails, naming the page and the task, when one differs from its source. `task docs:gen` regenerates them too, with the two indexes.

## Decisions

- Write an ADR in `docs/adr/` for any decision others must follow, such as a layout, a dependency's role or a security model. A pull request description disappears; an ADR stays.
- Change an accepted ADR only by a dated amendment. Readers rely on the record of what was decided when.
- Put the design of a change in its work item's spec, and update `docs/engineering/` and `docs/reference/` to describe it as built. `docs/history/` is a record and is not updated.

## Writing

- Write short sentences in plain words, one term per concept. The people reading did not write the docs and need them the first time.
- Commit no process output: no test logs, command transcripts or run captures. The docs keep what a reader needs to act on ([ADR 0013](../../docs/adr/0013-repository-layout.md)).
- Use `acme` and `example.com` in every sample. Employer and client names never appear in the repository.

## Skills

Vendored skills for this area:

- technical-writing for any prose: docs, specs, READMEs, pull request descriptions and UI copy.
- reviewing-technical-prose as the last check before delivering a document.
- documenting-contracts for the API and CLI pages.
- writing-changelogs for the changelog.
- astro for the docs site in `apps/docs`.
- architecture-decision-records and documentation-and-adrs for ADRs.
