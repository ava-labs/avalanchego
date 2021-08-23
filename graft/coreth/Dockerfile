# syntax=docker/dockerfile:experimental

# ============= Setting up base Stage ================
# Set required AVALANCHE_VERSION parameter in build image script
ARG AVALANCHE_VERSION

# ============= Compilation Stage ================
FROM golang:1.15.5-buster AS builder
RUN apt-get update && apt-get install -y --no-install-recommends bash=5.0-4 git=1:2.20.1-2+deb10u3 make=4.2.1-1.2 gcc=4:8.3.0-1 musl-dev=1.1.21-2 ca-certificates=20200601~deb10u2 linux-headers-amd64=4.19+105+deb10u12

WORKDIR /build
# Copy and download avalanche dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Pass in CORETH_COMMIT as an arg to allow the build script to set this externally
ARG CORETH_COMMIT
ARG CURRENT_BRANCH

RUN export CORETH_COMMIT=$CORETH_COMMIT && export CURRENT_BRANCH=$CURRENT_BRANCH && ./scripts/build.sh /build/evm

# ============= Cleanup Stage ================
FROM avaplatform/avalanchego:$AVALANCHE_VERSION AS builtImage

# Copy the evm binary into the correct location in the container
COPY --from=builder /build/evm /avalanchego/build/plugins/evm
