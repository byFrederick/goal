# ADR 0004 — Frontend architecture and monorepo

Status: accepted · 2026-10-23

## Decision

1. **One repository**: a Go module at the root plus a pnpm workspace (`web`, `worker`, `packages/*`) orchestrated by Turborepo. `task check` runs Go and TypeScript checks together.
2. **Contract first**: `api/openapi.yaml` is the single API contract. Go server interfaces (oapi-codegen) and TypeScript types and client (openapi-typescript, openapi-fetch) are generated from it; CI fails on drift.
3. **Static React SPA**: React 19, Vite, TypeScript strict, TanStack Router and Query, Tailwind v4 with tokens generated from `design-system/goal/MASTER.md`, shadcn/ui on Radix in `packages/ui`.
4. **Realtime by SSE** from goald, mapped to query invalidation.
5. **Feature folders with enforced boundaries** (dependency-cruiser), mirroring the Go side's depguard rules.
6. **One responsive app** for desktop and mobile; bottom tab bar under 768 px; PWA later.
7. **Hosting**: embedded in goald on a laptop; goald or S3 + CloudFront on AWS; static on Vercel with the API on AWS.

## Rejected

- **Next.js.** SSR and a Node server add nothing to an authenticated tool that needs no SEO, and would be a second backend beside goald. A static build deploys to Vercel, S3 or goald unchanged.
- **Separate repositories for UI and backend.** The contract would drift; one PR should be able to change API and UI together.
- **A global client store (Redux, Zustand) by default.** Server state belongs to TanStack Query and view state to the URL.
- **A chart library.** The design system needs a few precise marks (hatched bars, bridge, sparkline); small SVG components are lighter and exact.
- **Nx.** Turborepo is enough for two apps and four packages, with less configuration.

## Consequences

- The UI cannot call an endpoint that is not in the contract; adding one starts with `api/openapi.yaml`.
- The worker shares contract types with goald and the UI.
- Phase 2 scaffolds `web/` and `packages/` as goal-factory work items following this ADR.
