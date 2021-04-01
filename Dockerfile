# syntax=docker/dockerfile:experimental

# Set required AVALANCHE_VERSION parameter in build image script
ARG AVALANCHE_VERSION
# Pass in CORETH_COMMIT as an arg to allow the build script to set this externally
# (without copying the .git/ directory)
ARG CORETH_COMMIT

FROM avaplatform/avalanchego:$AVALANCHE_VERSION

ARG CORETH_COMMIT

WORKDIR $GOPATH/src/github.com/ava-labs

COPY . coreth
WORKDIR $GOPATH/src/github.com/ava-labs/coreth

# Export CORETH_COMMIT so the build script can set the GitCommit on the binary correctly
RUN export CORETH_COMMIT=$CORETH_COMMIT
RUN ./scripts/build.sh
