#!/usr/bin/env bash
# Bring up the host-side OCI registry the e2e suite pushes component
# versions to (`http://image-registry:5000`). On macOS, AirPlay Receiver
# binds *:5000 via ControlCenter — when the docker container is missing
# every push 403s as `Server: AirTunes`. This script enforces that the
# container exists, is on `127.0.0.1:5000`, and actually answers /v2/.
#
# Idempotent: starts the container only if missing; the network connect
# happens later (after the kind cluster is created) in 20_kind_cluster.sh.

set -euo pipefail

reg_name='image-registry'
reg_port='5000'

# 1. Container.
if [[ "$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)" != 'true' ]]; then
  echo "starting ${reg_name} container"
  docker run \
    -d --restart=always \
    -p "127.0.0.1:${reg_port}:${reg_port}" \
    --network bridge \
    --name "${reg_name}" \
    registry:2 >/dev/null
else
  echo "${reg_name} container already running"
fi

# 2. /etc/hosts entry. The hostname is used inside the cluster (via
#    kind's containerd certs.d wiring) AND on the host (via /etc/hosts);
#    the latter must point to 127.0.0.1 so OCM's `transfer ctf` reaches
#    the docker-published port instead of falling through to a wildcard
#    listener like macOS AirPlay.
if [[ ! -f /etc/hosts ]]; then
  echo "no /etc/hosts file present" >&2
  exit 1
fi
if ! grep -q "[[:space:]]${reg_name}\b" /etc/hosts; then
  echo "adding '127.0.0.1 ${reg_name}' to /etc/hosts (sudo required)"
  echo "127.0.0.1 ${reg_name}" | sudo tee -a /etc/hosts >/dev/null
else
  echo "/etc/hosts already maps ${reg_name}"
fi

# 3. Health check. This catches the AirPlay-on-port-5000 silent failure
#    immediately rather than 10 minutes later in the e2e suite. The
#    real registry returns 200 with header `Docker-Distribution-Api-Version`;
#    AirPlay returns `Server: AirTunes`.
echo "verifying registry is reachable on 127.0.0.1:${reg_port}"
status="$(curl -sI -o /dev/null -w '%{http_code}' "http://127.0.0.1:${reg_port}/v2/" || true)"
if [[ "${status}" != "200" ]]; then
  echo "registry health check failed: HTTP ${status:-no-response} from http://127.0.0.1:${reg_port}/v2/" >&2
  echo "if this is macOS, check that nothing else is bound to *:5000:" >&2
  echo "  lsof -i :${reg_port} -P" >&2
  exit 1
fi

# Cross-check via the /etc/hosts alias too — proves the resolution path
# the e2e suite actually takes works end-to-end.
status_via_alias="$(curl -sI -o /dev/null -w '%{http_code}' "http://${reg_name}:${reg_port}/v2/" || true)"
if [[ "${status_via_alias}" != "200" ]]; then
  echo "registry health check via ${reg_name}:${reg_port} failed: HTTP ${status_via_alias:-no-response}" >&2
  exit 1
fi

echo "${reg_name}:${reg_port} healthy"
