@AGENTS.md

## Claude Code specifics

- Before editing Go, read `docs/engineering/architecture.md`; before editing web/, worker/ or packages/, `docs/engineering/frontend.md`; before writing tests, `docs/engineering/testing.md`.
- Use the skills listed in AGENTS.md for the area you touch; for UI work, run ui-ux-pro-max's pre-delivery checklist.
- Run `task check` before saying a change is done, and report its real output. Run `task hooks:install` once per clone so pushes are gated on it.
