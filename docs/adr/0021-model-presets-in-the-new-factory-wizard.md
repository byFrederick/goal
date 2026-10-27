# ADR 0021 — Model presets in the new-factory wizard

Status: accepted · 2026-10-27 · Builds on [ADR 0016](0016-ui-revamp.md) (the screens are the contract) and [ADR 0005](0005-factory-definitions-in-object-storage.md) (the wizard edits the factory's draft)

## Context

The new-factory wizard's line step ([`docs/design/screens/get-started/line.html`](../design/screens/get-started/line.html), line 159) draws a Models segmented control with two choices, Quality first and Cost first. Nothing behind it exists.

A definition already says which model each run uses: `spec.runtime.model` is the factory's default, and an agent's own `model` (`spec.agents[].model`) wins over it (`internal/core/domain/line.go`, `Runtime.Model` and `AgentSpec.Model`; `contracts/definitions/factory.schema.json`). The wizard writes the factory's draft like the editor does.

## Decision

1. **The preset is a draft edit the wizard makes, not a definition field.** Choosing a preset changes `factory.yaml` in the draft once. No key records which preset was chosen, the schema gains nothing, and the engine knows nothing of presets. A person may change any model afterwards, in the wizard or the editor.
2. **Quality first** sets `spec.runtime.model` to `claude-opus-5-5` and keeps each agent's `model` override as it is.
3. **Cost first** sets `spec.runtime.model` to `claude-sonnet-5-5` and removes each agent's `model` override, so every agent runs on the default.
4. **The two ids live in one module.** `apps/web/src/features/factory-editor/start/model-presets.ts` holds both model ids and is the only place they are written. Changing either id later is an amendment to this ADR.

## Consequences

- The control is a pure UI feature: no API, schema or engine change, and no migration.
- The control shows which preset matches the draft only by comparing the draft with decisions 2 and 3; a draft edited by hand may match neither, and then no choice is pressed.
- Cost first throws away agent overrides a person set; the wizard is the place a factory starts, so there are few to lose, and switching back to Quality first does not bring them back.
- A new model generation needs an amendment that changes the two ids and the module together.
