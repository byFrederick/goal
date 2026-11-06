# ADR 0042 — The install's starter templates are committed product content

Status: accepted · 2026-11-06 · Amends [ADR 0005](0005-factory-definitions-in-object-storage.md) and AGENTS.md rule 1 ("no definition committed") for the starter templates only · Builds on [ADR 0032](0032-self-serve-onboarding.md) (onboarding's template step, `starter.item` and starter triggers) and [ADR 0041](0041-code-and-github-without-an-app.md) (a template's code access, decision 9)

## Context

Onboarding's Factory step ([ADR 0032](0032-self-serve-onboarding.md), step 3; [template.html](../design/screens/get-started/template.html)) offers the templates of the bucket's catalog (`catalog/templates/<name>/<version>/`, read by `TemplateCatalog` in `internal/authoring/templates.go`) beside Start empty. Production's catalog is empty, so a new organization sees only Start empty where the design shows Software delivery, Incident response and Support replies.

The operator fills the catalog with `goal catalog templates push <dir>`, which reads `template.yaml` and the definition under `factory/` (`readTemplate` in `internal/cli/catalog.go`). The folders it pushes have to live somewhere. Today they would live on one operator's machine: nobody reviews them, no test checks them against the definition checks, and a second install cannot get the same three.

AGENTS.md rule 1 and `.claude/rules/architecture.md` keep definitions out of the repository, because an organization's factories are data that lives in the bucket and PostgreSQL ([ADR 0005](0005-factory-definitions-in-object-storage.md)). A starter template is not an organization's factory. Goal ships it to every install, as it ships the design's copy: it is product content, and each factory made from it gets its own id and lives in the bucket like any other.

## Decision

1. **The install's starter templates are committed under `deploy/catalog/templates/<name>/<version>/`.** Each folder is what `goal catalog templates push` reads: `template.yaml` beside the definition under `factory/` (`factory.yaml`, an `agents/<name>.md` prompt per agent, `environments/default.yaml`). These folders are the only definitions in the repository besides the test fixtures; no organization's factory is ever committed.
2. **A committed version never changes.** A change to a template is a new version folder (`<name>/2/`), as the catalog itself refuses to overwrite a version (`ErrTemplateExists`). A version folder is never edited or deleted once merged.
3. **A starter template carries no organization's data.** Its `metadata.id` is the nil UUID, which Push requires. It names no connection, no repository and no `{{secret}}` or `credential:` reference; onboarding adds repositories at the Code step ([ADR 0041](0041-code-and-github-without-an-app.md)). Its triggers are written `enabled: false` ([ADR 0032](0032-self-serve-onboarding.md), decision 5) and need no webhook or connection. Its `template.yaml` says `code: none` or `code: read`; write follows only from a line that merges (ADR 0041, decision 9).
4. **A test pushes every committed version.** `TestStarterTemplates_PushCleanly` in `internal/authoring` pushes each folder into an in-memory catalog and fails on any problem the definition checks report, warnings included.
5. **The operator pushes them into the bucket's catalog.** The repository holds the templates; an install has them only after the operator pushes each version with `goal catalog templates push`. Goal never pushes them on its own at start.

## Consequences

- Every install can offer the same reviewed templates, and a broken template fails CI instead of onboarding.
- AGENTS.md rule 1 and the architecture rule name this exception; the rule numbers stay the same.
- Changing a template's wording or line costs a new version folder and a push, so small fixes are not free.
- An install whose operator has not pushed a version still shows the catalog it has. Pushing production's catalog is a separate task.
