#!/usr/bin/env bash
# Runs inside a clean distribution container for scripts/check-dist.sh: check-dist-inner.sh <mode> <dist>
# Every mode runs as root in a container with Docker's default confinement (its /proc/sys is
# read-only: a package's sysctl settings never reach the host). The GUI self-tests need WebKit's
# sandbox, which needs an unconfined container: they are only listed here (/selftests), and
# check-dist.sh runs them in a second, unconfined container of this one's image as "tester", who
# cannot write the host's /proc/sys either. Mode "selftests" runs that list.
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

# self_test lists a command whose --self-test the second container runs as tester (see the top).
self_test() {
	id tester >/dev/null 2>&1 || useradd -m tester
	echo "$1" >>/selftests
}

# run_selftests (as tester, in the unconfined container): each listed --self-test with a session
# bus, under Xvfb, or on Rocky Linux 10 (no Xorg server in RHEL 10) under a headless Weston, on Wayland.
run_selftests() {
	[[ $(id -u) != 0 ]] || { echo "FAIL: the self-tests run as tester, never as root" >&2; exit 1; }
	local display="Xvfb :57 -screen 0 1280x800x24 >/dev/null 2>&1 & sleep 1; export DISPLAY=:57"
	$apt || display="weston --backend=headless --socket=wl-57 --idle-time=0 >/dev/null 2>&1 & sleep 2; export WAYLAND_DISPLAY=wl-57 GDK_BACKEND=wayland"
	while read -r cmd; do
		bash -c "export XDG_RUNTIME_DIR=\$(mktemp -d) APPIMAGE_EXTRACT_AND_RUN=1; $display; dbus-run-session -- timeout 120 $cmd --self-test" \
			|| { echo "FAIL: $cmd --self-test" >&2; exit 1; }
		echo "ok: $cmd --self-test"
	done </selftests
}
x11() { if $apt; then pkgs xvfb dbus-daemon 2>/dev/null || pkgs xvfb dbus; else # the test display only: EPEL's weston needs CRB's libturbojpeg (kvm-gui itself does not)
	pkgs dnf-plugins-core && dnf config-manager --set-enabled crb && { pkgs weston dbus-daemon 2>/dev/null || pkgs weston dbus; }
fi; }

case $mode in
selftests)
	run_selftests
	;;
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
	# exactly the README's commands for the AppImage and the binary
	if $apt; then pkgs libgtk-4-1 libwebkitgtk-6.0-4 qemu-system-x86 qemu-utils ovmf pkexec; else pkgs gtk4 webkitgtk6.0 qemu-kvm qemu-img edk2-ovmf polkit; fi
	command -v pkexec >/dev/null || { echo "FAIL: no pkexec for Set up host" >&2; exit 1; }
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
	if rpm -q swtpm >/dev/null 2>&1 || dpkg -s swtpm 2>/dev/null | grep -q "^Status: install ok installed"; then
		echo "FAIL: swtpm was installed without recommended packages" >&2
		exit 1
	fi
	echo "ok: swtpm not installed (recommended only)"
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
	withvms=true
	if $apt; then
		pkgs /old/macos-kvm-gui_0.13.0_amd64.deb
	else
		# The v0.13.0 .rpm requires qemu-system-x86, which Rocky Linux 10 does not have, so it never
		# installed there: forced in with --nodeps to check Obsoletes; the VM list part needs Ubuntu.
		pkgs qemu-kvm qemu-img
		rpm -i --nodeps /old/macos-kvm-gui-0.13.0-1.x86_64.rpm
		withvms=false
	fi
	id tester >/dev/null 2>&1 || useradd -m tester
	if $withvms; then
		su tester -s /bin/bash -c 'macos-kvm-gui vm create "Upgrade test" --version sonoma --folder /home/tester/vms/upgrade >/dev/null 2>&1 || macos-kvm-gui vm list' >/dev/null
		su tester -s /bin/bash -c 'macos-kvm-gui vm list' | grep -q "Upgrade test" || { echo "FAIL: the old app has no VM" >&2; exit 1; }
	fi
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
	$withvms || exit 0
	su tester -s /bin/bash -c 'kvm-gui vm list' | grep -q "Upgrade test" || { echo "FAIL: the VM list did not survive" >&2; exit 1; }
	su tester -s /bin/bash -c 'macos-kvm-gui vm list' | grep -q "Upgrade test"
	echo "ok: the VM list survives the upgrade (kvm-gui and the macos-kvm-gui link)"
	;;
*) echo "unknown mode $mode" >&2; exit 2 ;;
esac
