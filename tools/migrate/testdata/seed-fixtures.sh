#!/usr/bin/env bash
set -euo pipefail

# This helper is deliberately safe to invoke from a checkout: every bd write
# runs in a fresh temporary workspace. Checked-in JSONL fixtures are sanitized
# snapshots; this script documents the public-command seed path used to refresh
# them for a supported bd release. It intentionally leaves generated snapshots
# in the temporary directory; promote a reviewed, sanitized copy explicitly.
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
before=""
if [ -d "$repo_dir/.beads" ]; then before=$(tar -cf - -C "$repo_dir" .beads | shasum -a 256 | awk '{print $1}'); fi
seed_dir=$(mktemp -d "${TMPDIR:-/tmp}/bdd-migration-fixtures.XXXXXX")
trap 'rm -rf "$seed_dir"' EXIT
for shape in orcha ocp; do
  work="$seed_dir/$shape"
  mkdir -p "$work"
  ( cd "$work"
    bd init --non-interactive --quiet --prefix "$shape" --skip-agents --skip-hooks
    bd config set status.custom awaiting_review
    bd config set types.custom role
    # Explicit IDs make the exported graph representative of the IDs that the
    # importer must preserve. Each role has both a blocking and a non-blocking
    # edge, while closed_at remains null for these open issues.
    if [ "$shape" = orcha ]; then
      role_id=orcha-wisp-abc
      blocker_id=orcha-dep
      related_id=orcha-related
      memory_key=orcha/agent
      title='[role] Programmer'
      description=$'first line\nsecond line'
      design=$'design\nnotes'
      acceptance=$'works\nwith multiline acceptance'
      labels='runtime:codex,café'
    else
      role_id=ocp-123
      blocker_id=ocp-122
      related_id=ocp-100
      memory_key=ocp/migration
      title='[role] OCP migration'
      description=$'multiline\ndescription'
      design=$'keep\nraw'
      acceptance=$'all fields\nare retained'
      labels='release,日本語'
    fi

    bd create --id "$blocker_id" --title="Fixture blocker" --silent >/dev/null
    bd create --id "$related_id" --title="Fixture related issue" --silent >/dev/null
    bd create --id "$role_id" --title="$title" --type=role \
      --description "$description" --design "$design" --acceptance "$acceptance" \
      --notes 'initial note' --labels "$labels" --silent >/dev/null
    bd update "$role_id" --append-notes 'accumulated note' >/dev/null
    bd comment "$role_id" $'structured\ncomment' >/dev/null
    bd dep add "$role_id" "$blocker_id" --type blocks >/dev/null
    bd dep add "$role_id" "$related_id" --type related >/dev/null
    bd remember "remember this $shape fixture" --key "$memory_key" >/dev/null

    version=$(bd --readonly version | awk '{print $3}')
    bd --readonly export --all > "$work/${shape}-bd-${version}.generated.jsonl"
    echo "generated sanitized-review input: $work/${shape}-bd-${version}.generated.jsonl"
  )
done
after=""
if [ -d "$repo_dir/.beads" ]; then after=$(tar -cf - -C "$repo_dir" .beads | shasum -a 256 | awk '{print $1}'); fi
[ "$before" = "$after" ] || { echo "error: invoking repo .beads changed" >&2; exit 1; }
