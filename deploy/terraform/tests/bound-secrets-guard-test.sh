#!/bin/sh
# Runs deploy/terraform/scripts/bound-secrets-guard.sh against the fake plans and Worker settings in
# tests/fixtures/bound-secrets (names only, no values): a plan that destroys or replaces a bound
# secret fails naming it, one that destroys an unbound secret or destroys none passes, and input it
# cannot read fails. Run from the repository root: task tf:guard-test.
set -eu

guard=deploy/terraform/scripts/bound-secrets-guard.sh
fixtures=deploy/terraform/tests/fixtures/bound-secrets
settings="$fixtures/worker-settings.json"
failures=0

# expect <name> <exit code> <text the output must hold, or ""> <guard arguments...>
expect() {
  name="$1" want="$2" text="$3"
  shift 3
  set +e
  out="$(sh "$guard" "$@" 2>&1)"
  got=$?
  set -e
  if [ "$got" -ne "$want" ]; then
    echo "fail: $name: exit $got, want $want: $out" >&2
    failures=$((failures + 1))
  elif [ -n "$text" ] && ! printf '%s\n' "$out" | grep -qF -- "$text"; then
    echo "fail: $name: output lacks \"$text\": $out" >&2
    failures=$((failures + 1))
  else
    echo "ok: $name"
  fi
}

if ! command -v jq >/dev/null 2>&1; then
  echo "fail: jq not found: the guard needs it" >&2
  exit 1
fi

expect "destroys a bound secret" 1 "destroys or replaces goal-test-OLD_KEY, which the deployed goal Worker binds. Keep it until a deploy no longer binds it" \
  "$fixtures/plan-destroys-bound.json" "$settings"
expect "replaces a bound secret" 1 "goal-test-GATEWAY_KEY" \
  "$fixtures/plan-replaces-bound.json" "$settings"
expect "destroys an unbound secret" 0 "" "$fixtures/plan-destroys-unbound.json" "$settings"
expect "destroys nothing" 0 "" "$fixtures/plan-no-destroys.json" "$settings"
expect "plan through a pipe" 1 "goal-test-OLD_KEY" /dev/stdin "$settings" < "$fixtures/plan-destroys-bound.json"
expect "settings without bindings" 2 "no result.bindings array" "$fixtures/plan-no-destroys.json" "$fixtures/settings-no-bindings.json"
expect "settings as the plan" 2 "not the JSON of terraform show -json" "$settings" "$settings"
expect "no arguments" 2 "usage:"

if [ "$failures" -ne 0 ]; then
  echo "$failures bound-secrets guard case(s) failed" >&2
  exit 1
fi
