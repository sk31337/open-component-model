#!/usr/bin/env bash
# Install the Flux source/helm/kustomize controllers. The Flux CLI's
# `install` command is itself idempotent — repeated runs converge.

set -euo pipefail

flux install
