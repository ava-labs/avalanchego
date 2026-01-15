# ============= Setting up base Stage ================
# AVALANCHEGO_NODE_IMAGE needs to identify an existing node image and should include the tag
# This value is not intended to be used but silences a warning
ARG AVALANCHEGO_NODE_IMAGE="invalid-image"

# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION=INVALID # This value is not intended to be used but silences a warning

# ============= Compilation Stage ================
FROM --platform=$BUILDPLATFORM golang:$GO_VERSION-bookworm AS builder

WORKDIR /build

# Copy avalanche dependencies first (intermediate docker image caching)
# Note: Context is now the repo root, so we copy from there
COPY go.mod go.sum ./
COPY graft ./graft
# Download avalanche dependencies using go mod
RUN go mod download && go mod tidy

# Copy the entire repo to preserve module structure
COPY . .

# Ensure pre-existing builds are not available for inclusion in the final image
RUN [ -d ./build ] && rm -rf ./build/* || true


ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Configure a cross-compiler if the target platform differs from the build platform.
#
# build_env.sh is used to capture the environmental changes required by the build step since RUN
# environment state is not otherwise persistent.
RUN cd graft/subnet-evm && \
  if [ "$TARGETPLATFORM" = "linux/arm64" ] && [ "$BUILDPLATFORM" != "linux/arm64" ]; then \
  apt-get update && apt-get install -y gcc-aarch64-linux-gnu && \
  echo "export CC=aarch64-linux-gnu-gcc" > ./build_env.sh \
  ; elif [ "$TARGETPLATFORM" = "linux/amd64" ] && [ "$BUILDPLATFORM" != "linux/amd64" ]; then \
  apt-get update && apt-get install -y gcc-x86-64-linux-gnu && \
  echo "export CC=x86_64-linux-gnu-gcc" > ./build_env.sh \
  ; else \
  echo "export CC=gcc" > ./build_env.sh \
  ; fi

# Pass in SUBNET_EVM_COMMIT as an arg to allow the build script to set this externally
ARG SUBNET_EVM_COMMIT
ARG CURRENT_BRANCH

RUN cd graft/subnet-evm && \
  . ./build_env.sh && \
  echo "{CC=$CC, TARGETPLATFORM=$TARGETPLATFORM, BUILDPLATFORM=$BUILDPLATFORM}" && \
  export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
  export CURRENT_BRANCH=$CURRENT_BRANCH && \
  export SUBNET_EVM_COMMIT=$SUBNET_EVM_COMMIT && \
  ./scripts/build.sh build/subnet-evm

# ============= Cleanup Stage ================
FROM $AVALANCHEGO_NODE_IMAGE AS execution

# Copy the evm binary into the correct location in the container
ARG VM_ID=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
ENV AVAGO_PLUGIN_DIR="/avalanchego/build/plugins"
COPY --from=builder /build/graft/subnet-evm/build/subnet-evm $AVAGO_PLUGIN_DIR/$VM_ID
