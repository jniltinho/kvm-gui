package cmd

import (
	"archive/tar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testOVA writes a small appliance: an Ubuntu VM with EFI, a 1 MiB raw disk and a bridged adapter.
func testOVA(t *testing.T) string {
	t.Helper()
	ovf := `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1"
 xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData" xmlns:vbox="http://www.virtualbox.org/ovf/machine">
 <References><File ovf:id="f1" ovf:href="d.img"/></References>
 <DiskSection><Info>d</Info><Disk ovf:diskId="d1" ovf:fileRef="f1" ovf:capacity="1048576" ovf:format="http://example.com/raw"/></DiskSection>
 <VirtualSystem ovf:id="Appliance lab"><Info>x</Info>
  <OperatingSystemSection ovf:id="94"><Info>os</Info><vbox:OSType ovf:required="false">Ubuntu_64</vbox:OSType></OperatingSystemSection>
  <VirtualHardwareSection><Info>hw</Info>
   <Item><rasd:InstanceID>1</rasd:InstanceID><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>2</rasd:VirtualQuantity></Item>
   <Item><rasd:AllocationUnits>MegaBytes</rasd:AllocationUnits><rasd:InstanceID>2</rasd:InstanceID><rasd:ResourceType>4</rasd:ResourceType><rasd:VirtualQuantity>2048</rasd:VirtualQuantity></Item>
   <Item><rasd:InstanceID>3</rasd:InstanceID><rasd:ResourceSubType>AHCI</rasd:ResourceSubType><rasd:ResourceType>20</rasd:ResourceType></Item>
   <Item><rasd:HostResource>/disk/d1</rasd:HostResource><rasd:InstanceID>4</rasd:InstanceID><rasd:Parent>3</rasd:Parent><rasd:ResourceType>17</rasd:ResourceType></Item>
   <Item><rasd:Connection>Bridged</rasd:Connection><rasd:InstanceID>5</rasd:InstanceID><rasd:ResourceSubType>virtio</rasd:ResourceSubType><rasd:ResourceType>10</rasd:ResourceType></Item>
  </VirtualHardwareSection>
  <vbox:Machine ovf:required="false" name="Appliance lab"><Hardware><Platform><Firmware type="EFI"/></Platform></Hardware></vbox:Machine>
 </VirtualSystem>
</Envelope>`
	p := filepath.Join(t.TempDir(), "lab.ova")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, m := range []struct{ name, data string }{{"lab.ovf", ovf}, {"d.img", strings.Repeat("x", 1<<20)}} {
		tw.WriteHeader(&tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.data)), Typeflag: tar.TypeReg})
		tw.Write([]byte(m.data))
	}
	tw.Close()
	f.Close()
	return p
}

// vm import-ova: --dry-run shows the VM and the notes and creates nothing; then it imports it.
func TestVMImportOVA(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("no qemu-img")
	}
	dir := t.TempDir()
	isolateXDG(t, dir)
	ova := testOVA(t)
	ws := filepath.Join(dir, "vms")
	out, err := run(t, "vm", "import-ova", ova, "--dry-run", "--folder", ws+"/lab")
	if err != nil {
		t.Fatal(err, out)
	}
	for _, want := range []string{"Appliance lab: ubuntu (linux), 2048 MiB, 2 vCPUs, UEFI firmware", "disk: disk0.qcow2", "adapter 1: nat, virtio-net-pci", "note: adapter 1 was bridged"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(ws + "/lab"); err == nil {
		t.Fatal("the dry run created the folder")
	}
	ovaDryRun = false
	out, err = run(t, "vm", "import-ova", ova, "--folder", ws+"/lab", "--name", "Lab")
	if err != nil || !strings.Contains(out, "imported Lab") {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(ws, "lab", "disk0.qcow2")); err != nil {
		t.Error(err)
	}
	if out, _ := run(t, "vm", "list"); !strings.Contains(out, "Lab") {
		t.Errorf("vm list: %s", out)
	}
}
