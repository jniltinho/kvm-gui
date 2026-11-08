package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jniltinho/kvm-gui/internal/core"
)

func TestCoresAndScale(t *testing.T) {
	for v, want := range map[int]int{1: 1, 2: 1, 3: 3, 8: 4, 16: 8} {
		if got := cores(v); got != want {
			t.Errorf("cores(%d) = %d, want %d", v, got, want)
		}
	}
	if zoomOf("150") != 1.5 || zoomOf("") != 1 || zoomOf("999") != 1 {
		t.Error("zoomOf: only 100/125/150/200 are scales; anything else is 100")
	}
}

func TestMemArgAndX11(t *testing.T) {
	if memArg(8192) != "8G" || memArg(1536) != "1536M" {
		t.Errorf("memArg: 8192 → %s, 1536 → %s", memArg(8192), memArg(1536))
	}
	for _, c := range []struct {
		gdk, wayland, display string
		want                  bool
	}{
		{"", "", ":0", true},              // X11 session
		{"", "wayland-0", ":0", false},    // GTK picks Wayland
		{"x11", "wayland-0", ":0", false}, // XWayland on a Wayland desktop: never (design D6)
		{"x11", "", ":0", true},
		{"wayland,x11", "", ":0", false},
		{"", "", "", false},
	} {
		t.Setenv("GDK_BACKEND", c.gdk)
		t.Setenv("WAYLAND_DISPLAY", c.wayland)
		t.Setenv("DISPLAY", c.display)
		if onX11() != c.want {
			t.Errorf("onX11 with %+v = %v", c, !c.want)
		}
	}
}

func TestDiskFormat(t *testing.T) {
	dir := t.TempDir()
	q, r := filepath.Join(dir, "a.img"), filepath.Join(dir, "b.qcow2")
	os.WriteFile(q, []byte("QFI\xfb\x00\x00\x00\x03"), 0o644)
	os.WriteFile(r, []byte("raw data"), 0o644)
	if f, _ := diskFormat(q); f != "qcow2" {
		t.Errorf("qcow2 header read as %s", f)
	}
	if f, _ := diskFormat(r); f != "raw" {
		t.Errorf("raw file named .qcow2 read as %s", f)
	}
}

// The wizard's answers become a guest Config, or a FieldError naming the section's field.
func TestGuestConfig(t *testing.T) {
	iso := filepath.Join(t.TempDir(), "x.iso")
	os.WriteFile(iso, nil, 0o644)
	n := NewVM{Name: "U", Guest: "linux", Profile: "ubuntu", ISO: iso, RAMMiB: 4096, VCPUs: 4, Firmware: "uefi", Disk: "new", DiskGiB: 20}
	c, err := guestConfig(n)
	if err != nil || c.DiskGiB != 20 || c.Cores != 2 || len(c.Disks) != 1 || c.Optical[0] != iso {
		t.Fatalf("guestConfig = %+v, %v", c, err)
	}
	field := func(n NewVM) string {
		var fe *core.FieldError
		if _, err := guestConfig(n); errors.As(err, &fe) {
			return fe.Field
		}
		return ""
	}
	none := n
	none.Disk = "none"
	if c, err := guestConfig(none); err != nil || len(c.Disks) != 0 {
		t.Errorf("no disk: %+v, %v", c.Disks, err)
	}
	for want, m := range map[string]func(*NewVM){
		"profile":  func(n *NewVM) { n.Guest = "windows" },
		"diskGiB":  func(n *NewVM) { n.DiskGiB = 0 },
		"diskPath": func(n *NewVM) { n.Disk = "existing" },
		"iso":      func(n *NewVM) { n.ISO = "/nonexistent.iso" },
	} {
		b := n
		m(&b)
		if got := field(b); got != want {
			t.Errorf("want FieldError %s, got %q", want, got)
		}
	}
}

// A second launch's --show reaches the running app in either spelling.
func TestShowArg(t *testing.T) {
	for args, want := range map[string]string{"kvm-gui --show Nilton": "Nilton", "kvm-gui gui --show=a b": "a", "kvm-gui --show": "", "kvm-gui": ""} {
		if got := showArg(strings.Fields(args)); got != want {
			t.Errorf("%q: %q, want %q", args, got, want)
		}
	}
}
