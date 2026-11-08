package core

import (
	"archive/tar"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarMember is one member of a crafted .ova.
type tarMember struct {
	name string
	typ  byte
	data string
}

func writeOVA(t *testing.T, members ...tarMember) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.ova")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, m := range members {
		h := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.data)), Typeflag: m.typ}
		if m.typ == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size, h.Linkname = 0, "target"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			tw.Write([]byte(m.data))
		}
	}
	tw.Close()
	f.Close()
	return p
}

// A descriptor referencing one disk file, for the archive tests.
func smallOVF(disk string) string {
	return strings.NewReplacer(`lab-disk001.vmdk`, disk).Replace(vboxOVF)
}

// Pass 1 accepts a well-formed .ova and refuses each layout an attacker could use (task 1.3).
func TestOpenAppliance(t *testing.T) {
	good := writeOVA(t, tarMember{name: "lab.ovf", data: smallOVF("d.vmdk")}, tarMember{name: "d.vmdk", data: "KDMV"}, tarMember{name: "lab.mf", data: "x"})
	s, err := openAppliance(good)
	if err != nil {
		t.Fatal(err)
	}
	if s.DescName != "lab.ovf" || s.Manifest != "lab.mf" || s.Sizes["d.vmdk"] != 4 || s.Appliance.Name != "lab" {
		t.Errorf("source %+v", s)
	}
	ovf := tarMember{name: "lab.ovf", data: smallOVF("d.vmdk")}
	disk := tarMember{name: "d.vmdk", data: "KDMV"}
	many := []tarMember{ovf, disk}
	for i := 0; i < 63; i++ {
		many = append(many, tarMember{name: fmt.Sprintf("f%d.cert", i), data: "x"})
	}
	for name, c := range map[string]struct {
		members []tarMember
		want    string
	}{
		"dot-dot":         {[]tarMember{ovf, disk, {name: "../x.cert", data: "x"}}, "not a plain file name"},
		"slash":           {[]tarMember{ovf, disk, {name: "a/b.cert", data: "x"}}, "not a plain file name"},
		"absolute":        {[]tarMember{ovf, disk, {name: "/etc/x.cert", data: "x"}}, "not a plain file name"},
		"symlink":         {[]tarMember{ovf, {name: "d.vmdk", typ: tar.TypeSymlink}}, "not a plain file"},
		"hard link":       {[]tarMember{ovf, {name: "d.vmdk", typ: tar.TypeLink}}, "not a plain file"},
		"device":          {[]tarMember{ovf, {name: "d.vmdk", typ: tar.TypeChar}}, "not a plain file"},
		"duplicate":       {[]tarMember{ovf, disk, disk}, "twice"},
		"too many":        {many, "more than 64"},
		"descriptor late": {[]tarMember{disk, ovf}, "not its descriptor"},
		"missing disk":    {[]tarMember{ovf}, "lacks d.vmdk"},
		"extra file":      {[]tarMember{ovf, disk, {name: "evil.sh", data: "x"}}, "does not reference"},
		"two manifests":   {[]tarMember{ovf, disk, {name: "a.mf", data: "x"}, {name: "b.mf", data: "x"}}, "two manifests"},
		"size mismatch":   {[]tarMember{{name: "lab.ovf", data: strings.Replace(smallOVF("d.vmdk"), `ovf:href="d.vmdk"`, `ovf:href="d.vmdk" ovf:size="99"`, 1)}, disk}, "says 99"},
	} {
		if _, err := openAppliance(writeOVA(t, c.members...)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	// An .ovf beside its files; a link there is refused.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "lab.ovf"), []byte(smallOVF("d.vmdk")), 0o644)
	os.WriteFile(filepath.Join(dir, "d.vmdk"), []byte("KDMV"), 0o644)
	if s, err := openAppliance(filepath.Join(dir, "lab.ovf")); err != nil || s.OVA || s.Sizes["d.vmdk"] != 4 {
		t.Errorf(".ovf: %+v %v", s, err)
	}
	os.Remove(filepath.Join(dir, "d.vmdk"))
	os.Symlink("/etc/passwd", filepath.Join(dir, "d.vmdk"))
	if _, err := openAppliance(filepath.Join(dir, "lab.ovf")); err == nil || !strings.Contains(err.Error(), "not a plain file") {
		t.Errorf(".ovf with a link: %v", err)
	}
}
