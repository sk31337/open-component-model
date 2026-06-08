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

# ---------------------------------------------------------------------------
# Globals for result accumulation
# ---------------------------------------------------------------------------
RESULT_OK=0
RESULT_UNHEALTHY=0
RESULT_PENDING=0

# Temp dir for parallel job output — cleaned up on exit
TMPDIR_RESULTS="${TMPDIR:-/tmp}/check-cluster-$$"
mkdir -p "$TMPDIR_RESULTS" || { echo "ERROR: Could not create temp directory" >&2; exit 1; }
trap 'rm -rf "$TMPDIR_RESULTS"' EXIT

# ---------------------------------------------------------------------------
# age_seconds <creationTimestamp>
# Returns the age of a resource in seconds.
# ---------------------------------------------------------------------------
age_seconds() {
  local ts="$1"
  local created
  created="$(date -d "$ts" +%s 2>/dev/null || date -j -f "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null || echo 0)"
  echo $(( $(date +%s) - created ))
}

# ---------------------------------------------------------------------------
# age_human <seconds>
# Returns a human-readable age string like "2m30s".
# ---------------------------------------------------------------------------
age_human() {
  local secs="$1"
  if (( secs < 60 )); then
    echo "${secs}s"
  elif (( secs < 3600 )); then
    echo "$(( secs / 60 ))m$(( secs % 60 ))s"
  else
    echo "$(( secs / 3600 ))h$(( (secs % 3600) / 60 ))m"
  fi
}

# ---------------------------------------------------------------------------
# emit_unhealthy <group/Kind> <namespace/name> <age_secs> <detail_lines...>
# emit_pending   <group/Kind> <namespace/name> <age_secs> <detail_lines...>
# emit_stuck     <group/Kind> <namespace/name> <age_secs> <detail_lines...>
# ---------------------------------------------------------------------------
emit_unhealthy() {
  local gk="$1" ref="$2" age_secs="$3"; shift 3
  (( RESULT_UNHEALTHY++ )) || true
  printf '\n[UNHEALTHY] %s  %s  (age: %s)\n' "$gk" "$ref" "$(age_human "$age_secs")"
  for line in "$@"; do printf '  %s\n' "$line"; done
}

emit_pending() {
  local gk="$1" ref="$2" age_secs="$3"; shift 3
  (( RESULT_PENDING++ )) || true
  printf '\n[PENDING] %s  %s  (age: %s)\n' "$gk" "$ref" "$(age_human "$age_secs")"
  for line in "$@"; do printf '  %s\n' "$line"; done
}

emit_stuck() {
  local gk="$1" ref="$2" age_secs="$3"; shift 3
  (( RESULT_PENDING++ )) || true
  printf '\n[STUCK] %s  %s  (age: %s)\n' "$gk" "$ref" "$(age_human "$age_secs")"
  for line in "$@"; do printf '  %s\n' "$line"; done
}
