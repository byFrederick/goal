## When you change

A Dockerfile (`cmd/goald`, `cmd/goal-egress`, `auth`, `worker`)

## Also change

Its `Dockerfile.dockerignore`; its inputs in `deploy/cloudflare/scripts/images.mjs`

## Run

`task cloudflare:check`, the image's task (`task goald:image`, and `task goald:coldstart` for goald's sizes and cold start; `task egress:image`, `task auth:image`, `task worker:image`)
