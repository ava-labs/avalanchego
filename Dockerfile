# ============= Compilation Stage ================
FROM golang:1.15.5-alpine AS builder
RUN apk add --no-cache bash git make gcc musl-dev linux-headers git ca-certificates

WORKDIR /build
# Copy and download dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build avalanchego and plugins
RUN ./scripts/build.sh

# ============= Cleanup Stage ================
FROM alpine:3.13 AS execution

WORKDIR /run

# Copy the executables into the container
COPY --from=builder /build/build/ .
