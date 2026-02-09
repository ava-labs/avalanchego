# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION=INVALID # This value is not intended to be used but silences a warning

ARG AVALANCHEGO_NODE_IMAGE="invalid-image"

# ============= Base Stage ================
# Shared Go setup, dependency download, and cross-compilation configuration.
# Always use the native platform to ensure fast builds.
FROM --platform=$BUILDPLATFORM golang:$GO_VERSION-bookworm AS base

WORKDIR /build

# Copy and download avalanche dependencies using go mod
COPY go.mod .
COPY go.sum .
COPY graft/coreth ./graft/coreth
COPY graft/subnet-evm ./graft/subnet-evm
COPY graft/evm ./graft/evm
RUN go mod download

# Copy the code into the container
COPY . .

# Ensure pre-existing builds are not available for inclusion in the final image
RUN [ -d ./build ] && rm -rf ./build/* || true

ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Configure a cross-compiler if the target platform differs from the build platform.
#
# build_env.sh captures CC and GOARCH since RUN environment state is not persistent.
RUN GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    if [ "$TARGETPLATFORM" = "linux/arm64" ] && [ "$BUILDPLATFORM" != "linux/arm64" ]; then \
    apt-get update && apt-get install -y gcc-aarch64-linux-gnu && \
    printf 'export CC=aarch64-linux-gnu-gcc\nexport GOARCH=%s\n' "$GOARCH" > ./build_env.sh \
    ; elif [ "$TARGETPLATFORM" = "linux/amd64" ] && [ "$BUILDPLATFORM" != "linux/amd64" ]; then \
    apt-get update && apt-get install -y gcc-x86-64-linux-gnu && \
    printf 'export CC=x86_64-linux-gnu-gcc\nexport GOARCH=%s\n' "$GOARCH" > ./build_env.sh \
    ; else \
    printf 'export CC=gcc\nexport GOARCH=%s\n' "$GOARCH" > ./build_env.sh \
    ; fi

# ============= AvalancheGo Build Stage ================
FROM base AS avalanchego-builder

ARG RACE_FLAG=""
ARG BUILD_SCRIPT=build.sh
ARG AVALANCHEGO_COMMIT=""
RUN . ./build_env.sh && \
    export AVALANCHEGO_COMMIT="${AVALANCHEGO_COMMIT}" && \
    ./scripts/${BUILD_SCRIPT} ${RACE_FLAG}

# Create this directory in the builder to avoid requiring anything to be executed in the
# potentially emulated execution container.
RUN mkdir -p /avalanchego/build

# ============= AvalancheGo Final Stage ================
# Commands executed in this stage may be emulated (i.e. very slow) if TARGETPLATFORM and
# BUILDPLATFORM have different arches.
FROM debian:12-slim AS avalanchego

# Maintain compatibility with previous images
COPY --from=avalanchego-builder /avalanchego/build /avalanchego/build
WORKDIR /avalanchego/build

# Copy the executables into the container
COPY --from=avalanchego-builder /build/build/ .

CMD [ "./avalanchego" ]

# ============= Subnet-EVM Build Stage ================
FROM base AS subnet-evm-builder

RUN . ./build_env.sh && \
    cd graft/subnet-evm && \
    ./scripts/build.sh build/subnet-evm

# ============= Subnet-EVM Final Stage ================
FROM $AVALANCHEGO_NODE_IMAGE AS subnet-evm

# Copy the evm binary into the correct location in the container
ARG VM_ID=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
ENV AVAGO_PLUGIN_DIR="/avalanchego/build/plugins"
COPY --from=subnet-evm-builder /build/graft/subnet-evm/build/subnet-evm $AVAGO_PLUGIN_DIR/$VM_ID
