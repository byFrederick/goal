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

# Dev variant: the runtime plus the toolchain a factory that builds this repository needs to do
# its work and to pass its station checks (`task check`, `task migrate-check`) in an isolated
# container: Go (with a C compiler for -race), task, golangci-lint, pnpm, Terraform with the AWS provider mirrored for
# `terraform init`, and a PostgreSQL 18 server for throwaway clusters. Built by
# `task worker:image-dev`; goald never mounts the Docker socket, so nothing here can reach the
# host's Docker. Module downloads are prewarmed from go.mod as a local GOPROXY.
FROM runtime AS dev
ARG TARGETARCH
ARG GO_VERSION=1.26.10
ARG TASK_VERSION=3.46.1
ARG GOLANGCI_VERSION=2.15.0
ARG TERRAFORM_VERSION=1.15.7
ARG PNPM_VERSION=12.15.0
ARG PG_MAJOR=18
USER root
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN apt-get update \
 && apt-get install --yes --no-install-recommends curl unzip make gcc libc6-dev \
 && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /usr/share/keyrings/pgdg.asc \
 && echo "deb [signed-by=/usr/share/keyrings/pgdg.asc] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list \
 && apt-get update \
 && apt-get install --yes --no-install-recommends "postgresql-${PG_MAJOR}" \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /tmp/dl
# Every download is checked against the checksums its project publishes.
RUN curl -fsSLO "https://dl.google.com/go/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
 && echo "$(curl -fsSL "https://dl.google.com/go/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz.sha256")  go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" | sha256sum -c - \
 && tar -C /usr/local -xzf "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
 && curl -fsSLO "https://github.com/go-task/task/releases/download/v${TASK_VERSION}/task_linux_${TARGETARCH}.tar.gz" \
 && curl -fsSL "https://github.com/go-task/task/releases/download/v${TASK_VERSION}/task_checksums.txt" | grep " task_linux_${TARGETARCH}.tar.gz$" | sha256sum -c - \
 && tar -C /usr/local/bin -xzf "task_linux_${TARGETARCH}.tar.gz" task \
 && curl -fsSLO "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/golangci-lint-${GOLANGCI_VERSION}-linux-${TARGETARCH}.tar.gz" \
 && curl -fsSL "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/golangci-lint-${GOLANGCI_VERSION}-checksums.txt" | grep " golangci-lint-${GOLANGCI_VERSION}-linux-${TARGETARCH}.tar.gz$" | sha256sum -c - \
 && tar -C /usr/local/bin --strip-components=1 -xzf "golangci-lint-${GOLANGCI_VERSION}-linux-${TARGETARCH}.tar.gz" "golangci-lint-${GOLANGCI_VERSION}-linux-${TARGETARCH}/golangci-lint" \
 && curl -fsSLO "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip" \
 && curl -fsSL "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_SHA256SUMS" | grep " terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip$" | sha256sum -c - \
 && unzip -q "terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip" terraform -d /usr/local/bin \
 && npm install --global --no-fund --no-audit "pnpm@${PNPM_VERSION}" \
 && rm -rf /tmp/dl /root/.npm
ENV PATH=/usr/local/go/bin:/usr/lib/postgresql/${PG_MAJOR}/bin:${PATH} \
    NODE_ENV=development \
    GOTOOLCHAIN=local \
    CGO_ENABLED=1 \
    GOPROXY=file:///opt/goproxy/cache/download,https://proxy.golang.org,direct \
    TF_CLI_CONFIG_FILE=/opt/terraform/terraformrc \
    GOTMPDIR=/home/node/.cache/go-tmp
# Go modules of this repository, served as a read-only local proxy; new modules still download.
COPY go.mod go.sum /tmp/gomod/
RUN cd /tmp/gomod && GOMODCACHE=/opt/goproxy GOPROXY=https://proxy.golang.org,direct go mod download \
 && chmod -R a+rX /opt/goproxy && rm -rf /tmp/gomod /root/.cache
# Terraform providers of terraform/, mirrored so `terraform init` installs them offline.
COPY terraform/ /tmp/tf/
RUN cd /tmp/tf && rm -rf .terraform \
 && TF_CLI_CONFIG_FILE= terraform init -backend=false -input=false >/dev/null \
 && TF_CLI_CONFIG_FILE= terraform providers mirror -platform="linux_${TARGETARCH}" /opt/terraform/providers >/dev/null \
 && printf '%s\n' 'provider_installation {' \
      '  filesystem_mirror {' '    path    = "/opt/terraform/providers"' '    include = ["registry.terraform.io/*/*"]' '  }' \
      '  direct {' '    exclude = ["registry.terraform.io/*/*"]' '  }' '}' > /opt/terraform/terraformrc \
 && rm -rf /tmp/tf /root/.terraform.d
# /tmp is mounted noexec, so Go builds test binaries under the home volume instead.
RUN install -d -o 1000 -g 1000 /home/node/.cache /home/node/.cache/go-tmp
USER 1000:1000
WORKDIR /workspace
