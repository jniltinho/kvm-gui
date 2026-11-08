package core

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func testGuest(t *testing.T, profile string) Config {
	t.Helper()
	c, err := NewGuest(profile, "", "/isos/install.iso")
	if err != nil {
		t.Fatal(err)
	}
	c.Workspace = t.TempDir()
	c.SSHPort = FirstSSHPort
	return c
}

func argPairs(args []string) string { return strings.Join(args, " ") }

// Each profile's defaults (task 3.3).
func TestProfileDefaults(t *testing.T) {
	w11 := testGuest(t, "windows11")
	if w11.Guest != GuestWindows || w11.Firmware != FirmwareUEFI || !w11.SecureBoot || !w11.TPM ||
		w11.Disks[0].Bus != BusSATA || w11.Networks[0].Model != "e1000e" || w11.Graphics != GraphicsVGA {
		t.Fatalf("windows11: %+v", w11)
	}
	ub := testGuest(t, "ubuntu")
	if ub.Guest != GuestLinux || ub.Firmware != FirmwareUEFI || ub.SecureBoot || ub.TPM ||
		ub.Disks[0].Bus != BusVirtio || ub.Networks[0].Model != "virtio-net-pci" || ub.Graphics != GraphicsVirtio {
		t.Fatalf("ubuntu: %+v", ub)
	}
	if o := testGuest(t, "other"); o.Firmware != FirmwareBIOS || o.Networks[0].Model != "e1000" {
		t.Fatalf("other: %+v", o)
	}
	for _, p := range Profiles {
		if err := testGuest(t, p.ID).Validate(); err != nil {
			t.Errorf("%s defaults do not validate: %v", p.ID, err)
		}
	}
	if _, err := NewGuest("plan9", "", ""); err == nil {
		t.Error("unknown profile accepted")
	}
}

