# Public readiness audit

The audit of this repository before it goes public: credentials in the whole git history, employer
or client names (AGENTS.md rule 9) and private infrastructure details. It changes nothing outside
the tree: the owner flips the visibility, rotates and rewrites history (the steps are at the end).

- **Date:** 2026-10-25
- **Scope:** every commit reachable from any ref of a fresh clone: 301 before this audit, **302**
  with the audit's own commit (`git rev-list --all | wc -l` on this branch), plus the working tree.
- **Result:** no credential, private key, Terraform state, plaintext `.env` or decrypted SOPS file
  anywhere in history. One personal detail (the owner's own email address as the author of three
  commits) is the owner's call. Nothing in the tree had to be removed.

## 1. Secret scan of the whole history

Tool: **gitleaks v8.28.0** (`go run github.com/zricethezav/gitleaks/v8@v8.28.0`), its default rules.

```sh
gitleaks git --redact --log-opts=--all .                          # 301 commits before the audit: 15 findings with the default rules alone
gitleaks git --redact --log-opts=--all --config .gitleaks.toml .  # 302 commits with the audit's: no leaks found, exit 0
gitleaks dir --redact --config .gitleaks.toml .                   # the working tree: no leaks found
```

gitleaks reads `.gitleaks.toml` from the scanned folder by itself, so the plain command gives the
same result. Every finding was a value made up for a test or a vendored document's example; each is
in `.gitleaks.toml` by file and exact value (so a real secret added to the same file is still
reported), with a one-line reason.

| # | Rule | Commit | Path:line | Verdict | Remediation |
| --- | --- | --- | --- | --- | --- |
| 1 | hashicorp-tf-password | `3bd7b72b0280` | `deploy/terraform/tests/database_urls_unit_test.tftest.hcl:10` | test fixture: the mocked PlanetScale role's made-up password | none (allowlisted) |
| 2 | generic-api-key | `2a04d5224074` | `internal/engine/connections_test.go:35` | test fixture: a fake leased key | none (allowlisted) |
| 3 | gitlab-pat | `9d1af0be718c` | `internal/adapters/gitlab/gitlab_test.go:20` | test fixture: a `glpat-` prefixed string that names itself a test token | none (allowlisted) |
| 4 | generic-api-key | `3a5b7f636db9` | `internal/adapters/s3store/s3store_integration_test.go:155` | false positive: an object key (`orgs/acme/…`) | none (allowlisted) |
| 5, 6 | generic-api-key | `87b384620f44` | `cmd/goal/up_test.go:119` | test fixture: the same fake secret twice, to test that goal up refuses it | none (allowlisted) |
| 7 | generic-api-key | `9d8f6ff1fbfe` | `cmd/goal/up_test.go:110` | test fixture: the earlier form of 5 and 6 | none (allowlisted) |
| 8 | generic-api-key | `6d0703302c2a` | `internal/api/credential_internal_test.go:25` | test fixture: a fabricated task token (already marked `//nolint:gosec`) | none (allowlisted) |
| 9 | generic-api-key | `991018c3b186` | `internal/authoring/templates_test.go:114` | false positive: a fixture UUID | none (allowlisted) |
| 10 | generic-api-key | `b4a9780d501b` | `internal/adapters/s3store/s3store_integration_test.go:227` | false positive: an object key | none (allowlisted) |
| 11, 12 | generic-api-key | `ea2ba255393a` | `.claude/skills/academic-paper/examples/version_family_reconciliation_example.md:28, 42` | false positive: citation keys in a vendored skill | none (allowlisted) |
| 13, 14 | generic-api-key | `ea2ba255393a` | `.claude/skills/golang-security/references/secrets.md:31, 37` | doc example: a vendored skill's "don't do this" snippet (AWS's documented example key) | none (allowlisted) |
| 15 | generic-api-key | `ea2ba255393a` | `.claude/skills/playwright-best-practices/advanced/authentication-flows.md:54` | doc example: a mocked token in a vendored skill | none (allowlisted) |

**Real credentials found: none.** Nothing needs rotating because of this repository's history.

Checked by hand as well, because a decrypted key or plain `.env` would be critical:

- `git log --all -- '*.env' 'deploy/terraform/secrets/*'`: only `deploy/terraform/secrets/production.sops.yaml`,
  in four commits (`201fab3`, `3bd7b72`, `e2c593f`, `a3e191d`). In every version each value is an
  `ENC[AES256_GCM,…]` or an empty string; the file never held a plain value. No `.env` was ever
  committed (only `.env.example`, with no values).
- `git log --all -- '*.tfstate*' '*.tfvars' '*.pem' '*.key' '*keys.txt' '*.p12' '.dev.vars'`:
  nothing. No `AGE-SECRET-KEY-1…` in any commit; the only `BEGIN … PRIVATE KEY` text is the
  pattern table of a vendored skill.
- No connection string with a real host or password: every `postgres://` in history points at
  `localhost`, a compose service or `*.example.com`, or is a template.

## 2. Employer and client names, personal data, private infrastructure

