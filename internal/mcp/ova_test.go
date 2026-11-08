package mcp

import (
	"archive/tar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// vm_import_ova returns the preview without confirm and imports with it; relative paths are refused.
func TestVMImportOVA(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("no qemu-img")
	}
	dir := t.TempDir()
	isolateXDG(t, dir)
	ovf := `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1"
 xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData">
 <References><File ovf:id="f1" ovf:href="d.img"/></References>
 <DiskSection><Info>d</Info><Disk ovf:diskId="d1" ovf:fileRef="f1" ovf:capacity="1048576" ovf:format="http://example.com/raw"/></DiskSection>
 <VirtualSystem ovf:id="Agent lab"><Info>x</Info><OperatingSystemSection ovf:id="96"><Info>os</Info></OperatingSystemSection>
  <VirtualHardwareSection><Info>hw</Info>
   <Item><rasd:InstanceID>1</rasd:InstanceID><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>1</rasd:VirtualQuantity></Item>
   <Item><rasd:InstanceID>2</rasd:InstanceID><rasd:ResourceType>17</rasd:ResourceType><rasd:HostResource>/disk/d1</rasd:HostResource></Item>
  </VirtualHardwareSection></VirtualSystem></Envelope>`
	ova := filepath.Join(dir, "agent.ova")
	f, _ := os.Create(ova)
	tw := tar.NewWriter(f)
	for _, m := range []struct{ name, data string }{{"agent.ovf", ovf}, {"d.img", strings.Repeat("y", 1<<20)}} {
		tw.WriteHeader(&tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.data)), Typeflag: tar.TypeReg})
		tw.Write([]byte(m.data))
	}
	tw.Close()
	f.Close()
	cs := session(t)
	mustFail(t, cs, "vm_import_ova", map[string]any{"path": "agent.ova"}, "absolute")
	out := callOut(t, cs, "vm_import_ova", map[string]any{"path": ova, "folder": dir + "/vms/agent"})
	pv := out["preview"].(map[string]any)
	if out["id"] != nil || pv["config"].(map[string]any)["profile"] != "debian" || !strings.Contains(strings.Join(anyStrings(pv["notes"]), "\n"), "BIOS assumed") {
		t.Fatalf("preview: %v", out)
	}
	if _, err := os.Stat(dir + "/vms/agent"); err == nil {
		t.Fatal("the preview created the folder")
	}
	out = callOut(t, cs, "vm_import_ova", map[string]any{"path": ova, "folder": dir + "/vms/agent", "confirm": true})
	if out["id"] == nil || out["id"] == "" {
		t.Fatalf("import: %v", out)
	}
	if _, err := os.Stat(dir + "/vms/agent/disk0.qcow2"); err != nil {
		t.Error(err)
	}
}

func anyStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
