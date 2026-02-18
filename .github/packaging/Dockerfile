# Build container for RPM packaging of avalanchego and subnet-evm.
#
# Based on Rocky Linux 9 (glibc 2.34) to match RHEL 9.x target.
# Source tree is bind-mounted at runtime, not COPY'd.
#
# Usage (via build-builder-image.sh, which fetches GO_CHECKSUM automatically):
#   .github/packaging/scripts/build-builder-image.sh
#   docker run --rm -v .:/build -v ./build/rpm:/output avalanchego-rpm-builder ...

FROM rockylinux:9

ARG GO_VERSION=INVALID
ARG GO_CHECKSUM=INVALID
ARG TARGETARCH

# Install build dependencies
# - gcc: required for cgo (CGO_ENABLED=1 in scripts/constants.sh)
# - gnupg2: GPG signing of RPM packages
# - git: version detection in build scripts
RUN dnf install -y \
        gcc \
        gnupg2 \
        git \ # vcs-ok: package install, not a command invocation
    && dnf clean all

# Install Go (with SHA256 verification)
RUN curl -fsSL -o /tmp/go.tar.gz \
        "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    && echo "${GO_CHECKSUM}  /tmp/go.tar.gz" | sha256sum -c - \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# Install nfpm (with SHA256 verification via checksums.txt)
ARG NFPM_VERSION=2.41.1
# nfpm releases use x86_64 and arm64 (not aarch64)
RUN case "${TARGETARCH}" in \
        amd64) NFPM_ARCH="x86_64" ;; \
        arm64) NFPM_ARCH="arm64" ;; \
        *) echo "Unsupported arch: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    NFPM_TARBALL="nfpm_${NFPM_VERSION}_Linux_${NFPM_ARCH}.tar.gz" && \
    curl -fsSL -o /tmp/nfpm.tar.gz \
        "https://github.com/goreleaser/nfpm/releases/download/v${NFPM_VERSION}/${NFPM_TARBALL}" && \
    curl -fsSL -o /tmp/checksums.txt \
        "https://github.com/goreleaser/nfpm/releases/download/v${NFPM_VERSION}/checksums.txt" && \
    EXPECTED=$(grep "  ${NFPM_TARBALL}$" /tmp/checksums.txt | awk '{print $1}') && \
    echo "${EXPECTED}  /tmp/nfpm.tar.gz" | sha256sum -c - && \
    tar -C /usr/local/bin -xzf /tmp/nfpm.tar.gz nfpm && \
    rm /tmp/nfpm.tar.gz /tmp/checksums.txt

WORKDIR /build
