# Changes to the minimum golang version must also be replicated in
# scripts/ansible/roles/golang_based/defaults/main.yml
# scripts/build_avalanche.sh
# scripts/local.Dockerfile
# Dockerfile (here)
# README.md
# go.mod
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

# Build avalanchego and plugins
RUN ./scripts/build.sh

# ============= Cleanup Stage ================
FROM alpine:3.13 AS execution

# Maintain compatibility with previous images
RUN mkdir -p /avalanchego/build
WORKDIR /avalanchego/build

# Copy the executables into the container
COPY --from=builder /build/build/ .




