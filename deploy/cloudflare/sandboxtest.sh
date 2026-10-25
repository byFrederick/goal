#!/bin/sh
# sandboxtest against the sandbox API in wrangler dev (task cloudflare:sandboxtest): the Worker's
# own code, its boxes local Docker containers built from test/sandboxtest/box.Dockerfile.
# PORT (default 18787) is the Worker's port.
set -eu
cd "$(dirname "$0")"
. ./test/dev.sh
dev test/sandboxtest/wrangler.jsonc "${PORT:-18787}"

cd ../..
GOAL_TEST_SANDBOX_URL="http://127.0.0.1:${PORT:-18787}" GOAL_TEST_SANDBOX_KEY="$key" GOAL_TEST_SANDBOX_IMAGE=sandboxtest \
  go test -race -count=1 -tags cloudflare -run TestSandboxes_Conformance -v ./internal/adapters/cloudflare
