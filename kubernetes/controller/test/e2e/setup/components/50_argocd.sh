#!/usr/bin/env bash
# Install ArgoCD into its own namespace and register the host-side
# image-registry as a plain-HTTP OCI Helm repo-creds template. Any
# Application whose repoURL starts with `oci://image-registry:5000`
# inherits the insecureOCIForceHttp setting.
#
# Idempotent: namespace creation is `apply -k` style; install.yaml is
# applied with `--server-side --force-conflicts`; the repo-creds Secret
# is `kubectl apply` of stable content.

set -euo pipefail

reg_name='image-registry'
reg_port='5000'

# Namespace. `apply` is idempotent where `create` would fail-fast.
kubectl get namespace argocd >/dev/null 2>&1 || kubectl create namespace argocd

kubectl apply -n argocd --server-side --force-conflicts \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

kubectl wait -n argocd deployment \
  argocd-server \
  argocd-repo-server \
  argocd-redis \
  argocd-dex-server \
  argocd-applicationset-controller \
  argocd-notifications-controller \
  --for=condition=Available --timeout=5m

# Plain-HTTP OCI repo-creds template. `enableOCI: true` and
# `insecureOCIForceHttp: true` are required because the local registry
# is HTTP-only; ArgoCD otherwise defaults to HTTPS and fails the chart
# pull.
kubectl apply -n argocd -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: image-registry-creds
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: repo-creds
stringData:
  url: oci://${reg_name}:${reg_port}
  type: helm
  enableOCI: "true"
  insecureOCIForceHttp: "true"
EOF
