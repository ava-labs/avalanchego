# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION=INVALID # This value is not intended to be used but silences a warning

# AVALANCHEGO_NODE_IMAGE needs to identify an existing node image and should include the tag
ARG AVALANCHEGO_NODE_IMAGE="invalid-image" # This value is not intended to be used but silences a warning

# ============= Compilation Stage ================
FROM golang:$GO_VERSION-bookworm AS builder

WORKDIR /build

# Copy and download avalanche dependencies using go mod
COPY go.mod .
COPY go.sum .
COPY graft/coreth ./graft/coreth
COPY graft/subnet-evm ./graft/subnet-evm
COPY graft/evm ./graft/evm
# proxy.golang.org intermittently drops a module download mid-transfer with an
# HTTP/2 INTERNAL_ERROR, failing the whole build. Retry before giving up.
RUN for i in 1 2 3 4 5; do \
        go mod download && break; \
        if [ "$i" -eq 5 ]; then echo "go mod download failed after $i attempts" >&2; exit 1; fi; \
        echo "go mod download failed (attempt $i/5), retrying in 15s" >&2; \
        sleep 15; \
    done

# Copy the code into the container
COPY . .

# Build xsvm
RUN ./scripts/build_xsvm.sh

# ============= Cleanup Stage ================
FROM $AVALANCHEGO_NODE_IMAGE AS execution

# Configure the node with the location of the xsvm plugin
ENV AVAGO_PLUGIN_DIR=/avalanchego/build/plugins

# Copy the xsvm binary to the default plugin dir for images
COPY --from=builder /build/build/xsvm $AVAGO_PLUGIN_DIR/v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH

# The node image's entrypoint will be reused.
