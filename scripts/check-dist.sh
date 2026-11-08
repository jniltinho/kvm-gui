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

# One clean container per mode, as root but with Docker's default confinement, so nothing the
# packages do (a sysctl --system among them) can change the host's kernel settings; then, if the mode
# listed GUI self-tests, a second container of its image, unconfined for WebKit's sandbox but as
# the ordinary user "tester", runs them (check-dist-inner.sh).
run() {
	local name="kvm-gui-check-$$-$1" img="kvm-gui-check:$$-$1"
	trap 'docker rm -f "${name:-x}" >/dev/null 2>&1; docker rmi -f "${img:-x}" >/dev/null 2>&1' RETURN EXIT
	docker run --name "$name" --init -v "$art:/dist:ro" -v "$root/scripts/check-dist-inner.sh:/check.sh:ro" -v "$old:/old:ro" \
		"$image" bash /check.sh "$1" "$dist"
	if docker cp "$name:/selftests" - >/dev/null 2>&1; then
		docker commit "$name" "$img" >/dev/null
		docker run --rm --init --user tester --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
			--security-opt systempaths=unconfined -v "$root/scripts/check-dist-inner.sh:/check.sh:ro" \
			"$img" bash /check.sh selftests "$dist"
	fi
}
# The host's kernel settings must come out as they went in (a container once lowered
# vm.max_map_count below what Reims vGPU needs).
vm_snapshot() { grep -r . /proc/sys/vm 2>/dev/null | sort || true; } # some entries are unreadable: grep fails
vm_before=$(vm_snapshot)
check_host() {
	[[ "$(vm_snapshot)" == "$vm_before" ]] && return 0
	echo "FAIL: a container changed the host's kernel settings:" >&2
	diff <(echo "$vm_before") <(vm_snapshot) >&2 || true
	echo "restore them with: sudo sysctl --system" >&2
	return 1
}
modes=$mode
[[ $mode == all ]] && modes="package libs norecommends upgrade"
for m in $modes; do
	[[ $m == upgrade && $dist != ubuntu24.04 && $dist != rocky10 ]] && continue
	echo "=== $dist: $m"
	mkdir -p "$old"
	run "$m"
	check_host
done
echo "=== $dist: all passed"
