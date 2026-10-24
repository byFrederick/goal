# syntax=docker/dockerfile:1
# Goal's auth service image, the sidecar container in goald's ECS task (spec 3 §5.5). Build from
# the repository root (task auth:image); never pushed from here:
#   docker build -f auth/Dockerfile -t ghcr.io/byfrederick/goal-auth:0.1.0 .
# It listens on loopback only (GOAL_AUTH_LISTEN, default 127.0.0.1:8081), which goald's container
# shares in the task; goald proxies /api/auth to it. Its settings and secrets come from the task's
# environment (Secrets Manager in the cloud), never from the image.

ARG NODE_IMAGE=node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d

FROM ${NODE_IMAGE} AS build
ARG PNPM_VERSION=12.15.0
RUN npm install --global --no-fund --no-audit "pnpm@${PNPM_VERSION}"
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY auth/package.json auth/
RUN pnpm install --frozen-lockfile --filter @goal/auth
COPY auth/ auth/
RUN pnpm --filter @goal/auth build \
 && pnpm --filter @goal/auth deploy --prod /out

FROM ${NODE_IMAGE} AS runtime
ENV NODE_ENV=production
COPY --from=build /out /opt/goal-auth
USER 1000:1000
WORKDIR /opt/goal-auth
# The same check ECS runs: Better Auth's GET /api/auth/ok on the loopback listener.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["node", "--eval", "fetch('http://'+(process.env.GOAL_AUTH_LISTEN||'127.0.0.1:8081')+'/api/auth/ok').then(r=>process.exit(r.ok?0:1),()=>process.exit(1))"]
ENTRYPOINT ["node", "/opt/goal-auth/dist/server.js"]
