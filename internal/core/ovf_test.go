package core

import (
	"strings"
	"testing"
)

// vboxOVF is shaped like what VirtualBox 7 writes: OVF 1.0, a streamOptimized VMDK in GiB units,
// rasd items (SATA controller by subtype), the CIM id with vbox:OSType, and a vbox:Machine with
// EFI, Secure Boot and a bridged adapter.
const vboxOVF = `<?xml version="1.0"?>
<Envelope ovf:version="1.0" xml:lang="en-US" xmlns="http://schemas.dmtf.org/ovf/envelope/1"
  xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1"
  xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData"
  xmlns:vssd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_VirtualSystemSettingData"
  xmlns:vbox="http://www.virtualbox.org/ovf/machine">
  <References><File ovf:id="file1" ovf:href="lab-disk001.vmdk"/></References>
  <DiskSection><Info>disks</Info>
    <Disk ovf:capacity="40" ovf:capacityAllocationUnits="byte * 2^30" ovf:diskId="vmdisk1" ovf:fileRef="file1"
      ovf:format="http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized"/>
  </DiskSection>
  <NetworkSection><Info>nets</Info><Network ovf:name="Bridged"><Description>bridged</Description></Network></NetworkSection>
  <VirtualSystem ovf:id="lab">
    <Info>a VM</Info>
    <OperatingSystemSection ovf:id="94"><Info>os</Info><Description>Ubuntu_64</Description><vbox:OSType ovf:required="false">Ubuntu22_LTS_64</vbox:OSType></OperatingSystemSection>
    <VirtualHardwareSection><Info>hw</Info>
      <System><vssd:VirtualSystemType>virtualbox-2.2</vssd:VirtualSystemType></System>
      <Item><rasd:InstanceID>1</rasd:InstanceID><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>4</rasd:VirtualQuantity></Item>
      <Item><rasd:AllocationUnits>MegaBytes</rasd:AllocationUnits><rasd:InstanceID>2</rasd:InstanceID><rasd:ResourceType>4</rasd:ResourceType><rasd:VirtualQuantity>8192</rasd:VirtualQuantity></Item>
      <Item><rasd:InstanceID>3</rasd:InstanceID><rasd:ResourceSubType>AHCI</rasd:ResourceSubType><rasd:ResourceType>20</rasd:ResourceType></Item>
      <Item><rasd:AddressOnParent>0</rasd:AddressOnParent><rasd:HostResource>/disk/vmdisk1</rasd:HostResource><rasd:InstanceID>4</rasd:InstanceID><rasd:Parent>3</rasd:Parent><rasd:ResourceType>17</rasd:ResourceType></Item>
      <Item><rasd:AddressOnParent>1</rasd:AddressOnParent><rasd:InstanceID>5</rasd:InstanceID><rasd:Parent>3</rasd:Parent><rasd:ResourceType>15</rasd:ResourceType></Item>
      <Item><rasd:AutomaticAllocation>true</rasd:AutomaticAllocation><rasd:Connection>Bridged</rasd:Connection><rasd:InstanceID>6</rasd:InstanceID><rasd:ResourceSubType>E1000</rasd:ResourceSubType><rasd:ResourceType>10</rasd:ResourceType></Item>
      <Item ovf:required="false"><rasd:InstanceID>7</rasd:InstanceID><rasd:ResourceSubType>ensoniq1371</rasd:ResourceSubType><rasd:ResourceType>35</rasd:ResourceType></Item>
      <Item><rasd:InstanceID>8</rasd:InstanceID><rasd:ResourceType>14</rasd:ResourceType></Item>
    </VirtualHardwareSection>
    <vbox:Machine ovf:required="false" name="lab" OSType="Ubuntu22_LTS_64">
      <Hardware><Platform><Firmware type="EFI"/><x86/></Platform><SecureBoot enabled="true"/>
        <Network><Adapter slot="0" enabled="true" type="82540EM"><BridgedInterface name="enp5s0"/></Adapter>
          <Adapter slot="1" enabled="false" type="82540EM"/></Network>
      </Hardware>
    </vbox:Machine>
  </VirtualSystem>
</Envelope>`

