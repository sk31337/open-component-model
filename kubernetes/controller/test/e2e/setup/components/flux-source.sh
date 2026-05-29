#!/usr/bin/env bash
# Install Flux's source-controller (used by every Flux-driven scenario).
# `flux install --components` is idempotent across repeated invocations.
#
# We always include source-controller plus whichever controllers other
# component scripts already installed: `flux install --components` is
# additive only on first install, but on subsequent runs the controllers
# we don't list are not removed (the install reconciles by name).
set -euo pipefail

flux install --components=source-controller
