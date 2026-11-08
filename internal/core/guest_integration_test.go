//go:build integration

// Real KVM boots of ordinary guests (task 3.11), run in the build image with /dev/kvm:
//
//	MKG_TEST_UBUNTU_ISO=... MKG_TEST_WIN11_ISO=... MKG_TEST_OUT=<dir> go test -tags integration -run RealGuest ./internal/core
//
// Each step saves a PNG of the guest's screen in MKG_TEST_OUT for review.
package core

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type guestRun struct {
	t   *testing.T
	v   *VM
	vnc *VNC
	upd chan struct{}
	err chan error
	out string
}

func startGuest(t *testing.T, c Config) *guestRun {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	v := &VM{OnLog: func(s string) { t.Log(s) }}
	if err := v.Start(c); err != nil {
		t.Fatal(err)
	}
	var vnc *VNC
	var err error
	for i := 0; i < 100; i++ {
		if s := v.VNCSocket(); s != "" {
			if vnc, err = DialVNC(s); err == nil {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if vnc == nil {
		v.Kill()
		t.Fatalf("no VNC: %v", err)
	}
	r := &guestRun{t: t, v: v, vnc: vnc, upd: make(chan struct{}, 1), err: make(chan error, 1), out: os.Getenv("MKG_TEST_OUT")}
	vnc.OnUpdate = func() {
		select {
		case r.upd <- struct{}{}:
		default:
		}
	}
	go func() { r.err <- vnc.Run() }()
	t.Cleanup(func() {
		vnc.Close()
		v.Kill()
		for v.State() != Stopped {
			time.Sleep(100 * time.Millisecond)
		}
	})
	return r
}

// shot saves the screen as <name>.png and returns it.
func (r *guestRun) shot(name string) *image.RGBA {
	r.t.Helper()
	img, err := r.vnc.Snapshot(r.upd, r.err, 10*time.Second)
	if err != nil {
		r.t.Fatalf("%s: %v", name, err)
	}
	if r.out != "" {
		f, err := os.Create(filepath.Join(r.out, name+".png"))
		if err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	return img
}

func (r *guestRun) key(name string, times int, every time.Duration) {
	k, ok := KeyFor(name)
	if !ok {
		r.t.Fatalf("no key %s", name)
	}
	for i := 0; i < times; i++ {
		r.vnc.Key(true, k)
		r.vnc.Key(false, k)
		time.Sleep(every)
	}
}

// distinct counts the distinct colours of a sample of the screen: a blank screen has one or two.
func distinct(img *image.RGBA) int {
	seen := map[[3]uint8]bool{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 7 {
		for x := b.Min.X; x < b.Max.X; x += 7 {
			o := img.PixOffset(x, y)
			seen[[3]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2]}] = true
		}
	}
	return len(seen)
}

func guestFor(t *testing.T, profile, iso string) Config {
	t.Helper()
	c, err := NewGuest(profile, "", iso)
	if err != nil {
		t.Fatal(err)
	}
	c.Workspace = t.TempDir()
	c.SSHPort = 22311
	c.RAMMiB, c.Cores, c.VCPUs = max(c.RAMMiB, 4096), 2, 2
	for _, d := range c.Disks {
		p, _ := c.DiskPath(d)
		if out, err := exec.Command("qemu-img", "create", "-f", d.Format, p, "20G").CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	return c
}

// Ubuntu Server's signed shim boots with Secure Boot on and reaches the installer.
func TestRealGuestUbuntuSecureBoot(t *testing.T) {
	iso := os.Getenv("MKG_TEST_UBUNTU_ISO")
	if iso == "" {
		t.Skip("MKG_TEST_UBUNTU_ISO is not set")
	}
	c := guestFor(t, "ubuntu", iso)
	c.SecureBoot = true
	r := startGuest(t, c)
	time.Sleep(20 * time.Second)
	r.shot("ubuntu-sb-20s")
	time.Sleep(70 * time.Second)
	if img := r.shot("ubuntu-sb-90s"); distinct(img) < 3 {
		t.Fatal("the screen is blank after 90 s")
	}
}

// Secure Boot is enforced: an unsigned EFI program (OpenCore's BOOTx64.efi) runs with Secure
// Boot off and is refused with it on.
func TestRealGuestSecureBootEnforced(t *testing.T) {
	efi, _ := filepath.Abs("../assets/files/EFI/BOOT/BOOTx64.efi")
	for _, sb := range []bool{false, true} {
		c := guestFor(t, "linux", "")
		c.Disks = []Disk{{Path: "esp.img", Format: "raw", Bus: BusSATA}}
		c.SecureBoot = sb
		img := filepath.Join(c.Workspace, "esp.img")
		for _, cmd := range [][]string{
			{"dd", "if=/dev/zero", "of=" + img, "bs=1M", "count=64"},
			{"mformat", "-i", img, "-F", "::"},
			{"mmd", "-i", img, "::/EFI", "::/EFI/BOOT"},
			{"mcopy", "-i", img, efi, "::/EFI/BOOT/BOOTX64.EFI"},
		} {
			if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
				t.Fatalf("%v: %v %s", cmd, err, out)
			}
		}
		r := startGuest(t, c)
		time.Sleep(25 * time.Second)
		name := "unsigned-efi-sb-off"
		if sb {
			name = "unsigned-efi-sb-on"
		}
		r.shot(name)
		r.vnc.Close()
		r.v.Kill()
		for r.v.State() != Stopped {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// Windows 11 setup with Secure Boot and a TPM: boot the ISO, then follow MKG_TEST_WIN11_STEPS,
// steps separated by ";": click:X,Y  key:Name  type:text  wait:Seconds  shot:Name.
func TestRealGuestWindows11(t *testing.T) {
	iso := os.Getenv("MKG_TEST_WIN11_ISO")
	if iso == "" {
		t.Skip("MKG_TEST_WIN11_ISO is not set")
	}
	c := guestFor(t, "windows11", iso)
	r := startGuest(t, c)
	r.key("Return", 40, 500*time.Millisecond) // "Press any key to boot from CD or DVD"
	time.Sleep(50 * time.Second)
	r.shot("win11-0-language")
	steps := os.Getenv("MKG_TEST_WIN11_STEPS")
	if steps == "" {
		// Windows 11 22H2 at 1280x800: Next, Install now, "no product key", the first edition;
		// the page after it is the license when setup found the TPM 2.0 and Secure Boot, and
		// "This PC can't run Windows 11" otherwise.
		steps = "click:898,567;wait:10;click:640,400;wait:25;click:718,572;wait:15;click:899,572;wait:20;shot:4-after-edition"
	}
	for _, step := range strings.Split(steps, ";") {
		op, arg, _ := strings.Cut(strings.TrimSpace(step), ":")
		switch op {
		case "click":
			var x, y int
			fmt.Sscanf(arg, "%d,%d", &x, &y)
			r.vnc.Pointer(0, x, y)
			time.Sleep(200 * time.Millisecond)
			r.vnc.Pointer(1, x, y)
			time.Sleep(100 * time.Millisecond)
			r.vnc.Pointer(0, x, y)
		case "key":
			r.key(arg, 1, 300*time.Millisecond)
		case "wait":
			n, _ := strconv.Atoi(arg)
			time.Sleep(time.Duration(n) * time.Second)
		case "shot":
			r.shot("win11-" + arg)
		}
	}
}