// The VirtualBox shape: name, OS, CPUs, memory, EFI and Secure Boot from vbox:Machine, the SATA
// controller by subtype, the disk's capacity in bytes, the bridged interface (task 1.1).
func TestReadOVFVirtualBox(t *testing.T) {
	a, err := readOVF([]byte(vboxOVF))
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "lab" || a.OSID != 94 || a.OSType != "Ubuntu22_LTS_64" || a.CPUs != 4 || a.MemoryBytes != 8192<<20 {
		t.Errorf("system: %+v", a)
	}
	if a.Firmware != "efi" || !a.SecureBoot || !a.FromVirtualBox {
		t.Errorf("firmware %q secure boot %v", a.Firmware, a.SecureBoot)
	}
	if len(a.Controllers) != 1 || a.Controllers[0].Kind != "sata" {
		t.Errorf("controllers %+v", a.Controllers)
	}
	if len(a.Disks) != 1 || a.Disks[0].Href != "lab-disk001.vmdk" || a.Disks[0].Format != "vmdk" || a.Disks[0].Capacity != 40<<30 || a.Disks[0].Controller != "3" {
		t.Errorf("disks %+v", a.Disks)
	}
	if a.Opticals != 1 || len(a.NICs) != 1 || a.NICs[0].Attachment != "bridged" || a.NICs[0].HostIF != "enp5s0" || a.NICs[0].Adapter != "82540EM" {
		t.Errorf("opticals %d, NICs %+v", a.Opticals, a.NICs)
	}
	if !strings.Contains(strings.Join(a.Notes, "; "), "floppy") {
		t.Errorf("no note for the floppy drive: %v", a.Notes)
	}
}

// A VMware-shaped descriptor says EFI through vmw:Config; an OVF 2.0 one uses sasd and epasd items.
func TestReadOVFOtherTools(t *testing.T) {
	vmw := strings.NewReplacer(
		`<vbox:Machine ovf:required="false" name="lab" OSType="Ubuntu22_LTS_64">`, `<!--`,
		`</vbox:Machine>`, `-->`,
		`<Info>hw</Info>`, `<Info>hw</Info><vmw:Config ovf:required="false" vmw:key="firmware" vmw:value="efi"/>`,
		`xmlns:vbox=`, `xmlns:vmw="http://www.vmware.com/schema/ovf" xmlns:vbox=`,
	).Replace(vboxOVF)
	a, err := readOVF([]byte(vmw))
	if err != nil {
		t.Fatal(err)
	}
	if a.Firmware != "efi" || a.FromVirtualBox || len(a.NICs) != 1 || a.NICs[0].Attachment != "bridged" {
		t.Errorf("VMware shape: firmware %q, NICs %+v", a.Firmware, a.NICs)
	}
	ovf2 := `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/2" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/2"
  xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData"
  xmlns:sasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_StorageAllocationSettingData"
  xmlns:epasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_EthernetPortAllocationSettingData">
  <References><File ovf:id="f1" ovf:href="d.qcow2" ovf:size="1024"/></References>
  <DiskSection><Info>d</Info><Disk ovf:diskId="d1" ovf:fileRef="f1" ovf:capacity="1073741824" ovf:format="http://www.qemu.org/qcow2"/></DiskSection>
  <VirtualSystem ovf:id="v2"><Info>x</Info><Name>Two</Name>
    <OperatingSystemSection ovf:id="101"><Info>os</Info></OperatingSystemSection>
    <VirtualHardwareSection><Info>hw</Info>
      <Item><rasd:InstanceID>1</rasd:InstanceID><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>2</rasd:VirtualQuantity></Item>
      <Item><rasd:AllocationUnits>byte * 2^20</rasd:AllocationUnits><rasd:InstanceID>2</rasd:InstanceID><rasd:ResourceType>4</rasd:ResourceType><rasd:VirtualQuantity>2048</rasd:VirtualQuantity></Item>
      <Item><rasd:InstanceID>3</rasd:InstanceID><rasd:ResourceSubType>NVMe</rasd:ResourceSubType><rasd:ResourceType>20</rasd:ResourceType></Item>
      <StorageItem><sasd:HostResource>ovf:/disk/d1</sasd:HostResource><sasd:InstanceID>4</sasd:InstanceID><sasd:Parent>3</sasd:Parent><sasd:ResourceType>17</sasd:ResourceType></StorageItem>
      <EthernetPortItem><epasd:Connection>NAT</epasd:Connection><epasd:InstanceID>5</epasd:InstanceID><epasd:ResourceSubType>virtio</epasd:ResourceSubType><epasd:ResourceType>10</epasd:ResourceType></EthernetPortItem>
    </VirtualHardwareSection>
  </VirtualSystem>
</Envelope>`
	b, err := readOVF([]byte(ovf2))
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "Two" || b.OSID != 101 || b.MemoryBytes != 2048<<20 || len(b.Disks) != 1 || b.Disks[0].Format != "qcow2" ||
		b.Disks[0].Capacity != 1<<30 || b.Controllers[0].Kind != "nvme" || len(b.NICs) != 1 || b.NICs[0].Attachment != "nat" || b.Firmware != "" {
		t.Errorf("OVF 2.0: %+v", b)
	}
}

