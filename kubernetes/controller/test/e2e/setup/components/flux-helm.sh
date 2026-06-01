#!/usr/bin/env bash
# Install Flux's helm-controller (+ source-controller dependency).
set -euo pipefail

if kubectl get deployment helm-controller -n flux-system >/dev/null 2>&1 \
   && kubectl get deployment helm-controller -n flux-system -o jsonpath='{.status.availableReplicas}' | grep -q '[1-9]'; then
  echo "flux helm-controller already installed and running, skipping"
  exit 0
fi

flux install --components=source-controller,helm-controller
