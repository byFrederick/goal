# Security

## Reporting a vulnerability

Report it privately through the repository's GitHub Security tab ("Report a vulnerability"), not in
a public issue. Include what you found, how to reproduce it and what it lets someone do.

## Threat model

[Spec 3 §11.1](docs/specs/3-identity-and-access.md#111-threat-model) lists every threat to the
install, its control and the test that holds it.

## Dependencies

- Go modules and npm packages are pinned exactly (`go.sum`, `pnpm-lock.yaml`); pnpm runs no
  install scripts and holds back releases younger than its minimum release age.
- `pnpm audit --prod` runs in `task check` for the identity service (`auth/`) and the web UI
  (`web/`): an advisory fails the check until it is fixed or listed under "Exceptions" below.
- **Patch SLA:** critical advisories within 48 hours, high within 7 days, moderate and low with the
  next release ([auth/README.md](auth/README.md) for the identity service).

### Overrides

Transitive dependencies forced to a fixed release (`pnpm-workspace.yaml`, `overrides`). Each is
dropped once its parent depends on a fixed version itself.

| Package | Forced to | Why | Assessment |
| --- | --- | --- | --- |
| `dompurify` (through `monaco-editor` 0.57.0, which pins 3.4.15) | 3.4.17 | GHSA-cg2g-xh64-v4q7 (critical: `SAFE_FOR_TEMPLATES` bypass in shadow-root content with DOM output), GHSA-mrv2-3pqr-j5jr (high: `IN_PLACE` with node-removing hooks), GHSA-p98j-92pf-mc4p, GHSA-6688-9rhm-gjv2, GHSA-c6gr-qvv4-5v3f (low: `IN_PLACE`, `PARSER_MEDIA_TYPE` xhtml) | Not exploitable as Goal uses it: Monaco's sanitizer (`vs/base/browser/domSanitize.js`) renders hover and diagnostic markdown with DOMPurify and uses none of `SAFE_FOR_TEMPLATES`, `IN_PLACE` or `PARSER_MEDIA_TYPE`; the editor shows the organization's own definitions and Goal's JSON Schemas; the SPA's CSP allows no inline script. Fixed anyway: Monaco imports a copy of DOMPurify it ships, so `web/vite.config.ts` points that import at the `dompurify` package, and the override makes it 3.4.17 (the built `monaco-editor` chunk carries 3.4.17). |

### Exceptions

None: `pnpm audit --prod` reports no known vulnerability (2026-10-24).