// Descriptors kvm-gui refuses, each with the reason (task 1.1).
func TestReadOVFRefusals(t *testing.T) {
	for name, c := range map[string]struct{ from, to, want string }{
		"compressed":   {`ovf:href="lab-disk001.vmdk"`, `ovf:href="lab-disk001.vmdk" ovf:compression="gzip"`, "compressed"},
		"chunked":      {`ovf:href="lab-disk001.vmdk"`, `ovf:href="lab-disk001.vmdk" ovf:chunkSize="1000"`, "chunks"},
		"collection":   {`<VirtualSystem ovf:id="lab">`, `<VirtualSystemCollection ovf:id="c"/><VirtualSystem ovf:id="lab">`, "several VMs"},
		"required":     {`<VirtualSystem ovf:id="lab">`, `<VirtualSystem ovf:id="lab"><FancySection ovf:required="true"><Info>x</Info></FancySection>`, "requires FancySection"},
		"unknown item": {`<rasd:ResourceType>14</rasd:ResourceType>`, `<rasd:ResourceType>33</rasd:ResourceType>`, "type 33"},
		"format":       {`vmdk.html#streamOptimized`, `example.com/vhd`, "only VMDK"},
		"delta":        {`ovf:diskId="vmdisk1"`, `ovf:diskId="vmdisk1" ovf:parentRef="other"`, "delta"},
		"no file":      {`ovf:fileRef="file1"`, `ovf:fileRef="file9"`, "does not list"},
		"dtd":          {`<?xml version="1.0"?>`, `<?xml version="1.0"?><!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]>`, "DTD"},
		"capacity":     {`ovf:capacity="40"`, `ovf:capacity="18446744073709551615"`, "impossible"},
	} {
		_, err := readOVF([]byte(strings.Replace(vboxOVF, c.from, c.to, 1)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if _, err := readOVF(make([]byte, maxDescriptor+1)); err == nil {
		t.Error("a descriptor over 4 MiB was read")
	}
}

// The OS table both ways and its fallbacks (task 1.2, design D5).
func TestProfileForAppliance(t *testing.T) {
	for _, c := range []struct {
		osType string
		cim    int
		want   string
		note   bool
	}{
		{"Ubuntu22_LTS_64", 94, "ubuntu", false}, {"Ubuntu_64", 0, "ubuntu", false}, {"Debian12_64", 0, "debian", false},
		{"Fedora_64", 0, "fedora", false}, {"RedHat9_64", 0, "rocky", false}, {"Oracle8_64", 0, "rocky", false},
		{"ArchLinux_64", 0, "arch", false}, {"OpenSUSE_Leap_64", 0, "opensuse", false}, {"Linux26_64", 0, "linux", false},
		{"Windows11_64", 0, "windows11", false}, {"Windows2022_64", 0, "windows11", false}, {"Windows10_64", 0, "windows10", false},
		{"Windows2019_64", 0, "windows10", false}, {"Windows7_64", 0, "windows10", true}, {"Other_64", 0, "other", false},
		{"", 94, "ubuntu", false}, {"", 96, "debian", false}, {"", 80, "rocky", false}, {"", 122, "windows11", false},
		{"", 101, "linux", true}, {"", 120, "windows10", true}, {"", 0, "other", true}, {"FreeBSD_64", 78, "other", true},
	} {
		p, note, err := profileForAppliance(appliance{OSType: c.osType, OSID: c.cim})
		if err != nil || p != c.want || (note != "") != c.note {
			t.Errorf("%q/%d: %q, note %q, %v; want %q (note %v)", c.osType, c.cim, p, note, err, c.want, c.note)
		}
	}
	for _, a := range []appliance{{OSType: "MacOS_64"}, {OSID: 2}, {OSType: "Ubuntu_arm64"}} {
		if _, _, err := profileForAppliance(a); err == nil {
			t.Errorf("%+v was accepted", a)
		}
	}
	for _, e := range osTable { // every export row reads back as its own profile
		if p, _, _ := profileForAppliance(appliance{OSType: e.OSType, OSID: e.CIM}); p != e.Profile {
			t.Errorf("%s exports as %s, which reads back as %s", e.Profile, e.OSType, p)
		}
	}
}

// The VirtualBox appliance maps to a complete, valid Config with its notes (task 1.2).
func TestPlanImport(t *testing.T) {
	ws := t.TempDir()
	a, err := readOVF([]byte(vboxOVF))
	if err != nil {
		t.Fatal(err)
	}
	p, err := planImport(a, ImportOptions{Folder: ws + "/lab"}, 6144, 2)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Config
	if c.Guest != GuestLinux || c.Profile != "ubuntu" || c.RAMMiB != 6144 || c.VCPUs != 2 || c.Cores != 2 || c.Firmware != FirmwareUEFI || !c.SecureBoot {
		t.Errorf("config %+v", c)
	}
	if len(c.Disks) != 1 || c.Disks[0].Bus != BusSATA || c.Disks[0].Path != "disk0.qcow2" || len(p.Disks) != 1 || p.Disks[0].Capacity != 40<<30 {
		t.Errorf("disks %+v / %+v", c.Disks, p.Disks)
	}
	if len(c.Optical) != 1 || c.Optical[0] != "" || len(c.Networks) != 1 || c.Networks[0].Mode != NetNAT || c.Networks[0].Model != "e1000" || c.Networks[0].MAC == "" {
		t.Errorf("optical %q, networks %+v", c.Optical, c.Networks)
	}
	all := strings.Join(p.Notes, "\n")
	for _, want := range []string{"memory lowered", "processors lowered", "bridged to enp5s0", "fallback loader", "floppy"} {
		if !strings.Contains(all, want) {
			t.Errorf("no note %q in:\n%s", want, all)
		}
	}
	if err := validatePlan(c); err != nil {
		t.Errorf("planned VM does not validate: %v", err)
	}
	// A chosen bridge keeps the adapter bridged; an IDE disk becomes SATA with a note.
	a.Controllers[0].Kind = "ide"
	p, _ = planImport(a, ImportOptions{Folder: ws + "/lab", Bridge: "br0"}, 0, 0)
	if p.Config.Networks[0].Mode != NetBridged || p.Config.Networks[0].Bridge != "br0" || p.Config.Disks[0].Bus != BusSATA ||
		!strings.Contains(strings.Join(p.Notes, "\n"), "IDE controller") {
		t.Errorf("bridge/IDE: %+v %v", p.Config.Networks, p.Notes)
	}
	// No firmware said: BIOS with a note; Windows 11 keeps its (empty) TPM with a note.
	p, _ = planImport(appliance{OSType: "Windows11_64", CPUs: 4, MemoryBytes: 8 << 30}, ImportOptions{}, 0, 0)
	if p.Config.Firmware != FirmwareBIOS || !p.Config.TPM || !strings.Contains(strings.Join(p.Notes, "\n"), "BitLocker") ||
		!strings.Contains(strings.Join(p.Notes, "\n"), "BIOS assumed") {
		t.Errorf("Windows 11 without firmware: %+v %v", p.Config, p.Notes)
	}
}

// An appliance without adapters gets one disabled adapter, so the VM is valid and has no network.
func TestPlanImportNoNIC(t *testing.T) {
	p, err := planImport(appliance{OSType: "Debian_64", CPUs: 1}, ImportOptions{Folder: t.TempDir() + "/d"}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Config.Networks) != 1 || p.Config.Networks[0].Enabled || validatePlan(p.Config) != nil {
		t.Errorf("networks %+v, validate %v", p.Config.Networks, validatePlan(p.Config))
	}
}
