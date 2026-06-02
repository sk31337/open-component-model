# Authoring an e2e scenario

A short, task-oriented guide for adding a new e2e scenario. For the architectural
rationale, locked decisions (Q1–Q16), and the full schema reference see
[`DESIGN.md`](./DESIGN.md).

> **Status:** implemented. The runner described here is live; all scenarios
> follow the declarative `e2e.yaml` schema. See `DESIGN.md` for the full
> architectural rationale and migration history.

## How a scenario runs

### High-level: the 30-second mental model

```mermaid
graph LR
    AUTHOR["You write e2e.yaml"] --> RUNNER["Runner executes it"]
    RUNNER --> CLUSTER["Cluster gets resources"]
    CLUSTER --> CHECK["Runner verifies state"]
```

### Mid-level: phases

```mermaid
flowchart TD
    YAML["e2e.yaml"] --> LOAD["loadScenario(): parse + substitute ${VAR}"]
    LOAD --> VAL["validate: requires, hooks, variables"]
    VAL --> REQ["install components (requires:)"]
    REQ --> PREP["transfer OCM component (prepare:)"]
    PREP --> DEP["deploy: apply manifests + waitFor"]
    DEP --> ASSERT["assert: kubectl wait + fieldEquals"]
    ASSERT --> CLEAN["cleanup: DeferCleanup deletes resources"]

    DEP -. "failure" .-> DEBUG["debug: run diagnostic kubectl commands"]
    ASSERT -. "failure" .-> DEBUG
```

### Detailed: what each phase does to the cluster

```mermaid
sequenceDiagram
    participant Author as e2e.yaml
    participant Runner as Go Runner
    participant OCM as ocm CLI
    participant K8s as kubectl
    participant Cluster as Kind Cluster

    Author->>Runner: loadScenario()
    Runner->>Runner: substituteVars(${SCENARIO_SIMPLE_NAME})
    Runner->>K8s: bash components/kro.sh (requires)
    K8s->>Cluster: helm upgrade --install kro
    Runner->>OCM: ocm add componentversions + ocm transfer ctf
    OCM->>Cluster: push to image-registry:5000
    Runner->>K8s: kubectl apply -f bootstrap.yaml (deploy)
    K8s->>Cluster: create Repository, Component, Resource, Deployer
    Cluster->>Cluster: Deployer reconciles → creates RGD
    Runner->>K8s: kubectl wait --for=condition=Ready rgd/name
    Runner->>K8s: kubectl apply -f instance.yaml
    Cluster->>Cluster: kro instance → OCIRepository → HelmRelease → Deployment
    Runner->>K8s: kubectl wait --for=condition=Available deployment
    Runner->>K8s: kubectl get -o jsonpath (fieldEquals)
    Note over Runner,Cluster: DeferCleanup: kubectl delete -f bootstrap.yaml
```

### Decision tree: what happens when things go wrong

```mermaid
flowchart TD
    START["runScenario()"] --> REQ_OK{{"requires: scripts succeed?"}}
    REQ_OK -- "no" --> FAIL1["FAIL: 'requires component X failed'"]
    REQ_OK -- "yes" --> PREP_OK{{"prepare: ocm transfer succeeds?"}}
    PREP_OK -- "no" --> FAIL2["FAIL: 'PrepareOCMComponent failed'"]
    PREP_OK -- "yes" --> DEP_OK{{"deploy: apply + waitFor succeed?"}}
    DEP_OK -- "no" --> FAIL3["FAIL: 'kubectl apply/wait failed'"]
    DEP_OK -- "yes" --> ASSERT_OK{{"assert: all resources ready?"}}
    ASSERT_OK -- "no" --> FAIL4["FAIL: 'wait condition on resource failed'"]
    ASSERT_OK -- "yes" --> PASS["PASS ✓"]

    FAIL1 --> DBG["debug: kubectl commands run"]
    FAIL2 --> DBG
    FAIL3 --> DBG
    FAIL4 --> DBG
```

## Folder structure at a glance

### High-level: two roots

```mermaid
graph LR
    A["examples/ (user-facing)"] --> R["Same runner"]
    B["test/e2e/scenarios/ (test-only)"] --> R
    R --> C["20 scenarios total"]
```

### Mid-level: family grouping

```mermaid
graph TD
    ROOT["kubernetes/controller/"] --> EX["examples/"]
    ROOT --> TEST["test/e2e/"]

    EX --> HELM["helm/"]
    EX --> KUST["kustomize/"]
    EX --> K8S["k8s-manifest/"]

    HELM --> FLUX["fluxcd/"]
    HELM --> ARGO["argocd/"]
    FLUX --> S1["simple/"]
    FLUX --> S2["nested/"]
    FLUX --> S3["configuration-localization/"]
    ARGO --> A1["simple/"]
    ARGO --> A2["nested/"]
    ARGO --> A3["configuration-localization/"]

    TEST --> SCENARIOS["scenarios/"]
    TEST --> SETUP["setup/"]
    SCENARIOS --> APP["applyset/pruning/"]
    SCENARIOS --> CRED["credentials/basic-auth/"]
    SETUP --> COMP["components/*.sh"]
    SETUP --> CL["cluster.sh"]
```

