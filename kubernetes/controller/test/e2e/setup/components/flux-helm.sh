#!/usr/bin/env bash
# Install Flux's helm-controller. Source-controller is its hard
# dependency, so this script also ensures source-controller is present.
# Idempotent via `flux install`.
set -euo pipefail

flux install --components=source-controller,helm-controller
