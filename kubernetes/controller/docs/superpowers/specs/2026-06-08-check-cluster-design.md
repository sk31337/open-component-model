# Design: hack/check-cluster.sh

**Date:** 2026-06-08  
**Status:** Approved

## Problem

When an AI agent sets up a kind cluster and applies test resources, it gets stuck waiting for resources to become ready without knowing whether they are healthy, pending, or errored. Running repeated `kubectl` commands floods the context window. A compact, cluster-wide health check script is needed that is quiet when everything is fine and precise when something is wrong.

## Goal

A single `hack/check-cluster.sh` script that:
- Sweeps every resource type currently registered on the kind cluster
- Is silent (dots only) when everything is healthy
- Prints a structured status block for any unhealthy, stuck, or errored resource
- Is easy to extend with per-group/kind custom checks

## Script location

`hack/check-cluster.sh`

## Resource discovery

Use `kubectl api-resources --verbs=list -o name` at runtime to get all listable types. Skip a hardcoded ignore-list of types that never carry meaningful status:

```
configmaps, secrets, serviceaccounts, endpoints, endpointslices,
events (v1 and events.k8s.io), leases, controllerrevisions,
rolebindings, roles, clusterroles, clusterrolebindings,
networkpolicies, podtemplates, replicationcontrollers, resourcequotas,
limitranges, priorityclasses, runtimeclasses, storageclasses,
csidrivers, csinodes, ingressclasses, ipaddresses, servicecidrs,
deviceclasses, resourceslices, resourceclaimtemplates, locks,
imageconfigs, deploymentruntimeconfigs, environmentconfigs,
componentstatuses, poddisruptionbudgets, csistoragecapacities,
volumeattributesclasses, prioritylevelconfigurations, flowschemas,
servicecidr, mutatingwebhookconfigurations, validatingwebhookconfigurations,
validatingadmissionpolicies, validatingadmissionpolicybindings,
apiservices, certificatesigningrequests, horizontalpodautoscalers,
cronjobs (status-only if active), persistentvolumes, persistentvolumeclaims,
replicasets, statefulsets, daemonsets, ingresses
```

For every non-skipped type: `kubectl get <type> -A -o json` run in background (parallel). Results collected with `wait`.

## Check dispatch table

A bash associative array maps `group/Kind` → function name. Lookup order:

1. Exact match: `group/Kind`
2. Wildcard match: `group/*`
3. Fallback: `check_generic` (conditions array only)

```bash
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
```

**To add a new check:** add one line to the table and one bash function.

## Check functions

### `check_conditions` (generic — used by most types)

Iterates `.status.conditions[]`. Flags if:
- Any condition has `status: "False"`
- `Ready` condition has `status: "Unknown"` (→ PENDING/STUCK)
- No conditions at all and resource is older than 30s (→ PENDING/STUCK)

### `check_kro_rgd`

`check_conditions` + flag if `.status.state != "Active"`.

### `check_kro_instance`

`check_conditions` + flag if `.status.state != "Ready"`.

### `check_argocd_app`

- Flag if `.status.health.status` not in `{Healthy, Progressing}`
- Flag if `.status.sync.status` not in `{Synced, OutOfSync}` (OutOfSync = warning, not error)
- Also runs `check_conditions`

### `check_deployment`

- Flag if `.status.unavailableReplicas > 0`
- Flag if `Available` condition is `False`

### `check_pod`

- Flag if `.status.phase` not in `{Running, Succeeded}`
- Flag any container with `state.waiting.reason` in `{CrashLoopBackOff, OOMKilled, Error, ImagePullBackOff, ErrImagePull}`
- Ignore system namespaces: `kube-system`, `local-path-storage` (unless `--include-system` flag set)

### `check_pvc`

- Flag if `.status.phase != "Bound"`

## Pending vs Error vs Stuck

Based on resource age (from `.metadata.creationTimestamp`) and condition status:

| Condition | Age < 2min | Age >= 2min |
|-----------|-----------|-------------|
| `Ready=Unknown` or no conditions | `[PENDING]` | `[STUCK]` |
| `Ready=False` | `[UNHEALTHY]` | `[UNHEALTHY]` |
| Error container state | `[UNHEALTHY]` | `[UNHEALTHY]` |

## Output format

**Healthy (per type):** one `.` character on a shared progress line (no newline until unhealthy or end).

**Unhealthy block:**
```
[UNHEALTHY] delivery.ocm.software/Component  default/my-component  (age: 5m30s)
  conditions:
    Ready: False — ComponentNotReady: failed to fetch component version
```

**Pending block:**
```
[PENDING] kro.run/HelmFluxcdSimple  default/helm-fluxcd-simple  (age: 45s)
  state: (not set)
  conditions: (none)
```

**Final summary:**
```
✓ 42 resources OK   ✗ 2 unhealthy   ⏳ 1 pending/stuck
```

Exit code: `0` if no UNHEALTHY resources, `1` if any UNHEALTHY found. PENDING/STUCK alone → exit `0` (still bootstrapping).

## Flags

- `--namespace <ns>` — restrict to one namespace instead of `-A`
- `--include-system` — include `kube-system`, `local-path-storage` pods
- `--verbose` — print full `.status` JSON for every resource, not just unhealthy

## Dependencies

- `kubectl` (in PATH, context already set to kind cluster)
- `jq` (for JSON parsing)
- Bash 4+ (associative arrays)
