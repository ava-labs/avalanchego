# Changes to the minimum golang version must also be replicated in
# tests/antithesis/Dockerfile.node
# tests/antithesis/Dockerfile.workload
# Dockerfile (here)
# README.md
# go.mod
# ============= Compilation Stage ================
# Always use the native platform to ensure fast builds
FROM --platform=$BUILDPLATFORM golang:1.21.9-bullseye AS builder

WORKDIR /build

ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Configure a cross-compiler if the target platform differs from the build platform.
#
# build_env.sh is used to capture the environmental changes required by the build step since RUN
# environment state is not otherwise persistent.
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

# Build avalanchego. The build environment is configured with build_env.sh from the step
# enabling cross-compilation.
ARG RACE_FLAG=""
RUN . ./build_env.sh && \
    echo "{CC=$CC, TARGETPLATFORM=$TARGETPLATFORM, BUILDPLATFORM=$BUILDPLATFORM}" && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    ./scripts/build.sh ${RACE_FLAG}

# Create this directory in the builder to avoid requiring anything to be executed in the
# potentially emulated execution container.
RUN mkdir -p /avalanchego/build

# ============= Cleanup Stage ================
# Commands executed in this stage may be emulated (i.e. very slow) if TARGETPLATFORM and
# BUILDPLATFORM have different arches.
FROM debian:11-slim AS execution

# Maintain compatibility with previous images
COPY --from=builder /avalanchego/build /avalanchego/build
WORKDIR /avalanchego/build

# Copy the executables into the container
COPY --from=builder /build/build/ .

CMD [ "./avalanchego" ]
