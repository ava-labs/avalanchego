# ============= Compilation Stage ================
FROM golang:1.23.6-bullseye AS builder

ARG AVALANCHE_VERSION

RUN mkdir -p $GOPATH/src/github.com/ava-labs
WORKDIR $GOPATH/src/github.com/ava-labs

RUN git clone -b $AVALANCHE_VERSION --single-branch https://github.com/ava-labs/avalanchego.git

# Copy coreth repo into desired location
COPY . coreth

# Set the workdir to AvalancheGo and update coreth dependency to local version
WORKDIR $GOPATH/src/github.com/ava-labs/avalanchego
# Run go mod download here to improve caching of AvalancheGo specific depednencies
RUN go mod download
# Replace the coreth dependency
RUN go mod edit -replace github.com/ava-labs/coreth=../coreth
RUN go mod download && go mod tidy -compat=1.23

# Build the AvalancheGo binary with local version of coreth.
RUN ./scripts/build_avalanche.sh
# Create the plugins directory in the standard location so the build directory will be recognized
# as valid.
RUN mkdir build/plugins

# ============= Cleanup Stage ================
FROM debian:11-slim AS execution

# Maintain compatibility with previous images
RUN mkdir -p /avalanchego/build
WORKDIR /avalanchego/build

# Copy the executables into the container
COPY --from=builder /go/src/github.com/ava-labs/avalanchego/build .

CMD [ "./avalanchego" ]
