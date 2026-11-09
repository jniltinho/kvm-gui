package core

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExportOptions are the user's answers before an export.
type ExportOptions struct {
	Overwrite     bool // replace an existing destination file
	AcceptTPMLoss bool // the VM has a TPM: secrets sealed to it will not open elsewhere
}

// ExportNotes is what an export of e leaves behind (spec "What an export leaves behind"); the
// caller shows them before it starts.
func ExportNotes(e VMEntry) []string {
	c := e.Config
	notes := []string{}
	if snaps, err := listSnapshots(e.ID, false); err == nil && len(snaps) > 0 {
		notes = append(notes, fmt.Sprintf("its %d snapshot(s) are not included: the appliance holds each disk's current state", len(snaps)))
	}
	if c.TPM {
		notes = append(notes, "its TPM state is not included: secrets sealed to it, such as a BitLocker key, need their recovery key (or suspend BitLocker first)")
	}
	if c.Firmware == FirmwareUEFI {
		notes = append(notes, "its UEFI variables (boot entries) are not included: the guest starts from its fallback loader")
	}
	if c.SecureBoot {
		notes = append(notes, "Secure Boot is not carried: turn it on again in VirtualBox (Settings › System)")
	}
	for i, d := range c.Disks {
		if d.Bus != BusSATA && d.Bus != BusNVMe {
			notes = append(notes, fmt.Sprintf("disk %d is %s here and goes on a SATA controller there: the guest needs its AHCI driver at boot", i+1, strings.ToUpper(d.Bus)))
		}
	}
	return notes
}

// exportDisk is a disk being exported: where it is, its format, its virtual size, and its file in
// the appliance.
type exportDisk struct {
	Src, Format, Href, UUID string
	NVMe                    bool // on the NVMe controller; every other disk is on SATA
	Capacity                uint64
}

