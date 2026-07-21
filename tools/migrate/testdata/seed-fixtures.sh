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
    bd create --title="[role] Programmer" --type=role --description $'first line\nsecond line' --design $'design\nnotes' --notes 'accumulated note' --labels 'runtime:codex,café' --silent >/dev/null
    bd export --all > "$work/${shape}-bd-$(bd version | awk '{print $3}').generated.jsonl"
    echo "generated sanitized-review input: $work/${shape}-bd-$(bd version | awk '{print $3}').generated.jsonl"
  )
done
after=""
if [ -d "$repo_dir/.beads" ]; then after=$(tar -cf - -C "$repo_dir" .beads | shasum -a 256 | awk '{print $1}'); fi
[ "$before" = "$after" ] || { echo "error: invoking repo .beads changed" >&2; exit 1; }
