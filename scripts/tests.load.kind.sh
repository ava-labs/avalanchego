#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/tests.load.kind.sh ]]; then
    echo "must be run from repository root"
    exit 255
fi

# Start kind cluster
./scripts/start_kind_cluster.sh

# Build docker image for load test
DOCKERFILE="./tests/load/c/Dockerfile.loadtest"
DOCKER_IMAGE="localhost:5001/avalanchego"
SKIP_BUILD_RACE=1
DOCKERFILE="$DOCKERFILE" DOCKER_IMAGE="$DOCKER_IMAGE" SKIP_BUILD_RACE="$SKIP_BUILD_RACE" ./scripts/build_image.sh

# Construct pod manifest
source ./scripts/git_commit.sh

POD_MANIFEST=$(cat << EOF
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: load-test
  namespace: tmpnet

---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: load-test
  namespace: tmpnet
rules:
- apiGroups: ["apps"]
  resources: ["statefulsets"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["apps"]
  resources: ["statefulsets/scale"]
  verbs: ["get", "patch", "update"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: load-test
  namespace: tmpnet
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: load-test
subjects:
- kind: ServiceAccount
  name: load-test
  namespace: tmpnet

---
apiVersion: v1
kind: Pod
metadata:
  name: load-test
  namespace: tmpnet
spec:
  serviceAccountName: load-test
  containers:
  - name: load-test
    image: $DOCKER_IMAGE:$commit_hash
  restartPolicy: Never
EOF
)

# Deploy pod to cluster
echo "$POD_MANIFEST" | kubectl apply --context kind-kind -f -