// Validation per guest type (tasks 3.3, 3.5; networking delta).
func TestGuestValidation(t *testing.T) {
	cases := map[string]func(*Config){
		"vmxnet3 on Windows":   func(c *Config) { c.Networks[0].Model = "vmxnet3" },
		"BIOS with SecureBoot": func(c *Config) { c.Firmware = FirmwareBIOS },
		"Reims on Windows":     func(c *Config) { c.Graphics = GraphicsReims },
		"GPU on Windows":       func(c *Config) { c.GPU = "0000:03:00.0" },
		"root patch":           func(c *Config) { c.RootPatch = true },
		"shared folder":        func(c *Config) { c.SharedFolders = []SharedFolder{{Path: "/srv", Tag: "srv"}} },
		"relative ISO":         func(c *Config) { c.Optical = []string{"install.iso"} },
		"bad USB id":           func(c *Config) { c.USBDevices = []USBDevice{{Vendor: "zz", Product: "0001"}} },
		"bad disk bus":         func(c *Config) { c.Disks[0].Bus = "scsi" },
		"low RAM":              func(c *Config) { c.RAMMiB = 1024 },
	}
	for name, mutate := range cases {
		c := testGuest(t, "windows11")
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// macOS keeps its own models: e1000e stays refused there.
	m := testDefaults()
	m.Networks = []NIC{{Enabled: true, Mode: NetNAT, Model: "e1000e", MAC: RandomMAC()}}
	if err := m.Validate(); err == nil {
		t.Error("e1000e accepted on macOS")
	}
	// A Linux VM may share folders and have no disk (a live ISO).
	l := testGuest(t, "ubuntu")
	l.SharedFolders = []SharedFolder{{Path: "/srv/share", Tag: "share", ReadOnly: true}}
	l.Disks = nil
	if err := l.Validate(); err != nil {
		t.Errorf("Linux with a share and no disk: %v", err)
	}
}

// guestArgs for each profile (task 3.4).
func TestGuestArgs(t *testing.T) {
	defer func(f func() bool) { amdHost = f }(amdHost)
	amdHost = func() bool { return false } // the host's CPU must not change the expected arguments
	fw := Firmware{Code: "/fw/CODE.fd", Vars: "/fw/VARS.fd"}
	w11 := testGuest(t, "windows11")
	a, err := guestArgs(w11, w11.Workspace, fw, "/run/x/qmp.sock", "/run/x/swtpm.sock")
	if err != nil {
		t.Fatal(err)
	}
	s := argPairs(a)
	for _, want := range []string{
		"-machine q35,smm=on", "-cpu host,hv_relaxed,hv_vapic,hv_spinlocks=0x1fff,hv_time",
		"-global driver=cfi.pflash01,property=secure,value=on",
		"if=pflash,format=raw,readonly=on,file=/fw/CODE.fd", "file=" + w11.Workspace + "/OVMF_VARS.fd",
		"-name guest=Windows 11", "media=cdrom,readonly=on,format=raw", "ide-cd,bus=sata.0,drive=cd0,bootindex=0", "ide-hd,bus=sata.1,drive=disk0,bootindex=1",
		"-device e1000e,netdev=net0", "-device VGA", "-tpmdev emulator,id=tpm0,chardev=chrtpm", "-device tpm-crb,tpmdev=tpm0",
		"-serial none", "-qmp unix:/run/x/qmp.sock",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("windows11 lacks %q in\n%s", want, s)
		}
	}
	for _, macOnly := range []string{"isa-applesmc", "OpenCore", "-smbios", "vmware-svga"} {
		if strings.Contains(s, macOnly) {
			t.Errorf("windows11 has macOS-only %q", macOnly)
		}
	}
	ub := testGuest(t, "ubuntu")
	ub.Optical = nil
	ub.SharedFolders = []SharedFolder{{Path: "/srv/share", Tag: "share", ReadOnly: true}}
	ub.Serial = SerialFile
	ub.USBDevices = []USBDevice{{Vendor: "046D", Product: "c52b"}}
	a, _ = guestArgs(ub, ub.Workspace, fw, "/run/x/qmp.sock", "")
	s = argPairs(a)
	for _, want := range []string{"virtio-blk-pci,drive=disk0,bootindex=0", "-device virtio-vga", "virtio-rng-pci",
		"mount_tag=share,security_model=mapped-xattr,readonly=on", "-serial file:" + ub.Workspace + "/serial.log",
		"usb-host,bus=xhci.0,vendorid=0x046d,productid=0xc52b"} {
		if !strings.Contains(s, want) {
			t.Errorf("ubuntu lacks %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "smm=on") || strings.Contains(s, "tpm") || strings.Contains(s, "hv_") {
		t.Errorf("ubuntu has Windows-only arguments:\n%s", s)
	}
	other := testGuest(t, "other")
	a, _ = guestArgs(other, other.Workspace, Firmware{}, "/run/x/qmp.sock", "")
	if s = argPairs(a); strings.Contains(s, "pflash") {
		t.Errorf("BIOS VM has pflash:\n%s", s)
	}
	// Disk first when asked.
	ub.BootOrder, ub.Optical = "disk", []string{"/isos/x.iso"}
	a, _ = guestArgs(ub, ub.Workspace, fw, "/run/x/qmp.sock", "")
	if s = argPairs(a); !strings.Contains(s, "drive=disk0,bootindex=0") || !strings.Contains(s, "drive=cd0,bootindex=1") {
		t.Errorf("boot order disk:\n%s", s)
	}
}

// Host firmware pairs per distribution and the VARS rules (task 3.5).
func TestHostFirmware(t *testing.T) {
	for _, layout := range [][2]string{
		{"/usr/share/OVMF/OVMF_CODE_4M.secboot.fd", "/usr/share/OVMF/OVMF_VARS_4M.ms.fd"},
		{"/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd", "/usr/share/edk2/ovmf/OVMF_VARS.secboot.fd"},
	} {
		root := t.TempDir()
		for _, f := range layout {
			write(t, filepath.Join(root, f), "fw")
		}
		fw, err := HostFirmware(root, true)
		if err != nil || fw.Code != filepath.Join(root, layout[0]) || fw.Vars != filepath.Join(root, layout[1]) {
			t.Fatalf("%v: %+v %v", layout, fw, err)
		}
		if _, err := HostFirmware(root, false); err == nil || !strings.Contains(err.Error(), "ovmf") {
			t.Errorf("plain firmware missing: %v", err)
		}
	}
	if _, err := HostFirmware(t.TempDir(), true); err == nil || !strings.Contains(err.Error(), "Secure Boot") {
		t.Errorf("no firmware: %v", err)
	}
	// The VM's variables are made once, kept, and refused after a Secure Boot change.
	root, dir := t.TempDir(), t.TempDir()
	write(t, filepath.Join(root, "ms.fd"), "microsoft keys")
	sec := Firmware{Vars: filepath.Join(root, "ms.fd"), Secure: true}
	if err := ensureVars(dir, sec); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, GuestVarsFile), "guest changes")
	if err := ensureVars(dir, sec); err != nil || readFile(t, filepath.Join(dir, GuestVarsFile)) != "guest changes" {
		t.Fatalf("variables were overwritten: %v", err)
	}
	if err := ensureVars(dir, Firmware{Vars: sec.Vars}); err != ErrVarsTemplate {
		t.Fatalf("Secure Boot change not caught: %v", err)
	}
	// Variables without their marker (an interrupted first start) are refused.
	os.Remove(filepath.Join(dir, GuestVarsKind))
	if err := ensureVars(dir, sec); err != ErrVarsTemplate {
		t.Fatalf("missing marker accepted: %v", err)
	}
}

