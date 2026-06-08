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

# ---------------------------------------------------------------------------
# check_conditions <group/Kind> <namespace> <name> <creationTimestamp>
# Reads resource JSON from stdin.
# Returns 0 = healthy, 1 = unhealthy, 2 = pending/stuck.
# ---------------------------------------------------------------------------
check_conditions() {
  local gk="$1" ns="$2" name="$3" ts="$4"
  local ref="${ns}/${name}"
  local age
  age="$(age_seconds "$ts")"
  local json
  json="$(cat)"

  local conditions
  conditions="$(echo "$json" | jq -c '.status.conditions // []')"

  local n_conditions
  n_conditions="$(echo "$conditions" | jq 'length')"

  # No conditions at all
  if (( n_conditions == 0 )); then
    if (( age < 30 )); then
      return 0  # too young to judge
    elif (( age < 120 )); then
      emit_pending "$gk" "$ref" "$age" "conditions: (none yet)"
      return 2
    else
      emit_stuck "$gk" "$ref" "$age" "conditions: (none — resource may not be reconciling)"
      return 2
    fi
  fi

  local bad_lines=()
  local pending_lines=()
  local healthy=true

  while IFS= read -r cond; do
    local ctype cstatus creason cmsg
    ctype="$(echo "$cond" | jq -r '.type')"
    cstatus="$(echo "$cond" | jq -r '.status')"
    creason="$(echo "$cond" | jq -r '.reason // ""')"
    cmsg="$(echo "$cond" | jq -r '.message // ""')"

    if [[ "$cstatus" == "False" ]]; then
      healthy=false
      bad_lines+=("conditions:" "  ${ctype}: False — ${creason}: ${cmsg}")
    elif [[ "$cstatus" == "Unknown" && "$ctype" == "Ready" ]]; then
      healthy=false
      if (( age < 120 )); then
        pending_lines+=("conditions:" "  ${ctype}: Unknown — ${creason}: ${cmsg}")
      else
        pending_lines+=("conditions:" "  ${ctype}: Unknown (stuck) — ${creason}: ${cmsg}")
      fi
    fi
  done < <(echo "$conditions" | jq -c '.[]')

  if [[ "${#bad_lines[@]}" -gt 0 ]]; then
    emit_unhealthy "$gk" "$ref" "$age" "${bad_lines[@]}"
    return 1
  fi

  if [[ "${#pending_lines[@]}" -gt 0 ]]; then
    if (( age < 120 )); then
      emit_pending "$gk" "$ref" "$age" "${pending_lines[@]}"
    else
      emit_stuck "$gk" "$ref" "$age" "${pending_lines[@]}"
    fi
    return 2
  fi

  if [[ "$VERBOSE" == "true" ]]; then
    printf '\n[OK] %s  %s  (age: %s)\n' "$gk" "$ref" "$(age_human "$age")"
    echo "$json" | jq '.status'
  fi

  return 0
}

# ---------------------------------------------------------------------------
# check_kro_rgd — ResourceGraphDefinition: conditions + state must be Active
# ---------------------------------------------------------------------------
check_kro_rgd() {
  local gk="$1" ns="$2" name="$3" ts="$4"
  local json
  json="$(cat)"
  local age
  age="$(age_seconds "$ts")"
  local ref="${ns}/${name}"

  local state
  state="$(echo "$json" | jq -r '.status.state // ""')"

  echo "$json" | check_conditions "$gk" "$ns" "$name" "$ts"
  local rc=$?

  if [[ "$state" != "Active" && "$state" != "" ]]; then
    if (( rc == 0 )); then
      if (( age < 120 )); then
        emit_pending "$gk" "$ref" "$age" "state: ${state} (expected: Active)"
      else
        emit_stuck "$gk" "$ref" "$age" "state: ${state} (expected: Active)"
      fi
    fi
  fi
}

# ---------------------------------------------------------------------------
# check_kro_instance — kro-managed instances: conditions + state must be Ready
# ---------------------------------------------------------------------------
check_kro_instance() {
  local gk="$1" ns="$2" name="$3" ts="$4"
  local json
  json="$(cat)"
  local age
  age="$(age_seconds "$ts")"
  local ref="${ns}/${name}"

  local state
  state="$(echo "$json" | jq -r '.status.state // ""')"

  echo "$json" | check_conditions "$gk" "$ns" "$name" "$ts"
  local rc=$?

  if [[ -n "$state" && "$state" != "Ready" ]]; then
    if (( rc == 0 )); then
      if (( age < 120 )); then
        emit_pending "$gk" "$ref" "$age" "state: ${state} (expected: Ready)"
      else
        emit_stuck "$gk" "$ref" "$age" "state: ${state} (expected: Ready)"
      fi
    fi
  fi
}

