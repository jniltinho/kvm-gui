#!/usr/bin/env bash
# Packages a built binary into dist/: kvm-gui's .deb (Ubuntu 24.04, 26.04, Debian 13) and .rpm
# (Rocky Linux 10). Both replace an installed macos-kvm-gui and keep that name as a command link.
# Runs in the build image (make package). Usage: scripts/package.sh <version, e.g. v1.0.0> <binary>
set -euo pipefail
VERSION="${1:?version}"
BIN="${2:?binary}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
DEBVER="${VERSION#v}"
mkdir -p "$DIST"
# Remove earlier packages only: the binary lives in dist/ too.
rm -rf "$DIST"/*.deb "$DIST"/*.rpm "$DIST/deb" "$DIST/rpm"

PKG="$DIST/deb"
install -D -m 0755 "$BIN" "$PKG/usr/bin/kvm-gui"
ln -s kvm-gui "$PKG/usr/bin/macos-kvm-gui"
install -D -m 0644 "$ROOT/internal/assets/logo/logo.svg" "$PKG/usr/share/icons/hicolor/scalable/apps/kvm-gui.svg"
install -D -m 0644 "$ROOT/build/linux/kvm-gui.desktop" "$PKG/usr/share/applications/kvm-gui.desktop"
install -D -m 0644 "$ROOT/LICENSE" "$PKG/usr/share/doc/kvm-gui/copyright"
install -D -m 0644 "$ROOT/THIRD_PARTY_NOTICES.md" "$PKG/usr/share/doc/kvm-gui/THIRD_PARTY_NOTICES.md"
install -D -m 0644 "$ROOT/THIRD_PARTY_LICENSES.txt" "$PKG/usr/share/doc/kvm-gui/THIRD_PARTY_LICENSES.txt"

mkdir -p "$PKG/DEBIAN"
cat >"$PKG/DEBIAN/control" <<EOF
Package: kvm-gui
Version: $DEBVER
Section: utils
Priority: optional
Architecture: amd64
Depends: libgtk-4-1, libwebkitgtk-6.0-4, qemu-system-x86, qemu-system-gui, qemu-utils, ovmf, pkexec
Recommends: swtpm
Replaces: macos-kvm-gui
Breaks: macos-kvm-gui
Provides: macos-kvm-gui
Maintainer: Nilton OS <jniltinho@gmail.com>
Homepage: https://github.com/jniltinho/kvm-gui
Description: Create and run macOS and other VMs on Linux KVM (home lab use)
 One binary with built-in OpenCore and OVMF: a desktop GUI, a command line
 and an MCP server that check the host, download and verify macOS from
 Apple, create disks and control the VMs. macOS's license only allows it
 on Apple-branded hardware.
EOF
dpkg-deb --build --root-owner-group "$PKG" "$DIST/kvm-gui_${DEBVER}_amd64.deb" >/dev/null
rm -rf "$PKG/DEBIAN"

# RPM from the same tree. Rocky Linux 10 ships QEMU as qemu-kvm (/usr/libexec/qemu-kvm).
RPMVER="${DEBVER/-/\~}"
RPMVER="${RPMVER//-/.}"
TOP="$DIST/rpm"
mkdir -p "$TOP/SPECS"
cat >"$TOP/SPECS/kvm-gui.spec" <<EOF
Name:           kvm-gui
Version:        $RPMVER
Release:        1
Summary:        Create and run macOS and other VMs on Linux KVM (home lab use)
License:        BSD-3-Clause
URL:            https://github.com/jniltinho/kvm-gui
BuildArch:      x86_64
Requires:       gtk4, webkitgtk6.0, qemu-kvm, qemu-img, edk2-ovmf, polkit
Recommends:     swtpm
Obsoletes:      macos-kvm-gui < 1.0.0
Provides:       macos-kvm-gui = %{version}
AutoReqProv:    no
%description
One binary with built-in OpenCore and OVMF: checks the host, downloads and
verifies macOS from Apple, creates disks and controls the VMs, from the
command line and an MCP server. macOS's license only allows it on
Apple-branded hardware.
%install
cp -a "$PKG"/. %{buildroot}/
%files
%license /usr/share/doc/kvm-gui/copyright
/usr/bin/kvm-gui
/usr/bin/macos-kvm-gui
/usr/share/icons/hicolor/scalable/apps/kvm-gui.svg
/usr/share/applications/kvm-gui.desktop
/usr/share/doc/kvm-gui/THIRD_PARTY_NOTICES.md
%license /usr/share/doc/kvm-gui/THIRD_PARTY_LICENSES.txt
EOF
rpmbuild --quiet -bb --define "_topdir $TOP" --define "_build_id_links none" --define "__strip /bin/true" \
  "$TOP/SPECS/kvm-gui.spec" >/dev/null
mv "$TOP"/RPMS/x86_64/*.rpm "$DIST/"
rm -rf "$PKG" "$TOP"
ls -lh "$DIST"/*.deb "$DIST"/*.rpm
