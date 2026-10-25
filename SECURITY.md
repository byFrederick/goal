# Security

## Reporting a vulnerability

Report it privately through the repository's GitHub Security tab ("Report a vulnerability"), not in
a public issue. Include what you found, how to reproduce it and what it lets someone do.

## Threat model

[Spec 3 §11.1](docs/specs/3-identity-and-access.md#111-threat-model) lists every threat to the
install, its control and the test that holds it. Phase 4 (built: connections, runs in their own
containers with public internet, the knowledge base) adds its threat rows and writes down the risks
it accepts in [spec 4 §11](docs/specs/4-connections-runs-and-knowledge.md#11-security-and-tenancy).

On Cloudflare, **the Terraform state is as sensitive as the master key**: it holds every platform
secret in plain text, the master keys and the database administrator's URL included. The state
bucket and its token are the operator's alone
([runbook](docs/engineering/cloudflare-operations.md#workspaces-and-state)).

The Cloudflare install (`goal.byfrederick.work`, since 2026-10-25) keeps a run's box from private,
loopback, link-local and metadata addresses by Cloudflare's container networking, not by Goal's
egress proxy. That was shown there on 2026-10-25 (spec 4 §5.12 check 1, threat row
5) and is shown again before any deploy that changes the boxes' networking (runbook bootstrap
step 9). The install signs in with Google only: it has no mail server, so no sign-in codes.

## Known limits, by design

- **Anyone can lock an address out of sign-in by code for a day** (phase 3 review L3). Ten wrong
  codes for an address in 24 hours lock sign-in by code for that address until the oldest failure
  is a day old, whoever sent them: the bootstrap owner's address too. This is the brute-force
  control of threat row 10 (six digits, five minutes, three tries a code): counting only the
  owner's own failures would need to know who is typing. The person gets one email saying so;
  Google sign-in, an existing session and `goal db recover-admin` / `goal db recover-owner` (they
  need only the database) still work. Keying failures by address and network, or not counting
  them while no code is outstanding, may come later.
- **Over plain http, other local web servers receive the session cookie** (review L5). Browsers do
  not separate cookies by port, so with `GOAL_PUBLIC_URL=http://127.0.0.1:8080` (or `localhost`)
  any other web server you open on that host name, `127.0.0.1:3000` say, is sent
  `goal.session_token`. When you run other local dev servers, give Goal a name of its own under
  `.localhost`, which browsers resolve to this machine themselves (RFC 6761), and open it only
  there: `GOAL_PUBLIC_URL=http://goal.localhost:8080` (goald and the identity service accept http
  on `*.localhost` names as on `localhost`; where a browser or tool does not resolve it, add
  `127.0.0.1 goal.localhost` to `/etc/hosts`). The CLI keeps `GOAL_API_URL=http://127.0.0.1:8080`:
  it sends credentials over http only to a loopback address. Any install other people reach uses
  https, whose cookie is `__Secure-` and `Secure`.

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
