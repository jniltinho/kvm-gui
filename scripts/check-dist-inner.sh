#!/usr/bin/env bash
# Runs inside a clean distribution container for scripts/check-dist.sh: check-dist-inner.sh <mode> <dist>
set -euo pipefail
mode=$1 dist=$2
export DEBIAN_FRONTEND=noninteractive
apt=false
[[ $dist == rocky10 ]] || apt=true

pkgs() { # install distribution packages quietly
	if $apt; then
		apt-get update -qq >/dev/null
		apt-get install -y -qq "$@" >/dev/null
	else
		dnf install -y -q "$@" >/dev/null
	fi
}
epel() { $apt || dnf install -y -q epel-release >/dev/null; }

# self_test runs a command's --self-test as an ordinary user under Xvfb with a session bus.
self_test() {
	id tester >/dev/null 2>&1 || useradd -m tester
	su tester -s /bin/bash -c "export XDG_RUNTIME_DIR=\$(mktemp -d) APPIMAGE_EXTRACT_AND_RUN=1; Xvfb :57 -screen 0 1280x800x24 >/dev/null 2>&1 & sleep 1; DISPLAY=:57 dbus-run-session -- timeout 120 $1 --self-test" \
		|| { echo "FAIL: $1 --self-test" >&2; exit 1; }
	echo "ok: $1 --self-test"
}
x11() { if $apt; then pkgs xvfb dbus-daemon 2>/dev/null || pkgs xvfb dbus; else pkgs xorg-x11-server-Xvfb dbus-daemon dbus-tools 2>/dev/null || pkgs xorg-x11-server-Xvfb dbus; fi; }

case $mode in
package)
	epel
	if $apt; then pkgs /dist/kvm-gui_*_amd64.deb; else pkgs /dist/kvm-gui-*.x86_64.rpm; fi
	x11
	self_test kvm-gui
	[[ $(readlink -f /usr/bin/macos-kvm-gui) == /usr/bin/kvm-gui ]] || { echo "FAIL: macos-kvm-gui link" >&2; exit 1; }
	echo "ok: /usr/bin/macos-kvm-gui -> kvm-gui"
	;;
libs)
	epel
	if $apt; then pkgs libgtk-4-1 libwebkitgtk-6.0-4; else pkgs gtk4 webkitgtk6.0; fi
	x11
	cp /dist/kvm-gui-*-x86_64.AppImage /dist/kvm-gui-v*-linux-amd64 /usr/local/bin/
	chmod 755 /usr/local/bin/kvm-gui-*
	self_test "$(ls /usr/local/bin/kvm-gui-*-x86_64.AppImage)"
	self_test "$(ls /usr/local/bin/kvm-gui-v*-linux-amd64)"
	;;
norecommends)
	epel
	if $apt; then
		apt-get update -qq >/dev/null
		apt-get install -y -qq --no-install-recommends /dist/kvm-gui_*_amd64.deb >/dev/null
		qemu=qemu-system-x86_64
		$qemu -display help | grep -qx gtk || { echo "FAIL: QEMU has no gtk display module" >&2; exit 1; }
		echo "ok: QEMU display modules: $($qemu -display help | grep -vE '^(Available|$)' | tr '\n' ' ')"
	else
		dnf install -y -q --setopt=install_weak_deps=False /dist/kvm-gui-*.x86_64.rpm >/dev/null
		qemu=/usr/libexec/qemu-kvm
	fi
	! rpm -q swtpm >/dev/null 2>&1 && ! dpkg -s swtpm >/dev/null 2>&1 && echo "ok: swtpm not installed (recommended only)"
	# A short headless VM: TCG (no /dev/kvm in the container), no disk, stopped by the timeout.
	set +e
	timeout 5 $qemu -nodefaults -display none -machine q35 -accel tcg -m 128 -serial none -monitor none
	rc=$?
	set -e
	[[ $rc == 124 ]] || { echo "FAIL: $qemu exited with $rc" >&2; exit 1; }
	echo "ok: $qemu runs a headless VM"
	;;
upgrade)
	epel
	if $apt; then pkgs /old/macos-kvm-gui_0.13.0_amd64.deb; else pkgs /old/macos-kvm-gui-0.13.0-1.x86_64.rpm; fi
	id tester >/dev/null 2>&1 || useradd -m tester
	su tester -s /bin/bash -c 'macos-kvm-gui vm create "Upgrade test" --version sonoma --folder /home/tester/vms/upgrade >/dev/null 2>&1 || macos-kvm-gui vm list' >/dev/null
	su tester -s /bin/bash -c 'macos-kvm-gui vm list' | grep -q "Upgrade test" || { echo "FAIL: the old app has no VM" >&2; exit 1; }
	if $apt; then
		pkgs /dist/kvm-gui_*_amd64.deb
		! dpkg -s macos-kvm-gui 2>/dev/null | grep -q "^Status: install ok installed" || { echo "FAIL: macos-kvm-gui still installed" >&2; exit 1; }
		[[ $(dpkg -S /usr/bin/macos-kvm-gui | cut -d: -f1) == kvm-gui ]] || { echo "FAIL: the link is not kvm-gui's" >&2; exit 1; }
	else
		dnf install -y -q /dist/kvm-gui-*.x86_64.rpm >/dev/null
		! rpm -q macos-kvm-gui >/dev/null 2>&1 || { echo "FAIL: macos-kvm-gui still installed" >&2; exit 1; }
		[[ $(rpm -qf /usr/bin/macos-kvm-gui --qf '%{NAME}') == kvm-gui ]] || { echo "FAIL: the link is not kvm-gui's" >&2; exit 1; }
	fi
	echo "ok: macos-kvm-gui replaced, its link belongs to kvm-gui"
	su tester -s /bin/bash -c 'kvm-gui vm list' | grep -q "Upgrade test" || { echo "FAIL: the VM list did not survive" >&2; exit 1; }
	su tester -s /bin/bash -c 'macos-kvm-gui vm list' | grep -q "Upgrade test"
	echo "ok: the VM list survives the upgrade (kvm-gui and the macos-kvm-gui link)"
	;;
*) echo "unknown mode $mode" >&2; exit 2 ;;
esac
