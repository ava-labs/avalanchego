# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION

# AVALANCHEGO_NODE_IMAGE needs to identify an existing node image and should include the tag
ARG AVALANCHEGO_NODE_IMAGE

# ============= Compilation Stage ================
# Always use the native platform to ensure fast builds
FROM --platform=$BUILDPLATFORM golang:$GO_VERSION-bullseye AS builder

WORKDIR /builder_workdir

ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Configure a cross-compiler if the target platform differs from the build platform.
#
# build_env.sh is used to capture the environmental changes required by the build step since RUN
# environment state is not otherwise persistent.
# TODO(marun) Extract a common multi-platform builder from this and the avalanchego Dockerfile.
RUN if [ "$TARGETPLATFORM" = "linux/arm64" ] && [ "$BUILDPLATFORM" != "linux/arm64" ]; then \
    apt-get update && apt-get install -y gcc-aarch64-linux-gnu && \
    echo "export CC=aarch64-linux-gnu-gcc" > ./build_env.sh \
    ; elif [ "$TARGETPLATFORM" = "linux/amd64" ] && [ "$BUILDPLATFORM" != "linux/amd64" ]; then \
    apt-get update && apt-get install -y gcc-x86-64-linux-gnu && \
    echo "export CC=x86_64-linux-gnu-gcc" > ./build_env.sh \
    ; else \
    echo "export CC=gcc" > ./build_env.sh \
    ; fi

# Copy and download avalanche dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Ensure pre-existing builds are not available for inclusion in the final image
RUN [ -d ./build ] && rm -rf ./build/* || true

# Build tester binary
RUN . ./build_env.sh && \
    echo "{CC=$CC, TARGETPLATFORM=$TARGETPLATFORM, BUILDPLATFORM=$BUILDPLATFORM}" && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    ./scripts/build_bootstrap_tester.sh

# ============= Cleanup Stage ================
FROM $AVALANCHEGO_NODE_IMAGE AS execution

COPY --from=builder /builder_workdir/build/bootstrap-tester /avalanchego/build/bootstrap-tester

# Clear the CMD set by the base image
CMD [ "" ]

ENTRYPOINT [ "/avalanchego/build/bootstrap-tester" ]