// swtpm: socket readiness, stop after the client goes, no process left, private state (task 3.6).
func TestStartTPM(t *testing.T) {
	old := swtpmBin
	defer func() { swtpmBin = old }()
	swtpmBin = os.Args[0] // TestMain runs as the fake swtpm with FAKE_SWTPM=1
	t.Setenv("FAKE_SWTPM", "1")
	dir, run := t.TempDir(), t.TempDir()
	sock, stop, err := startTPM(dir, run)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close() // QEMU going away: the fake exits, like swtpm --terminate
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
	if st, err := os.Stat(filepath.Join(dir, GuestTPMDir)); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("TPM state directory: %v %v", st.Mode(), err)
	}
	// A swtpm that never answers is killed after the readiness deadline.
	t.Setenv("FAKE_SWTPM", "mute")
	oldReady := swtpmReady
	swtpmReady = 300 * time.Millisecond
	defer func() { swtpmReady = oldReady }()
	if _, _, err := startTPM(dir, t.TempDir()); err == nil {
		t.Fatal("a mute swtpm was accepted")
	}
	// Missing binary.
	swtpmBin = "/nonexistent/swtpm"
	if _, _, err := startTPM(dir, t.TempDir()); err == nil || !strings.Contains(err.Error(), "install the swtpm package") {
		t.Fatalf("missing swtpm: %v", err)
	}
}

// fakeSwtpm is the test binary run as swtpm: it listens on --ctrl's path and exits when its one
// client disconnects ("1", like --terminate), or never listens ("mute").
func fakeSwtpm() {
	if os.Getenv("FAKE_SWTPM") == "mute" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	var sock string
	for i, a := range os.Args {
		if a == "--ctrl" && i+1 < len(os.Args) {
			_, p, _ := strings.Cut(os.Args[i+1], "path=")
			sock = p
		}
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		os.Exit(1)
	}
	c, err := l.Accept() // QEMU: like --terminate, exit once it disconnects
	if err != nil {
		os.Exit(1)
	}
	c.Read(make([]byte, 1))
	os.Exit(0)
}

// ISO volume labels (task 3.7).
func TestDetectProfile(t *testing.T) {
	if _, err := exec.LookPath("xorriso"); err != nil {
		needTool(t, "xorriso missing")
	}
	for label, want := range map[string]string{
		"Ubuntu-Server 24.04.2 LTS amd64": "ubuntu", "Kubuntu 24.04.2 LTS amd64": "ubuntu",
		"Debian 12.4.0 amd64 n": "debian", "Fedora-WS-Live-41-1-4": "fedora", "Rocky-10-0-x86_64-dvd": "rocky",
		"CCCOMA_X64FRE_PT-BR_DV9": "windows11", "SSS_X64FREE_EN-US_DV9": "windows10", "TOTALLY_UNKNOWN": "",
	} {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "src", "README"), "x")
		iso := filepath.Join(dir, "x.iso")
		if out, err := exec.Command("xorriso", "-as", "mkisofs", "-quiet", "-V", label, "-o", iso, filepath.Join(dir, "src")).CombinedOutput(); err != nil {
			t.Fatalf("xorriso: %v %s", err, out)
		}
		p, got, ok := DetectProfile(iso)
		if got != label[:min(len(label), 32)] {
			t.Errorf("label %q read as %q", label, got)
		}
		if want == "" {
			if ok {
				t.Errorf("%q matched %s", label, p.ID)
			}
		} else if !ok || p.ID != want {
			t.Errorf("%q: got %q, want %q", label, p.ID, want)
		}
	}
	if _, err := ISOLabel(os.Args[0]); err == nil {
		t.Error("a non-ISO file has a label")
	}
}

// USB passthrough needs a plugged-in device the user can open (task 3.8).
func TestCheckUSBDevices(t *testing.T) {
	sys, dev := t.TempDir(), t.TempDir()
	for f, v := range map[string]string{"idVendor": "046d\n", "idProduct": "c52b\n", "busnum": "3\n", "devnum": "7\n"} {
		write(t, filepath.Join(sys, "3-1", f), v)
	}
	node := filepath.Join(dev, "003", "007")
	write(t, node, "")
	c := testGuest(t, "ubuntu")
	c.USBDevices = []USBDevice{{Vendor: "046D", Product: "C52B"}}
	if err := checkUSBDevices(c, sys, dev); err != nil {
		t.Fatalf("readable device refused: %v", err)
	}
	if os.Getuid() != 0 {
		os.Chmod(node, 0o400)
		if err := checkUSBDevices(c, sys, dev); err == nil || !strings.Contains(err.Error(), node) {
			t.Fatalf("unwritable device: %v", err)
		}
	}
	c.USBDevices = []USBDevice{{Vendor: "dead", Product: "beef"}}
	if err := checkUSBDevices(c, sys, dev); err == nil || !strings.Contains(err.Error(), "not plugged in") {
		t.Fatalf("absent device: %v", err)
	}
}

