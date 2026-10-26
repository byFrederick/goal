---
paths:
  - "deploy/**"
  - "deploy/terraform/**"
  - "apps/control-worker/**"
  - ".github/**"
  - "compose.yaml"
  - "**/Dockerfile"
---

# Infrastructure

Code here describes only the platform's own infrastructure: its bucket, DNS, secrets, routes and tokens, its database branch and the sign-in project. Organizations, connections, knowledge sources and factories never belong here (AGENTS rule 8).

## Who applies

- Run `task tf:check` and `task tf:plan` and never `task tf:apply`. The apply-on-merge workflow is the one path to production, and an agent's apply skips the review.
- Take a Terraform change to production by pull request with its plan. A person merges after reading the plan ([ADR 0010](../../docs/adr/0010-infrastructure-applied-from-main.md)).
- Keep Cloudflare-only code in `apps/control-worker/` (the control Worker), `deploy/cloudflare/` (its wrangler config and deploy scripts) and `internal/adapters/cloudflare`. The same code must run on Compose, so nothing else may depend on Cloudflare ([ADR 0011](../../docs/adr/0011-production-deploys-from-main.md)).
- Follow the runbook, [cloudflare-operations.md](../../docs/engineering/cloudflare-operations.md), for bootstrap, deploys, rotations and restores. Its procedures change in step with the platform, so this file does not repeat them.

## Secrets

- Generate every secret Terraform can, and keep outside values only in the SOPS file. A value committed in the clear is readable by everyone with the repository.
- Keep provider tokens out of both Terraform files and SOPS. They come from the operator's session, not from the repository.

## Workflows and images

- Pin every GitHub Action to a full commit SHA with its version in a comment. A tag can be moved after review.
- Give each workflow the smallest `permissions:` it needs, starting from `contents: read`. A compromised step then holds only what its job needs.
- Build images reproducibly: pin base images and tool versions, and keep a `.dockerignore` beside each Dockerfile. A rebuild must not change the digest when its source did not change.

## Checks

- Keep Terraform formatted and validated, and keep its `terraform test` passing. Mock-provider tests catch wiring mistakes before a plan does.
- Run `task tf:check` after the last edit.

## Skills

Vendored skills for this area: terraform-style-guide, terraform-test, terraform-module-library, multi-stage-dockerfile, docker-compose-patterns, github-actions-templates, github-actions-hardening, ci-cd-and-automation.
