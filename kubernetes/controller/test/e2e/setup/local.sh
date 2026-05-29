#!/usr/bin/env bash
# Orchestrator for the local e2e environment.
#
# Runs the always-on bootstrap (cluster.sh) followed by every per-
# component script under components/. Each script is idempotent and
# self-contained; running this orchestrator twice should converge.
#
# DESIGN.md §"Setup composition" describes the contract:
#   * cluster.sh provisions the kind cluster + host registry + RBAC and
#     is always required.
#   * Every file in components/ corresponds to an opt-in component a
#     scenario can declare via `requires:` in its e2e.yaml. Locally we
#     install all of them so any focused run finds its dependencies up.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
components_dir="${script_dir}/components"
cluster_script="${script_dir}/cluster.sh"

if [[ ! -f "${cluster_script}" ]]; then
  echo "cluster bootstrap script not found: ${cluster_script}" >&2
  exit 1
fi

echo ">>> cluster.sh"
bash "${cluster_script}"

if [[ ! -d "${components_dir}" ]]; then
  echo "components directory not found: ${components_dir}" >&2
  exit 1
fi

shopt -s nullglob
scripts=("${components_dir}"/*.sh)
shopt -u nullglob

if [[ ${#scripts[@]} -eq 0 ]]; then
  echo "no component scripts found in ${components_dir}" >&2
  exit 1
fi

for s in "${scripts[@]}"; do
  echo
  echo ">>> components/$(basename "${s}")"
  bash "${s}"
done

echo
echo "setup/local.sh complete."