### Detailed: what's inside a scenario folder

```mermaid
graph TD
    SCENARIO["helm/fluxcd/simple/"] --> E2E["e2e.yaml — declares the test"]
    SCENARIO --> CC["component-constructor.yaml — OCM component spec"]
    SCENARIO --> BOOT["bootstrap.yaml — Repository + Component + Resource + Deployer"]
    SCENARIO --> RGD["rgd.yaml — kro ResourceGraphDefinition (deployed by Deployer)"]
    SCENARIO --> INST["instance.yaml — kro custom resource instance"]
    SCENARIO --> KEY["ocm.software (optional — private signing key)"]
    SCENARIO --> PUB["ocm.software.pub (optional — public key)"]
    SCENARIO --> OCMCFG[".ocmconfig (optional — registry credentials)"]
```

### Discovery: how the walker finds scenarios

```mermaid
flowchart TD
    ROOT["walkScenarios(root)"] --> WALK["filepath.WalkDir()"]
    WALK --> DIR{{"is directory?"}}
    DIR -- "no" --> SKIP1["skip file"]
    DIR -- "yes" --> HAS{{"contains e2e.yaml?"}}
    HAS -- "yes" --> ADD["append to found[] + SkipDir"]
    HAS -- "no" --> CONT["descend into children"]
    ADD --> SORT["sort.Strings(found)"]
    SORT --> RETURN["return found"]
```

## Local iteration workflow

### High-level: the loop

```mermaid
graph LR
    SETUP["Setup cluster"] --> TEST["Run test"] --> FIX["Fix"] --> TEST
```

### Mid-level: commands

```mermaid
flowchart LR
    DEV["Developer"] --> SETUP["task test/e2e/setup/local"]
    SETUP --> CLUSTER["Kind cluster ready"]
    CLUSTER --> RUN["task test/e2e -- scenario"]
    RUN --> PASS{{"pass?"}}
    PASS -- "yes" --> NEXT["edit code / scenario"]
    PASS -- "no" --> FIX["read debug output, fix"]
    NEXT --> RUN
    FIX --> RUN
    CLUSTER --> TEAR["task test/e2e/teardown"]
```

### Detailed: what each command does under the hood

```mermaid
flowchart TD
    subgraph "task test/e2e/setup/local"
        SL1["docker build controller image (--load)"]
        SL1 --> SL2["bash setup/local.sh"]
        SL2 --> SL3["cluster.sh: kind create + registry + RBAC"]
        SL3 --> SL4["kind load docker-image controller:latest"]
    end

    subgraph "task test/e2e -- helm/fluxcd/simple"
        TE1["helm upgrade --install controller chart/"]
        TE1 --> TE2["anchor focus: '^.*helm/fluxcd/simple$'"]
        TE2 --> TE3["go test ./test/e2e/ -ginkgo.focus=..."]
        TE3 --> TE4["Ginkgo matches 1 of 20 specs"]
        TE4 --> TE5["runScenario(cfg)"]
    end

    subgraph "task test/e2e/teardown"
        TD1["kind delete cluster"]
        TD1 --> TD2["docker rm -f image-registry"]
    end

    subgraph "task test/e2e/fresh -- scenario"
        TF1["teardown"] --> TF2["setup/local"]
        TF2 --> TF3["test/e2e -- scenario"]
    end
```

### CI vs local: side-by-side comparison

```mermaid
graph TD
    subgraph "Local Developer"
        L1["persistent kind cluster"]
        L1 --> L2["--all-components (optional)"]
        L2 --> L3["task test/e2e -- scenario (repeat)"]
        L3 --> L4["components already installed → skip"]
    end

    subgraph "CI Shard (ephemeral)"
        C1["fresh kind cluster per job"]
        C1 --> C2["runner installs requires: on demand"]
        C2 --> C3["single scenario runs"]
        C3 --> C4["cluster destroyed after job"]
    end
```

---

## Two audiences, two locations

Pick the location based on **who the scenario is for**:

| Scenario kind | Location | Discovery |
|---|---|---|
| **User-facing demo** — something a user might copy as a starting point | `kubernetes/controller/examples/<family>/<scenario>/` | Auto-discovered |
| **Test-only fixture** — exercises a corner case, edge condition, or feature whose only consumer is the test suite | `kubernetes/controller/test/e2e/scenarios/<family>/<scenario>/` | Auto-discovered |