# ---------------------------------------------------------------------------
# check_argocd_app — health.status + sync.status + conditions
# ---------------------------------------------------------------------------
check_argocd_app() {
  local gk="$1" ns="$2" name="$3" ts="$4"
  local json
  json="$(cat)"
  local age
  age="$(age_seconds "$ts")"
  local ref="${ns}/${name}"

  local health sync
  health="$(echo "$json" | jq -r '.status.health.status // ""')"
  sync="$(echo "$json"   | jq -r '.status.sync.status // ""')"

  echo "$json" | check_conditions "$gk" "$ns" "$name" "$ts"

  local lines=()
  if [[ -n "$health" && "$health" != "Healthy" && "$health" != "Progressing" ]]; then
    lines+=("health: ${health}")
  fi
  # OutOfSync is a warning, not an error — still report when not Synced
  if [[ -n "$sync" && "$sync" != "Synced" ]]; then
    lines+=("sync: ${sync}")
  fi

  if [[ "${#lines[@]}" -gt 0 ]]; then
    emit_unhealthy "$gk" "$ref" "$age" "${lines[@]}"
  fi
}

# ---------------------------------------------------------------------------
# check_deployment — unavailableReplicas + Available condition
# ---------------------------------------------------------------------------
check_deployment() {
  local gk="$1" ns="$2" name="$3" ts="$4"
  local json
  json="$(cat)"
  local age
  age="$(age_seconds "$ts")"
  local ref="${ns}/${name}"

  local unavail
  unavail="$(echo "$json" | jq -r '.status.unavailableReplicas // 0')"

  echo "$json" | check_conditions "$gk" "$ns" "$name" "$ts"

  if (( unavail > 0 )); then
    local desired
    desired="$(echo "$json" | jq -r '.spec.replicas // 1')"
    emit_unhealthy "$gk" "$ref" "$age" \
      "unavailableReplicas: ${unavail} / ${desired}"
  fi
}

# ---------------------------------------------------------------------------
# check_pod — phase + container error states
# System namespace pods skipped unless --include-system
# ---------------------------------------------------------------------------
check_pod() {
  local gk="$1" ns="$2" name="$3" ts="$4"

  if [[ "$INCLUDE_SYSTEM" == "false" ]]; then
    case "$ns" in
      kube-system|local-path-storage|kube-node-lease|kube-public) return 0 ;;
    esac
  fi

  local json
  json="$(cat)"
  local age
  age="$(age_seconds "$ts")"
  local ref="${ns}/${name}"

  local phase
  phase="$(echo "$json" | jq -r '.status.phase // ""')"

  if [[ "$phase" == "Succeeded" ]]; then
    return 0
  fi

  if [[ "$phase" != "Running" && -n "$phase" ]]; then
    if (( age < 120 )); then
      emit_pending "$gk" "$ref" "$age" "phase: ${phase}"
    else
      emit_stuck "$gk" "$ref" "$age" "phase: ${phase}"
    fi
    return 2
  fi

  # Check container states for error conditions
  local bad_containers
  bad_containers="$(echo "$json" | jq -r '
    .status.containerStatuses[]? |
    select(.state.waiting.reason != null) |
    select(.state.waiting.reason | IN(
      "CrashLoopBackOff","OOMKilled","Error",
      "ImagePullBackOff","ErrImagePull","CreateContainerConfigError"
    )) |
    "\(.name): \(.state.waiting.reason) — \(.state.waiting.message // "")"
  ')"

  if [[ -n "$bad_containers" ]]; then
    local lines=()
    while IFS= read -r line; do
      lines+=("container: $line")
    done <<< "$bad_containers"
    emit_unhealthy "$gk" "$ref" "$age" "${lines[@]}"
    return 1
  fi

  return 0
}

# ---------------------------------------------------------------------------
# check_pvc — phase must be Bound
# ---------------------------------------------------------------------------
check_pvc() {
  local gk="$1" ns="$2" name="$3" ts="$4"
  local json
  json="$(cat)"
  local age
  age="$(age_seconds "$ts")"
  local ref="${ns}/${name}"

  local phase
  phase="$(echo "$json" | jq -r '.status.phase // ""')"

  if [[ "$phase" != "Bound" && -n "$phase" ]]; then
    if (( age < 120 )); then
      emit_pending "$gk" "$ref" "$age" "phase: ${phase} (expected: Bound)"
    else
      emit_stuck "$gk" "$ref" "$age" "phase: ${phase} (expected: Bound)"
    fi
    return 2
  fi

  return 0
}
