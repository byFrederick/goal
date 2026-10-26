#!/bin/sh
# Proves that deploy/terraform/.sops.yaml's creation rule matches the production SOPS file: encrypts
# and decrypts a sample with a throwaway age key, under a config of the same shape (the real
# path_regex, a throwaway recipient). Never reads the operator's key or the real secrets file.
# Run from the repository root: task tf:sops-test.
set -eu

if ! command -v sops >/dev/null 2>&1 || ! command -v age-keygen >/dev/null 2>&1; then
  echo "skip: sops or age-keygen not found"
  exit 0
fi

config=deploy/terraform/.sops.yaml
target=deploy/terraform/secrets/production.sops.yaml

regex="$(sed -n 's/^[[:space:]]*-[[:space:]]*path_regex:[[:space:]]*//p' "$config")"
if [ -z "$regex" ] || [ "$(printf '%s\n' "$regex" | wc -l)" -ne 1 ]; then
  echo "fail: $config must hold exactly one path_regex" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

# The throwaway key only: nothing from the shell may point sops at another one.
unset SOPS_AGE_KEY SOPS_AGE_KEY_CMD
export SOPS_AGE_KEY_FILE="$tmp/key.txt"
age-keygen -o "$tmp/key.txt" 2>/dev/null
recipient="$(age-keygen -y "$tmp/key.txt")"

cat >"$tmp/sops.yaml" <<EOF
creation_rules:
  - path_regex: $regex
    age: $recipient
EOF

printf 'sample: test-value\n' >"$tmp/plain.yaml"

sops --config "$tmp/sops.yaml" encrypt --filename-override "$target" \
  --output "$tmp/encrypted.yaml" "$tmp/plain.yaml"
if grep -q 'test-value' "$tmp/encrypted.yaml"; then
  echo "fail: the encrypted file holds the plaintext value" >&2
  exit 1
fi
sops --config "$tmp/sops.yaml" decrypt --input-type yaml --output-type yaml \
  --output "$tmp/decrypted.yaml" "$tmp/encrypted.yaml"

if ! cmp -s "$tmp/plain.yaml" "$tmp/decrypted.yaml"; then
  echo "fail: the round trip changed the content" >&2
  exit 1
fi

# A path the rule must not match: sops finds no creation rule and refuses.
if sops --config "$tmp/sops.yaml" encrypt --filename-override deploy/terraform/secrets/other.yaml \
  "$tmp/plain.yaml" >/dev/null 2>&1; then
  echo "fail: the creation rule matches a path it should not" >&2
  exit 1
fi

echo "ok: $config's creation rule encrypts and decrypts $target"
