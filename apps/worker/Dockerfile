# syntax=docker/dockerfile:1
# goal-agent: the one image every run's sandbox starts from (spec 4 §4.1, ADR 0008). Build from
# the repository root (task worker:image):
#   docker build -f apps/worker/Dockerfile -t ghcr.io/byfrederick/goal-agent:0.2.0 .
# A sandbox idles in it until goald execs `goal-agent prepare`, `goal-agent run` and the station's
# checks; apps/worker/README.md has the contract. It carries the worker (Claude Code through the Agent
# SDK), git, gh, mise with Go, Node and Python, a C compiler and PostgreSQL 18's server binaries with pgvector;
# a repository's other tools come from its mise config at prepare. Every version is pinned and
# every download checked against the checksums its project publishes. The production deploy builds
# and pushes it reproducibly (deploy/cloudflare/scripts/deploy.sh): the slow stages (apt, mise,
# pnpm install from the manifests) come before COPY apps/worker/, and no layer keeps apt's lists or logs,
# a package manager's cache or pnpm's dated .modules.yaml.

ARG DEBIAN_IMAGE=debian:trixie-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad

# downloads: the binaries and keys the image takes from outside Debian, checked.
FROM ${DEBIAN_IMAGE} AS downloads
ARG TARGETARCH
ARG MISE_VERSION=2026.10.3
ARG GH_VERSION=2.102.0
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN apt-get update \
 && apt-get install --yes --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /dl
RUN mise="mise-v${MISE_VERSION}-linux-$([ "${TARGETARCH}" = amd64 ] && echo x64 || echo "${TARGETARCH}")" \
 && curl -fsSLO "https://github.com/jdx/mise/releases/download/v${MISE_VERSION}/${mise}" \
 && curl -fsSL "https://github.com/jdx/mise/releases/download/v${MISE_VERSION}/SHASUMS256.txt" | grep " ./${mise}$" | sha256sum -c - \
 && install -D -m 0755 "${mise}" /out/usr/local/bin/mise \
 && gh="gh_${GH_VERSION}_linux_${TARGETARCH}" \
 && curl -fsSLO "https://github.com/cli/cli/releases/download/v${GH_VERSION}/${gh}.tar.gz" \
 && curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_checksums.txt" | grep " ${gh}.tar.gz$" | sha256sum -c - \
 && tar -xzf "${gh}.tar.gz" \
 && install -D -m 0755 "${gh}/bin/gh" /out/usr/local/bin/gh \
 && curl -fsSL --create-dirs https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /out/usr/share/keyrings/pgdg.asc

# base: Debian with the system packages, PostgreSQL's server binaries and pgvector, gh and mise with the
# image's Go, Node and Python, installed as the sandbox's user into /opt/mise.
FROM ${DEBIAN_IMAGE} AS base
ARG PG_MAJOR=18
ARG GO_VERSION=1.26.10
ARG NODE_VERSION=22.23.3
ARG PYTHON_VERSION=3.14.8
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
COPY --from=downloads /out/usr/local/bin/ /usr/local/bin/
COPY --from=downloads --chmod=0644 /out/usr/share/keyrings/pgdg.asc /usr/share/keyrings/pgdg.asc
# No cluster at install: a run that wants PostgreSQL starts its own (initdb, pg_ctl).
RUN apt-get update \
 && apt-get install --yes --no-install-recommends ca-certificates \
 && echo "deb [signed-by=/usr/share/keyrings/pgdg.asc] https://apt.postgresql.org/pub/repos/apt trixie-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list \
 && mkdir -p /etc/postgresql-common/createcluster.d \
 && echo "create_main_cluster = false" > /etc/postgresql-common/createcluster.d/no-main.conf \
 && apt-get update \
 && apt-get install --yes --no-install-recommends \
      curl git openssh-client ripgrep jq unzip xz-utils zstd bzip2 procps less \
      gcc libc6-dev make "postgresql-${PG_MAJOR}" "postgresql-${PG_MAJOR}-pgvector" \
 && rm -rf /var/lib/apt/lists/* /var/log/apt /var/log/dpkg.log /var/log/alternatives.log \
      /var/cache/debconf/*-old /var/cache/ldconfig/aux-cache \
 && useradd --uid 1000 --user-group --create-home --home-dir /home/node --shell /bin/bash node \
 && install -d -o 1000 -g 1000 /opt/mise /workspace /goal \
 && printf '%s\n' '[user]' '	name = Goal agent' '	email = goal-agent@example.com' '[init]' '	defaultBranch = main' \
      > /etc/gitconfig \
 && mkdir -p /etc/mise \
 && printf '%s\n' '[tools]' "go = \"${GO_VERSION}\"" "node = \"${NODE_VERSION}\"" "python = \"${PYTHON_VERSION}\"" \
      '' '[settings]' \
      '# npm: tools install with npm itself. The auto choice (aube) writes bin shims that run every' \
      '# bin through node, so a package whose bin is a native binary, such as pnpm 12, fails to start.' \
      'npm.package_manager = "npm"' \
      > /etc/mise/config.toml
ENV HOME=/home/node \
    MISE_DATA_DIR=/opt/mise \
    MISE_TRUSTED_CONFIG_PATHS=/workspace \
    MISE_YES=1 \
    MISE_DISABLE_UPDATE_WARNING=1 \
    GIT_TERMINAL_PROMPT=0 \
    PATH=/opt/mise/shims:/usr/lib/postgresql/${PG_MAJOR}/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
USER 1000:1000
RUN mise install \
 && rm -rf /home/node/.cache /home/node/.local/state /opt/mise/downloads

# worker: the TypeScript worker, built with the image's own Node and deployed with its
# production dependencies only.
FROM base AS worker
ARG PNPM_VERSION=12.15.0
WORKDIR /home/node/src
RUN npm install --global --no-fund --no-audit "pnpm@${PNPM_VERSION}" && mise reshim
COPY --chown=1000:1000 package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY --chown=1000:1000 apps/worker/package.json apps/worker/
RUN pnpm install --frozen-lockfile --filter @goal/worker
COPY --chown=1000:1000 apps/worker/ apps/worker/
RUN pnpm --filter @goal/worker build \
 && pnpm --filter @goal/worker deploy --prod /home/node/out \
 && rm -f /home/node/out/node_modules/.modules.yaml

# runtime: base plus the worker, nothing of its build.
FROM base AS runtime
ARG NODE_VERSION=22.23.3
COPY --from=worker /home/node/out /opt/goal-worker
USER root
# The worker runs on the image's Node whatever Node a repository pins for the agent.
RUN printf '%s\n' '#!/bin/sh' \
      "NODE_ENV=production exec /opt/mise/installs/node/${NODE_VERSION}/bin/node /opt/goal-worker/dist/main.js \"\$@\"" \
      > /usr/local/bin/goal-agent \
 && chmod 0755 /usr/local/bin/goal-agent
USER 1000:1000
WORKDIR /workspace
CMD ["sleep", "infinity"]
