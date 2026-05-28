#!/usr/bin/env bash
# Apply the in-cluster protected registries (used by the credentials
# scenarios) and the testing RBAC manifests, then wait for the registry
# pods to become Ready.
#
# Idempotent via `kubectl apply` + a Ready wait that's a no-op if the
# pods are already Ready.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifests_dir="$(cd "${script_dir}/../manifests" && pwd)"

image_registries="${manifests_dir}/image-registries.yaml"
rbac="${manifests_dir}/rbac.yaml"

if [[ ! -f "${image_registries}" ]]; then
  echo "missing manifest: ${image_registries}" >&2
  exit 1
fi
if [[ ! -f "${rbac}" ]]; then
  echo "missing manifest: ${rbac}" >&2
  exit 1
fi

kubectl apply -f "${image_registries}"
kubectl apply -f "${rbac}"

kubectl wait pod -l app=protected-registry1 --for=condition=Ready --timeout=5m
kubectl wait pod -l app=protected-registry2 --for=condition=Ready --timeout=5m
