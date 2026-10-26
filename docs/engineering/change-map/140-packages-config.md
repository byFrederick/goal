## When you change

`packages/config` (the Biome config or a tsconfig base)

## Also change

The `biome.json` and `tsconfig.json` files that extend it (`apps/web`, `apps/auth`, `apps/worker`, `apps/control-worker`, `packages/api-client`), which keep only their own settings; the Dockerfiles that copy it (`cmd/goald`, `apps/auth`, `apps/worker`) and their `.dockerignore` allowlists; the image inputs in `deploy/cloudflare/scripts/images.mjs`; `tools/checkplan`'s rule for it; `pnpm-lock.yaml` when a dependency on it changes; the `packages/config` section of ADR 0013

## Run

`pnpm install --frozen-lockfile`, `task web:check`, `task auth:check`, `task worker:check`, `task cloudflare:check`, `task api-client:check`, `task layout:check`
