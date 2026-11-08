# The one build image of kvm-gui: every make target runs in it, so the host needs only Docker and
# make. Ubuntu 24.04 is the oldest supported ABI (glibc 2.39).
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
# The Wails window: GTK 4 and WebKitGTK 6.0 headers (cgo). Tests: qemu-img for disk and media, QEMU,
# swtpm and OVMF for guests, xorriso for test ISOs, Xvfb, a window manager and X11/Vulkan clients
# (lavapipe) for the window and embedding checks. Packaging: dpkg-dev, rpm, desktop-file-utils and
# appimagetool. The OpenCore validator fetch: curl and unzip.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git unzip xz-utils file build-essential pkg-config \
      libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev \
      qemu-utils qemu-system-x86 swtpm swtpm-tools ovmf xorriso \
      xvfb openbox x11-apps x11-utils xdotool imagemagick vulkan-tools mesa-vulkan-drivers \
      dbus dbus-x11 mtools dpkg-dev rpm desktop-file-utils librsvg2-bin \
    && rm -rf /var/lib/apt/lists/*
ARG APPIMAGETOOL_VERSION=1.9.1
ARG APPIMAGE_RUNTIME_VERSION=20251108
RUN curl -fsSL -o /usr/local/bin/appimagetool \
      https://github.com/AppImage/appimagetool/releases/download/${APPIMAGETOOL_VERSION}/appimagetool-x86_64.AppImage \
    && curl -fsSL -o /usr/local/share/appimage-runtime-x86_64 \
      https://github.com/AppImage/type2-runtime/releases/download/${APPIMAGE_RUNTIME_VERSION}/runtime-x86_64 \
    && mkdir -p /usr/local/share/appimage-runtime-licenses && cd /usr/local/share/appimage-runtime-licenses \
    && curl -fsSL -o type2-runtime.txt https://raw.githubusercontent.com/AppImage/type2-runtime/${APPIMAGE_RUNTIME_VERSION}/LICENSE \
    && curl -fsSL -o musl.txt https://git.musl-libc.org/cgit/musl/plain/COPYRIGHT \
    && curl -fsSL -o libfuse-LGPL2.txt https://raw.githubusercontent.com/libfuse/libfuse/master/LGPL2.txt \
    && curl -fsSL -o squashfuse.txt https://raw.githubusercontent.com/vasi/squashfuse/master/LICENSE \
    && curl -fsSL -o zstd.txt https://raw.githubusercontent.com/facebook/zstd/dev/LICENSE \
    && curl -fsSL -o zlib.txt https://raw.githubusercontent.com/madler/zlib/develop/LICENSE \
    && cd / \
    && chmod +x /usr/local/bin/appimagetool
ARG GO_VERSION=1.27.3
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz | tar -C /usr/local -xz
# Node runs the OpenSpec validator and the frontend build (Vite Plus).
ARG NODE_VERSION=24.11.1
ARG OPENSPEC_VERSION=1.14.2
RUN curl -fsSL https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz \
      | tar -C /usr/local --strip-components=1 -xJ \
    && npm install -g --no-audit --no-fund @fission-ai/openspec@${OPENSPEC_VERSION}
# No FUSE in containers: appimagetool runs extracted.
ENV PATH=/usr/local/go/bin:$PATH APPIMAGE_EXTRACT_AND_RUN=1
