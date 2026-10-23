# ADR 0001 — Technology stack

Status: accepted · 2026-10-23

## Decision

| Layer | Choice | Why |
| --- | --- | --- |
| Control plane (`goald`) and CLI (`goal`) | **Go** | One static binary; goroutines handle hundreds of concurrent agent streams; Docker Engine, AWS SDK v2 and the official MCP Go SDK (1Password SDK in the last phase) are all native; ~30 MB RSS next to many containers on a laptop. |
| Agent worker (inside each container) | **TypeScript on Node 22, Claude Agent SDK** | The SDK is first-class in TS: in-process PreToolUse hooks, MCP config, skills, subagents, `maxBudgetUsd`. Same language as the UI. |
| UI | **React 19 + Vite + TypeScript**, TanStack Router/Query, Tailwind v4 + shadcn/ui, dnd-kit, xyflow, Monaco (YAML + schema) | Built static and embedded in the Go binary with `go:embed`: `goal up` serves everything. |
| Contracts | **JSON Schema 2020-12** (`schema/`) | One source for Go validation, generated TS types, and editor autocompletion via yaml-language-server. |
| State | **PostgreSQL 18 only** (`compose.yaml` locally), `pgx/v5`, `sqlc`, `goose` forward-only migrations | One datastore for everything: event log, projections, the work queue (`FOR UPDATE SKIP LOCKED` leases) and realtime (`LISTEN/NOTIFY`). `uuidv7()` keys. No SQLite, Redis or other stores. |
| Realtime | Postgres `LISTEN/NOTIFY` → Server-Sent Events from `goald` | One-way stream of board and trace events is all the UI needs. |
| Containers | Docker Engine API (Docker Desktop, OrbStack or Colima) | Local-first; an ECS runner can implement the same interface later. |
| Tools for agents | **Goal MCP gateway** (Go, inside `goald`) | Credentials stay on the host; each agent gets one MCP URL that exposes only its allowed connections. |
| Secrets | **Environment variables** (`env:NAME`, git-ignored `.env`) now; AWS Secrets Manager in the cloud; **1Password** service accounts in the last phase | References resolved on the host, passed to the gateway, never into agent context. The `Secrets` port makes the switch an adapter change. |
| Knowledge | **S3 + Bedrock Knowledge Bases + S3 Vectors** | Managed RAG with per-factory isolation and near-zero idle cost. |
| Telemetry | Claude Code OpenTelemetry → `goald` OTLP receiver; optional export to Langfuse/Grafana | Cost, tokens and tool events per item without extra agents. |
| Infrastructure | **Terraform ≥ 1.10, AWS provider 6.x**, S3 backend with native lockfile | `terraform/` reads `factories/*/factory.yaml` directly. |
| Tooling | mise (versions), Taskfile, golangci-lint, Biome | |

## Languages

Three: **Go** (engine, gateway, CLI), **TypeScript** (worker, UI), **HCL** (infrastructure). Skills may carry Python scripts; they run inside agent containers only.

## Rejected

- **Rust for the engine.** Faster per request, but the constraint of this system is LLM latency and quota, not CPU. Applying the Theory of Constraints to our own stack: optimizing a non-bottleneck is waste. Go keeps iteration fast.
- **All-TypeScript (Node control plane).** Viable, but a long-running supervisor of many child processes and streams is where Go is strongest, and a single binary beats a Node runtime for install.
- **Python control plane.** Good AI ecosystem, weaker for a concurrent daemon and single-file distribution.
- **Temporal / Restate for durability.** Excellent, but a heavy local dependency. A lease-based queue plus idempotent, checkpointed steps covers a single-user factory. Revisit for multi-user cloud mode.
- **OpenSearch Serverless for vectors.** Always-on minimum cost is far above a personal budget; S3 Vectors is pay-per-use.
- **SQLite, Redis, NATS or any second datastore.** Rejected by decision: PostgreSQL is the only database. Its `SKIP LOCKED` queue and `LISTEN/NOTIFY` cover queueing and realtime at this scale.