`<family>` groups related scenarios: `helm/`, `kustomize/`, `k8s-manifest/`,
`applyset/`, `credentials/`. Within `helm/`, examples are split a second
level by delivery tool: `helm/fluxcd/<scenario>/` for Flux-only and
`helm/argocd/<scenario>/` for ArgoCD-only (each scenario uses one tool, not
both — see DESIGN.md Q5b). `kustomize/` follows the same per-tool split:
Flux variants live under `kustomize/fluxcd/<scenario>/` and ArgoCD variants
under `kustomize/argocd/<scenario>/`. The runner walks both trees and stops
descending at the first `e2e.yaml` it finds. Anything below that file is
treated as scenario-private content.

If you cannot decide: ask yourself "would a user reading the examples folder
benefit from seeing this?" If no, it belongs under `test/e2e/scenarios/`.

---

## The scenario folder

Every scenario folder contains:

```
<scenario>/
├── e2e.yaml                  # required — declares how to run the scenario
├── component-constructor.yaml # required — OCM component to build
├── bootstrap.yaml            # required — top-level resource the runner deploys
├── rgd.yaml                  # optional — kro ResourceGraphDefinition
├── instance.yaml             # optional — RGD instance
├── ocm.software / ocm.software.pub  # optional — signing keys
└── ...                       # any other files referenced from e2e.yaml
```

The runner **only reads `e2e.yaml`**. Everything else is opaque content
referenced from inside it.

---

## Naming

Two variables are exposed to your `e2e.yaml`:

- `${SCENARIO_FOLDER}` — slash-joined family + folder, e.g. `helm/fluxcd/simple`.
  Used for log lines and Ginkgo spec descriptions.
- `${SCENARIO_SIMPLE_NAME}` — full path with `/` replaced by `-`, e.g.
  `helm-fluxcd-simple`. Safe to use in Kubernetes resource names. Use this
  anywhere a name lands on the cluster.

