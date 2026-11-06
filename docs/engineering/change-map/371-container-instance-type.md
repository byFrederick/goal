## When you change

A container's `instance_type` in `deploy/cloudflare/wrangler.jsonc` (`Goald`, `Auth`)

## Also change

The same class in the topology fixture `apps/control-worker/test/topology/wrangler.jsonc`; the Container sizes section of `docs/engineering/cloudflare-operations.md` (the size, the measured peaks, the cost and how to go back)

## Run

`node --test deploy/cloudflare/scripts/`, `task cloudflare:check`; after the deploy, watch the containers as Container sizes says
