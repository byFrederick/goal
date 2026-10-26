# syntax=docker/dockerfile:1
# goald with the web UI embedded (spec 4 §5.12). Build from the repository root (task goald:image,
# or compose.yaml's goald service):
#   docker build -f cmd/goald/Dockerfile --target runtime -t ghcr.io/byfrederick/goald:0.1.0 .
# The production deploy builds and pushes it reproducibly (deploy/cloudflare/scripts/deploy.sh):
# keep the slow steps (pnpm and go mod download from the manifests) before the source COPYs.
# Its settings and secrets come from the container's environment, never from the image.

ARG NODE_IMAGE=node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d
ARG GO_IMAGE=golang:1.26.10-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba
ARG RUNTIME_IMAGE=alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f

# The web UI (web/dist), from the workspace manifests first so dependencies cache.
FROM ${NODE_IMAGE} AS web
ARG PNPM_VERSION=12.15.0
RUN npm install --global --no-fund --no-audit "pnpm@${PNPM_VERSION}"
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json web/
COPY packages/api-client/package.json packages/api-client/
RUN pnpm install --frozen-lockfile --filter "@goal/web..."
COPY contracts/definitions/ contracts/definitions/
COPY packages/api-client/ packages/api-client/
COPY web/ web/
RUN pnpm --filter @goal/web run build

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/goald/ cmd/goald/
COPY internal/ internal/
COPY db/migrations/ db/migrations/
COPY contracts/definitions/ contracts/definitions/
COPY web/ web/
COPY --from=web /src/web/dist/ web/dist/
RUN CGO_ENABLED=0 go build -trimpath -tags webui -ldflags="-s -w" -o /out/goald ./cmd/goald

# Alpine for wget (the health check) and the CA bundle, which Go's system pool reads: a managed
# PostgreSQL's verify-full (sslrootcert=system), R2 and every connection over TLS need it (spec 4
# §5.12). Runs clone in their own box, so goald needs no git.
FROM ${RUNTIME_IMAGE} AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/goald /usr/local/bin/goald
USER 65532:65532
EXPOSE 8080 8090
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/v1/health"]
ENTRYPOINT ["/usr/local/bin/goald"]
