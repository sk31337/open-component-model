#!/usr/bin/env bash
# Install the kro operator via its upstream Helm chart.
# Idempotent via `helm upgrade --install`.
set -euo pipefail

KRO_VERSION="${KRO_VERSION:-0.9.0}"

helm upgrade --install kro \
  oci://registry.k8s.io/kro/charts/kro \
  --namespace kro \
  --create-namespace \
  --version "${KRO_VERSION}"
