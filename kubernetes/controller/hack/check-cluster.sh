#!/usr/bin/env bash
# Cluster-wide health check for a kind (or any) Kubernetes cluster.
# Quiet when healthy; prints structured blocks for unhealthy/pending/stuck resources.
# Usage: hack/check-cluster.sh [--namespace <ns>] [--include-system] [--verbose]
set -euo pipefail

NAMESPACE_FLAG="-A"
INCLUDE_SYSTEM=false
VERBOSE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace) NAMESPACE_FLAG="-n $2"; shift 2 ;;
    --include-system) INCLUDE_SYSTEM=true; shift ;;
    --verbose) VERBOSE=true; shift ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

for dep in kubectl jq; do
  if ! command -v "$dep" &>/dev/null; then
    echo "ERROR: $dep is required but not found in PATH" >&2
    exit 1
  fi
done