Plus everything in the **fixed variable list** (see DESIGN.md §"Templated
variables"): `${IMAGE_REGISTRY}`, `${IMAGE_REGISTRY_HOST}`,
`${CONTROLLER_NAMESPACE}`, `${PROTECTED_REGISTRY_BASIC_AUTH}`,
`${PROTECTED_REGISTRY_DOCKER_CONFIG_JSON}`, `${SCENARIO_DIR}`.

No Go templates. No `{{ ... }}`. Only `${VAR}` envsubst-style substitution
against the fixed list. Unknown variables are a hard error at parse time.

---

## Minimal `e2e.yaml`

Smallest viable scenario — bootstrap a kro RGD, wait for a deployment:

```yaml
apiVersion: e2e.ocm.software/v1
kind: Scenario

requires:
  - kro
  - flux-source
  - flux-helm

prepare:
  components:
    - constructor: component-constructor.yaml

deploy:
  - apply: bootstrap.yaml
  - waitFor:
      kind: rgd
      name: ${SCENARIO_SIMPLE_NAME}
      conditions: [create, condition=Ready=true]
  - apply: instance.yaml

assert:
  resources:
    - kind: deployment.apps
      name: ${SCENARIO_SIMPLE_NAME}-podinfo
      waitFor: [create, condition=Available]
```

That's the whole file. The runner handles ordering, OCM transfer, namespace
scoping, log dumping on failure, and cleanup.

---

## Adding behaviour the schema does not cover

If a scenario needs imperative work — generate a secret, mutate a CR mid-flight,
verify a side-effect that is not a Kubernetes resource — **do not** add fields
to `e2e.yaml`. Instead, write a hook.

```yaml
preDeployHooks:
  - createBasicAuthSecret
postAssertHooks:
  - verifySignedComponent
```

Each name must exist in `kubernetes/controller/test/e2e/hooks/registry.go`.
Hooks run in array order. The six phases — `preDeployHooks`, `postDeployHooks`,
`preAssertHooks`, `postAssertHooks`, `preCleanupHooks`, `postCleanupHooks` — are
documented in DESIGN.md.

A hook is a Go function:

```go
func createBasicAuthSecret(ctx context.Context, s *hooks.Scenario) error {
    // s.Folder, s.SimpleName, s.Dir
}
```

Adding a hook is a code change. Reviewers will push back on hooks that
duplicate something `assert.resources` or `assert.fieldEquals` could express.

---

## Picking a delivery tool for helm scenarios

Helm scenarios pick exactly one delivery tool — Flux or ArgoCD — based on
their folder:

| Folder | Delivery tool | `requires:` | rgd.yaml resources |
|---|---|---|---|
| `examples/helm/fluxcd/<name>/` | Flux | `kro`, `flux-source`, `flux-helm` | `Resource` → `OCIRepository` → `HelmRelease` |
| `examples/helm/argocd/<name>/` | ArgoCD | `kro`, `argocd` | `Resource` → `Application` |

A Flux scenario asserts the Flux-managed deployment:

```yaml
assert:
  resources:
    - kind: deployment.apps
      name: ${SCENARIO_SIMPLE_NAME}-podinfo
      waitFor: [create, condition=Available]
      pods:
        selector: app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo
        condition: condition=Ready=true
```

An ArgoCD scenario asserts both the `Application`'s sync/health status and
the ArgoCD-managed deployment in `default-argocd`:

```yaml
assert:
  resources:
    - kind: applications.argoproj.io
      name: ${SCENARIO_SIMPLE_NAME}
      namespace: argocd
      waitFor:
        - create
        - jsonpath={.status.sync.status}=Synced
        - jsonpath={.status.health.status}=Healthy
    - kind: deployment.apps
      name: ${SCENARIO_SIMPLE_NAME}-podinfo
      namespace: default-argocd
      waitFor: [create, condition=Available]
      pods:
        selector: app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo
        condition: condition=Ready=true
```

ArgoCD-managed releases use the suffix `-argocd` to avoid colliding with the
Flux release name; the namespace is `default-argocd`. See
`examples/helm/fluxcd/simple/` and `examples/helm/argocd/simple/` for the
canonical wiring of each tool.

If you need a side-by-side parity demo (both tools deploying the same chart),
that belongs under `test/e2e/scenarios/helm/parity/`, not under `examples/`
(see DESIGN.md Q5b).

---

## When the OCM component must be signed

Place the private key alongside `component-constructor.yaml` as
`ocm.software`, and the public key as `ocm.software.pub`. The runner detects
the keys by name and signs/verifies automatically — no `e2e.yaml` field
required.

If signature behaviour is the *thing being tested*, write a hook that flips
the public key, re-runs the relevant resource transfer, and asserts the
controller error condition. Don't try to express that in YAML.

---

## Cleanup

By default the runner deletes only the resources it deployed. If your scenario
needs the full OCM-managed graph torn down (component → resource → release),
opt in:

```yaml
cleanup:
  cascadeFromBootstrap: true
```

This is opt-in because OCM cleanup cascade is itself a behaviour worth testing
deliberately, not a side-effect every scenario should pay for.

---

## Diagnostics on failure

When a scenario fails, the runner executes kubectl commands declared in `debug:`.
If omitted, a default set runs (controller pods/logs, kro pods/events, RGD
conditions). Override it to add scenario-specific diagnostics:

```yaml
debug:
  - kubectl: get pods -n argocd -o wide
    label: argocd-pods
  - kubectl: get applications.argoproj.io -A -o wide
    label: argocd-apps
  - kubectl: logs -n ${CONTROLLER_NAMESPACE} deploy/ocm-k8s-toolkit-controller-manager --tail=80
    label: controller-logs
```

Each `kubectl:` value is passed directly to `kubectl` (split on whitespace).
`label:` is optional — used to group output lines in the log.

---

## Local iteration

```sh
# Teardown + fresh cluster + run one scenario (full clean slate)
task kubernetes/controller:test/e2e/fresh -- helm/fluxcd/simple

# Provision a fresh kind cluster (components installed on demand by the runner)
task kubernetes/controller:test/e2e/setup/local

# Or: pre-install all components for fast repeated focused runs
task kubernetes/controller:test/e2e/setup/local -- --all-components

# Run everything
task kubernetes/controller:test/e2e

# Run one scenario (exact match — won't run nested-signed when you say nested)
task kubernetes/controller:test/e2e -- helm/fluxcd/simple

# Tear down the cluster and registry when done
task kubernetes/controller:test/e2e/teardown
```

The scenario name passed via `--` is matched exactly (anchored). The local
cluster is persistent across runs; CI uses the default (cluster only) so each
shard only installs the components its scenario declares in `requires:`.
See DESIGN.md §"Operator UX" for the full command table.

---

## Checklist before opening a PR

- [ ] Scenario folder is in the correct audience location.
- [ ] `e2e.yaml` parses (`task kubernetes/controller:test/e2e -- --focus="<scenario>" --dry-run`).
- [ ] Every `${VAR}` is in the fixed variable list.
- [ ] Every hook name resolves at `BeforeSuite`.
- [ ] Local run passes from a fresh kind cluster.
- [ ] If user-facing: scenario shows up in `examples/README.md` family table.
- [ ] If test-only: brief comment at the top of `e2e.yaml` saying *what corner
      case* this exists to cover.

---

## Where to read more

- [`DESIGN.md`](./DESIGN.md) — full schema, locked decisions, migration plan.
- `hooks/registry.go` — list of named hooks (once Stage 1 lands).
- `examples/helm/fluxcd/simple/` — canonical Flux helm reference.
- `examples/helm/argocd/simple/` — canonical ArgoCD helm reference.
- `test/e2e/scenarios/applyset/pruning/` — canonical test-only reference.
