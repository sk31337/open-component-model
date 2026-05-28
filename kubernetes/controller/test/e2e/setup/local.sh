#!/usr/bin/env bash
# Orchestrator for the local e2e environment. Runs every script in
# components/ in lexical order. Each component script is idempotent and
# self-contained; running this script twice should converge.
#
# DESIGN.md §"Setup composition" describes the contract. Stage 2 of the
# migration plan (split monolithic hacks/setup.sh into per-component
# scripts) lands this file along with components/*.sh.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
components_dir="${script_dir}/components"

if [[ ! -d "${components_dir}" ]]; then
  echo "components directory not found: ${components_dir}" >&2
  exit 1
fi

# Run every executable .sh file in components/ in lexical order. The
# numeric prefix on filenames defines the canonical order.
shopt -s nullglob
scripts=("${components_dir}"/*.sh)
shopt -u nullglob

if [[ ${#scripts[@]} -eq 0 ]]; then
  echo "no component scripts found in ${components_dir}" >&2
  exit 1
fi

for s in "${scripts[@]}"; do
  echo
  echo ">>> $(basename "${s}")"
  bash "${s}"
done

echo
echo "setup/local.sh complete."
