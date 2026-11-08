#!/usr/bin/env bash
# Checks the artifacts in dist/ on a clean distribution, in throwaway containers (task 8.2):
#   scripts/check-dist.sh <ubuntu24.04|ubuntu26.04|debian13|rocky10> [package|libs|norecommends|upgrade|all]
# package:      install the .deb with apt or the .rpm with dnf, run kvm-gui --self-test
# libs:         install only the documented libraries, run the AppImage and the plain binary --self-test
# norecommends: install the package without recommended packages; the QEMU it selects runs a short headless VM
# upgrade:      (ubuntu24.04, rocky10) install the last macos-kvm-gui package, add a VM, upgrade to
#               kvm-gui: the old package is gone, the macos-kvm-gui link is kvm-gui's, the VM is listed
set -euo pipefail
dist=${1:?usage: check-dist.sh <ubuntu24.04|ubuntu26.04|debian13|rocky10> [mode]}
mode=${2:-all}
root=$(cd "$(dirname "$0")/.." && pwd)
case $dist in
ubuntu24.04) image=ubuntu:24.04 ;;
ubuntu26.04) image=ubuntu:26.04 ;;
debian13) image=debian:13 ;;
rocky10) image=rockylinux/rockylinux:10 ;;
*) echo "unknown distribution $dist" >&2; exit 2 ;;
esac
ls "$root"/dist/kvm-gui_*_amd64.deb "$root"/dist/kvm-gui-*.x86_64.rpm "$root"/dist/kvm-gui-*-x86_64.AppImage "$root"/dist/kvm-gui-*-linux-amd64 >/dev/null

old="$HOME/.cache/kvm-gui-build/old-packages"
if [[ $mode == upgrade || $mode == all ]] && [[ $dist == ubuntu24.04 || $dist == rocky10 ]]; then
	mkdir -p "$old"
	if ! ls "$old"/macos-kvm-gui_0.13.0_amd64.deb "$old"/macos-kvm-gui-0.13.0-1.x86_64.rpm >/dev/null 2>&1; then
		gh release download v0.13.0 --repo jniltinho/kvm-gui -p 'macos-kvm-gui_0.13.0_amd64.deb*' -p 'macos-kvm-gui-0.13.0-1.x86_64.rpm*' -D "$old" --clobber
	fi
	(cd "$old" && sha256sum -c macos-kvm-gui_0.13.0_amd64.deb.sha256 macos-kvm-gui-0.13.0-1.x86_64.rpm.sha256)
fi

run() { # one clean container per mode
	docker run --rm --init --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined \
		-v "$root/dist:/dist:ro" -v "$root/scripts/check-dist-inner.sh:/check.sh:ro" -v "$old:/old:ro" \
		"$image" bash /check.sh "$1" "$dist"
}
modes=$mode
[[ $mode == all ]] && modes="package libs norecommends upgrade"
for m in $modes; do
	[[ $m == upgrade && $dist != ubuntu24.04 && $dist != rocky10 ]] && continue
	echo "=== $dist: $m"
	mkdir -p "$old"
	run "$m"
done
echo "=== $dist: all passed"
