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
├── helm/                    # Flux HelmRelease + (optionally) ArgoCD Application
│   ├── simple/
│   ├── signing/
│   ├── nested/
│   ├── nested-signed/
│   ├── simple-nested-status/
│   └── configuration-localization/
├── kustomize/               # Flux Kustomization
│   ├── simple/
│   └── configuration-localization/
└── k8s-manifest/            # raw manifest delivery
    └── simple/
```

> **Migration note.** The grouped layout above is the target state described in
> `DESIGN.md`. Today the folders still sit flat at the top level
> (`helm-simple/`, `kustomize-simple/`, …) and will move during Stage 3 of the
> migration. Treat the family grouping as the authoritative shape going
> forward.

---

## Families

### `helm/` — Helm delivery via Flux + ArgoCD

OCM resource → `OCIRepository` → `HelmRelease` (Flux) **and** an ArgoCD
`Application` pointing at the same chart, deploying into `default-argocd`.
Demonstrates that the same OCM-published chart can drive both delivery tools
in parallel.

| Folder | Shows |
|---|---|
| `simple/` | Smallest end-to-end: chart from OCM, Flux release, ArgoCD release. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape (`additional.oci.{registry,repository,tag,digest}`) instead of flat fields. |
| `configuration-localization/` | OCM configuration + localization rules rewriting image references and env vars at delivery time. |

### `kustomize/` — Kustomize delivery via Flux

OCM resource → `OCIRepository` → Flux `Kustomization`.

| Folder | Shows |
|---|---|
| `simple/` | Plain Kustomize delivery. |
| `configuration-localization/` | Configuration + localization applied to a Kustomize tree. |

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

# A single example (regex over family/scenario)
task kubernetes/controller:test/e2e -- --focus="helm/simple"

# A whole family
task kubernetes/controller:test/e2e -- --focus="^helm/"
```

See [`../test/e2e/DESIGN.md`](../test/e2e/DESIGN.md) §"Operator UX" for the
full command surface.

---

## Adding a new example

1. Decide on the family (`helm/`, `kustomize/`, `k8s-manifest/`) — or propose a
   new one in your PR description if none fit.
2. Create `examples/<family>/<scenario>/` with at minimum:
   - `component-constructor.yaml`
   - `bootstrap.yaml`
   - `e2e.yaml` (declares how the e2e runner deploys and asserts)
3. Add a row to the family table above.
4. Follow [`../test/e2e/AUTHORING.md`](../test/e2e/AUTHORING.md) for the
   `e2e.yaml` schema, naming variables, and hook conventions.

If your scenario only exists to exercise an edge case the suite needs — not as
a pattern a user would copy — put it under `test/e2e/scenarios/` instead.
