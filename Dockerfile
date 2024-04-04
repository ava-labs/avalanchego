# Changes to the minimum golang version must also be replicated in
# scripts/build_avalanche.sh
# tests/antithesis/Dockerfile.node
# tests/antithesis/Dockerfile.workload
# Dockerfile (here)
# README.md
# go.mod
# ============= Compilation Stage ================
# Always use the build platform to ensure fast builds
FROM --platform=$BUILDPLATFORM golang:1.21.9-bullseye AS builder

# Configure a cross-compiler if the target platform differs from the builder platform
RUN if [ "$TARGETPLATFORM" = "linux/arm64" ] && [ "$BUILDPLATFORM" != "linux/arm64" ]; then \
    apt-get update && apt-get install -y gcc-aarch64-linux-gnu && \
    export CC=aarch64-linux-gnu-gcc \
    ; elif [ "$TARGETPLATFORM" = "linux/amd64" ] && [ "$BUILDPLATFORM" != "linux/amd64" ]; then \
    apt-get update && apt-get install -y gcc-x86-64-linux-gnu && \
    export CC=x86_64-linux-gnu-gcc \
    ; else \
    export CC=gcc \
    ; fi

WORKDIR /build
# Copy and download avalanche dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build avalanchego
ARG RACE_FLAG=""
RUN ./scripts/build.sh ${RACE_FLAG}

# Create the build directory in the builder to avoid requiring
# anything to be executed in the (potentially emulated) execution
# container.
RUN mkdir -p /avalanchego/build

# ============= Cleanup Stage ================
# Will be TARGETPLATFORM and very slow if not the same as BUILDPLATFORM
FROM debian:11-slim AS execution

# Maintain compatibility with previous images
COPY --from=builder /avalanchego/build /avalanchego/build
WORKDIR /avalanchego/build

# Copy the executables into the container
COPY --from=builder /build/build/ .

CMD [ "./avalanchego" ]
