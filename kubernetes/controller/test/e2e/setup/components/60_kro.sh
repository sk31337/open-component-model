#!/usr/bin/env bash
# Install the kro operator via its upstream Helm chart. `helm upgrade
# --install` is idempotent; the original `helm install` would fail on
# repeated runs, so this is a small hardening over the legacy script.

set -euo pipefail

KRO_VERSION="${KRO_VERSION:-0.9.0}"

helm upgrade --install kro \
  oci://registry.k8s.io/kro/charts/kro \
  --namespace kro \
  --create-namespace \
  --version "${KRO_VERSION}"