// ExportAppliance writes a powered-off Linux, Windows or Other VM as an .ova that VirtualBox 7.2
// imports (design D9): under the run lock for the whole export, each disk converted to a
// monolithicSparse VMDK beside the destination, then the tar (descriptor, disks, SHA-256
// manifest) published without replacing an existing file unless o.Overwrite.
func ExportAppliance(ctx context.Context, e VMEntry, dst string, o ExportOptions, progress func(done, total int64)) error {
	if e.Config.MacOS() {
		return errors.New("macOS VMs are not exported as appliances: move them between kvm-gui hosts as bundles (vm export --full)")
	}
	if !filepath.IsAbs(dst) {
		return errors.New("the destination must be an absolute path")
	}
	if err := recoverSnapshotOps(e); err != nil {
		return err
	}
	if e.Path == "" {
		var err error
		if e, err = reload(e); err != nil {
			return err
		}
	}
	c := e.Config
	if c.TPM && !o.AcceptTPMLoss {
		return errors.New("the VM has a TPM, whose secrets (a BitLocker key) will not open on the other side: have the recovery key or suspend BitLocker, then export with the TPM note acknowledged")
	}
	dir, err := c.WorkspaceDir()
	if err != nil {
		return err
	}
	if _, ok := FindRun(e); ok {
		return errors.New("shut the VM down first")
	}
	if _, err := os.Lstat(dst); err == nil && !o.Overwrite {
		return fmt.Errorf("%s already exists", dst)
	}
	var disks []exportDisk
	var used []string // every file the VM uses: the destination is never one of them
	for i, d := range c.Disks {
		p, err := c.DiskPath(d)
		if err != nil {
			return err
		}
		format := d.Format
		if format == "" {
			format = "qcow2"
		}
		disks = append(disks, exportDisk{Src: p, Format: format, Href: fmt.Sprintf("%s-disk%d.vmdk", Slug(c.Name), i+1), UUID: newUUID(), NVMe: d.Bus == BusNVMe})
		used = append(used, p)
	}
	used = append(used, c.Optical...)
	// Disks only a snapshot still names (qcow2, as snapshots need), and every file a disk depends on
	// (backing chain, external data files), are the VM's too.
	roots := map[string]string{}
	for _, d := range disks {
		roots[d.Src] = d.Format
	}
	snaps, err := listSnapshots(e.ID, true)
	if err != nil {
		return err
	}
	for _, sn := range snaps {
		for _, d := range sn.Manifest.Disks {
			if !filepath.IsAbs(d) {
				d = filepath.Join(dir, d)
			}
			used = append(used, d)
			if _, ok := roots[d]; !ok {
				if _, err := os.Stat(d); err == nil {
					roots[d] = "qcow2"
				}
			}
		}
	}
	for src, format := range roots {
		deps, err := diskDependencies(ctx, src, format)
		if err != nil {
			return err
		}
		used = append(used, deps...)
	}
	for _, u := range used {
		if u != "" && sameFile(u, dst) {
			return fmt.Errorf("the destination %s is a file the VM uses: choose another one", dst)
		}
	}
	if err := outsideVM(dir, dst); err != nil {
		return err
	}
	unlock, err := (&VM{ID: e.ID}).lockRun(NewWorkspace(dir), c) // no start while the disks are read
	if err != nil {
		return fmt.Errorf("cannot export it: %w", err)
	}
	defer unlock()
	stage, err := os.MkdirTemp(filepath.Dir(dst), ".export-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	var total, done int64
	report := func(n int64) {
		if progress != nil {
			progress(min(n, total), total)
		}
	}
	for i := range disks {
		b, err := qemuImgLimited(ctx, 2<<30, 120, "info", "-f", disks[i].Format, "--output=json", disks[i].Src)
		if err != nil {
			return err
		}
		var info imgInfo
		if err := json.Unmarshal(b, &info); err != nil {
			return err
		}
		if info.VirtualSize > maxDiskBytes {
			return fmt.Errorf("disk %d is over the 16 TiB kvm-gui exports", i+1)
		}
		disks[i].Capacity = vmdkCapacity(int64(info.VirtualSize)) * vmdkSector // the VMDK's, in whole grains
		total += 2 * int64(disks[i].Capacity)                                  // conversion to raw, then the stream
	}
	for _, d := range disks {
		if err := ctx.Err(); err != nil {
			return err
		}
		// VirtualBox reads an .ova as a stream, so its disks must be streamOptimized, and qemu-img's
		// streamOptimized output fails there (task 3.0): qemu-img writes a sparse raw image, and
		// kvm-gui's own writer turns it into the stream layout VirtualBox reads.
		raw := filepath.Join(stage, d.Href+".raw")
		base, size := done, int64(d.Capacity)
		if err := convertLimited(ctx, func(f float64) { report(base + int64(f*float64(size))) }, "-f", d.Format, "-O", "raw", d.Src, raw); err != nil {
			return err
		}
		err := writeStreamVMDK(ctx, raw, filepath.Join(stage, d.Href), func(n, _ int64) { report(base + size + n) })
		os.Remove(raw)
		if err != nil {
			return err
		}
		done += 2 * size
	}
	ovf := []byte(writeOVF(c, disks))
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := writeApplianceTar(ctx, tmp, Slug(c.Name), ovf, stage, disks); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if o.Overwrite {
		return os.Rename(tmpName, dst)
	}
	if err := os.Link(tmpName, dst); err != nil { // fails if dst appeared meanwhile: never replaced
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s was created while the VM was exported: it is kept, and nothing was written", dst)
		}
		return err
	}
	return nil
}

// diskDependencies is every file the disk at src reads: its backing chain and external data files.
func diskDependencies(ctx context.Context, src, format string) ([]string, error) {
	b, err := qemuImgLimited(ctx, 2<<30, 120, "info", "--backing-chain", "-U", "-f", format, "--output=json", src)
	if err != nil {
		return nil, err
	}
	var chain []struct {
		Filename string `json:"filename"`
		Spec     struct {
			Data struct {
				DataFile string `json:"data-file"`
			} `json:"data"`
		} `json:"format-specific"`
	}
	if err := json.Unmarshal(b, &chain); err != nil {
		return nil, fmt.Errorf("reading qemu-img info: %w", err)
	}
	var out []string
	for _, f := range chain {
		out = append(out, f.Filename)
		if df := f.Spec.Data.DataFile; df != "" {
			// A relative data file is opened from the working directory, not the image's: both count.
			if abs, err := filepath.Abs(df); err == nil {
				out = append(out, abs)
			}
			if !filepath.IsAbs(df) {
				out = append(out, filepath.Join(filepath.Dir(f.Filename), df))
			}
		}
	}
	return out, nil
}

