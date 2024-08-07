# Changes to the minimum golang version must also be replicated in
# scripts/ansible/roles/golang_base/defaults/main.yml
# scripts/build_camino.sh
# scripts/local.Dockerfile
# Dockerfile (here)
# README.md
# go.mod
# ============= Compilation Stage ================
FROM golang:1.19.6-buster AS builder
RUN apt-get update && apt-get install -y --no-install-recommends bash=5.0-4 git=1:2.20.1-2+deb10u8 make=4.2.1-1.2 gcc=4:8.3.0-1 musl-dev=1.1.21-2 ca-certificates=20200601~deb10u2 linux-headers-amd64
WORKDIR /build
# Copy and download caminogo dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build caminogo and plugins
RUN ./scripts/build.sh
# Build tools
RUN ./scripts/build_tools.sh

# ============= Cleanup Stage ================
FROM debian:11-slim AS execution

# installing wget to get static ip with wget -O - -q icanhazip.com
RUN apt-get update && apt-get install -y wget

# Maintain compatibility with previous images
RUN mkdir -p /caminogo/build
WORKDIR /caminogo/build

# Copy the executables into the container
COPY --from=builder /build/build/ .

CMD [ "./caminogo" ]
