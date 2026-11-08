package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// diskOVF is vboxOVF with one disk file, its format URI and its declared capacity in bytes.
func diskOVF(href, formatURI string, capacity uint64) string {
	return strings.NewReplacer(
		`lab-disk001.vmdk`, href,
		`ovf:capacity="40" ovf:capacityAllocationUnits="byte * 2^30"`, fmt.Sprintf(`ovf:capacity="%d"`, capacity),
		`http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized`, formatURI,
	).Replace(vboxOVF)
}

const vmdkURI = "http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized"

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// rawDisk is a 2 MiB raw image with a recognizable pattern.
func rawDisk(t *testing.T) (string, []byte) {
	t.Helper()
	data := bytes.Repeat([]byte("kvm-gui appliance "), (2<<20)/18+1)[:2<<20]
	p := filepath.Join(t.TempDir(), "disk.raw")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p, data
}

// ovaWith writes an .ova: descriptor, the disk file under href, and a SHA-256 manifest (tamper
// replaces the disk's digest).
func ovaWith(t *testing.T, ovf, href, disk string, tamper bool) string {
	t.Helper()
	b, err := os.ReadFile(disk)
	if err != nil {
		t.Fatal(err)
	}
	sum := func(x []byte) string { h := sha256.Sum256(x); return hex.EncodeToString(h[:]) }
	diskSum := sum(b)
	if tamper {
		diskSum = strings.Repeat("0", 64)
	}
	mf := fmt.Sprintf("SHA256(lab.ovf)= %s\nSHA256(%s)= %s\n", sum([]byte(ovf)), href, diskSum)
	return writeOVA(t, tarMember{name: "lab.ovf", data: ovf}, tarMember{name: href, data: string(b)}, tarMember{name: "lab.mf", data: mf})
}

// importEnv isolates the VM list and the workspace.
func importEnv(t *testing.T) string {
	t.Helper()
	needQemuImg(t)
	dir := t.TempDir()
	isolateXDG(t, dir)
	ws := filepath.Join(dir, "vms")
	if err := SetWorkspaceRoot(ws); err != nil {
		t.Fatal(err)
	}
	return ws
}

// leftovers lists what an import left in the workspace and the VM list.
func leftovers(t *testing.T, ws string) string {
	t.Helper()
	ents, _ := os.ReadDir(ws)
	vms, _ := ListVMs()
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return fmt.Sprintf("files %v, VMs %d", names, len(vms))
}

