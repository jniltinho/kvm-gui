package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeStat writes /proc/<pid>/stat with start time ticks (field 22).
func fakeStat(t *testing.T, proc, pid string, ticks string) {
	f := make([]string, 20)
	for i := range f {
		f[i] = "0"
	}
	f[19] = ticks
	write(t, filepath.Join(proc, pid, "stat"), pid+" (qemu-system-x86) "+strings.Join(f, " ")+"\n")
}

func fakeRecord(t *testing.T, r RunRecord) string {
	dir, err := makeRunDir(r.VMID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(r)
	write(t, filepath.Join(dir, "run.json"), string(data))
	return dir
}

// Runs belong to the VM their run.json names; a shared disk only marks the other VM busy, and a
// stale record (a reused pid) is nobody's run (task 4.1).
func TestRunRecords(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	proc, root := t.TempDir(), t.TempDir()
	a := VMEntry{ID: "a", Config: testDefaults()}
	a.Config.Workspace = filepath.Join(root, "a")
	write(t, filepath.Join(a.Config.Workspace, DiskFile), "")
	dir := fakeRecord(t, RunRecord{VMID: "a", PID: 500, StartTicks: 42, Starter: StarterGUI})
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("run dir mode %v", st.Mode())
	}
	fakeProc(t, proc, "500", "qemu-system-x86", "", "qemu-system-x86_64", "-drive", "file="+filepath.Join(a.Config.Workspace, DiskFile),
		"-qmp", "unix:"+filepath.Join(dir, "qmp.sock")+",server=on,wait=off", "-qmp", "unix:"+filepath.Join(dir, "qmp-control.sock")+",server=on,wait=off")
	fakeStat(t, proc, "500", "42")
	if r, ok := findRunIn(proc, a); !ok || r.VMID != "a" || r.Starter != StarterGUI || !r.ByApp || r.Control == "" || r.OtherVM != "" {
		t.Fatalf("own run: %+v %v", r, ok)
	}
	// Another VM using the same disk: busy, without sockets.
	b := VMEntry{ID: "b", Config: a.Config}
	if r, ok := findRunIn(proc, b); !ok || r.OtherVM != "a" || r.Control != "" || r.VNCSocket != "" {
		t.Fatalf("shared disk: %+v %v", r, ok)
	}
	// A reused pid (another start time): the record is stale.
	fakeStat(t, proc, "500", "43")
	if r, ok := findRunIn(proc, a); !ok || r.VMID != "" || r.Starter != "" {
		t.Fatalf("stale record: %+v %v", r, ok)
	}
	// A VM without a disk (a live ISO) is found through its record only.
	live := VMEntry{ID: "live", Config: testGuest(t, "ubuntu")}
	live.Config.Disks = nil
	if _, ok := findRunIn(proc, live); ok {
		t.Fatal("no record, no run")
	}
	fakeRecord(t, RunRecord{VMID: "live", PID: 600, StartTicks: 7, Starter: StarterCLI})
	fakeProc(t, proc, "600", "qemu-kvm", "", "qemu-kvm", "-cdrom", "x.iso")
	fakeStat(t, proc, "600", "7")
	if r, ok := findRunIn(proc, live); !ok || r.PID != 600 || r.Starter != StarterCLI {
		t.Fatalf("diskless run: %+v %v", r, ok)
	}
	// The real writer: this process's own start time round-trips.
	d, _ := makeRunDir("self")
	if err := writeRunRecord(d, "self", StarterMCP, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if r, ok := readRunRecord("/proc", "self"); !ok || r.PID != os.Getpid() || r.Generation == "" {
		t.Fatalf("own record: %+v %v", r, ok)
	}
	// A run directory others can open is refused.
	os.Chmod(d, 0o755)
	if _, ok := readRunRecord("/proc", "self"); ok {
		t.Fatal("a 0755 run directory was trusted")
	}
	if _, err := makeRunDir("../x"); err == nil {
		t.Fatal("a VM id with a slash was accepted")
	}
}

// Two VMs on one image (one through a link) cannot both be starting: the second one's image lock
// is busy; a VM on another image is not (task 4.1).
func TestImageLocks(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	root := t.TempDir()
	img := filepath.Join(root, "shared.qcow2")
	write(t, img, "")
	os.Symlink(img, filepath.Join(root, "alias.qcow2"))
	write(t, filepath.Join(root, "own.qcow2"), "")
	vm := func(dir string, disks ...string) Config {
		c := testGuest(t, "ubuntu")
		c.Workspace = filepath.Join(root, dir)
		os.MkdirAll(c.Workspace, 0o700)
		c.Disks = nil
		for _, d := range disks {
			c.Disks = append(c.Disks, Disk{Path: filepath.Join(root, d), Format: "qcow2", Bus: BusVirtio})
		}
		return c
	}
	a, b, other := vm("a", "own.qcow2", "shared.qcow2"), vm("b", "alias.qcow2"), vm("c", "own.qcow2")
	unlock, err := (&VM{ID: "a"}).lockRun(NewWorkspace(a.Workspace), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&VM{ID: "b"}).lockRun(NewWorkspace(b.Workspace), b); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second VM on the shared image (through a link): %v", err)
	}
	if _, err := (&VM{ID: "c"}).lockRun(NewWorkspace(other.Workspace), other); !errors.Is(err, ErrBusy) {
		t.Fatalf("a secondary disk shared with the first VM: %v", err)
	}
	unlock()
	unlock, err = (&VM{ID: "b"}).lockRun(NewWorkspace(b.Workspace), b)
	if err != nil {
		t.Fatalf("after the first VM's locks are released: %v", err)
	}
	unlock()
}
