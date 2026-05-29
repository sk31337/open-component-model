#!/usr/bin/env bash
# Install Flux's kustomize-controller. Source-controller is a hard
# dependency, so this script also ensures source-controller is present.
# Idempotent via `flux install`.
set -euo pipefail

flux install --components=source-controller,kustomize-controller
