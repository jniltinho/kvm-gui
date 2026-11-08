#!/usr/bin/env bash
# Runs inside the clean container of check-gnome.sh: root installs, then the session as "tester".
set -euo pipefail
v=$1
if [[ $(id -u) == 0 ]]; then
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq >/dev/null
	apt-get install -y -qq --no-install-recommends /dist/kvm-gui_*_amd64.deb mutter dbus-daemon dbus x11-utils wmctrl procps \
		$([[ $v == 24.04 ]] && echo xvfb || echo xwayland) >/dev/null 2>&1 || { echo "FAIL: install" >&2; exit 1; }
	useradd -m tester
	exec su tester -s /bin/bash -c "bash /check.sh $v"
fi
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-$(mktemp -d)} KVM_GUI_SAFE_GPU=1
[[ -n ${DBUS_SESSION_BUS_ADDRESS:-} ]] || exec dbus-run-session -- bash /check.sh "$v"
log=~/.local/state/kvm-gui/logs/embed.log
fail() {
	echo "FAIL: $*" >&2
	for f in "$log" /tmp/gui.log /tmp/mutter.log /tmp/fake.log; do echo "--- $f" >&2; tail -20 "$f" >&2 2>/dev/null || true; done
	pgrep -a 'kvm-gui|mutter|Xwayland|Xvfb' >&2 || true
	exit 1
}
# until <seconds> <command…>: polls every 0.5 s
until_() { local n=$(($1 * 2)); shift; while ((n-- > 0)); do "$@" && return 0; sleep 0.5; done; return 1; }
is() { [[ $(timeout 5 /fake/fakereims where "Reims vGPU ($1)") == "$2" ]]; }
alive() { [[ $(awk '{print $3}' "/proc/$1/stat" 2>/dev/null) != Z && -e /proc/$1 ]]; }

if [[ $v == 24.04 ]]; then
	Xvfb :57 -screen 0 1600x1000x24 >/dev/null 2>&1 &
	export DISPLAY=:57
	sleep 1
	mutter --x11 --replace >/tmp/mutter.log 2>&1 &
else
	# GNOME on Wayland: mutter starts XWayland itself (Reims vGPU's window is an X11 client).
	mutter --headless --wayland --virtual-monitor 1600x1000 --wayland-display wl-57 >/tmp/mutter.log 2>&1 &
	export WAYLAND_DISPLAY=wl-57
	until_ 10 bash -c 'ls "$XDG_RUNTIME_DIR"/.mutter-Xwaylandauth.* >/dev/null 2>&1' || fail "mutter started no XWayland"
	export DISPLAY=:0 XAUTHORITY=$(ls "$XDG_RUNTIME_DIR"/.mutter-Xwaylandauth.*) # what mutter gives its own clients
fi
sleep 2
for n in A B; do
	kvm-gui vm create "Reims $n" --version sequoia >/dev/null
	kvm-gui config set graphics=reims-vgpu >/dev/null
done
ids=$(kvm-gui vm list | grep -o 'reims-[ab]' | sort -u | tr '\n' ' ')
[[ $ids == "reims-a reims-b " ]] || fail "VM ids: '$ids'"
mkdir -p /tmp/bin && ln -s /fake/fakereims /tmp/bin/qemu-system-x86_64
declare -A qemu
for id in reims-a reims-b; do
	FAKE_VMID=$id /tmp/bin/qemu-system-x86_64 -qmp "unix:$XDG_RUNTIME_DIR/kvm-gui/$id/qmp-gui.sock,server=on,wait=off" \
		-device apple-gfx-pci,id=gpu 2>>/tmp/fake.log &
	qemu[$id]=$!
	until_ 10 is "$id" desktop || fail "the fake $id has no window"
done
kvm-gui vm list | grep -qi running || fail "the fake runs are not seen as running: $(kvm-gui vm list; ls -lR "$XDG_RUNTIME_DIR/kvm-gui"; cat "$XDG_RUNTIME_DIR"/kvm-gui/*/run.json)"

kvm-gui --show reims-a >/tmp/gui.log 2>&1 &
gui=$!
[[ $v == 26.04 ]] || until_ 30 bash -c 'wmctrl -l | grep -q "Reims A \[Running\]"' || fail "the VM window of Reims A did not open"
until_ 30 grep -q "reims-a" "$log" || fail "the VM window of Reims A did not ask for its screen"
kvm-gui --show reims-b >/dev/null 2>&1 || true # the running app opens it
if [[ $v == 26.04 ]]; then
	until_ 30 grep -q "reims-b: not embedded in the vm window" "$log" || fail "the app did not decline embedding on Wayland"
	is reims-a desktop && is reims-b desktop || fail "a VM window was taken on Wayland"
	alive "${qemu[reims-a]}" && alive "${qemu[reims-b]}" || fail "a fake QEMU died"
	echo "ok: GNOME $v on Wayland: no embedding, the VM windows stay their own"
	exit 0
fi
until_ 40 is reims-a embedded || fail "Reims A was not embedded"
until_ 40 is reims-b embedded || fail "Reims B was not embedded"
echo "ok: two VM windows embedded under mutter on X11"
wmctrl -c "Reims A [Running]"
until_ 10 is reims-a desktop || fail "closing its VM window did not give Reims A's window back"
sleep 2
is reims-b embedded || fail "Reims B left its VM window"
alive "${qemu[reims-a]}" || fail "Reims A's QEMU died after its window was given back"
echo "ok: closing one VM window gave its VM's window back, the other stayed embedded"
kill -KILL "$gui"
until_ 10 is reims-b desktop || fail "after the SIGKILL Reims B's window is $(/fake/fakereims where 'Reims vGPU (reims-b)')"
sleep 2
alive "${qemu[reims-a]}" && alive "${qemu[reims-b]}" || fail "a fake QEMU died: $(cat /tmp/fake.log)"
echo "ok: the app killed with SIGKILL: Reims B's window is back on the desktop, both QEMUs alive"
cat "$log"

