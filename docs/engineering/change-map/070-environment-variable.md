## When you change

A new environment variable or platform secret

## Also change

Its entry in `contracts/env.yaml` (the row above); `.env.example`; `docs/engineering/cloudflare-operations.md`; for a secret, its Terraform secret and consumers in `deploy/terraform/secrets.tf`; the container environment of the control Worker (`deploy/cloudflare/wrangler.jsonc`, `apps/control-worker/src/env.ts`); reading it through the `Secrets` port (`internal/core/ports/ports.go`), wired in `cmd/<binary>/main.go`

## Run

`task env:check`, `task test`, `task cloudflare:check`, `task tf:check`
