# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION

# AVALANCHEGO_NODE_IMAGE needs to identify an existing node image and should include the tag
ARG AVALANCHEGO_NODE_IMAGE

FROM golang:$GO_VERSION-bullseye AS builder

WORKDIR /builder_workdir

# Copy and download avalanche dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Ensure pre-existing builds are not available for inclusion in the final image
RUN [ -d ./build ] && rm -rf ./build/* || true

# Build tester binary
RUN ./scripts/build_bootstrap_tester.sh

# ============= Cleanup Stage ================
FROM $AVALANCHEGO_NODE_IMAGE AS execution

COPY --from=builder /builder_workdir/build/bootstrap-tester /avalanchego/build/bootstrap-tester

# Clear the CMD set by the base image
CMD [ "" ]

ENTRYPOINT [ "/avalanchego/build/bootstrap-tester" ]
