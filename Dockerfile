# syntax=docker/dockerfile:experimental

# ============= Setting up base Stage ================
# Set required AVALANCHE_VERSION parameter in build image script
ARG AVALANCHE_VERSION
FROM avaplatform/avalanchego:$AVALANCHE_VERSION AS builtImage

# ============= Compilation Stage ================
FROM golang:1.15.5-alpine AS builder
RUN apk add --no-cache bash git make gcc musl-dev linux-headers git ca-certificates

WORKDIR /build
# Copy and download avalanche dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Run the unit tests
RUN go test ./...

# Pass in CORETH_COMMIT as an arg to allow the build script to set this externally
ARG CORETH_COMMIT
RUN export CORETH_COMMIT=$CORETH_COMMIT
ARG CURRENT_BRANCH
RUN export CURRENT_BRANCH=$CURRENT_BRANCH

RUN ./scripts/build.sh /build/evm

# ============= Cleanup Stage ================
FROM alpine:3.13 AS execution

# Maintain compatibility with previous images
RUN mkdir -p /avalanchego/build
WORKDIR /avalanchego/build

# Copy the executables into the container
COPY --from=builtImage /avalanchego/build .
COPY --from=builder /build/evm ./plugins/evm