// A VirtualBox-shaped .ova with a streamOptimized VMDK imports as a VM whose disk has the same
// bytes; a second import gets another name, MAC and SSH port (tasks 2.1, 2.3).
func TestImportAppliance(t *testing.T) {
	ws := importEnv(t)
	raw, data := rawDisk(t)
	vmdk := filepath.Join(t.TempDir(), "d.vmdk")
	run(t, "qemu-img", "convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=streamOptimized", raw, vmdk)
	ova := ovaWith(t, diskOVF("d.vmdk", vmdkURI, 2<<20), "d.vmdk", vmdk, false)
	pv, err := PlanImport(ova, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pv.Folder != filepath.Join(ws, "lab") || len(pv.Disks) != 1 || pv.Config.Firmware != FirmwareUEFI {
		t.Errorf("preview %+v", pv)
	}
	if ents, _ := os.ReadDir(ws); len(ents) != 0 {
		t.Fatal("the preview wrote files")
	}
	var last int64
	e, err := ImportAppliance(t.Context(), ova, ImportOptions{}, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if e.Config.Name != "lab" || e.Config.Workspace != filepath.Join(ws, "lab") || last == 0 {
		t.Errorf("entry %+v, progress %d", e.Config, last)
	}
	back := filepath.Join(t.TempDir(), "back.raw")
	run(t, "qemu-img", "convert", "-f", "qcow2", "-O", "raw", filepath.Join(ws, "lab", "disk0.qcow2"), back)
	if got, _ := os.ReadFile(back); !bytes.Equal(got, data) {
		t.Error("the imported disk's contents differ")
	}
	for _, left := range []string{"d.vmdk", "lab.ovf", "lab.mf"} { // extracted files are gone
		if _, err := os.Stat(filepath.Join(ws, "lab", left)); err == nil {
			t.Errorf("%s was left in the VM folder", left)
		}
	}
	e2, err := ImportAppliance(t.Context(), ova, ImportOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e2.Config.Name == e.Config.Name || e2.Config.Workspace == e.Config.Workspace || e2.Config.SSHPort == e.Config.SSHPort ||
		e2.Config.Networks[0].MAC == e.Config.Networks[0].MAC {
		t.Errorf("second import shares identity: %+v / %+v", e.Config, e2.Config)
	}
}

// Hostile disks are refused before qemu-img opens them, and a refused, damaged or cancelled
// import leaves nothing (tasks 2.1, 2.2).
func TestImportApplianceRefusals(t *testing.T) {
	ws := importEnv(t)
	tmp := t.TempDir()
	raw, _ := rawDisk(t)
	img := func(name string, args ...string) string {
		p := filepath.Join(tmp, name)
		run(t, "qemu-img", append(append([]string{"create"}, args[:len(args)-1]...), p, args[len(args)-1])...)
		return p
	}
	vmdk := filepath.Join(tmp, "good.vmdk")
	run(t, "qemu-img", "convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=streamOptimized", raw, vmdk)
	backed := img("backed.qcow2", "-f", "qcow2", "-F", "raw", "-b", raw, "2M")
	dataFile := img("datafile.qcow2", "-f", "qcow2", "-o", "data_file="+filepath.Join(tmp, "outside.raw"), "2M")
	flat := img("flat.vmdk", "-f", "vmdk", "-o", "subformat=monolithicFlat", "2M")
	split := img("split.vmdk", "-f", "vmdk", "-o", "subformat=twoGbMaxExtentSparse", "2M")
	huge := img("huge.qcow2", "-f", "qcow2", "17T")
	big := filepath.Join(tmp, "big.vmdk") // a streamOptimized VMDK far larger than it declares
	run(t, "qemu-img", "create", "-f", "vmdk", "-o", "subformat=streamOptimized", big, "64G")
	qcowURI := "http://www.qemu.org/qcow2"
	for name, c := range map[string]struct {
		disk, uri string
		capacity  uint64
		tamper    bool
		want      string
	}{
		"backing file":   {backed, qcowURI, 2 << 20, false, "backing file"},
		"data file":      {dataFile, qcowURI, 2 << 20, false, "external file"},
		"flat VMDK":      {flat, vmdkURI, 2 << 20, false, "single-file"},
		"split VMDK":     {split, vmdkURI, 2 << 20, false, "single-file"},
		"raw as VMDK":    {raw, vmdkURI, 2 << 20, false, "single-file"},
		"over 16 TiB":    {huge, qcowURI, 17 << 40, false, "16 TiB"},
		"size mismatch":  {vmdk, vmdkURI, 4 << 20, false, "declares"},
		"expanding VMDK": {big, vmdkURI, 2 << 20, false, "declares"},
		"bad digest":     {vmdk, vmdkURI, 2 << 20, true, "does not match"},
	} {
		ext := filepath.Ext(c.disk)
		ova := ovaWith(t, diskOVF("d"+ext, c.uri, c.capacity), "d"+ext, c.disk, c.tamper)
		_, err := ImportAppliance(t.Context(), ova, ImportOptions{}, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
		if l := leftovers(t, ws); l != "files [], VMs 0" {
			t.Errorf("%s left %s", name, l)
		}
	}
	// A cancelled import leaves nothing.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ova := ovaWith(t, diskOVF("d.vmdk", vmdkURI, 2<<20), "d.vmdk", vmdk, false)
	if _, err := ImportAppliance(ctx, ova, ImportOptions{}, nil); err == nil {
		t.Error("a cancelled import succeeded")
	}
	if l := leftovers(t, ws); l != "files [], VMs 0" {
		t.Errorf("cancel left %s", l)
	}
	// Raw data with a qcow2 header is converted as raw: its "backing file" is never opened.
	trap := img("trap.qcow2", "-f", "qcow2", "-u", "-F", "raw", "-b", filepath.Join(tmp, "never-opened.raw"), "2M")
	st, _ := os.Stat(trap)
	ova = ovaWith(t, diskOVF("d.img", "http://example.com/raw", uint64(st.Size())), "d.img", trap, false)
	if _, err := ImportAppliance(t.Context(), ova, ImportOptions{}, nil); err != nil {
		t.Errorf("raw data with a qcow2 header: %v", err)
	}
}