// A device the host's QEMU lacks stops the start, naming the setting (task 3.9).
func TestCheckDevices(t *testing.T) {
	qemu := filepath.Join(t.TempDir(), "qemu-kvm")
	write(t, qemu, "#!/bin/sh\necho 'name \"virtio-vga\", bus PCI'\necho 'name \"VGA\", bus PCI'\n")
	os.Chmod(qemu, 0o755)
	if err := checkDevices(qemu, []string{"-device", "virtio-vga", "-device", "VGA,foo=1"}); err != nil {
		t.Fatal(err)
	}
	err := checkDevices(qemu, []string{"-device", "vmware-svga"})
	if err == nil || !strings.Contains(err.Error(), "vmware-svga") || !strings.Contains(err.Error(), "Graphics") {
		t.Fatalf("vmware-svga: %v", err)
	}
	if err := checkDevices(qemu, []string{"-virtfs", "local,id=fs0"}); err == nil || !strings.Contains(err.Error(), "Shared Folders") {
		t.Fatalf("9p: %v", err)
	}
	if !slices.Contains(GuestNICModels, "e1000e") {
		t.Fatal("e1000e missing")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A guest VM's own files: private, deleted with --purge, moved with the VM; images elsewhere stay.
func TestGuestOwnedFiles(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		needTool(t, "qemu-img missing")
	}
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	ext := filepath.Join(t.TempDir(), "shared.qcow2")
	write(t, ext, "")
	c, _ := NewGuest("ubuntu", "Lab", "")
	c.Workspace = filepath.Join(t.TempDir(), "lab")
	c.DiskGiB = 1
	c.Disks = append(c.Disks, Disk{Path: ext, Format: "qcow2", Bus: BusVirtio})
	e, err := CreateGuestVM(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(c.Workspace, "disk0.qcow2")
	if st, _ := os.Stat(disk); st.Mode().Perm() != 0o600 {
		t.Fatalf("disk mode %v", st.Mode())
	}
	if st, _ := os.Stat(c.Workspace); st.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", st.Mode())
	}
	write(t, filepath.Join(c.Workspace, GuestVarsFile), "vars")
	moved := filepath.Join(t.TempDir(), "moved")
	if e, err = MoveVM(e, moved); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(moved, "disk0.qcow2")) || !exists(filepath.Join(moved, GuestVarsFile)) || !exists(ext) {
		t.Fatal("move: the VM's files must move and the shared image stay")
	}
	if _, err := DeleteVM(e, true); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(moved, "disk0.qcow2")) || !exists(ext) {
		t.Fatal("purge: the VM's disk goes, the shared image stays")
	}
	// A relative path leaving the folder is refused.
	bad := testGuest(t, "ubuntu")
	for _, p := range []string{"../other.qcow2", "linked/disk.qcow2"} {
		bad.Disks = []Disk{{Path: p, Format: "qcow2", Bus: BusVirtio}}
		if err := bad.Validate(); err == nil {
			t.Fatalf("%s accepted", p)
		}
	}
}

// Threads per core on an AMD host need topoext, or the guest sees an impossible topology (QEMU:
// "This family of AMD CPU doesn't support hyperthreading"); one thread per core, or Intel, never.
func TestGuestTopoext(t *testing.T) {
	defer func(f func() bool) { amdHost = f }(amdHost)
	ub := testGuest(t, "ubuntu")
	ub.VCPUs, ub.Cores = 2, 1
	for _, c := range []struct {
		amd          bool
		vcpus, cores int
		want         string
	}{{true, 2, 1, "-cpu host,topoext=on -m"}, {true, 2, 2, "-cpu host -m"}, {false, 2, 1, "-cpu host -m"}} {
		amdHost = func() bool { return c.amd }
		ub.VCPUs, ub.Cores = c.vcpus, c.cores
		a, err := guestArgs(ub, ub.Workspace, Firmware{}, "/run/x/qmp.sock", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(argPairs(a), c.want) {
			t.Errorf("AMD %v, %d vCPUs on %d cores: no %q in %v", c.amd, c.vcpus, c.cores, c.want, argPairs(a))
		}
	}
}
