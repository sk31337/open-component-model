# check-cluster.sh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `hack/check-cluster.sh` — a cluster-wide Kubernetes health check script that is silent when everything is healthy and prints structured status blocks for any unhealthy, pending, or stuck resource.

**Architecture:** A single bash script that discovers all listable resource types via `kubectl api-resources`, fetches each type in parallel with `kubectl get -A -o json`, then dispatches each resource instance through a `group/Kind`-keyed checker function table. Checkers emit output only on problems; the main loop prints dots for healthy types.

**Tech Stack:** Bash 4+ (associative arrays), `kubectl`, `jq`

---

## File Structure

| File | Action | Purpose |
|------|--------|---------|
| `hack/check-cluster.sh` | Create | The script |

---

### Task 1: Script skeleton, flag parsing, dependency checks

**Files:**
- Create: `hack/check-cluster.sh`

- [ ] **Step 1: Create the file with shebang, strict mode, flag parsing, and dependency check**

```bash
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
```

- [ ] **Step 2: Make executable and verify it runs cleanly**

```bash
chmod +x hack/check-cluster.sh
./hack/check-cluster.sh --help 2>&1 || true
./hack/check-cluster.sh
```

Expected: exits with "Unknown flag: --help" error (acceptable — we have no --help yet), and `./hack/check-cluster.sh` exits 0 with no output (nothing to check yet).

- [ ] **Step 3: Commit**

```bash
git add hack/check-cluster.sh
git commit -m "feat(hack): add check-cluster.sh skeleton with flag parsing"
```

---

### Task 2: Age helper and PENDING/STUCK/UNHEALTHY classification utilities

**Files:**
- Modify: `hack/check-cluster.sh`

These helpers are used by every checker function. Define them before the checker dispatch table.

- [ ] **Step 1: Add the age-in-seconds helper and the result-emitter functions**

Append to `hack/check-cluster.sh` after the dependency check:

```bash
# ---------------------------------------------------------------------------
# Globals for result accumulation
# ---------------------------------------------------------------------------
RESULT_OK=0
RESULT_UNHEALTHY=0
RESULT_PENDING=0

# Temp dir for parallel job output — cleaned up on exit
TMPDIR_RESULTS="$(mktemp -d)"
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
```

- [ ] **Step 2: Verify the script still runs cleanly**

```bash
./hack/check-cluster.sh
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add hack/check-cluster.sh
git commit -m "feat(hack): add age helpers and result emitters to check-cluster.sh"
```

---

### Task 3: `check_conditions` — the generic conditions-array checker

**Files:**
- Modify: `hack/check-cluster.sh`

This is the most-used checker. It receives a single resource JSON object on stdin.

- [ ] **Step 1: Add `check_conditions` function**

Append to `hack/check-cluster.sh`:

```bash
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
```

- [ ] **Step 2: Verify the script still runs cleanly**

```bash
./hack/check-cluster.sh
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add hack/check-cluster.sh
git commit -m "feat(hack): add check_conditions function to check-cluster.sh"
```

---

### Task 4: Specialized checker functions

**Files:**
- Modify: `hack/check-cluster.sh`

- [ ] **Step 1: Add `check_kro_rgd`, `check_kro_instance`, `check_argocd_app`, `check_deployment`, `check_pod`, `check_pvc`**

Append to `hack/check-cluster.sh`:

```bash
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
```

- [ ] **Step 2: Verify the script still runs cleanly**

```bash
./hack/check-cluster.sh
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add hack/check-cluster.sh
git commit -m "feat(hack): add specialized checker functions to check-cluster.sh"
```

---

### Task 5: Checker dispatch table

**Files:**
- Modify: `hack/check-cluster.sh`

- [ ] **Step 1: Add the `CHECKER` associative array and `resolve_checker` function**

Append to `hack/check-cluster.sh`:

