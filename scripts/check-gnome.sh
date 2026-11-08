#!/usr/bin/env bash
# Embedding under GNOME's window manager, in a throwaway container with the .deb from dist/ (task 6.4):
#   scripts/check-gnome.sh <24.04|26.04>
# 24.04, GNOME on X11 (mutter 46): two fake Reims vGPU runs are embedded into their VM windows;
# closing one window puts its VM's window back on the desktop while the other stays embedded; a
# SIGKILL of the app puts the other back too (its embed guard), and neither fake QEMU loses its window.
# 26.04, GNOME on Wayland only (mutter 50, XWayland on demand): the app embeds nothing and both VM
# windows stay their own (design D6: XWayland crashes when an app holding a window dies).
set -euo pipefail
v=${1:?usage: check-gnome.sh <24.04|26.04>}
[[ $v == 24.04 || $v == 26.04 ]] || { echo "unknown Ubuntu $v" >&2; exit 2; }
root=$(cd "$(dirname "$0")/.." && pwd)
ls "$root"/dist/kvm-gui_*_amd64.deb >/dev/null
fake=$(mktemp -d) && chmod 755 "$fake"
trap 'rm -rf "$fake"' EXIT
docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/gomod -e GOFLAGS=-buildvcs=false \
	-e CGO_ENABLED=0 -v "$root:/src" -v "$HOME/.cache/kvm-gui-build:/cache" -v "$fake:/out" -w /src kvm-gui-build:24.04 \
	go build -o /out/fakereims ./internal/embed/testdata/fakereims
docker run --rm --init --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt systempaths=unconfined \
	-v "$root/dist:/dist:ro" -v "$fake:/fake:ro" -v "$root/scripts/check-gnome-inner.sh:/check.sh:ro" \
	"ubuntu:$v" timeout 600 bash /check.sh "$v"
echo "=== GNOME $v: passed"
