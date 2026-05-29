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
│   │   ├── simple/
│   │   ├── signing/
│   │   ├── nested/
│   │   ├── nested-signed/
│   │   ├── simple-nested-status/
│   │   └── configuration-localization/
│   └── argocd/              # ArgoCD Application
│       ├── simple/
│       ├── signing/
│       ├── nested/
│       ├── nested-signed/
│       ├── simple-nested-status/
│       └── configuration-localization/
├── kustomize/               # Kustomize delivery, split by delivery tool
│   ├── simple/              # (flat = Flux Kustomization)
│   ├── configuration-localization/
│   └── argocd/              # ArgoCD Application
│       ├── simple/
│       └── configuration-localization/
└── k8s-manifest/            # raw manifest delivery
    └── simple/
```

> **Migration note.** The grouped layout above is the target state described in
> `DESIGN.md`. Some folders may still sit flat at the top level during the
> migration; treat the family grouping as the authoritative shape going
> forward.

---

## Families

### `helm/` — Helm chart delivery, split by delivery tool

OCM publishes the chart (and any referenced image resources) to OCI; the chart
is then delivered into the cluster by a delivery tool. Each scenario exists in
two parallel variants — one under `helm/fluxcd/` using Flux's `OCIRepository` +
`HelmRelease`, and one under `helm/argocd/` using an ArgoCD `Application`.
Pick the variant that matches your delivery tool.

#### `helm/fluxcd/` — Flux HelmRelease

OCM resource → `OCIRepository` → `HelmRelease`.

| Folder | Shows |
|---|---|
| `simple/` | Smallest end-to-end: chart from OCM, Flux release. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape (`additional.oci.{registry,repository,tag,digest}`) instead of flat fields. |
| `configuration-localization/` | OCM configuration + localization rules rewriting image references and env vars at delivery time, applied via `HelmRelease.spec.values`. |

#### `helm/argocd/` — ArgoCD Application

OCM resource → ArgoCD `Application` (Helm OCI source) → release in
`default-argocd`.

| Folder | Shows |
|---|---|
| `simple/` | Smallest end-to-end: chart from OCM, ArgoCD release. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape (`additional.oci.{registry,repository,tag,digest}`) instead of flat fields. |
| `configuration-localization/` | OCM configuration + localization applied via `Application.spec.source.helm.parameters` (the ArgoCD equivalent of `HelmRelease.spec.values`). |

### `kustomize/` — Kustomize delivery, split by delivery tool

OCM publishes the kustomize tree (and any referenced image resources); the
tree is then delivered into the cluster by a delivery tool. The Flux variants
sit flat under `kustomize/` (legacy layout); the ArgoCD variants live under
`kustomize/argocd/`.

#### `kustomize/` (flat) — Flux Kustomization

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
task kubernetes/controller:test/e2e -- --focus="helm/fluxcd/simple"

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
   new one in your PR description if none fit. For `helm/`, also pick the
   delivery tool sub-folder (`fluxcd/` or `argocd/`). For `kustomize/`, ArgoCD
   variants live under `kustomize/argocd/`; the Flux variants currently sit
   flat under `kustomize/`. Add the scenario under both if it should exist for
   each delivery tool.
2. Create `examples/<family>[/<tool>]/<scenario>/` with at minimum:
   - `component-constructor.yaml`
   - `bootstrap.yaml`
   - `e2e.yaml` (declares how the e2e runner deploys and asserts)
3. Add a row to the family table above.
4. Follow [`../test/e2e/AUTHORING.md`](../test/e2e/AUTHORING.md) for the
   `e2e.yaml` schema, naming variables, and hook conventions.

If your scenario only exists to exercise an edge case the suite needs — not as
a pattern a user would copy — put it under `test/e2e/scenarios/` instead.