Searched with `git log --all -p` (every patch of every commit) and `git grep` over the tree, for
email addresses, a list of common employers, banks, consultancies and universities, private IPv4
ranges, internal host suffixes (`.internal`, `.local`, `.lan`, `.corp`) and 32-hex ids. The
vendored skills in `.claude/skills` (copies of public skills.sh packages, pinned in
`skills-lock.json`) were checked for credentials but not held to rule 9: their sample people and
companies are their authors' fiction.

| Item | Where | Verdict | Reason |
| --- | --- | --- | --- |
| Company and product names (GitHub, GitLab, Cloudflare, Google, Anthropic, PlanetScale, Datadog, Confluence, Jira, AWS, OpenAI, …) | throughout | keep | providers Goal integrates with or sample connection names, not employers or clients |
| Employer or client names | — | none found | sample data uses `acme`, `example-labs` and `example.com` |
| The owner's email address (a personal Gmail address) as **author** | commits `a570b92` (2026-10-23), `c19d01d` and `59ddcae` (2026-10-25; `59ddcae` is on `main`) | owner's call | a public repository shows every commit's author email. Every other commit uses the GitHub noreply address. Keep it, or rewrite (below) |
| The owner's name, `F. Cruz` / `f.cruz@example.com` | test fixtures, `apps/web/mock`, specs | keep | the owner's own name on a fictional address |
| PlanetScale organization `freddycruz5227` | `deploy/terraform/locals.tf:29`, `deploy/terraform/tests/platform_unit_test.tftest.hcl`, `docs/engineering/cloudflare-operations.md` | keep | Terraform needs it to address the branch; it is useless without a PlanetScale service token. It does repeat the local part of the email above: renaming it is a PlanetScale setting, not a repository change |
| Cloudflare account id | `deploy/cloudflare/wrangler.jsonc` (`GOAL_S3_ENDPOINT`, `GOAL_EMBED_URL`), since the deploy config was added | keep | not a credential: every API call also needs a token. [Cloudflare's own docs](https://developers.cloudflare.com/sandbox/sdk/api/tunnels/) call account and zone ids "non-secret IDs" for a Wrangler config's `vars` |
| Secrets Store id | `deploy/cloudflare/wrangler.jsonc` (every `store_id`) | keep | an address, not a secret: reading a store needs an account token with Secrets Store access, and Workers read it only through their bindings. The deploy item may still turn both ids into deploy-time values so a fork deploys its own account |
| Public hostname `goal.byfrederick.work`, zone `byfrederick.work`, `goal@byfrederick.work` (an example sender in the runbook) | README, SECURITY.md, the runbook, specs, Terraform | keep | public by design: it is the install's address and in public DNS |
| Google Cloud project id `goal-byfrederick`, state bucket `goal-terraform-state` | `deploy/terraform/locals.tf`, the runbook | keep | names, useless without the owner's Google and R2 credentials |
| age recipient in `.sops.yaml` | `.sops.yaml` | keep | an age **public** key: it can only encrypt to the owner |
| `*.internal` hosts (`gateway.internal`, `sandbox.internal`, `auth.internal`, `host.docker.internal`, …) | the control Worker, compose, tests | keep | names inside Cloudflare's container network and Docker, not the owner's network |
| Private IPs (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`, `172.17.0.1`) | egress policy tests, compose, docs | keep | the ranges the egress policy refuses and Docker's default bridge, as test data |

Nothing was a "remove", so nothing in the tree changed for this section.

## 3. Added for a public repository

- `LICENSE`: the Apache License 2.0, full text as published at
  `https://www.apache.org/licenses/LICENSE-2.0.txt` (sha256 `cfc7749b…3d30`). The license is the
  owner's choice: change it in review if Apache-2.0 is not it.
- `README.md` opens with a one-sentence description (under 350 characters, for GitHub's About
  field) and ends with a License section.
- `SECURITY.md` already existed (private vulnerability reporting); it now says how history is
  scanned.
- `.gitleaks.toml`: the default rules plus the allowlist above.

## 4. The owner's steps, in order

1. **Your email in three commits.** Decide whether your personal address may be public. If not,
   rewrite before going public (it cannot be undone once clones exist):
   `git filter-repo --mailmap <file mapping it to 43914037+byFrederick@users.noreply.github.com>`
   on a fresh mirror clone, force-push every branch, and have open clones re-clone. Then turn on
   GitHub's *Keep my email addresses private* and *Block command line pushes that expose my email*
   so web merges use the noreply address. Nothing needs rotating: no credential was found.
2. `gh repo edit byFrederick/goal --description "Goal is a config-driven platform for AI agent factories: a definition declares stations, agents, skills and WIP limits, and a pull-based flow engine moves the work through agents that each run in their own container, on Docker Compose or on Cloudflare."`
3. `gh repo edit byFrederick/goal --visibility public --accept-visibility-change-consequences`
4. Turn on private vulnerability reporting, which SECURITY.md relies on:
   `gh api -X PUT repos/byFrederick/goal/private-vulnerability-reporting` (or Settings › Code
   security › Private vulnerability reporting), and secret scanning with push protection while there.

To repeat the scan later: `gitleaks git --redact --log-opts=--all .` must report no leaks; a new
false positive goes into `.gitleaks.toml` with its file, exact value and a reason.
