# syntax=docker/dockerfile:1

FROM golang:1.26.6-trixie

ARG DEBIAN_FRONTEND=noninteractive
ARG TARGETARCH
ARG TSDUCK_VERSION=3.45-4798
ARG TSDUCK_SHA256_AMD64=b9960bb4347384342e583f3df7301c1c23489956ad0acf3e8f05703ff36c2259
ARG TSDUCK_SHA256_ARM64=f529beac3a5b7962d3d1c9be083e013212de03e4eba55d6d5288c64126f166fc

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        ffmpeg \
        git \
        make \
    && case "${TARGETARCH}" in \
        amd64) tsduck_sha256="${TSDUCK_SHA256_AMD64}" ;; \
        arm64) tsduck_sha256="${TSDUCK_SHA256_ARM64}" ;; \
        *) echo "Unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && curl --fail --location --show-error \
        --output /tmp/tsduck.deb \
        "https://github.com/tsduck/tsduck/releases/download/v${TSDUCK_VERSION}/tsduck_${TSDUCK_VERSION}.debian13_${TARGETARCH}.deb" \
    && echo "${tsduck_sha256}  /tmp/tsduck.deb" | sha256sum --check - \
    && apt-get install -y --no-install-recommends /tmp/tsduck.deb \
    && rm -f /tmp/tsduck.deb \
    && rm -rf /var/lib/apt/lists/* \
    && git config --system --add safe.directory /workspace

WORKDIR /workspace

CMD ["bash"]
