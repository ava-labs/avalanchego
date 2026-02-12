# Build container for RPM packaging of avalanchego and subnet-evm.
#
# Based on Rocky Linux 9 (glibc 2.34) to match RHEL 9.x target.
# Source tree is bind-mounted at runtime, not COPY'd.
#
# Usage:
#   docker build --build-arg GO_VERSION=1.24.12 -t avalanchego-rpm-builder .github/packaging/
#   docker run --rm -v .:/build -v ./build/rpm:/output avalanchego-rpm-builder ...

FROM rockylinux:9

ARG GO_VERSION=INVALID
ARG TARGETARCH

# Install build dependencies
RUN dnf install -y \
        gcc \
        make \
        rpm-build \
        rpm-sign \
        gnupg2 \
        git \
    && dnf clean all

# Install Go
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

# Install nfpm
ARG NFPM_VERSION=2.41.1
# nfpm releases use x86_64 and arm64 (not aarch64)
RUN case "${TARGETARCH}" in \
        amd64) NFPM_ARCH="x86_64" ;; \
        arm64) NFPM_ARCH="arm64" ;; \
        *) echo "Unsupported arch: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/goreleaser/nfpm/releases/download/v${NFPM_VERSION}/nfpm_${NFPM_VERSION}_linux_${NFPM_ARCH}.tar.gz" \
    | tar -C /usr/local/bin -xz nfpm

WORKDIR /build
