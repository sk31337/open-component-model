#!/usr/bin/env bash
# Create the kind cluster the e2e suite runs against, configure
# containerd to trust the host-side image-registry over plain HTTP,
# and connect the registry container to the kind network so cluster
# pods can reach it as `http://image-registry:5000`.
#
# Idempotent: skips cluster creation if it already exists; rewrites
# containerd hosts.toml files on every run (cheap and self-correcting).

set -euo pipefail

reg_name='image-registry'
reg_port='5000'

KIND_NODE_IMAGE_VERSION="${KIND_NODE_IMAGE_VERSION:?must be set, e.g. 1.35.1}"
KIND_NODE_IMAGE="kindest/node:v${KIND_NODE_IMAGE_VERSION}"

# 1. Cluster.
if kind get clusters | grep -q '^kind$'; then
  echo "kind cluster already exists, skipping create"
else
  echo "creating kind cluster (${KIND_NODE_IMAGE})"
  cat <<EOF | kind create cluster --image="${KIND_NODE_IMAGE}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 31002
        hostPort: 31002
      - containerPort: 31003
        hostPort: 31003
  - role: worker
containerdConfigPatches:
- |-
 [plugins."io.containerd.grpc.v1.cri".registry]
   config_path = "/etc/containerd/certs.d"
EOF
fi

# 2. Per-node containerd hosts.toml entries. Re-applied unconditionally
#    so a node that lost its config (e.g. after a restart) is repaired.
add_hosts_toml() {
  local node="$1" path="$2" host="$3"
  docker exec "${node}" mkdir -p "${path}"
  cat <<EOF | docker exec -i "${node}" cp /dev/stdin "${path}/hosts.toml"
[host."${host}"]
  skip_verify = true
EOF
}

CONTAINERD_CONFIG_PATH="/etc/containerd/certs.d"
for node in $(kind get nodes); do
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/${reg_name}:${reg_port}" "http://${reg_name}:${reg_port}"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31002"        "registry-internal.default.svc.cluster.local:5002"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31003"        "registry-internal.default.svc.cluster.local:5003"
done

# 3. Connect the host-side registry to kind's docker network so cluster
#    pods can resolve `image-registry` to the registry container's IP.
if [[ "$(docker inspect -f '{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]]; then
  echo "connecting ${reg_name} to kind network"
  docker network connect kind "${reg_name}"
else
  echo "${reg_name} already on kind network"
fi
