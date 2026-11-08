package core

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

// A streamOptimized VMDK of an image with data, holes and a partial last grain reads back
// identical through qemu-img and passes kvm-gui's own header check (export, design D9).
func TestWriteStreamVMDK(t *testing.T) {
	needQemuImg(t)
	dir := t.TempDir()
	raw := filepath.Join(dir, "d.raw")
	f, _ := os.Create(raw)
	buf := make([]byte, 3<<20)
	rand.Read(buf)
	f.WriteAt(buf, 0)      // 3 MiB of data
	f.WriteAt(buf, 40<<20) // past the first grain table (32 MiB)
	f.WriteAt(buf[:777], 70<<20+512)
	f.Truncate(70<<20 + 1536) // a last grain shorter than 64 KiB
	f.Close()
	out := filepath.Join(dir, "d.vmdk")
	var last int64
	if err := writeStreamVMDK(t.Context(), raw, out, func(d, total int64) { last = d }); err != nil {
		t.Fatal(err)
	}
	if last == 0 {
		t.Error("no progress")
	}
	run(t, "qemu-img", "check", "-f", "vmdk", out)
	run(t, "qemu-img", "compare", "-f", "raw", "-F", "vmdk", raw, out)
	if err := checkDiskHeader(out, "vmdk"); err != nil {
		t.Errorf("kvm-gui's own check refuses it: %v", err)
	}
	if st, _ := os.Stat(out); st.Size() > 7<<20 {
		t.Errorf("%d bytes: the empty regions were stored", st.Size())
	}
	if err := writeStreamVMDK(t.Context(), raw, out, nil); err == nil {
		t.Error("an existing file was overwritten")
	}
}
