package core

import (
	"archive/tar"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Manifests: SHA-1 lines are accepted, a changed descriptor or a file the manifest does not cover
// is refused, and the manifest may come before the disks (task 2.1).
func TestImportApplianceManifests(t *testing.T) {
	ws := importEnv(t)
	raw, _ := rawDisk(t)
	disk, _ := os.ReadFile(raw)
	ovf := diskOVF("d.img", "http://example.com/raw", 2<<20)
	s1 := func(b []byte) string { h := sha1.Sum(b); return hex.EncodeToString(h[:]) }
	s256 := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	for name, c := range map[string]struct {
		mf    string
		first bool // the manifest before the disk
		want  string
	}{
		"SHA-1":                  {fmt.Sprintf("SHA1(lab.ovf)= %s\nSHA1(d.img)= %s\n", s1([]byte(ovf)), s1(disk)), false, ""},
		"manifest first":         {fmt.Sprintf("SHA256(lab.ovf)= %s\nSHA256(d.img)= %s\n", s256([]byte(ovf)), s256(disk)), true, ""},
		"changed descriptor":     {fmt.Sprintf("SHA256(lab.ovf)= %s\nSHA256(d.img)= %s\n", s256([]byte(ovf+" ")), s256(disk)), false, "lab.ovf does not match"},
		"descriptor not covered": {fmt.Sprintf("SHA256(d.img)= %s\n", s256(disk)), false, "does not cover lab.ovf"},
		"disk not covered":       {fmt.Sprintf("SHA256(lab.ovf)= %s\n", s256([]byte(ovf))), false, "does not cover d.img"},
		"line too long":          {fmt.Sprintf("SHA256(lab.ovf)= %s\nSHA256(d.img)= %s\n%s\n", s256([]byte(ovf)), s256(disk), strings.Repeat("x", 70<<10)), false, "cannot be read"},
	} {
		members := []tarMember{{name: "lab.ovf", data: ovf}, {name: "d.img", data: string(disk)}, {name: "lab.mf", data: c.mf}}
		if c.first {
			members[1], members[2] = members[2], members[1]
		}
		before := leftovers(t, ws)
		_, err := ImportAppliance(t.Context(), writeOVA(t, members...), ImportOptions{}, nil)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
		if c.want != "" {
			if l := leftovers(t, ws); l != before {
				t.Errorf("%s left %s", name, l)
			}
		}
	}
	// A manifest over the small-file bound is refused before anything is extracted.
	big := writeOVA(t, tarMember{name: "lab.ovf", data: ovf}, tarMember{name: "d.img", data: string(disk)}, tarMember{name: "lab.mf", data: strings.Repeat("#", maxSmallFile+1)})
	if _, err := PlanImport(big, ImportOptions{}); err == nil {
		t.Error("an oversized manifest was accepted")
	}
}

// The appliance's own files never become the VM's: a disk named like the converted one, and an
// OVMF_VARS.fd the appliance carries, are not published; the VM's folder is the user's only.
func TestImportAppliancePayload(t *testing.T) {
	ws := importEnv(t)
	raw, data := rawDisk(t)
	_ = data
	ovf := strings.NewReplacer(
		`<References><File ovf:id="file1" ovf:href="disk0.qcow2"/></References>`, `<References><File ovf:id="file1" ovf:href="disk0.qcow2"/><File ovf:id="nv" ovf:href="OVMF_VARS.fd"/></References>`,
		`<Item><rasd:InstanceID>8</rasd:InstanceID><rasd:ResourceType>14</rasd:ResourceType></Item>`, `<Item><rasd:HostResource>ovf:/file/nv</rasd:HostResource><rasd:InstanceID>8</rasd:InstanceID><rasd:ResourceType>32768</rasd:ResourceType></Item>`,
	).Replace(diskOVF("disk0.qcow2", "http://example.com/raw", 2<<20))
	if !strings.Contains(ovf, "OVMF_VARS.fd") {
		t.Fatal("fixture not built")
	}
	b, _ := os.ReadFile(raw)
	ova := writeOVA(t, tarMember{name: "lab.ovf", data: ovf}, tarMember{name: "disk0.qcow2", data: string(b)}, tarMember{name: "OVMF_VARS.fd", data: "attacker vars"})
	e, err := ImportAppliance(t.Context(), ova, ImportOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(ws, "lab")
	if v, err := os.ReadFile(filepath.Join(dir, "OVMF_VARS.fd")); err == nil && string(v) == "attacker vars" {
		t.Error("the appliance's OVMF_VARS.fd was published")
	}
	if _, err := os.Stat(filepath.Join(dir, ".payload")); err == nil {
		t.Error("the payload folder was left")
	}
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("VM folder mode %v %v", st.Mode(), err)
	}
	if st, err := os.Stat(filepath.Join(dir, "disk0.qcow2")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("disk mode %v %v", st.Mode(), err)
	}
	if err := checkStandaloneDisk(filepath.Join(dir, "disk0.qcow2")); err != nil || len(e.Config.Disks) != 1 {
		t.Errorf("disk %v, config %+v", err, e.Config.Disks)
	}
}

// A dry run creates no folder; a failed import removes the parent folders it created; a long name
// gets a bounded folder name; freeFolder reports errors instead of looping (task 2.2).
func TestImportApplianceFolders(t *testing.T) {
	importEnv(t)
	raw, _ := rawDisk(t)
	ovf := diskOVF("d.img", "http://example.com/raw", 2<<20)
	good := ovaWith(t, ovf, "d.img", raw, false)
	bad := ovaWith(t, ovf, "d.img", raw, true)
	parent := filepath.Join(t.TempDir(), "new-parent")
	if _, err := PlanImport(good, ImportOptions{Folder: filepath.Join(parent, "vm")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent); err == nil {
		t.Error("the dry run created the parent folder")
	}
	if _, err := ImportAppliance(t.Context(), bad, ImportOptions{Folder: filepath.Join(parent, "a", "vm")}, nil); err == nil {
		t.Fatal("a damaged appliance imported")
	}
	if _, err := os.Stat(parent); err == nil {
		t.Error("a failed import left the parent folder it created")
	}
	pv, err := PlanImport(good, ImportOptions{Name: strings.Repeat("Long name ", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(filepath.Base(pv.Folder)); n > maxSlug {
		t.Errorf("folder name of %d bytes", n)
	}
	if _, err := freeFolder(filepath.Join(t.TempDir(), strings.Repeat("a", 300))); err == nil {
		t.Error("freeFolder accepted a name over the filesystem's limit")
	}
}

// A cancellation during extraction, or once the conversion has run (before publication), removes
// everything (task 2.2).
func TestImportApplianceCancelMidway(t *testing.T) {
	ws := importEnv(t)
	raw, _ := rawDisk(t)
	ova := ovaWith(t, diskOVF("d.img", "http://example.com/raw", 2<<20), "d.img", raw, false)
	for name, when := range map[string]func(done, total int64) bool{
		"extraction": func(done, _ int64) bool { return done > 0 },
		"conversion": func(done, total int64) bool { return done == total },
	} {
		ctx, cancel := context.WithCancel(t.Context())
		if _, err := ImportAppliance(ctx, ova, ImportOptions{}, func(done, total int64) {
			if when(done, total) {
				cancel()
			}
		}); err == nil {
			t.Errorf("%s: a cancelled import succeeded", name)
		}
		cancel()
		if l := leftovers(t, ws); l != "files [], VMs 0" {
			t.Errorf("%s: cancel left %s", name, l)
		}
	}
}

// vmdkWith is a sparse VMDK header (capacity in sectors) with an embedded descriptor.
func vmdkWith(t *testing.T, capacity uint64, desc string) string {
	t.Helper()
	b := make([]byte, 1024)
	copy(b, "KDMV")
	binary.LittleEndian.PutUint64(b[12:], capacity)
	binary.LittleEndian.PutUint64(b[28:], 1)
	binary.LittleEndian.PutUint64(b[36:], 1)
	copy(b[512:], desc)
	p := filepath.Join(t.TempDir(), "x.vmdk")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The VMDK preflight refuses what QEMU would read as a descriptor file or a delta (Codex P1 #1);
// kvm-gui's own stream VMDKs pass it.
func TestCheckDiskHeaderVMDK(t *testing.T) {
	ok := "# Disk DescriptorFile\nversion=1\nCID=12345678\nparentCID=ffffffff\ncreateType=\"streamOptimized\"\nRW 4096 SPARSE \"x.vmdk\"\n"
	for name, c := range map[string]struct {
		capacity uint64
		desc     string
		want     string
	}{
		"good":            {4096, ok, ""},
		"zero capacity":   {0, ok, "no capacity"},
		"two createTypes": {4096, strings.Replace(ok, "createType", "createType=\"monolithicFlat\"\ncreateType", 1), "single-file"},
		"flat extent":     {4096, strings.Replace(ok, "SPARSE", "FLAT", 1), "single sparse extent"},
		"two extents":     {4096, ok + "RW 4096 SPARSE \"y.vmdk\"\n", "extents"},
		"parent CID":      {4096, strings.Replace(ok, "parentCID=ffffffff", "parentCID=0badf00d", 1), "parent"},
		"parent hint":     {4096, ok + "parentFileNameHint=\"/etc/shadow\"\n", "parent"},
	} {
		err := checkDiskHeader(vmdkWith(t, c.capacity, c.desc), "vmdk")
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	raw, _ := rawDisk(t)
	out := filepath.Join(t.TempDir(), "s.vmdk")
	if err := writeStreamVMDK(t.Context(), raw, out, nil); err != nil {
		t.Fatal(err)
	}
	if err := checkDiskHeader(out, "vmdk"); err != nil {
		t.Errorf("kvm-gui's own VMDK: %v", err)
	}
}

// An .ovf's file is opened without following links or blocking, and must be the file pass 1 saw
// (P1 #3, new #21).
func TestOpenPlainAt(t *testing.T) {
	dir := t.TempDir()
	d, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p := filepath.Join(dir, "d.img")
	os.WriteFile(p, []byte("data"), 0o600)
	f, st, err := openPlainAt(d, "d.img", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	secret := filepath.Join(dir, "secret")
	os.WriteFile(secret, []byte("priv"), 0o600)
	os.Rename(p, filepath.Join(dir, "old")) // keeps the checked file's inode in use
	os.Symlink(secret, p)
	if _, _, err := openPlainAt(d, "d.img", st); err == nil {
		t.Error("a link was followed")
	}
	os.Remove(p)
	os.WriteFile(p, []byte("data"), 0o600) // another file of the same size
	if _, _, err := openPlainAt(d, "d.img", st); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("a swapped file: %v", err)
	}
	if err := unix.Mkfifo(filepath.Join(dir, "lab.ovf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openAppliance(filepath.Join(dir, "lab.ovf")); err == nil || !strings.Contains(err.Error(), "not a plain file") {
		t.Errorf("a FIFO descriptor: %v", err) // and it returned: the open did not block
	}
}

// Parent folders: only those this call created are removed again, and only while empty (#12, #22).
func TestMkdirsOwned(t *testing.T) {
	base := t.TempDir()
	created, err := mkdirsOwned(filepath.Join(base, "a", "b", "c"))
	if err != nil || len(created) != 3 || created[0].path != filepath.Join(base, "a", "b", "c") {
		t.Fatalf("created %v, %v", created, err)
	}
	os.WriteFile(filepath.Join(base, "a", "keep"), nil, 0o600) // someone else's file
	removeOwned(created)
	if _, err := os.Stat(filepath.Join(base, "a", "b")); err == nil {
		t.Error("an empty folder it created was left")
	}
	if _, err := os.Stat(filepath.Join(base, "a", "keep")); err != nil {
		t.Error("another file was removed")
	}
	// A folder replaced by a file meanwhile: the file stays.
	created, _ = mkdirsOwned(filepath.Join(base, "x", "y"))
	os.Remove(filepath.Join(base, "x", "y"))
	os.WriteFile(filepath.Join(base, "x", "y"), []byte("theirs"), 0o600)
	removeOwned(created)
	if b, _ := os.ReadFile(filepath.Join(base, "x", "y")); string(b) != "theirs" {
		t.Error("a file put in a created folder's place was removed")
	}
	// A created folder moved away and another empty one put at its path: that one stays.
	created, _ = mkdirsOwned(filepath.Join(base, "m"))
	os.Rename(filepath.Join(base, "m"), filepath.Join(base, "moved"))
	os.Mkdir(filepath.Join(base, "m"), 0o755)
	removeOwned(created)
	if _, err := os.Stat(filepath.Join(base, "m")); err != nil {
		t.Error("someone else's folder at a created folder's path was removed")
	}
	if again, _ := mkdirsOwned(filepath.Join(base, "a")); len(again) != 0 {
		t.Errorf("an existing folder counted as created: %v", again)
	}
}

// Without prlimit an appliance's disk is not processed (P2 #17); qemu-img -p's progress is read.
func TestQemuImgLimits(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := qemuImgLimited(t.Context(), 1<<30, 10, "info", "x"); err == nil || !strings.Contains(err.Error(), "prlimit") {
		t.Errorf("without prlimit: %v", err)
	}
	if m := convertPct.FindStringSubmatch("    (45.01/100%)"); m == nil || m[1] != "45.01" {
		t.Errorf("progress line: %v", m)
	}
}

// required, as an xs:boolean, and VirtioSCSI on a SCSI item (P1 #9, #11).
func TestReadOVFRequiredAndVirtioSCSI(t *testing.T) {
	shadowed := strings.Replace(vboxOVF, `<Info>a VM</Info>`, `<Info>a VM</Info><x:Thing xmlns:x="urn:x" x:required="false" ovf:required="true"/>`, 1)
	if _, err := readOVF([]byte(shadowed)); err == nil {
		t.Error("another namespace's required attribute hid ovf:required")
	}
	ovf2 := strings.Replace(vboxOVF, `<Info>a VM</Info>`, `<Info>a VM</Info><x:Thing xmlns:x="urn:x" xmlns:o2="http://schemas.dmtf.org/ovf/envelope/2" o2:required="false" ovf:required="true"/>`, 1)
	if _, err := readOVF([]byte(ovf2)); err == nil {
		t.Error("an OVF 2 required attribute hid the OVF 1 envelope's")
	}
	unknownSection := strings.Replace(vboxOVF, `<Info>a VM</Info>`, `<Info>a VM</Info><x:Thing xmlns:x="urn:x" ovf:required="1"/>`, 1)
	if _, err := readOVF([]byte(unknownSection)); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Errorf(`required="1" section: %v`, err)
	}
	unknownHW := strings.Replace(vboxOVF, `<System>`, `<x:Gadget xmlns:x="urn:x" ovf:required="true"/><System>`, 1)
	if _, err := readOVF([]byte(unknownHW)); err == nil || !strings.Contains(err.Error(), "requires hardware") {
		t.Errorf("required hardware extension: %v", err)
	}
	optionalHW := strings.Replace(vboxOVF, `<System>`, `<x:Gadget xmlns:x="urn:x" ovf:required="false"/><System>`, 1)
	if _, err := readOVF([]byte(optionalHW)); err != nil {
		t.Errorf("optional hardware extension: %v", err)
	}
	optionalItem := strings.Replace(vboxOVF, `<Item><rasd:InstanceID>8</rasd:InstanceID><rasd:ResourceType>14</rasd:ResourceType></Item>`,
		`<Item ovf:required="0"><rasd:InstanceID>8</rasd:InstanceID><rasd:ResourceType>99</rasd:ResourceType></Item>`, 1)
	if _, err := readOVF([]byte(optionalItem)); err != nil {
		t.Errorf(`required="0" item: %v`, err)
	}
	vs := strings.Replace(vboxOVF, `<rasd:ResourceSubType>AHCI</rasd:ResourceSubType><rasd:ResourceType>20</rasd:ResourceType>`,
		`<rasd:ResourceSubType>VirtioSCSI</rasd:ResourceSubType><rasd:ResourceType>6</rasd:ResourceType>`, 1)
	a, err := readOVF([]byte(vs))
	if err != nil || len(a.Controllers) != 1 || a.Controllers[0].Kind != "virtio-scsi" {
		t.Errorf("VirtioSCSI as type 6: %v %+v", err, a.Controllers)
	}
}

// Export: an NVMe disk stays on an NVMe controller beside a SATA one; the destination may not be
// a file in the VM's folder or a link to one; a destination created meanwhile is kept; a start
// during the export is refused; a mid-way cancellation leaves nothing (P1 #5, #10, task 3.2).
func TestExportApplianceMixedAndRaces(t *testing.T) {
	importEnv(t)
	e, _ := guestWithData(t, "Mix", false, "qcow2")
	dir, _ := e.Config.WorkspaceDir()
	run(t, "qemu-img", "create", "-f", "raw", filepath.Join(dir, "disk1.img"), "1M")
	c := e.Config
	c.Disks = append(c.Disks, Disk{Path: "disk1.img", Format: "raw", Bus: BusNVMe})
	if err := SaveVM(e, c); err != nil {
		t.Fatal(err)
	}
	e, _ = reload(e)
	out := t.TempDir()
	dst := filepath.Join(out, "mix.ova")
	tried := false
	if err := ExportAppliance(t.Context(), e, dst, ExportOptions{}, func(_, _ int64) {
		if !tried {
			tried = true
			if unlock, err := (&VM{ID: e.ID}).lockRun(NewWorkspace(dir), e.Config); err == nil {
				unlock()
				t.Error("the VM could be started during its export")
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	s, err := openAppliance(dst)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, ctl := range s.Appliance.Controllers {
		kinds[ctl.InstanceID] = ctl.Kind
	}
	if len(s.Appliance.Disks) != 2 || kinds[s.Appliance.Disks[0].Controller] != "sata" || kinds[s.Appliance.Disks[1].Controller] != "nvme" {
		t.Errorf("controllers %+v, disks %+v", s.Appliance.Controllers, s.Appliance.Disks)
	}
	if !strings.Contains(string(s.Descriptor), `<StorageController name="NVMe" type="NVMe"`) {
		t.Error("no NVMe controller in vbox:Machine")
	}
	// The VM's own files are never a destination, under any name.
	vars := filepath.Join(dir, "OVMF_VARS.fd") // the VM's UEFI variables
	if err := os.WriteFile(vars, []byte("boot entries"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ExportAppliance(t.Context(), e, vars, ExportOptions{Overwrite: true}, nil); err == nil || !strings.Contains(err.Error(), "VM's folder") {
		t.Errorf("a file in the VM's folder: %v", err)
	}
	// A backing file outside the VM's folder is the VM's too.
	ext, _ := guestWithData(t, "Backed", false, "qcow2")
	edir, _ := ext.Config.WorkspaceDir()
	base := filepath.Join(out, "base.qcow2")
	run(t, "mv", filepath.Join(edir, "disk0.qcow2"), base)
	run(t, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", base, filepath.Join(edir, "disk0.qcow2"))
	if err := ExportAppliance(t.Context(), ext, base, ExportOptions{Overwrite: true}, nil); err == nil || !strings.Contains(err.Error(), "uses") {
		t.Errorf("the disk's backing file as destination: %v", err)
	}
	// So is an external data file.
	dv, _ := guestWithData(t, "Data", false, "qcow2")
	ddir, _ := dv.Config.WorkspaceDir()
	data := filepath.Join(out, "data.raw")
	os.Remove(filepath.Join(ddir, "disk0.qcow2"))
	run(t, "qemu-img", "create", "-f", "qcow2", "-o", "data_file="+data, filepath.Join(ddir, "disk0.qcow2"), "1M")
	if err := ExportAppliance(t.Context(), dv, data, ExportOptions{Overwrite: true}, nil); err == nil || !strings.Contains(err.Error(), "uses") {
		t.Errorf("the disk's data file as destination: %v", err)
	}
	link := filepath.Join(out, "alias")
	if err := os.Link(vars, link); err != nil {
		t.Fatal(err)
	}
	if err := ExportAppliance(t.Context(), e, link, ExportOptions{Overwrite: true}, nil); err == nil || !strings.Contains(err.Error(), "file of the VM's") {
		t.Errorf("a hard link to a VM file: %v", err)
	}
	// A destination that appears during the export is kept.
	race := filepath.Join(out, "race.ova")
	err = ExportAppliance(t.Context(), e, race, ExportOptions{}, func(_, _ int64) {
		if _, err := os.Stat(race); err != nil {
			os.WriteFile(race, []byte("theirs"), 0o644)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "was created while") {
		t.Errorf("destination race: %v", err)
	}
	if b, _ := os.ReadFile(race); string(b) != "theirs" {
		t.Error("the file created meanwhile was replaced")
	}
	// A cancellation in the middle leaves only what was there.
	before, _ := os.ReadDir(out)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := ExportAppliance(ctx, e, filepath.Join(out, "c.ova"), ExportOptions{}, func(done, _ int64) {
		if done > 0 {
			cancel()
		}
	}); err == nil {
		t.Error("a cancelled export succeeded")
	}
	if after, _ := os.ReadDir(out); len(after) != len(before) {
		t.Errorf("cancel left %v", after)
	}
}

// Members of 8 GiB or more are written in the GNU format (sizes in base-256), smaller ones as
// plain ustar (P1 #8).
func TestTarHeaderLargeMember(t *testing.T) {
	if h := tarHeader("d.vmdk", 1<<33-1); h.Format != tar.FormatUSTAR {
		t.Errorf("under 8 GiB: %v", h.Format)
	}
	h := tarHeader("d.vmdk", 1<<33)
	if h.Format != tar.FormatGNU {
		t.Errorf("8 GiB: %v", h.Format)
	}
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		err := tw.WriteHeader(h)
		pw.CloseWithError(err)
	}()
	b := make([]byte, 512)
	if _, err := io.ReadFull(pr, b); err != nil {
		t.Fatal(err)
	}
	if b[124]&0x80 == 0 || string(b[257:265]) != "ustar  \x00" {
		t.Errorf("size field %x, magic %q", b[124:136], b[257:265])
	}
	pr.Close()
}
