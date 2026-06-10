# Examples

User-facing demos for the OCM Kubernetes Controller Toolkit. Each subfolder is
a runnable, copy-as-starting-point example showing one way to wire OCM into a
delivery flow.

For setup instructions see the [`Getting Started`](../README.md#getting-started)
section of the controller README.

> **Audience.** Folders here are demos a human user might read or fork.
> Test-only fixtures (corner cases the e2e suite needs but a user would not
> copy) live under [`../test/e2e/scenarios/`](../test/e2e/scenarios/) instead.
> The split — and how the e2e runner discovers both — is described in
> [`../test/e2e/DESIGN.md`](../test/e2e/DESIGN.md).

---

## Layout

Examples are grouped by delivery family:

```
examples/
├── helm/                    # Helm chart delivery, split by delivery tool
│   ├── fluxcd/              # Flux HelmRelease
│   │   ├── kro/             # kro-based scenarios
│   │   │   ├── simple/
│   │   │   ├── signing/
│   │   │   ├── nested/
│   │   │   ├── nested-signed/
│   │   │   ├── simple-nested-status/
│   │   │   └── configuration-localization/
│   │   └── crossplane/          # Crossplane delivery
│   │       ├── simple/
│   │       ├── signing/
│   │       ├── nested/
│   │       ├── nested-signed/
│   │       ├── simple-nested-status/
│   │       ├── configuration-localization/
│   │       └── simple-fnc-kro/  # function-kro variant
│   └── argocd/              # ArgoCD Application
│       ├── kro/             # kro-based scenarios
│       │   ├── simple/
│       │   ├── signing/
│       │   ├── nested/
│       │   ├── nested-signed/
│       │   ├── simple-nested-status/
│       │   └── configuration-localization/
│       └── crossplane/          # Crossplane delivery
│           ├── simple/
│           ├── signing/
│           ├── nested/
│           ├── nested-signed/
│           ├── simple-nested-status/
│           └── configuration-localization/
├── kustomize/               # Kustomize delivery, split by delivery tool
│   ├── fluxcd/              # Flux Kustomization
│   │   ├── simple/
│   │   └── configuration-localization/
│   └── argocd/              # ArgoCD Application
│       ├── simple/
│       └── configuration-localization/
└── k8s-manifest/            # raw manifest delivery
    └── simple/
```

---

## Families

### `helm/` — Helm chart delivery, split by delivery tool

OCM publishes the chart (and any referenced image resources) to OCI; the chart
is then delivered into the cluster by a delivery tool. Each scenario exists in
two parallel variants — one under `helm/fluxcd/` using Flux's `OCIRepository` +
`HelmRelease`, and one under `helm/argocd/` using an ArgoCD `Application`.
Pick the variant that matches your delivery tool.

#### `helm/fluxcd/kro/` — Flux HelmRelease (kro)

OCM resource → kro `ResourceGraphDefinition` → `OCIRepository` → `HelmRelease`.

| Folder | Shows |
|---|---|
| `simple/` | Smallest end-to-end: chart from OCM, kro + Flux release. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape (`additional.oci.{registry,repository,tag,digest}`) instead of flat fields. |
| `configuration-localization/` | OCM configuration + localization rules rewriting image references and env vars at delivery time, applied via `HelmRelease.spec.values`. |

#### `helm/fluxcd/crossplane/` — Flux HelmRelease (Crossplane)

OCM Deployer → Crossplane XRD + Composition (or function-kro `ResourceGraph`) → `OCIRepository` → `HelmRelease`.

| Folder | Shows |
|---|---|
| `simple/` | Crossplane Composition wiring the Flux delivery chain. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape. |
| `configuration-localization/` | OCM configuration + localization rewriting image references at delivery time. |
| `simple-fnc-kro/` | Crossplane pipeline using `function-kro` ResourceGraph with CEL expressions instead of a static Composition. |

#### `helm/argocd/kro/` — ArgoCD Application (kro)

OCM resource → kro `ResourceGraphDefinition` → ArgoCD `Application` → release in
`default-argocd`.

| Folder | Shows |
|---|---|
| `simple/` | Smallest end-to-end: chart from OCM, kro + ArgoCD release. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape (`additional.oci.{registry,repository,tag,digest}`) instead of flat fields. |
| `configuration-localization/` | OCM configuration + localization applied via `Application.spec.source.helm.parameters` (the ArgoCD equivalent of `HelmRelease.spec.values`). |

#### `helm/argocd/crossplane/` — ArgoCD Application (Crossplane)

OCM Deployer → Crossplane XRD + Composition → ArgoCD `Application` → release in `default-argocd`.

| Folder | Shows |
|---|---|
| `simple/` | Crossplane Composition wiring the ArgoCD delivery chain. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape. |
| `configuration-localization/` | OCM configuration + localization rewriting image references at delivery time. |

> **File layout note.** Like all Crossplane scenarios, these split bootstrap into
> `bootstrap.yaml` / `resource.yaml` / `deployer.yaml` — see the note under
> [`helm/fluxcd/crossplane/`](#helmfluxcdcrossplane--flux-helmrelease-crossplane)
> above for the rationale.

> **Why every Composition here has a `readinessChecks` block.**
> Crossplane's `function-auto-ready` determines whether a composite is `Ready` by
> looking for a `status.conditions[type=Ready]` entry on each composed resource.
> ArgoCD `Application` objects do **not** expose that condition — they use
> `status.health.status` (`Healthy`/`Degraded`) and `status.sync.status`
> (`Synced`/`OutOfSync`) instead. Without an explicit `readinessChecks` override,
> `function-auto-ready` can never observe readiness on the `Application` resource,
> so the composite stays `Ready=False` indefinitely even when ArgoCD has
> successfully deployed and the workload is running.
>
> The fix is a `readinessChecks` block on the `argocdApplication` resource in each
> Composition, telling `function-patch-and-transform` to evaluate readiness via a
> `MatchString` check on `status.health.status`:
>
> ```yaml
> readinessChecks:
>   - type: MatchString
>     fieldPath: status.health.status
>     matchString: Healthy
> ```
>
> This pattern is required for **every** `helm/argocd/crossplane/` Composition. The
> Flux equivalents (`helm/fluxcd/crossplane/`) do not need it because Flux
> `HelmRelease` objects expose a standard `Ready` condition that `function-auto-ready`
> reads natively.

### `kustomize/` — Kustomize delivery, split by delivery tool

OCM publishes the kustomize tree (and any referenced image resources); the
tree is then delivered into the cluster by a delivery tool. Each scenario
exists in two parallel variants — one under `kustomize/fluxcd/` using a Flux
`Kustomization`, and one under `kustomize/argocd/` using an ArgoCD
`Application`. Pick the variant that matches your delivery tool.

#### `kustomize/fluxcd/` — Flux Kustomization

OCM resource → Flux `GitRepository` → Flux `Kustomization`.

| Folder | Shows |
|---|---|
| `simple/` | Plain Kustomize delivery. |
| `configuration-localization/` | Configuration + localization applied to a Kustomize tree via Flux `Kustomization.spec.patches`. |

#### `kustomize/argocd/` — ArgoCD Application

OCM resource → ArgoCD `Application` (git source) → release in `default-argocd`.
ArgoCD ≥ 2.10 supports `kustomize.patches` with the same JSON6902 /
strategic-merge syntax as Flux, so the patch shape mirrors the Flux variant.

| Folder | Shows |
|---|---|
| `simple/` | Plain Kustomize delivery via ArgoCD. |
| `configuration-localization/` | Configuration + localization applied via `Application.spec.source.kustomize.patches`. |

### `k8s-manifest/` — Plain manifest delivery

| Folder | Shows |
|---|---|
| `simple/` | OCM resource carrying a raw Kubernetes manifest, delivered without Flux or ArgoCD. |

---

## Running an example

The e2e suite runs every example in this directory automatically. To run one
locally:

```sh
# All examples
task kubernetes/controller:test/e2e

# A single example (regex over family/tool/scenario)
task kubernetes/controller:test/e2e -- --focus="helm/fluxcd/kro/simple"

# A whole family
task kubernetes/controller:test/e2e -- --focus="^helm/"

# A whole tool within a family
task kubernetes/controller:test/e2e -- --focus="^helm/argocd/"
```

See [`../test/e2e/DESIGN.md`](../test/e2e/DESIGN.md) §"Operator UX" for the
full command surface.

---

## Adding a new example

1. Decide on the family (`helm/`, `kustomize/`, `k8s-manifest/`) — or propose a
   new one in your PR description if none fit. For `helm/` and `kustomize/`,
   also pick the delivery tool sub-folder (`fluxcd/` or `argocd/`). Within
   `helm/fluxcd/` and `helm/argocd/`, also pick the operator sub-folder (`kro/`
   for kro-based scenarios, `crossplane/` for Crossplane scenarios). Add the
   scenario under both delivery tool variants if it should exist for each tool.
2. Create `examples/<family>[/<tool>][/<operator>]/<scenario>/` with at minimum:
   - `component-constructor.yaml`
   - `bootstrap.yaml`
   - `e2e.yaml` (declares how the e2e runner deploys and asserts)
3. Add a row to the family table above.
4. Follow [`../test/e2e/AUTHORING.md`](../test/e2e/AUTHORING.md) for the
   `e2e.yaml` schema, naming variables, and hook conventions.

If your scenario only exists to exercise an edge case the suite needs — not as
a pattern a user would copy — put it under `test/e2e/scenarios/` instead.
