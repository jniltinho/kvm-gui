#!/usr/bin/env bash
# Embedding under GNOME's window manager, in a throwaway container with the .deb from dist/ (task 6.4):
#   scripts/check-gnome.sh <24.04|26.04>
# 24.04, GNOME on X11 (mutter 46): two fake Reims vGPU runs are embedded into their VM windows;
# closing one window puts its VM's window back on the desktop while the other stays embedded; a
# SIGKILL of the app puts the other back too (the root-target save-set), and neither fake QEMU loses
# its window.
# 26.04, GNOME on Wayland only (mutter 50, XWayland on demand): the app embeds nothing and both VM
# windows stay their own (design D6: XWayland crashes when an app holding a window dies).
set -euo pipefail
v=${1:?usage: check-gnome.sh <24.04|26.04>}
[[ $v == 24.04 || $v == 26.04 ]] || { echo "unknown Ubuntu $v" >&2; exit 2; }
root=$(cd "$(dirname "$0")/.." && pwd)
ls "$root"/dist/kvm-gui_*_amd64.deb >/dev/null
fake=$(mktemp -d) && chmod 755 "$fake"
docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/gomod -e GOFLAGS=-buildvcs=false \
	-e CGO_ENABLED=0 -v "$root:/src" -v "$HOME/.cache/kvm-gui-build:/cache" -v "$fake:/out" -w /src kvm-gui-build:24.04 \
	go build -o /out/fakereims ./internal/embed/testdata/fakereims
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
# Root installs in a confined container; the session runs unconfined (WebKit's sandbox) as tester.
name="kvm-gui-gnome-$$" img="kvm-gui-gnome:$$"
trap 'rm -rf "$fake"; docker rm -f "$name" >/dev/null 2>&1; docker rmi -f "$img" >/dev/null 2>&1' EXIT
docker run --name "$name" -v "$root/dist:/dist:ro" -v "$root/scripts/check-gnome-inner.sh:/check.sh:ro" "ubuntu:$v" bash /check.sh "$v"
docker commit "$name" "$img" >/dev/null
docker run --rm --init --user tester --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined \
	-v "$fake:/fake:ro" -v "$root/scripts/check-gnome-inner.sh:/check.sh:ro" "$img" timeout 600 bash /check.sh "$v"
check_host
echo "=== GNOME $v: passed"
