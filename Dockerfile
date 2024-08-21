# syntax=docker/dockerfile:experimental

# ============= Setting up base Stage ================
# AVALANCHEGO_NODE_IMAGE needs to identify an existing node image and should include the tag
ARG AVALANCHEGO_NODE_IMAGE

# ============= Compilation Stage ================
FROM golang:1.21.12-bullseye AS builder

WORKDIR /build

# Copy avalanche dependencies first (intermediate docker image caching)
# Copy avalanchego directory if present (for manual CI case, which uses local dependency)
COPY go.mod go.sum avalanchego* ./

# Download avalanche dependencies using go mod
RUN go mod download && go mod tidy -compat=1.21

# Copy the code into the container
COPY . .

# Ensure pre-existing builds are not available for inclusion in the final image
RUN [ -d ./build ] && rm -rf ./build/* || true

# Pass in SUBNET_EVM_COMMIT as an arg to allow the build script to set this externally
ARG SUBNET_EVM_COMMIT
ARG CURRENT_BRANCH

RUN export SUBNET_EVM_COMMIT=$SUBNET_EVM_COMMIT && export CURRENT_BRANCH=$CURRENT_BRANCH && ./scripts/build.sh build/subnet-evm

# ============= Cleanup Stage ================
FROM $AVALANCHEGO_NODE_IMAGE AS builtImage

# Copy the evm binary into the correct location in the container
ARG VM_ID=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
COPY --from=builder /build/build/subnet-evm /avalanchego/build/plugins/$VM_ID
