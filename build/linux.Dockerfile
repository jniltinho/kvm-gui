# The one build image of kvm-gui: every make target runs in it, so the host needs only Docker and
# make. Ubuntu 24.04 is the oldest supported ABI (glibc 2.39).
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
# Tests: qemu-img for disk and media, QEMU for the device checks, Xvfb for GUI checks; packaging:
# dpkg-dev and rpm; the OpenCore validator fetch: curl and unzip.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git unzip xz-utils file build-essential pkg-config \
      qemu-utils qemu-system-x86 xvfb dpkg-dev rpm \
    && rm -rf /var/lib/apt/lists/*
ARG GO_VERSION=1.27.3
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz | tar -C /usr/local -xz
# Node runs the OpenSpec validator now and the frontend build later.
ARG NODE_VERSION=24.11.1
ARG OPENSPEC_VERSION=1.14.2
RUN curl -fsSL https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz \
      | tar -C /usr/local --strip-components=1 -xJ \
    && npm install -g --no-audit --no-fund @fission-ai/openspec@${OPENSPEC_VERSION}
ENV PATH=/usr/local/go/bin:$PATH
