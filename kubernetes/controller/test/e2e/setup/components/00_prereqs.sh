#!/usr/bin/env bash
# Verify all host-side commands the e2e setup needs are on $PATH.
# Idempotent: pure check, no side effects.

set -euo pipefail

cmds=(docker flux helm jq kind kubectl)

missing=()
for cmd in "${cmds[@]}"; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    missing+=("${cmd}")
  fi
done

if [[ ${#missing[@]} -gt 0 ]]; then
  echo "missing required command(s): ${missing[*]}" >&2
  echo "install them and re-run setup/local.sh" >&2
  exit 1
fi

echo "all required commands present: ${cmds[*]}"
