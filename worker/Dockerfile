# syntax=docker/dockerfile:1
# Goal agent worker image. Build from the repository root (task worker:image):
#   docker build -f worker/Dockerfile -t ghcr.io/byfrederick/goal-agent:0.1.0 .
# Contract with goald's Docker runner (internal/adapters/docker): runs as uid 1000 with HOME
# /home/node, reads /goal/spec.json and /goal/agent, works in /workspace, writes JSON lines to
# stdout. worker/README.md has the details.

ARG NODE_IMAGE=node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d

FROM ${NODE_IMAGE} AS build
ARG PNPM_VERSION=12.15.0
RUN npm install --global --no-fund --no-audit "pnpm@${PNPM_VERSION}"
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY worker/package.json worker/
RUN pnpm install --frozen-lockfile --filter @goal/worker
COPY worker/ worker/
RUN pnpm --filter @goal/worker build \
 && pnpm --filter @goal/worker deploy --prod /out

FROM ${NODE_IMAGE} AS runtime
# git for the agent's local commits (it never gets credentials), ripgrep for search.
RUN apt-get update \
 && apt-get install --yes --no-install-recommends ca-certificates git ripgrep \
 && rm -rf /var/lib/apt/lists/*
ENV NODE_ENV=production \
    HOME=/home/node
COPY --from=build /out /opt/goal-worker
USER 1000:1000
WORKDIR /workspace
ENTRYPOINT ["node", "/opt/goal-worker/dist/main.js"]
