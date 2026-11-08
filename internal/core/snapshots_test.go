package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func needQemuImg(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"qemu-img", "qemu-io"} {
		if _, err := exec.LookPath(tool); err != nil {
			needTool(t, tool+" missing")
		}
	}
}

func qemuIO(t *testing.T, disk, cmd string) string {
	t.Helper()
	out, err := exec.Command("qemu-io", "-f", "qcow2", "-c", cmd, disk).CombinedOutput()
	if err != nil {
		t.Fatalf("qemu-io %s: %v %s", cmd, err, out)
	}
	return string(out)
}

// fill writes pattern p over the disk's first 64 KiB; pattern reads it back.
func fill(t *testing.T, disk string, p byte) {
	qemuIO(t, disk, fmt.Sprintf("write -P 0x%02x 0 64k", p))
}

func pattern(t *testing.T, disk string) byte {
	for p := 0; p < 256; p++ {
		out, err := exec.Command("qemu-io", "-f", "qcow2", "-c", fmt.Sprintf("read -P 0x%02x 0 64k", p), disk).CombinedOutput()
		if err == nil && !strings.Contains(string(out), "Pattern verification failed") {
			return byte(p)
		}
	}
	t.Fatalf("%s: no single pattern", disk)
	return 0
}

// snapVM makes a Linux VM with qcow2 disks (raw when the name ends in .img), UEFI variables
// "vars-1" and a TPM state file "tpm-1".
func snapVM(t *testing.T, name string, disks ...string) VMEntry {
	t.Helper()
	c := testGuest(t, "ubuntu")
	c.Name, c.Optical = name, nil
	c.Workspace = filepath.Join(t.TempDir(), Slug(name))
	c.Disks = nil
	os.MkdirAll(c.Workspace, 0o700)
	for _, d := range disks {
		f := "qcow2"
		if strings.HasSuffix(d, ".img") {
			f = "raw"
		}
		p := diskAbs(c.Workspace, d)
		if out, err := exec.Command("qemu-img", "create", "-f", f, p, "16M").CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		if f == "qcow2" {
			fill(t, p, 1)
		}
		c.Disks = append(c.Disks, Disk{Path: d, Format: f, Bus: BusVirtio})
	}
	write(t, filepath.Join(c.Workspace, GuestVarsFile), "vars-1")
	write(t, filepath.Join(c.Workspace, GuestTPMDir, "tpm2-00.permall"), "tpm-1")
	e, err := CreateVM(c)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func names(snaps []Snapshot) map[string]string {
	m := map[string]string{"": ""}
	for _, s := range snaps {
		m[s.ID] = s.Name
	}
	return m
}

// fingerprint is a VM's whole snapshot-relevant state, with snapshot ids replaced by names.
func fingerprint(t *testing.T, e VMEntry) string {
	t.Helper()
	cur, err := reload(e)
	if err != nil {
		t.Fatal(err)
	}
	snaps, err := listSnapshots(e.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	n := names(snaps)
	dir, _ := cur.Config.WorkspaceDir()
	var b strings.Builder
	fmt.Fprintf(&b, "current=%s ram=%d disks=%v\n", n[cur.Config.CurrentSnapshot], cur.Config.RAMMiB, snapshotDisks(cur.Config))
	for _, s := range snaps {
		fmt.Fprintf(&b, "snap %s parent=%s state=%s files=%v\n", s.Name, n[s.ParentID], s.state, s.Manifest.Files)
	}
	disks, _ := filepath.Glob(filepath.Join(dir, "*.qcow2"))
	for _, d := range disks {
		out, _ := exec.Command("qemu-img", "info", "--output=json", d).Output()
		var info struct{ Snapshots []struct{ Name string } }
		json.Unmarshal(out, &info)
		var tags []string
		for _, s := range info.Snapshots {
			tag := s.Name
			if id, ok := strings.CutPrefix(tag, "kvm-gui-"); ok && n[id] != "" {
				tag = "snap:" + n[id]
			}
			tags = append(tags, tag)
		}
		slices.Sort(tags)
		fmt.Fprintf(&b, "%s pattern=%d tags=%v\n", filepath.Base(d), pattern(t, d), tags)
	}
	vars, _ := os.ReadFile(filepath.Join(dir, GuestVarsFile))
	tpm, _ := os.ReadFile(filepath.Join(dir, GuestTPMDir, "tpm2-00.permall"))
	entries, _ := os.ReadDir(filepath.Join(dir, snapDir))
	fmt.Fprintf(&b, "vars=%s tpm=%s snapdir=%d\n", vars, tpm, len(entries))
	return b.String()
}

func setState(t *testing.T, e VMEntry, p byte, vars string) {
	dir, _ := e.Config.WorkspaceDir()
	for _, d := range snapshotDisks(e.Config) {
		fill(t, diskAbs(dir, d), p)
	}
	write(t, filepath.Join(dir, GuestVarsFile), vars)
	write(t, filepath.Join(dir, GuestTPMDir, "tpm2-00.permall"), "tpm-"+vars)
}

// The tree: branching after a restore, deleting a middle snapshot, two VMs with the same
// snapshot name, and the contents each restore brings back (task 5.1).
func TestSnapshotTree(t *testing.T) {
	needQemuImg(t)
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	ctx := t.Context()
	e := snapVM(t, "Tree", "disk0.qcow2", "disk1.qcow2")
	a, err := TakeSnapshot(ctx, e, "A", "first")
	if err != nil {
		t.Fatal(err)
	}
	setState(t, e, 2, "vars-2")
	b, _ := TakeSnapshot(ctx, e, "B", "")
	setState(t, e, 3, "vars-3")
	if err := RestoreSnapshot(ctx, e, a.ID); err != nil {
		t.Fatal(err)
	}
	dir, _ := e.Config.WorkspaceDir()
	if pattern(t, filepath.Join(dir, "disk1.qcow2")) != 1 || readFile(t, filepath.Join(dir, GuestVarsFile)) != "vars-1" ||
		readFile(t, filepath.Join(dir, GuestTPMDir, "tpm2-00.permall")) != "tpm-1" {
		t.Fatal("restore A")
	}
	c, _ := TakeSnapshot(ctx, e, "C", "")
	snaps, _ := ListSnapshots(e)
	parent := map[string]string{}
	for _, s := range snaps {
		parent[s.Name] = names(snaps)[s.ParentID]
	}
	if parent["B"] != "A" || parent["C"] != "A" || parent["A"] != "" {
		t.Fatalf("branch: %v", parent)
	}
	if cur, _ := reload(e); cur.Config.CurrentSnapshot != c.ID {
		t.Fatal("current state is under C")
	}
	// A chain C -> D, then delete C (a middle snapshot): D moves under A, and A and D still restore.
	setState(t, e, 4, "vars-4")
	d, _ := TakeSnapshot(ctx, e, "D", "")
	if err := DeleteSnapshot(ctx, e, c.ID); err != nil {
		t.Fatal(err)
	}
	snaps, _ = ListSnapshots(e)
	for _, s := range snaps {
		if s.ID == d.ID && s.ParentID != a.ID {
			t.Fatalf("D's parent after deleting C: %s", names(snaps)[s.ParentID])
		}
		if s.ID == c.ID {
			t.Fatal("C is still listed")
		}
	}
	if err := RestoreSnapshot(ctx, e, b.ID); err != nil || pattern(t, filepath.Join(dir, "disk0.qcow2")) != 2 {
		t.Fatalf("restore B: %v", err)
	}
	if err := RestoreSnapshot(ctx, e, d.ID); err != nil || pattern(t, filepath.Join(dir, "disk0.qcow2")) != 4 || readFile(t, filepath.Join(dir, GuestVarsFile)) != "vars-4" {
		t.Fatalf("restore D: %v", err)
	}
	// Another VM with a snapshot of the same name: its own tree, untouched by this one.
	o := snapVM(t, "Other", "disk0.qcow2")
	oa, err := TakeSnapshot(ctx, o, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := RestoreSnapshot(ctx, e, "A"); err != nil {
		t.Fatal(err)
	}
	if os, _ := ListSnapshots(o); len(os) != 1 || os[0].ID != oa.ID {
		t.Fatalf("other VM's tree: %+v", os)
	}
	if err := RestoreSnapshot(ctx, e, oa.ID); err == nil {
		t.Fatal("restored another VM's snapshot")
	}
}

// Eligibility: a raw disk, a disk another VM uses, a running VM; a macOS VM's recovery media
// stays out of the snapshot (task 5.1).
func TestSnapshotEligibility(t *testing.T) {
	needQemuImg(t)
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	ctx := t.Context()
	raw := snapVM(t, "Raw", "disk0.qcow2", "data.img")
	if _, err := TakeSnapshot(ctx, raw, "x", ""); err == nil || !strings.Contains(err.Error(), "data.img is raw") || !strings.Contains(err.Error(), "Media manager") {
		t.Fatalf("raw disk: %v", err)
	}
	a := snapVM(t, "Owner", "disk0.qcow2")
	dir, _ := a.Config.WorkspaceDir()
	b := testGuest(t, "ubuntu")
	b.Name, b.Workspace, b.Optical = "Sharer", filepath.Join(t.TempDir(), "s"), nil
	b.Disks = []Disk{{Path: filepath.Join(dir, "disk0.qcow2"), Format: "qcow2", Bus: BusVirtio}}
	if _, err := CreateVM(b); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeSnapshot(ctx, a, "x", ""); err == nil || !strings.Contains(err.Error(), "Sharer") {
		t.Fatalf("shared disk: %v", err)
	}
	// Running: a QEMU has the disk open.
	solo := snapVM(t, "Solo", "disk0.qcow2")
	sdir, _ := solo.Config.WorkspaceDir()
	sh, _ := os.ReadFile("/bin/sh")
	fake := filepath.Join(t.TempDir(), "qemu-system-x86_64")
	os.WriteFile(fake, sh, 0o755)
	q := exec.Command(fake, "-c", "sleep 30", "q", "file="+filepath.Join(sdir, "disk0.qcow2"))
	q.Start()
	t.Cleanup(func() { q.Process.Kill(); q.Wait() })
	for range 50 {
		if _, ok := FindRun(solo); ok {
			break
		}
	}
	before := fingerprint(t, solo)
	if _, err := TakeSnapshot(ctx, solo, "x", ""); err == nil || !strings.Contains(err.Error(), "power the VM off") {
		t.Fatalf("running VM: %v", err)
	}
	if fingerprint(t, solo) != before {
		t.Fatal("a refused snapshot changed the VM")
	}
	// macOS: mac_hdd_ng.img and the NVRAM; BaseSystem.img is left as it is.
	mc := testDefaults()
	mc.Name, mc.Workspace = "Mac", filepath.Join(t.TempDir(), "mac")
	os.MkdirAll(filepath.Join(mc.Workspace, BootDir), 0o755)
	exec.Command("qemu-img", "create", "-f", "qcow2", filepath.Join(mc.Workspace, DiskFile), "16M").Run()
	write(t, filepath.Join(mc.Workspace, MediaFile), "recovery")
	write(t, filepath.Join(mc.Workspace, VarsFile), "nvram")
	m, err := CreateVM(mc)
	if err != nil {
		t.Fatal(err)
	}
	s, err := TakeSnapshot(ctx, m, "Clean", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Manifest.Disks, []string{DiskFile}) || !slices.Equal(s.Manifest.Files, []string{VarsFile}) || readFile(t, filepath.Join(mc.Workspace, MediaFile)) != "recovery" {
		t.Fatalf("macOS snapshot: %+v", s.Manifest)
	}
}

// opSteps runs op once and returns the steps it went through.
func opSteps(t *testing.T, op func() error) []string {
	var steps []string
	snapStep = func(_, s string) error { steps = append(steps, s); return nil }
	defer func() { snapStep = func(string, string) error { return nil } }()
	if err := op(); err != nil {
		t.Fatal(err)
	}
	return steps
}

// Every step of take, restore and delete: a failure leaves the VM as it was, and a kill followed
// by reopening rolls back before the commit and forward after it; ENOSPC and cancellation too.
// A restore also brings back a disk detached since the snapshot (task 5.2).
func TestSnapshotJournals(t *testing.T) {
	needQemuImg(t)
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	ctx := t.Context()
	t.Cleanup(func() { snapStep = func(string, string) error { return nil } })
	n := 0
	// setup makes a VM with snapshot A (pattern 1) and a current state (pattern 2, disk1 detached).
	setup := func() (VMEntry, Snapshot) {
		n++
		e := snapVM(t, fmt.Sprintf("J%d", n), "disk0.qcow2", "disk1.qcow2")
		a, err := TakeSnapshot(ctx, e, "A", "")
		if err != nil {
			t.Fatal(err)
		}
		setState(t, e, 2, "vars-2")
		cur, _ := reload(e)
		c := cur.Config
		c.Disks, c.RAMMiB = c.Disks[:1], c.RAMMiB+512
		if err := SaveVM(cur, c); err != nil {
			t.Fatal(err)
		}
		cur, _ = reload(e)
		return cur, a
	}
	ops := map[string]func(e VMEntry, a Snapshot) error{
		"take":    func(e VMEntry, _ Snapshot) error { _, err := TakeSnapshot(ctx, e, "B", ""); return err },
		"restore": func(e VMEntry, a Snapshot) error { return RestoreSnapshot(ctx, e, a.ID) },
		"delete":  func(e VMEntry, a Snapshot) error { return DeleteSnapshot(ctx, e, a.ID) },
	}
	// pastCommit: the step comes after the operation's point of no return, so recovery finishes it.
	pastCommit := func(kind, st string) bool {
		return kind == "take" && st == "done" || kind == "restore" && st == "cleanup" || kind == "delete" && st != "mark"
	}
	for _, kind := range []string{"take", "restore", "delete"} {
		op := ops[kind]
		e, a := setup()
		steps := opSteps(t, func() error { return op(e, a) })
		after := fingerprint(t, e)
		if kind == "restore" {
			cur, _ := reload(e)
			dir, _ := cur.Config.WorkspaceDir()
			if len(cur.Config.Disks) != 2 || pattern(t, filepath.Join(dir, "disk1.qcow2")) != 1 || cur.Config.RAMMiB != a.Manifest.Config.RAMMiB {
				t.Fatalf("restore did not bring back the detached disk and the settings: %+v", cur.Config)
			}
		}
		for _, st := range steps {
			for _, mode := range []string{"fail", "kill"} {
				e, a := setup()
				want := fingerprint(t, e)
				if pastCommit(kind, st) {
					want = after
				}
				snapStep = func(_, s string) error {
					if s != st {
						return nil
					}
					if mode == "kill" {
						return errKilled
					}
					return fmt.Errorf("injected failure at %s", st)
				}
				err := op(e, a)
				snapStep = func(string, string) error { return nil }
				if err == nil {
					t.Fatalf("%s %s at %q: no error", kind, mode, st)
				}
				if err := recoverSnapshotOps(e); err != nil {
					t.Fatalf("%s %s at %q: recovery: %v", kind, mode, st, err)
				}
				if got := fingerprint(t, e); got != want {
					t.Errorf("%s %s at %q:\ngot\n%s\nwant\n%s", kind, mode, st, got, want)
				}
			}
		}
	}
	// ENOSPC and cancellation roll back.
	e, a := setup()
	want := fingerprint(t, e)
	snapStep = func(_, s string) error {
		if s == "apply files" {
			return syscall.ENOSPC
		}
		return nil
	}
	if err := RestoreSnapshot(ctx, e, a.ID); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("ENOSPC: %v", err)
	}
	snapStep = func(string, string) error { return nil }
	if got := fingerprint(t, e); got != want {
		t.Fatalf("after ENOSPC:\n%s\nwant\n%s", got, want)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := RestoreSnapshot(cctx, e, a.ID); err == nil {
		t.Fatal("a cancelled restore succeeded")
	}
	if got := fingerprint(t, e); got != want {
		t.Fatalf("after cancellation:\n%s\nwant\n%s", got, want)
	}
}

func TestSnapshotStepsCovered(t *testing.T) {
	needQemuImg(t)
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	e := snapVM(t, "Steps", "disk0.qcow2", "disk1.qcow2")
	var a Snapshot
	take := opSteps(t, func() error { var err error; a, err = TakeSnapshot(t.Context(), e, "A", ""); return err })
	restore := opSteps(t, func() error { return RestoreSnapshot(t.Context(), e, a.ID) })
	del := opSteps(t, func() error { return DeleteSnapshot(t.Context(), e, a.ID) })
	t.Logf("take %q\nrestore %q\ndelete %q", take, restore, del)
	if len(take) != 5 || len(restore) != 8 || len(del) != 5 {
		t.Fatal("unexpected step lists")
	}
}