```bash
# ---------------------------------------------------------------------------
# Checker dispatch table: "group/Kind" -> function name
# group is the API group (empty string for core/v1 resources -> use "v1")
# Wildcard: "group/*" matches any Kind in that group.
# ---------------------------------------------------------------------------
declare -A CHECKER

CHECKER["delivery.ocm.software/Component"]="check_conditions"
CHECKER["delivery.ocm.software/Repository"]="check_conditions"
CHECKER["delivery.ocm.software/Resource"]="check_conditions"
CHECKER["delivery.ocm.software/Deployer"]="check_conditions"
CHECKER["helm.toolkit.fluxcd.io/HelmRelease"]="check_conditions"
CHECKER["source.toolkit.fluxcd.io/OCIRepository"]="check_conditions"
CHECKER["source.toolkit.fluxcd.io/HelmChart"]="check_conditions"
CHECKER["source.toolkit.fluxcd.io/GitRepository"]="check_conditions"
CHECKER["source.toolkit.fluxcd.io/HelmRepository"]="check_conditions"
CHECKER["source.toolkit.fluxcd.io/Bucket"]="check_conditions"
CHECKER["source.toolkit.fluxcd.io/ExternalArtifact"]="check_conditions"
CHECKER["kustomize.toolkit.fluxcd.io/Kustomization"]="check_conditions"
CHECKER["kro.run/ResourceGraphDefinition"]="check_kro_rgd"
CHECKER["kro.run/*"]="check_kro_instance"
CHECKER["internal.kro.run/GraphRevision"]="check_conditions"
CHECKER["argoproj.io/Application"]="check_argocd_app"
CHECKER["argoproj.io/ApplicationSet"]="check_conditions"
CHECKER["apps/Deployment"]="check_deployment"
CHECKER["v1/Pod"]="check_pod"
CHECKER["v1/Node"]="check_conditions"
CHECKER["v1/PersistentVolumeClaim"]="check_pvc"
CHECKER["pkg.crossplane.io/*"]="check_conditions"
CHECKER["apiextensions.crossplane.io/*"]="check_conditions"
CHECKER["kubernetes.crossplane.io/*"]="check_conditions"
CHECKER["kubernetes.m.crossplane.io/*"]="check_conditions"
CHECKER["protection.crossplane.io/*"]="check_conditions"
CHECKER["ops.crossplane.io/*"]="check_conditions"

# ---------------------------------------------------------------------------
# resolve_checker <group> <kind>
# Prints the function name to call. Order: exact, wildcard, fallback.
# ---------------------------------------------------------------------------
resolve_checker() {
  local group="$1" kind="$2"
  local key="${group}/${kind}"
  local wildcard="${group}/*"

  if [[ -n "${CHECKER[$key]+_}" ]]; then
    echo "${CHECKER[$key]}"
  elif [[ -n "${CHECKER[$wildcard]+_}" ]]; then
    echo "${CHECKER[$wildcard]}"
  else
    echo "check_conditions"
  fi
}
```

- [ ] **Step 2: Verify the script still runs cleanly**

```bash
./hack/check-cluster.sh
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add hack/check-cluster.sh
git commit -m "feat(hack): add checker dispatch table to check-cluster.sh"
```

---

### Task 6: Resource type discovery, ignore-list, and parallel fetch

**Files:**
- Modify: `hack/check-cluster.sh`

- [ ] **Step 1: Add the ignore-list and `should_skip` function**

Append to `hack/check-cluster.sh`:

```bash
# ---------------------------------------------------------------------------
# Resource types to skip — no meaningful status to check.
# Keys are the plural resource names as returned by kubectl api-resources.
# ---------------------------------------------------------------------------
declare -A SKIP_TYPES
for t in \
  configmaps secrets serviceaccounts endpoints endpointslices \
  events leases controllerrevisions \
  rolebindings roles clusterroles clusterrolebindings \
  networkpolicies podtemplates replicationcontrollers resourcequotas \
  limitranges priorityclasses runtimeclasses storageclasses \
  csidrivers csinodes ingressclasses ipaddresses servicecidrs \
  deviceclasses resourceslices resourceclaimtemplates locks \
  imageconfigs deploymentruntimeconfigs environmentconfigs \
  componentstatuses poddisruptionbudgets csistoragecapacities \
  volumeattributesclasses prioritylevelconfigurations flowschemas \
  mutatingwebhookconfigurations validatingwebhookconfigurations \
  validatingadmissionpolicies validatingadmissionpolicybindings \
  apiservices certificatesigningrequests horizontalpodautoscalers \
  persistentvolumes replicasets statefulsets daemonsets ingresses \
  cronjobs namespaces nodes \
  providerconfigusages usages appprojects \
  compositionrevisions; do
  SKIP_TYPES["$t"]=1
done

# ---------------------------------------------------------------------------
# discover_resource_types
# Prints lines of "name apiVersion kind" for each non-skipped listable type.
# ---------------------------------------------------------------------------
discover_resource_types() {
  kubectl api-resources --verbs=list -o wide --no-headers 2>/dev/null \
  | awk '{print $1, $3, $5}' \
  | while read -r name apiversion kind; do
      if [[ -z "${SKIP_TYPES[$name]+_}" ]]; then
        echo "$name $apiversion $kind"
      fi
    done
}
```

