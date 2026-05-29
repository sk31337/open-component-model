#!/usr/bin/env bash
# Install the in-cluster password-protected registry used by the
# credentials/basic-auth scenario (htpasswd auth).
# Idempotent via `kubectl apply` + Ready wait.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifests_dir="$(cd "${script_dir}/../manifests" && pwd)"
manifest="${manifests_dir}/protected-registry-basic-auth.yaml"

if [[ ! -f "${manifest}" ]]; then
  echo "missing manifest: ${manifest}" >&2
  exit 1
fi

kubectl apply -f "${manifest}"
kubectl wait pod -l app=protected-registry1 --for=condition=Ready --timeout=5m
