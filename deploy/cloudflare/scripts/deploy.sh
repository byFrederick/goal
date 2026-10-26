#!/bin/sh
# The production deploy (task deploy:worker; cloudflare-operations.md, Deploys). Builds the images
# of wrangler.jsonc's Dockerfiles with docker buildx for linux/amd64, pushes them to
# registry.cloudflare.com/<account>/<repository>, writes wrangler.deploy.jsonc (git-ignored) with
# their digest-pinned references, and runs `wrangler deploy --config wrangler.deploy.jsonc`, with
# `--var GOAL_BOOTSTRAP_OWNER_EMAIL:<address>` when that variable is set. It retries wrangler deploy
# once, only when it timed out preparing the container image on Cloudflare's network.
#
# Reproducible: an image whose inputs (scripts/images.mjs) did not change gets the same digest, so
# Cloudflare has nothing new to prepare. Each build has SOURCE_DATE_EPOCH = the last commit time of
# its inputs and rewrite-timestamp=true, no provenance or SBOM attestation (they carry the build's
# time), and its tag is that commit. GOAL_IMAGE_CACHE picks the layer cache: registry (a
# :buildcache tag beside each image in Cloudflare's registry), gha (the GitHub Actions cache), or
# none (the default, locally). A cache that fails to export never fails the deploy.
#
# Needs CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID, Docker with buildx, and the full git
# history. Prints only repository names, commits, epochs and digests; the registry password is a
# masked, 60-minute credential that wrangler mints and docker logout drops at exit.
set -eu
set -f # $inputs splits into paths; nothing here globs

cd "$(dirname "$0")/.."
root=$(git rev-parse --show-toplevel)
if [ "$(git rev-parse --is-shallow-repository)" != false ]; then
  echo "deploy.sh: a shallow clone dates every image by HEAD; fetch the full history (fetch-depth: 0)" >&2
  exit 1
fi
registry=registry.cloudflare.com
cache=${GOAL_IMAGE_CACHE:-none}
case "$cache" in
  none | registry | gha) ;;
  *) echo "deploy.sh: GOAL_IMAGE_CACHE is none, registry or gha, not $cache" >&2; exit 1 ;;
esac

work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/goal-deploy.XXXXXX")
trap 'docker logout "$registry" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

# The docker driver can neither push with rewritten timestamps nor export a cache: use a
# docker-container builder (in CI, setup-buildx-action's, already the current one).
builder=
if [ "$(docker buildx inspect | sed -n 's/^Driver: *//p' | head -n 1)" = docker ]; then
  docker buildx inspect goal-deploy >/dev/null 2>&1 ||
    docker buildx create --name goal-deploy --driver docker-container >/dev/null
  builder="--builder goal-deploy"
fi

login() {
  creds=$(WRANGLER_HIDE_BANNER=true pnpm exec wrangler containers registries credentials "$registry" \
    --push --pull --json --expiration-minutes 60)
  user=$(printf %s "$creds" | node scripts/images.mjs field username)
  password=$(printf %s "$creds" | node scripts/images.mjs field password)
  if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::add-mask::$password"; fi
  printf %s "$password" | docker login --username "$user" --password-stdin "$registry" >/dev/null 2>&1 || {
    echo "deploy.sh: docker login to $registry failed" >&2
    exit 1
  }
}

node scripts/images.mjs list > "$work/images.tsv"
tab=$(printf '\t')
refs=
while IFS=$tab read -r repository dockerfile context inputs <&3; do
  # shellcheck disable=SC2086 # $inputs is a space-separated list of paths
  epoch=$(git -C "$root" log -1 --format=%ct -- $inputs)
  # shellcheck disable=SC2086
  rev=$(git -C "$root" log -1 --format=%H -- $inputs | cut -c1-12)
  name="$registry/$CLOUDFLARE_ACCOUNT_ID/$repository"
  case "$cache" in
    registry)
      set -- --cache-from "type=registry,ref=$name:buildcache" \
        --cache-to "type=registry,ref=$name:buildcache,mode=max,image-manifest=true,oci-mediatypes=true,ignore-error=true" ;;
    gha) set -- --cache-from "type=gha,scope=$repository" --cache-to "type=gha,scope=$repository,mode=max,ignore-error=true" ;;
    none) set -- ;;
  esac
  login
  echo "Building $repository at $rev (SOURCE_DATE_EPOCH $epoch, cache $cache)"
  start=$(date +%s)
  # shellcheck disable=SC2086 # $builder is empty or two words
  SOURCE_DATE_EPOCH=$epoch docker buildx build $builder "$@" \
    --platform linux/amd64 --provenance=false --sbom=false \
    --file "$dockerfile" --metadata-file "$work/$repository.json" \
    --output "type=image,name=$name:$rev,push=true,rewrite-timestamp=true" \
    "$context"
  digest=$(node scripts/images.mjs field containerimage.digest < "$work/$repository.json")
  echo "Pushed $repository@$digest in $(( $(date +%s) - start )) s"
  refs="$refs $repository=$name@$digest"
done 3< "$work/images.tsv"

# shellcheck disable=SC2086 # $refs is a space-separated list of repository=reference
node scripts/images.mjs config $refs > wrangler.deploy.jsonc
echo "Wrote wrangler.deploy.jsonc"

set -- deploy --config wrangler.deploy.jsonc
if [ -n "${GOAL_BOOTSTRAP_OWNER_EMAIL:-}" ]; then
  set -- "$@" --var "GOAL_BOOTSTRAP_OWNER_EMAIL:$GOAL_BOOTSTRAP_OWNER_EMAIL"
fi
# Streams wrangler's output and keeps a copy to look for the one transient failure retried.
log="$work/wrangler-deploy.log"
{ code=0; pnpm exec wrangler "$@" 2>&1 || code=$?; echo "$code" > "$work/status"; } | tee "$log"
status=$(cat "$work/status")
if [ "$status" -ne 0 ] && grep -qF "Timed out while preparing the container image on Cloudflare's network" "$log"; then
  echo "wrangler deploy timed out preparing an image on Cloudflare's network: retrying once"
  { code=0; pnpm exec wrangler "$@" 2>&1 || code=$?; echo "$code" > "$work/status"; } | tee "$log"
  status=$(cat "$work/status")
fi
exit "$status"