- [ ] **Step 2: Add `check_resource_type` — fetches one type and runs checker per instance**

Append to `hack/check-cluster.sh`:

```bash
# ---------------------------------------------------------------------------
# check_resource_type <name> <apiVersion> <kind>
# Fetches all instances of a resource type and runs the appropriate checker.
# Writes output to a temp file (called from background jobs).
# ---------------------------------------------------------------------------
check_resource_type() {
  local resname="$1" apiversion="$2" kind="$3"

  # Derive group from apiVersion (e.g. "delivery.ocm.software/v1alpha1" -> "delivery.ocm.software")
  # Core resources have apiVersion "v1" -> group is "v1" for dispatch purposes
  local group
  if [[ "$apiversion" == *"/"* ]]; then
    group="${apiversion%/*}"
  else
    group="$apiversion"  # "v1"
  fi

  local checker
  checker="$(resolve_checker "$group" "$kind")"

  local json
  json="$(kubectl get "$resname" ${NAMESPACE_FLAG} -o json 2>/dev/null)" || return 0

  local count
  count="$(echo "$json" | jq '.items | length')"
  (( count == 0 )) && return 0

  local dot_printed=false
  while IFS= read -r item; do
    local ns item_name ts
    ns="$(echo "$item" | jq -r '.metadata.namespace // "cluster"')"
    item_name="$(echo "$item" | jq -r '.metadata.name')"
    ts="$(echo "$item" | jq -r '.metadata.creationTimestamp')"

    echo "$item" | "$checker" "${group}/${kind}" "$ns" "$item_name" "$ts"
    dot_printed=true
  done < <(echo "$json" | jq -c '.items[]')

  # Emit a dot token to the progress pipe if we processed any instances
  [[ "$dot_printed" == "true" ]] && echo "DOT"
}
```

- [ ] **Step 3: Verify the script still runs cleanly**

```bash
./hack/check-cluster.sh
```

Expected: exits 0, no output.

- [ ] **Step 4: Commit**

```bash
git add hack/check-cluster.sh
git commit -m "feat(hack): add resource discovery and parallel fetch to check-cluster.sh"
```

---

### Task 7: Main loop, parallel execution, progress dots, summary, exit code

**Files:**
- Modify: `hack/check-cluster.sh`

- [ ] **Step 1: Add the main execution loop**

Append to `hack/check-cluster.sh`:

