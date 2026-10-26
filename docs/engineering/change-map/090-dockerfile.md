## When you change

A Dockerfile (`cmd/goald`, `cmd/goal-egress`, `auth`, `worker`)

## Also change

Its `Dockerfile.dockerignore`; its inputs in `deploy/cloudflare/scripts/images.mjs`; the app paths list of `.github/workflows/deploy.yml`

## Run

`task cloudflare:check`, the image's task (`task goald:image`, `task egress:image`, `task auth:image`, `task worker:image`)
