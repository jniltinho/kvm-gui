package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guestWithData creates a Linux VM (UEFI, TPM as given) whose disk holds a known pattern.
func guestWithData(t *testing.T, name string, tpm bool, format string) (VMEntry, []byte) {
	t.Helper()
	c, err := NewGuest("ubuntu", name, "")
	if err != nil {
		t.Fatal(err)
	}
	c.TPM, c.RAMMiB, c.VCPUs, c.Cores, c.DiskGiB = tpm, 2048, 2, 2, 1
	c.Description = `a <test> & "lab"`
	c.Optical = []string{""}
	c.Workspace = VMFolder(name)
	e, err := CreateGuestVM(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	raw, data := rawDisk(t)
	dir, _ := e.Config.WorkspaceDir()
	disk := filepath.Join(dir, "disk0.qcow2")
	if format == "raw" {
		os.Remove(disk)
		disk = filepath.Join(dir, "disk0.img")
		run(t, "cp", raw, disk)
		cur, _ := reload(e)
		c := cur.Config
		c.Disks[0].Path, c.Disks[0].Format = "disk0.img", "raw"
		if err := SaveVM(cur, c); err != nil {
			t.Fatal(err)
		}
	} else {
		run(t, "qemu-img", "convert", "-f", "raw", "-O", "qcow2", raw, disk)
	}
	e, _ = reload(e)
	return e, data
}

// Export, read back and re-import: the descriptor carries the VM, the disk's bytes survive, and
// the .ova holds descriptor, disk and manifest in that order (tasks 3.1, 3.2).
func TestExportAppliance(t *testing.T) {
	ws := importEnv(t)
	e, data := guestWithData(t, "Lab", false, "qcow2")
	dst := filepath.Join(t.TempDir(), "lab.ova")
	var last int64
	if err := ExportAppliance(t.Context(), e, dst, ExportOptions{}, func(d, total int64) { last = d }); err != nil {
		t.Fatal(err)
	}
	if last == 0 {
		t.Error("no progress")
	}
	s, err := openAppliance(dst)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Appliance
	if a.Name != "Lab" || a.Description != `a <test> & "lab"` || a.OSType != "Ubuntu_64" || a.OSID != 94 || a.CPUs != 2 || a.MemoryBytes != 2048<<20 ||
		a.Firmware != "efi" || a.Opticals != 1 || len(a.Disks) != 1 || a.Disks[0].Capacity != 2<<20 || len(a.NICs) != 1 || a.NICs[0].Adapter != "virtio" {
		t.Errorf("read back: %+v", a)
	}
	if s.Manifest != "lab.mf" || s.DescName != "lab.ovf" {
		t.Errorf("layout: %+v", s)
	}
	if _, err := DeleteVM(e, true); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(ws, "lab"))
	n, err := ImportAppliance(t.Context(), dst, ImportOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	back := filepath.Join(t.TempDir(), "back.raw")
	nd, _ := n.Config.WorkspaceDir()
	run(t, "qemu-img", "convert", "-f", "qcow2", "-O", "raw", filepath.Join(nd, "disk0.qcow2"), back)
	if got, _ := os.ReadFile(back); !bytes.Equal(got, data) {
		t.Error("the round trip changed the disk")
	}
	if n.Config.Firmware != FirmwareUEFI || n.Config.RAMMiB != 2048 || n.Config.Profile != "ubuntu" {
		t.Errorf("re-imported %+v", n.Config)
	}
}

// What the export refuses, and what it leaves when it fails (task 3.2).
func TestExportApplianceRefusals(t *testing.T) {
	importEnv(t)
	e, _ := guestWithData(t, "Tpm", true, "qcow2")
	out := t.TempDir()
	dst := filepath.Join(out, "x.ova")
	if err := ExportAppliance(t.Context(), e, dst, ExportOptions{}, nil); err == nil || !strings.Contains(err.Error(), "BitLocker") {
		t.Errorf("TPM without acknowledgment: %v", err)
	}
	if notes := strings.Join(ExportNotes(e), "\n"); !strings.Contains(notes, "TPM") || !strings.Contains(notes, "UEFI variables") {
		t.Errorf("notes: %s", notes)
	}
	os.WriteFile(dst, []byte("keep me"), 0o644)
	if err := ExportAppliance(t.Context(), e, dst, ExportOptions{AcceptTPMLoss: true}, nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("existing destination: %v", err)
	}
	disk, _ := e.Config.DiskPath(e.Config.Disks[0])
	if err := ExportAppliance(t.Context(), e, disk, ExportOptions{AcceptTPMLoss: true, Overwrite: true}, nil); err == nil || !strings.Contains(err.Error(), "uses") {
		t.Errorf("destination is the VM's disk: %v", err)
	}
	mac := e
	mac.Config.Guest = GuestMacOS
	if err := ExportAppliance(t.Context(), mac, filepath.Join(out, "m.ova"), ExportOptions{}, nil); err == nil || !strings.Contains(err.Error(), "bundles") {
		t.Errorf("macOS: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ExportAppliance(ctx, e, filepath.Join(out, "c.ova"), ExportOptions{AcceptTPMLoss: true}, nil); err == nil {
		t.Error("a cancelled export succeeded")
	}
	ents, _ := os.ReadDir(out)
	if len(ents) != 1 {
		t.Errorf("left %v, want only x.ova", ents)
	}
	if b, _ := os.ReadFile(dst); string(b) != "keep me" {
		t.Error("the existing file was changed")
	}
	// A raw disk exports and re-reads.
	r, data := guestWithData(t, "Raw", false, "raw")
	rd := filepath.Join(out, "raw.ova")
	if err := ExportAppliance(t.Context(), r, rd, ExportOptions{}, nil); err != nil {
		t.Fatalf("raw disk: %v", err)
	}
	ws := filepath.Join(t.TempDir(), "imported")
	n, err := ImportAppliance(t.Context(), rd, ImportOptions{Folder: ws}, nil)
	if err != nil {
		t.Fatal(err)
	}
	back := filepath.Join(t.TempDir(), "back.raw")
	run(t, "qemu-img", "convert", "-f", "qcow2", "-O", "raw", filepath.Join(ws, "disk0.qcow2"), back)
	if got, _ := os.ReadFile(back); !bytes.Equal(got, data) || n.Config.Name != "Raw (imported)" {
		t.Errorf("raw round trip: name %q, same bytes %v", n.Config.Name, bytes.Equal(got, data))
	}
}
