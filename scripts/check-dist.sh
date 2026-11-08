#!/usr/bin/env bash
# Checks the artifacts in dist/ on a clean distribution, in throwaway containers (task 8.2):
#   scripts/check-dist.sh <ubuntu24.04|ubuntu26.04|debian13|rocky10> [package|libs|norecommends|upgrade|all] [artifact folder]
# The artifact folder defaults to dist/; pass a release's download folder to check those files.
# package:      install the .deb with apt or the .rpm with dnf, run kvm-gui --self-test
# libs:         install only the documented libraries, run the AppImage and the plain binary --self-test
# norecommends: install the package without recommended packages; the QEMU it selects runs a short headless VM
# upgrade:      (ubuntu24.04, rocky10) install the last macos-kvm-gui package, add a VM, upgrade to
#               kvm-gui: the old package is gone, the macos-kvm-gui link is kvm-gui's, the VM is listed
set -euo pipefail
dist=${1:?usage: check-dist.sh <ubuntu24.04|ubuntu26.04|debian13|rocky10> [mode]}
mode=${2:-all}
root=$(cd "$(dirname "$0")/.." && pwd)
art=$(cd "${3:-$root/dist}" && pwd)
case $dist in
ubuntu24.04) image=ubuntu:24.04 ;;
ubuntu26.04) image=ubuntu:26.04 ;;
debian13) image=debian:13 ;;
rocky10) image=rockylinux/rockylinux:10 ;;
*) echo "unknown distribution $dist" >&2; exit 2 ;;
esac
ls "$art"/kvm-gui_*_amd64.deb "$art"/kvm-gui-*.x86_64.rpm "$art"/kvm-gui-*-x86_64.AppImage "$art"/kvm-gui-*-linux-amd64 >/dev/null
(cd "$art" && sha256sum -c --quiet SHA256SUMS)

old="$HOME/.cache/kvm-gui-build/old-packages"
if [[ $mode == upgrade || $mode == all ]] && [[ $dist == ubuntu24.04 || $dist == rocky10 ]]; then
	mkdir -p "$old"
	if ! ls "$old"/macos-kvm-gui_0.13.0_amd64.deb "$old"/macos-kvm-gui-0.13.0-1.x86_64.rpm >/dev/null 2>&1; then
		gh release download v0.13.0 --repo jniltinho/kvm-gui -p macos-kvm-gui_0.13.0_amd64.deb -p macos-kvm-gui-0.13.0-1.x86_64.rpm -D "$old" --clobber
	fi
	# Pinned here, not taken from the release's own .sha256 files: v0.13.0 is the upgrade fixture
	# (AGENTS.md keeps it out of the release rotation).
	(cd "$old" && sha256sum -c - <<'SUMS'
6a637b78057d2a62d865534699dc233247fc4902b147760b1445265388ea212c  macos-kvm-gui_0.13.0_amd64.deb
08f606c83d1fcbbd5a6dc339afaae73979f036c5074313804b5e6d2bbf0ff83e  macos-kvm-gui-0.13.0-1.x86_64.rpm
SUMS
	)
fi

run() { # one clean container per mode
	docker run --rm --init --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined \
		-v "$art:/dist:ro" -v "$root/scripts/check-dist-inner.sh:/check.sh:ro" -v "$old:/old:ro" \
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
