# syntax=docker/dockerfile:experimental

# ============= Setting up base Stage ================
# Set required AVALANCHE_VERSION parameter in build image script
ARG AVALANCHE_VERSION

# ============= Compilation Stage ================
FROM golang:1.15.5-alpine AS builder
RUN apk add --no-cache bash git make gcc musl-dev linux-headers git ca-certificates g++

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
COPY --from=builder /build/evm /avalanchego/build/avalanchego-latest/plugins/evm
