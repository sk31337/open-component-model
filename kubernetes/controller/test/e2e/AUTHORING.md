# Authoring an e2e scenario

A short, task-oriented guide for adding a new e2e scenario. For the architectural
rationale, locked decisions (Q1–Q16), and the full schema reference see
[`DESIGN.md`](./DESIGN.md).

> **Status:** target state. The runner described here is not yet implemented;
> the layout below is what you should build new scenarios against once Stage 1
> of the migration plan in `DESIGN.md` lands. Until then, follow the legacy
> `e2e_*_test.go` patterns.

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
both — see DESIGN.md Q5b). `kustomize/` follows the same per-tool split: the
Flux variants currently sit flat at `kustomize/<scenario>/` (legacy layout)
and the ArgoCD variants live under `kustomize/argocd/<scenario>/`. The
runner walks both trees and stops descending at the first `e2e.yaml` it
finds. Anything below that file is treated as scenario-private content.

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
variables"): `${IMAGE_REGISTRY}`, `${SIGNING_KEY}`, `${SIGNING_PUBKEY}`,
`${TIMEOUT}`, `${E2E_NAMESPACE}`.

No Go templates. No `{{ ... }}`. Only `${VAR}` envsubst-style substitution
against the fixed list. Unknown variables are a hard error at parse time.

---

## Minimal `e2e.yaml`

Smallest viable scenario — bootstrap a kro RGD, wait for a deployment:

```yaml
apiVersion: e2e.ocm.software/v1
kind: Scenario

prepare:
  components:
    - constructor: component-constructor.yaml

deploy:
  - apply: bootstrap.yaml
    waitFor:
      - target: rgd/${SCENARIO_SIMPLE_NAME}
        condition: Ready=true

assert:
  resources:
    - target: deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo
      condition: Available
```

That's the whole file. The runner handles ordering, OCM transfer, namespace
scoping, log dumping on failure, and cleanup.

---

## Adding behaviour the schema does not cover

If a scenario needs imperative work — generate a secret, mutate a CR mid-flight,
verify a side-effect that is not a Kubernetes resource — **do not** add fields
to `e2e.yaml`. Instead, write a hook.

```yaml
hooks:
  preDeploy:
    - createBasicAuthSecret
  postAssert:
    - verifySignedComponent
```

Each name must exist in `kubernetes/controller/test/e2e/hooks/registry.go`.
Hooks run in array order. The six phases — `preDeploy`, `postDeploy`,
`preAssert`, `postAssert`, `preCleanup`, `postCleanup` — are documented in
DESIGN.md.

A hook is a Go function:

```go
func createBasicAuthSecret(ctx context.Context, s ScenarioContext) error {
    // s.Folder, s.SimpleName, s.Namespace, s.Client, s.OCM, s.Logf
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
    - target: deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo
      condition: Available
```

An ArgoCD scenario asserts both the `Application`'s sync/health status and
the ArgoCD-managed deployment in `default-argocd`:

```yaml
assert:
  resources:
    - target: applications.argoproj.io/${SCENARIO_SIMPLE_NAME}
      namespace: argocd
      condition: jsonpath={.status.health.status}=Healthy
    - target: deployment.apps/${SCENARIO_SIMPLE_NAME}-argocd-podinfo
      namespace: default-argocd
      condition: Available
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

## Local iteration

```sh
# Run everything
task kubernetes/controller:test/e2e

# Run one scenario
task kubernetes/controller:test/e2e -- --focus="helm/fluxcd/simple"

# Run a family
task kubernetes/controller:test/e2e -- --focus="^helm/"

# Run a sub-family (Flux helm only)
task kubernetes/controller:test/e2e -- --focus="^helm/fluxcd/"
```

`--focus=` is a Ginkgo regex over `${SCENARIO_FOLDER}`. The local cluster is
persistent across runs; CI cluster is per-shard ephemeral. See DESIGN.md
§"Operator UX" for the full table.

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