```bash
# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
  local types
  types="$(discover_resource_types)"

  if [[ -z "$types" ]]; then
    echo "No resource types found (is kubectl connected to a cluster?)" >&2
    exit 1
  fi

  # Run each resource type check in parallel; collect output files
  local job_outputs=()
  while IFS= read -r line; do
    local resname apiversion kind
    read -r resname apiversion kind <<< "$line"
    local outfile
    outfile="$(mktemp "$TMPDIR_RESULTS/XXXXXX")"
    job_outputs+=("$outfile")
    check_resource_type "$resname" "$apiversion" "$kind" > "$outfile" 2>&1 &
  done <<< "$types"

  # Wait for all background jobs
  wait

  # Process output files: print dots for healthy types, print problem blocks
  printf 'Checking: '
  for outfile in "${job_outputs[@]}"; do
    local content
    content="$(cat "$outfile")"
    if [[ -z "$content" ]]; then
      continue
    fi
    # Count dots (healthy types that had instances)
    local dots
    dots="$(echo "$content" | grep -c '^DOT$' || true)"
    (( RESULT_OK += dots )) || true

    # Print non-DOT lines (problem blocks)
    local problems
    problems="$(echo "$content" | grep -v '^DOT$')"
    if [[ -n "$problems" ]]; then
      echo "$problems"
    else
      printf '.'
    fi
  done
  echo  # end the dots line

  # Summary
  echo
  printf '✓ %d resources OK   ✗ %d unhealthy   ⏳ %d pending/stuck\n' \
    "$RESULT_OK" "$RESULT_UNHEALTHY" "$RESULT_PENDING"

  if (( RESULT_UNHEALTHY > 0 )); then
    exit 1
  fi
  exit 0
}

main
```

- [ ] **Step 2: Smoke-test against the kind cluster (context must be kind-kind)**

```bash
kubectl config use-context kind-kind
./hack/check-cluster.sh
```

Expected: runs without error, prints dots then summary line. No crash.

- [ ] **Step 3: Smoke-test with --verbose**

```bash
./hack/check-cluster.sh --verbose 2>&1 | head -60
```

Expected: prints `[OK]` blocks with `.status` JSON for healthy resources.

- [ ] **Step 4: Verify exit code is 0 on a healthy cluster**

```bash
./hack/check-cluster.sh; echo "exit: $?"
```

Expected: `exit: 0`

- [ ] **Step 5: Commit**

```bash
git add hack/check-cluster.sh
git commit -m "feat(hack): add main loop, parallel execution, and summary to check-cluster.sh"
```

---

### Task 8: Wire into Taskfile and final polish

**Files:**
- Modify: `Taskfile.yml`

- [ ] **Step 1: Add a `check-cluster` task to the Taskfile**

Find the `test/e2e` section in `Taskfile.yml` and add the new task nearby:

```yaml
  check-cluster:
    desc: "Check the health of all resources on the current kubectl cluster context. Flags: --include-system, --verbose, --namespace <ns>"
    cmds:
      - 'bash {{.TASKFILE_DIR}}/hack/check-cluster.sh {{.CLI_ARGS}}'
```

- [ ] **Step 2: Verify the Taskfile task works**

```bash
task check-cluster
```

Expected: same output as running the script directly.

- [ ] **Step 3: Commit**

```bash
git add Taskfile.yml
git commit -m "feat(hack): add check-cluster task to Taskfile"
```

---

## Self-Review

**Spec coverage:**
- ✓ Script at `hack/check-cluster.sh`
- ✓ `kubectl api-resources` discovery with ignore-list
- ✓ Parallel fetch with background jobs + `wait`
- ✓ `group/Kind` dispatch table (exact, wildcard, fallback)
- ✓ `check_conditions` — False/Unknown/no-conditions
- ✓ `check_kro_rgd` — conditions + `state != Active`
- ✓ `check_kro_instance` — conditions + `state != Ready`
- ✓ `check_argocd_app` — health.status + sync.status + conditions
- ✓ `check_deployment` — unavailableReplicas + Available condition
- ✓ `check_pod` — phase + container error states + system namespace skip
- ✓ `check_pvc` — phase != Bound
- ✓ PENDING (<2min), STUCK (≥2min), UNHEALTHY classification
- ✓ Output: dots for healthy, blocks for problems
- ✓ Final summary line with counts
- ✓ Exit 0 = no UNHEALTHY, exit 1 = UNHEALTHY found
- ✓ `--namespace`, `--include-system`, `--verbose` flags
- ✓ Taskfile task

**Placeholder scan:** none found.

**Type consistency:** all function names used in the CHECKER table (`check_conditions`, `check_kro_rgd`, `check_kro_instance`, `check_argocd_app`, `check_deployment`, `check_pod`, `check_pvc`) are defined in Tasks 3–4. `resolve_checker` defined in Task 5 and called in Task 6. `age_seconds`/`age_human`/`emit_*` defined in Task 2 and used in Tasks 3–4. All consistent.