// outsideVM refuses a destination inside the VM's folder (its configuration, UEFI variables,
// TPM state, snapshots), or a link to any file there.
func outsideVM(vmDir, dst string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(dst))
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(vmDir); err == nil {
		if rel, err := filepath.Rel(real, parent); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return fmt.Errorf("the destination %s is inside the VM's folder: choose another folder", dst)
		}
	}
	st, err := os.Stat(dst)
	if err != nil {
		return nil // nothing there yet: nothing of the VM's can be replaced
	}
	return filepath.WalkDir(vmDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := os.Stat(p); err == nil && os.SameFile(fi, st) {
			return fmt.Errorf("the destination %s is a file of the VM's (%s): choose another one", dst, filepath.Base(p))
		}
		return nil
	})
}

// tarHeader is a member's header: plain ustar, or GNU (sizes in base-256) for a member of 8 GiB
// or more, which ustar cannot record; VirtualBox's own .ova writer uses the GNU format.
func tarHeader(name string, size int64) *tar.Header {
	format := tar.FormatUSTAR
	if size >= 1<<33 {
		format = tar.FormatGNU
	}
	return &tar.Header{Name: name, Mode: 0o644, Size: size, Typeflag: tar.TypeReg, Format: format}
}

// writeOVA writes descriptor, disks and the SHA-256 manifest, in that order, and syncs.
func writeApplianceTar(ctx context.Context, f *os.File, base string, ovf []byte, stage string, disks []exportDisk) error {
	tw := tar.NewWriter(f)
	var mf strings.Builder
	add := func(name string, size int64, r io.Reader) error {
		if err := tw.WriteHeader(tarHeader(name, size)); err != nil {
			return err
		}
		h := sha256.New()
		if _, err := io.Copy(io.MultiWriter(tw, h), &ctxReader{ctx: ctx, r: r}); err != nil {
			return err
		}
		fmt.Fprintf(&mf, "SHA256(%s)= %s\n", name, hex.EncodeToString(h.Sum(nil)))
		return nil
	}
	if err := add(base+".ovf", int64(len(ovf)), strings.NewReader(string(ovf))); err != nil {
		return err
	}
	for _, d := range disks {
		in, err := os.Open(filepath.Join(stage, d.Href))
		if err != nil {
			return err
		}
		st, err := in.Stat()
		if err == nil {
			err = add(d.Href, st.Size(), in)
		}
		in.Close()
		if err != nil {
			return err
		}
	}
	m := mf.String()
	if err := tw.WriteHeader(tarHeader(base+".mf", int64(len(m)))); err != nil {
		return err
	}
	if _, err := io.WriteString(tw, m); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// sameFile: a and b are the same file (by device and inode, or the same path).
func sameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(sa, sb)
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// xmlText escapes s for XML text and attributes.
func xmlText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// vboxAdapter is VirtualBox's adapter type for a guest NIC model (task 3.0).
func vboxAdapter(model string) (rasd, vbox string) {
	if model == "virtio-net-pci" {
		return "virtio", "virtio"
	}
	return "E1000", "82540EM"
}

// writeOVF is the OVF 1.0 descriptor of c with its disks (design D4, D9): complete standard
// sections, and the reduced vbox:Machine task 3.0 found VirtualBox 7.2 needs for EFI, the TPM and
// the adapters' types.
func writeOVF(c Config, disks []exportDisk) string {
	osRow := osForProfile(c.Profile)
	name := xmlText(c.Name)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>
<Envelope ovf:version="1.0" xml:lang="en-US" xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData" xmlns:vssd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_VirtualSystemSettingData" xmlns:vbox="http://www.virtualbox.org/ovf/machine">
  <References>
`)
	for i, d := range disks {
		fmt.Fprintf(&b, "    <File ovf:id=\"file%d\" ovf:href=\"%s\"/>\n", i+1, xmlText(d.Href))
	}
	b.WriteString("  </References>\n  <DiskSection>\n    <Info>Virtual disks</Info>\n")
	for i, d := range disks {
		fmt.Fprintf(&b, "    <Disk ovf:capacity=\"%d\" ovf:diskId=\"vmdisk%d\" ovf:fileRef=\"file%d\" ovf:format=\"http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized\" vbox:uuid=\"%s\"/>\n",
			d.Capacity, i+1, i+1, d.UUID)
	}
	b.WriteString("  </DiskSection>\n  <NetworkSection>\n    <Info>Logical networks</Info>\n")
	nets := map[string]bool{}
	for _, n := range c.Networks {
		if n.Enabled {
			nets[netName(n)] = true
		}
	}
	for _, n := range []string{"NAT", "Bridged"} {
		if nets[n] {
			fmt.Fprintf(&b, "    <Network ovf:name=\"%s\">\n      <Description>%s network</Description>\n    </Network>\n", n, n)
		}
	}
	b.WriteString("  </NetworkSection>\n")
	fmt.Fprintf(&b, "  <VirtualSystem ovf:id=\"%s\">\n    <Info>A virtual machine</Info>\n", name)
	if c.Description != "" {
		fmt.Fprintf(&b, "    <AnnotationSection>\n      <Info>Description</Info>\n      <Annotation>%s</Annotation>\n    </AnnotationSection>\n", xmlText(c.Description))
	}
	fmt.Fprintf(&b, "    <OperatingSystemSection ovf:id=\"%d\">\n      <Info>Guest operating system</Info>\n      <Description>%s</Description>\n      <vbox:OSType ovf:required=\"false\">%s</vbox:OSType>\n    </OperatingSystemSection>\n",
		osRow.CIM, osRow.OSType, osRow.OSType)
	fmt.Fprintf(&b, "    <VirtualHardwareSection>\n      <Info>Virtual hardware</Info>\n      <System>\n        <vssd:ElementName>Virtual Hardware Family</vssd:ElementName>\n        <vssd:InstanceID>0</vssd:InstanceID>\n        <vssd:VirtualSystemIdentifier>%s</vssd:VirtualSystemIdentifier>\n        <vssd:VirtualSystemType>virtualbox-2.2</vssd:VirtualSystemType>\n      </System>\n", name)
	id := 0
	item := func(fields string) {
		id++
		fmt.Fprintf(&b, "      <Item>\n%s        <rasd:InstanceID>%d</rasd:InstanceID>\n      </Item>\n", fields, id)
	}
	el := func(k, v string) string { return fmt.Sprintf("        <rasd:%s>%s</rasd:%s>\n", k, v, k) }
	item(el("Caption", fmt.Sprintf("%d virtual CPU", c.VCPUs)) + el("ElementName", fmt.Sprintf("%d virtual CPU", c.VCPUs)) + el("ResourceType", "3") + el("VirtualQuantity", fmt.Sprint(c.VCPUs)))
	item(el("AllocationUnits", "MegaBytes") + el("Caption", fmt.Sprintf("%d MB of memory", c.RAMMiB)) + el("ElementName", fmt.Sprintf("%d MB of memory", c.RAMMiB)) + el("ResourceType", "4") + el("VirtualQuantity", fmt.Sprint(c.RAMMiB)))
	item(el("Address", "0") + el("Caption", "sataController0") + el("ElementName", "sataController0") + el("ResourceSubType", "AHCI") + el("ResourceType", "20"))
	sata, nvme := id, 0
	nvmeDisks := 0
	for _, d := range disks {
		if d.NVMe {
			nvmeDisks++
		}
	}
	if nvmeDisks > 0 {
		item(el("Address", "0") + el("Caption", "nvmeController0") + el("ElementName", "nvmeController0") + el("ResourceSubType", "NVMe") + el("ResourceType", "20"))
		nvme = id
	}
	port, nport := 0, 0
	for i, d := range disks {
		parent, addr := sata, &port
		if d.NVMe {
			parent, addr = nvme, &nport
		}
		item(el("AddressOnParent", fmt.Sprint(*addr)) + el("Caption", fmt.Sprintf("disk%d", i+1)) + el("ElementName", fmt.Sprintf("disk%d", i+1)) +
			el("HostResource", fmt.Sprintf("/disk/vmdisk%d", i+1)) + el("Parent", fmt.Sprint(parent)) + el("ResourceType", "17"))
		*addr++
	}
	for range c.Optical {
		item(el("AddressOnParent", fmt.Sprint(port)) + el("AutomaticAllocation", "true") + el("Caption", "cdrom") + el("ElementName", "cdrom") +
			el("Parent", fmt.Sprint(sata)) + el("ResourceType", "15"))
		port++
	}
	for _, n := range c.Networks {
		if !n.Enabled {
			continue
		}
		sub, _ := vboxAdapter(n.Model)
		item(el("AutomaticAllocation", "true") + el("Caption", "Ethernet adapter on "+netName(n)) + el("Connection", netName(n)) +
			el("ElementName", "Ethernet adapter on "+netName(n)) + el("ResourceSubType", sub) + el("ResourceType", "10"))
	}
	b.WriteString("    </VirtualHardwareSection>\n")
	// The reduced vbox:Machine (task 3.0): VirtualBox builds the machine from it alone.
	fmt.Fprintf(&b, "    <vbox:Machine ovf:required=\"false\" version=\"1.16-linux\" uuid=\"{%s}\" name=\"%s\" OSType=\"%s\">\n      <Hardware>\n", newUUID(), name, osRow.OSType)
	fmt.Fprintf(&b, "        <CPU count=\"%d\"/>\n        <Memory RAMSize=\"%d\"/>\n", c.VCPUs, c.RAMMiB)
	if c.Firmware == FirmwareUEFI {
		b.WriteString("        <Firmware type=\"EFI\"/>\n")
	}
	if c.TPM {
		b.WriteString("        <TrustedPlatformModule type=\"v2_0\"/>\n")
	}
	b.WriteString("        <Network>\n")
	slot := 0
	for _, n := range c.Networks {
		if !n.Enabled {
			continue
		}
		_, typ := vboxAdapter(n.Model)
		attach := "<NAT/>"
		if n.Mode == NetBridged {
			attach = fmt.Sprintf("<BridgedInterface name=\"%s\"/>", xmlText(n.Bridge))
		}
		fmt.Fprintf(&b, "          <Adapter slot=\"%d\" enabled=\"true\" type=\"%s\">%s</Adapter>\n", slot, typ, attach)
		slot++
	}
	b.WriteString("        </Network>\n        <StorageControllers>\n")
	fmt.Fprintf(&b, "          <StorageController name=\"SATA\" type=\"AHCI\" PortCount=\"%d\" useHostIOCache=\"false\" Bootable=\"true\">\n", max(port, 1))
	port = 0
	for _, d := range disks {
		if !d.NVMe {
			fmt.Fprintf(&b, "            <AttachedDevice type=\"HardDisk\" hotpluggable=\"false\" port=\"%d\" device=\"0\"><Image uuid=\"{%s}\"/></AttachedDevice>\n", port, d.UUID)
			port++
		}
	}
	for range c.Optical {
		fmt.Fprintf(&b, "            <AttachedDevice passthrough=\"false\" type=\"DVD\" hotpluggable=\"false\" port=\"%d\" device=\"0\"/>\n", port)
		port++
	}
	b.WriteString("          </StorageController>\n")
	if nvmeDisks > 0 {
		fmt.Fprintf(&b, "          <StorageController name=\"NVMe\" type=\"NVMe\" PortCount=\"%d\" useHostIOCache=\"false\" Bootable=\"true\">\n", nvmeDisks)
		nport = 0
		for _, d := range disks {
			if d.NVMe {
				fmt.Fprintf(&b, "            <AttachedDevice type=\"HardDisk\" hotpluggable=\"false\" port=\"%d\" device=\"0\"><Image uuid=\"{%s}\"/></AttachedDevice>\n", nport, d.UUID)
				nport++
			}
		}
		b.WriteString("          </StorageController>\n")
	}
	b.WriteString("        </StorageControllers>\n      </Hardware>\n    </vbox:Machine>\n  </VirtualSystem>\n</Envelope>\n")
	return b.String()
}

func netName(n NIC) string {
	if n.Mode == NetBridged {
		return "Bridged"
	}
	return "NAT"
}
