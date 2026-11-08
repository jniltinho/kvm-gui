package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProc writes /proc/<pid>/{comm,cmdline} and a cwd link.
func fakeProc(t *testing.T, proc string, pid, comm, cwd string, args ...string) {
	d := filepath.Join(proc, pid)
	write(t, filepath.Join(d, "comm"), comm+"\n")
	write(t, filepath.Join(d, "cmdline"), strings.Join(args, "\x00")+"\x00")
	if cwd != "" {
		os.Symlink(cwd, filepath.Join(d, "cwd"))
	}
}

func TestRunningDetection(t *testing.T) {
	proc := t.TempDir()
	// Runs are found by the disk's identity (device and inode), so the disks exist.
	root := t.TempDir()
	c := testDefaults()
	c.Workspace = filepath.Join(root, "a,b")
	write(t, filepath.Join(c.Workspace, DiskFile), "")
	esc := strings.ReplaceAll(c.Workspace, ",", ",,")
	fakeProc(t, proc, "100", "bash", "", "bash", "-c", "file="+esc+"/mac_hdd_ng.img")
	if _, ok := runningIn(proc, c); ok {
		t.Fatal("only qemu processes count")
	}
	fakeProc(t, proc, "200", "qemu-system-x86", "", "qemu-system-x86_64", "-drive", "id=MacHDD,if=none,format=qcow2,file="+esc+"/mac_hdd_ng.img",
		"-vnc", "unix:/run/user/1000/macos-kvm-gui-1/vnc.sock,lock-key-sync=off", "-qmp", "unix:/run/user/1000/macos-kvm-gui-1/qmp.sock,server=on,wait=off",
		"-qmp", "unix:/run/user/1000/macos-kvm-gui-1/qmp-control.sock,server=on,wait=off",
		"-qmp", "unix:/run/user/1000/macos-kvm-gui-1/qmp-gui.sock,server=on,wait=off",
		"-netdev", "user,id=net0,hostfwd=tcp:127.0.0.1:2301-:22",
		"-drive", "id=InstallerDisc,if=none,format=raw,snapshot=on,file=/c/InstallAssistant.iso")
	r, ok := runningIn(proc, c)
	if !ok || r.PID != 200 || !r.ByApp || r.VNCSocket != "/run/user/1000/macos-kvm-gui-1/vnc.sock" ||
		r.Control != "/run/user/1000/macos-kvm-gui-1/qmp-control.sock" || r.GUI != "/run/user/1000/macos-kvm-gui-1/qmp-gui.sock" || !r.Disc || r.Reims || r.SSHPort != 2301 {
		t.Fatalf("app-started VM: %+v %v", r, ok)
	}
	// The running process tells whether it is on Reims vGPU, not the config.
	os.RemoveAll(filepath.Join(proc, "200"))
	fakeProc(t, proc, "201", "qemu-system-x86", "", "qemu-system-x86_64", "-drive", "file="+esc+"/mac_hdd_ng.img",
		"-device", "apple-gfx-pci,id=gfx,displays=1,resolution=1920x1080")
	if r, ok := runningIn(proc, c); !ok || !r.Reims {
		t.Fatalf("Reims VM: %+v %v", r, ok)
	}

	// OSX-KVM's OpenCore-Boot.sh: relative, quoted path under its working directory.
	proc2 := t.TempDir()
	o := testDefaults()
	o.Workspace = filepath.Join(root, "OSX-KVM")
	write(t, filepath.Join(o.Workspace, DiskFile), "")
	fakeProc(t, proc2, "300", "qemu-system-x86", o.Workspace, "qemu-system-x86_64", "-drive", `id=MacHDD,if=none,file="./mac_hdd_ng.img",format=qcow2`)
	if r, ok := runningIn(proc2, o); !ok || r.ByApp {
		t.Fatalf("a VM started outside the app must be found and marked as such: %+v %v", r, ok)
	}
	other := testDefaults()
	other.Workspace = filepath.Join(root, "elsewhere")
	write(t, filepath.Join(other.Workspace, DiskFile), "")
	if _, ok := runningIn(proc2, other); ok {
		t.Fatal("another workspace must not match")
	}
	// A link to the disk is the same disk.
	link := filepath.Join(root, "link.img")
	os.Symlink(filepath.Join(o.Workspace, DiskFile), link)
	fakeProc(t, proc2, "301", "qemu-kvm", "", "qemu-kvm", "-drive", "file="+link)
	os.RemoveAll(filepath.Join(proc2, "300"))
	if r, ok := runningIn(proc2, o); !ok || r.PID != 301 {
		t.Fatalf("a link to the disk (qemu-kvm): %+v %v", r, ok)
	}
}

func TestPreferences(t *testing.T) {
	isolateXDG(t, t.TempDir())
	if Preference("theme") != "" {
		t.Fatal("unset preference must be empty")
	}
	SetPreference("theme", "light")
	SetPreference("theme", "dark")
	if Preference("theme") != "dark" {
		t.Fatal("the last value must win")
	}
}
