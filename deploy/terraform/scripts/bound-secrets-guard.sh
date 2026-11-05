#!/bin/sh
# Refuses a plan that destroys or replaces a Secrets Store secret the deployed Worker still binds.
# Terraform applies on merge to main and the Worker deploys later (ADR 0010, ADR 0011), so removing
# or renaming a store secret first breaks the running Worker at its next restart: every boot fails
# with "Secrets Worker: Failed to fetch secret" (the 2026-11-04 outage, runbook: Secrets).
#
# Usage: bound-secrets-guard.sh <plan.json> <worker-settings.json>
#   plan.json             terraform show -json of a saved plan (a path, or /dev/stdin)
#   worker-settings.json  the answer of GET /accounts/{id}/workers/scripts/goal/settings
# Exits 1, with one ::error per secret, when the plan's actions on a cloudflare_secrets_store_secret
# include "delete" and its name is the secret_name of a secrets_store_secret binding; 2 on input it
# cannot read; else 0. It prints only secret names, never a value of either file.
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <plan.json> <worker-settings.json>" >&2
  exit 2
fi
plan="$1"
settings="$2"

# Each file is read once, so the plan can come through a pipe and never touch the disk (a plan's
# JSON holds its sensitive values in the clear).
# Destroyed (delete) and replaced (delete then create, or create then delete) store secrets.
if ! removed="$(jq -r '
    if type == "object" and has("format_version") then
      .resource_changes // [] | .[]
      | select(.type == "cloudflare_secrets_store_secret" and (.change.actions | index("delete")))
      | .change.before.name
    else error("not a plan") end
  ' "$plan" 2>/dev/null)"; then
  echo "::error title=Plan unreadable::$plan is not the JSON of terraform show -json of a saved plan." >&2
  exit 2
fi
# A settings answer without a bindings array would bind nothing and pass every plan: refuse it.
if ! bound="$(jq -r '
    if .success == true and (.result.bindings | type) == "array" then
      .result.bindings[] | select(.type == "secrets_store_secret") | .secret_name
    else error("no bindings") end
  ' "$settings" 2>/dev/null)"; then
  echo "::error title=Worker bindings unreadable::$settings holds no result.bindings array of the goal Worker: cannot tell which store secrets it binds." >&2
  exit 2
fi

status=0
for name in $removed; do
  if printf '%s\n' "$bound" | grep -qxF -- "$name"; then
    echo "::error title=Store secret still bound::The plan destroys or replaces $name, which the deployed goal Worker binds. Keep it until a deploy no longer binds it (runbook: Secrets, renaming a secret)."
    status=1
  fi
done

if [ "$status" -eq 0 ]; then
  echo "No store secret the deployed goal Worker binds is destroyed or replaced."
fi
exit "$status"
