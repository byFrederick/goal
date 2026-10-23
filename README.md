# Goal — config-driven AI agent factories

Goal runs AI agent factories on your machine. A factory is one folder: `factory.yaml` defines its stations, agents, skills, connections, WIP limits and knowledge base; Markdown files hold the agent prompts and skills. The engine moves work through the factory as a pull-based Kanban flow, built on the Theory of Constraints (*The Goal*) and the Three Ways (*The Phoenix Project*).

The first factory, `factories/goal`, builds Goal itself.

```
factories/
  goal/           builds this repository: architect → you approve → engineers → checks → reviewer → you merge
  sre/            triage → investigate → review → you approve
  university/     plan → research → you write → critique
schema/           factory.schema.json   (editor autocompletion + validation)
cmd/goal/         CLI (goal validate today; goal up in phase 1)
internal/config/  loader and cross-reference validation
migrations/       PostgreSQL schema (goose, forward-only)
queries/          SQL for sqlc (pgx/v5)
compose.yaml      local PostgreSQL 18
worker/           agent container image (TypeScript, Claude Agent SDK) — phase 1
web/              UI (React) — phase 2
terraform/        all AWS infrastructure: knowledge bucket, KMS, S3 Vectors, one Bedrock KB per factory
design-system/    UI rules generated with ui-ux-pro-max, plus the user's overrides (MASTER.md)
docs/adr/         decisions (0001 stack, 0002 Jonah evals, 0003 hexagonal architecture and deployment)
docs/engineering/ architecture and testing rules
docs/jonah/       evaluation and self-remediation plan
AGENTS.md         rules for every agent and human (CLAUDE.md imports it)
Makefile          make check = everything CI runs
.github/          CI: lint, race tests, migrations on PostgreSQL 18, factory validation, Terraform
.claude/skills/   skills.sh skills used to build Goal itself, pinned in skills-lock.json
```

## Quick start

```bash
make check                 # lint, tests, factory validation, terraform
make migrate-check         # migrations apply from zero on PostgreSQL 18
docker compose up -d postgres
```

Read `AGENTS.md` before changing anything.

## Factories are keyed by UUID

Every factory has an immutable `metadata.id`. Database rows, the knowledge base prefix (`s3://…/<id>/`) and every AWS resource use it, so you can rename a factory or its folder at any time. When you copy a factory, give the copy a new id (`uuidgen | tr A-Z a-z`); `goal validate` rejects duplicates.

## Add a factory

1. Copy a folder under `factories/`, set a new `metadata.id` and `metadata.name`, then edit `factory.yaml`.
2. `go run ./cmd/goal validate`
3. `cd terraform && terraform apply` creates the factory's knowledge base. No HCL changes.

## Database

PostgreSQL 18 is the only datastore: work items, the event log, leases (`FOR UPDATE SKIP LOCKED`), agent runs and realtime (`LISTEN/NOTIFY`). Schema changes are new files in `migrations/`; applied migrations are never edited.

## Infrastructure

```bash
cd terraform
cp backend.hcl.example backend.hcl        # set your state bucket
cp terraform.tfvars.example terraform.tfvars
terraform init -backend-config=backend.hcl
terraform plan
```

Creates: a versioned, KMS-encrypted bucket `goal-kb-<account>-<region>` with one prefix per factory id; an S3 Vectors bucket; per factory a vector index, a Bedrock Knowledge Base and S3 data source scoped to the prefix, an agent role limited to that prefix and KB, and an optional ingestion schedule.

Roles that agents use to investigate other AWS accounts (for example `goal-sre-readonly`) belong in those accounts' own Terraform, not here.

## Claude authentication

Run `claude setup-token` once, put the token in `.env` (copy `.env.example`; it is git-ignored), and Goal injects it as `CLAUDE_CODE_OAUTH_TOKEN` into agent containers. That token can call models only: claude.ai connectors are not available to it, which is why connections are declared per factory and served by Goal's MCP gateway. For personal use on your own subscription only; switch `runtime.auth` to `anthropic-api-key` if anyone else ever uses the platform.
