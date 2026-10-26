#!/bin/sh
# The control Worker's topology in wrangler dev (task cloudflare:topology): src/index.ts with
# stand-ins for goald's and the auth service's images
# (apps/control-worker/test/topology), real local containers.
# Checks a public request reaches goald, goald's container reaches auth.internal and
# sandbox.internal, a box reaches gateway.internal, and each container has only its own settings.
# PORT (default 18788) is the Worker's port.
set -eu
# wrangler dev runs from apps/control-worker, the Worker's package, with its node_modules.
cd "$(dirname "$0")/../../apps/control-worker"
port=${PORT:-18788}
. ./test/dev.sh
dev test/topology/wrangler.jsonc "$port"

fail() { echo "FAIL: $*" >&2; exit 1; }
has() { printf '%s' "$1" | grep -q "$2"; }

# A public request: goald's port, the client as Cloudflare names it (wrangler dev has no
# cf-connecting-ip, so none is passed on and the client's own X-Forwarded-For is dropped).
public=$(curl -sS --retry 30 --retry-all-errors --retry-delay 2 -H 'X-Forwarded-For: 10.9.9.9' "http://127.0.0.1:$port/v1/orgs")
has "$public" '"port":8080' || fail "public request: $public"
has "$public" '10.9.9.9' && fail "the client's X-Forwarded-For reached goald: $public"
has "$public" '"GOAL_MASTER_KEYS"' || fail "goald lacks its master key: $public"
has "$public" '"GOAL_SANDBOX_URL"' || fail "goald lacks the topology: $public"
has "$public" '"GOAL_TRUSTED_PROXIES"' || fail "goald believes no proxy, so the auth service would see only the Worker: $public"
for name in BETTER_AUTH_SECRETS UNLISTED_SETTING; do has "$public" "\"$name\"" && fail "goald has $name"; done
echo "ok: public https -> goald:8080, with goald's settings only, believing the Worker's X-Forwarded-For"
echo "    (the Worker's requests reach goald's container from $(printf '%s' "$public" | sed -n 's/.*"peer":"\([^"]*\)".*/\1/p'))"

goald=$(docker ps --format '{{.Names}}' | grep '^workerd-goal-topology-Goald' | grep -v proxy | head -n 1)
[ -n "$goald" ] || fail "no goald container"
inside() { docker exec "$goald" node --input-type=module -e "const r = await fetch(process.argv[1], { method: process.argv[2] ?? 'GET', headers: { authorization: 'Bearer ' + (process.argv[4] ?? '') }, body: process.argv[3] || undefined }); console.log(r.status, await r.text())" "$@"; }

authAnswer=$(inside http://auth.internal/api/auth/get-session)
has "$authAnswer" '"port":8081' || fail "auth.internal: $authAnswer"
has "$authAnswer" '"BETTER_AUTH_SECRETS"' || fail "auth lacks its secret: $authAnswer"
for name in GOAL_MASTER_KEYS GOAL_SANDBOX_KEY GOAL_DATABASE_URL UNLISTED_SETTING; do has "$authAnswer" "\"$name\"" && fail "auth has $name"; done
echo "ok: goald -> auth.internal -> auth:8081, with the auth service's settings only"

refused=$(inside http://sandbox.internal/v1/sandboxes GET '' wrong)
has "$refused" '^401' || fail "sandbox API without the key: $refused"
run=019a4000-0000-7000-8000-0000000000aa
deadline=$(date -u -v+1H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 hour' +%Y-%m-%dT%H:%M:%SZ)
created=$(inside "http://sandbox.internal/v1/sandboxes/$run" PUT \
  "{\"org\":\"org-acme\",\"image\":\"goal-agent\",\"size\":\"small\",\"env\":{\"FROM_GOALD\":\"box\"},\"deadline\":\"$deadline\"}" "$key")
has "$created" '^201' || fail "create a box: $created"
echo "ok: goald -> sandbox.internal (bearer) -> a box"

gateway=$(inside "http://sandbox.internal/v1/sandboxes/$run/execs" POST '{"id":"a1","argv":["curl","-sS","http://gateway.internal/run"]}' "$key")
has "$gateway" '\\"port\\":8090' || fail "box -> gateway.internal: $gateway"
echo "ok: box -> gateway.internal -> goald:8090"

names=$(inside "http://sandbox.internal/v1/sandboxes/$run/execs" POST \
  '{"id":"a2","argv":["sh","-c","env | cut -d= -f1 | sort | tr \"\\n\" \" \""],"env":{"FROM_EXEC":"exec"}}' "$key")
has "$names" 'FROM_GOALD' || fail "the box lacks goald's env: $names"
has "$names" 'FROM_EXEC' || fail "the command lacks its env: $names"
for name in GOAL_MASTER_KEYS GOAL_SANDBOX_KEY BETTER_AUTH_SECRETS GOAL_DATABASE_URL UNLISTED_SETTING; do
  has "$names" "$name" && fail "the box has $name"
done
echo "ok: a box's command has the image's env, goald's for the box and its own, nothing of the Worker's"

deleted=$(inside "http://sandbox.internal/v1/sandboxes/$run" DELETE '' "$key")
has "$deleted" '^204' || fail "delete the box: $deleted"
echo "ok: the box deleted"
