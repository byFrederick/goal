# Sourced by deploy/cloudflare's sandboxtest.sh and topology.sh, from apps/control-worker: dev CONFIG PORT starts wrangler dev on 127.0.0.1:PORT
# with a fresh random GOAL_SANDBOX_KEY ($key) and state in a temporary folder, waits until it is
# ready, and stops it at exit with every container it started. Locally only: nothing of a
# Cloudflare account reaches wrangler, and it never deploys.

dev() {
  config=$1
  key=$(openssl rand -hex 32)
  log=$(mktemp)
  state=$(mktemp -d)
  # The containers' names start with workerd-<the Worker's name>-.
  prefix="workerd-$(sed -n 's/^ *"name": *"\([^"]*\)".*/\1/p' "$config" | head -n 1)-"

  env -u CLOUDFLARE_API_TOKEN -u CLOUDFLARE_ACCOUNT_ID WRANGLER_SEND_METRICS=false \
    ./node_modules/.bin/wrangler dev --config "$config" --ip 127.0.0.1 --port "$2" \
    --var "GOAL_SANDBOX_KEY:$key" --persist-to "$state" >"$log" 2>&1 &
  wrangler=$!
  trap stop EXIT

  until grep -q "Ready on" "$log"; do
    if ! kill -0 "$wrangler" 2>/dev/null; then cat "$log" >&2; exit 1; fi
    sleep 1
  done
}

# stop ends wrangler, then removes its containers: wrangler dev leaves the outbound-interception
# sidecar of each container running, and its runtime may start one more while it shuts down.
stop() {
  status=$?
  kill "$wrangler" 2>/dev/null || true
  wait "$wrangler" 2>/dev/null || true # 143: it ended on our TERM
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    docker ps -aq --filter "name=$prefix" | xargs docker rm -f >/dev/null 2>&1 || true
    sleep 1
  done
  rm -rf "$log" "$state"
  exit "$status"
}
