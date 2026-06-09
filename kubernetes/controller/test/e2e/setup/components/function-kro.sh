#!/usr/bin/env bash
# Install function-kro and function-auto-ready Crossplane function packages.
set -euo pipefail

FUNCTION_KRO_VERSION="${FUNCTION_KRO_VERSION:-v0.2.1}"
FUNCTION_AUTO_READY_VERSION="${FUNCTION_AUTO_READY_VERSION:-v0.6.0}"

if kubectl get function crossplane-contrib-function-kro >/dev/null 2>&1; then
  echo "function-kro already installed, skipping"
else
  kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: crossplane-contrib-function-kro
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-kro:${FUNCTION_KRO_VERSION}
EOF

  kubectl wait function/crossplane-contrib-function-kro \
    --for=condition=Installed=True \
    --timeout=120s
  kubectl wait function/crossplane-contrib-function-kro \
    --for=condition=Healthy=True \
    --timeout=120s
fi

if kubectl get function crossplane-contrib-function-auto-ready >/dev/null 2>&1; then
  echo "function-auto-ready already installed, skipping"
else
  kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: crossplane-contrib-function-auto-ready
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-auto-ready:${FUNCTION_AUTO_READY_VERSION}
EOF

  kubectl wait function/crossplane-contrib-function-auto-ready \
    --for=condition=Installed=True \
    --timeout=120s
  kubectl wait function/crossplane-contrib-function-auto-ready \
    --for=condition=Healthy=True \
    --timeout=120s
fi
