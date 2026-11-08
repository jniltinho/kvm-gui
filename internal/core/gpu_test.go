package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeDev struct {
	addr, class, vendor, device, driver, group, bootVGA string
	monitor                                             bool
}

// fakeSysfs builds /sys/bus/pci/devices and /sys/kernel/iommu_groups for the given devices.
func fakeSysfs(t *testing.T, devs []fakeDev) Host {
	h := fakeHost(t, "svm", "Y")
	h.DevVFIO = filepath.Join(h.SysRoot, "dev-vfio")
	for _, d := range devs {
		dir := filepath.Join(h.pciDir(), d.addr)
		write(t, filepath.Join(dir, "class"), "0x"+d.class)
		write(t, filepath.Join(dir, "vendor"), "0x"+d.vendor)
		write(t, filepath.Join(dir, "device"), "0x"+d.device)
		if d.bootVGA != "" {
			write(t, filepath.Join(dir, "boot_vga"), d.bootVGA)
		}
		if d.driver != "" {
			os.Symlink("../../../bus/pci/drivers/"+d.driver, filepath.Join(dir, "driver"))
		}
		if d.monitor {
			card := filepath.Join(h.SysRoot, "class/drm/card"+d.group)
			os.MkdirAll(card, 0o755)
			os.Symlink(dir, filepath.Join(card, "device"))
			write(t, card+"-DP-1/status", "connected")
		}
		if d.driver == "vfio-pci" {
			write(t, filepath.Join(h.DevVFIO, d.group), "")
		}
		gdir := filepath.Join(h.SysRoot, "kernel/iommu_groups", d.group)
		os.MkdirAll(filepath.Join(gdir, "devices"), 0o755)
		os.Symlink(gdir, filepath.Join(dir, "iommu_group"))
		os.Symlink(dir, filepath.Join(gdir, "devices", d.addr))
	}
	return h
}

// referenceHost mirrors the real host, plus a hypothetical RX 6600 in slot 03:00.
var referenceHost = []fakeDev{
	{"0000:01:00.0", "030000", "10de", "2786", "nvidia", "13", "1", true},
	{"0000:01:00.1", "040300", "10de", "22bc", "snd_hda_intel", "13", "", false},
	{"0000:03:00.0", "030000", "1002", "73ff", "amdgpu", "14", "0", false},
	{"0000:03:00.1", "040300", "1002", "ab28", "snd_hda_intel", "14", "", false},
	{"0000:04:04.0", "060400", "1022", "43f5", "pcieport", "17", "", false},
	{"0000:08:00.0", "028000", "8086", "2725", "iwlwifi", "17", "", false},
	{"0000:0a:00.0", "030200", "10de", "1db6", "nvidia", "17", "", false},
	{"0000:75:00.0", "030000", "1002", "13c0", "amdgpu", "29", "0", false},
}

func gpuByAddr(gs []GPU, addr string) GPU {
	for _, g := range gs {
		if g.Addr == addr {
			return g
		}
	}
	return GPU{}
}

func TestGPUListingAndEligibility(t *testing.T) {
	h := fakeSysfs(t, referenceHost)
	gs := h.GPUs()
	if len(gs) != 4 {
		t.Fatalf("expected 4 GPUs, got %d", len(gs))
	}
	cases := []struct {
		addr     string
		compat   Compat
		eligible bool
		reason   string
	}{
		{"0000:01:00.0", CompatUnsupported, false, "no driver"}, // RTX 4070
		{"0000:0a:00.0", CompatUnsupported, false, "no driver"}, // Tesla V100
		{"0000:75:00.0", CompatUnknown, false, "Dortania"},      // Ryzen iGPU must never be reservable
		{"0000:03:00.0", CompatSupported, true, ""},             // RX 6600
	}
	for _, c := range cases {
		g := gpuByAddr(gs, c.addr)
		if g.Compat != c.compat || g.Eligible != c.eligible || !strings.Contains(g.Reason, c.reason) {
			t.Errorf("%s: compat=%v eligible=%v reason=%q", c.addr, g.Compat, g.Eligible, g.Reason)
		}
	}
	rx := gpuByAddr(gs, "0000:03:00.0")
	if len(rx.Funcs) != 2 || rx.Funcs[0].Addr != "0000:03:00.0" || rx.Funcs[1].ID != "1002:ab28" {
		t.Fatalf("slot functions: %+v", rx.Funcs)
	}
}

func TestCompatTable(t *testing.T) {
	cases := map[string]Compat{
		"1002:73ff": CompatSupported,   // RX 6600
		"1002:67df": CompatSupported,   // RX 580
		"1002:73df": CompatUnsupported, // RX 6700 XT: needs a patch kext
		"1002:73ef": CompatUnsupported, // RX 6650 XT: needs spoof
		"1002:744c": CompatUnsupported, // RX 7900
		"1002:13c0": CompatUnknown,     // Ryzen 9000 iGPU
		"10de:2786": CompatUnsupported, // RTX 4070
	}
	for id, want := range cases {
		v, d, _ := strings.Cut(id, ":")
		if got := compatOf(v, d); got != want {
			t.Errorf("%s: got %v want %v", id, got, want)
		}
	}
}

func TestEligibilityRefusals(t *testing.T) {
	boot := slices.Clone(referenceHost)
	boot[2].bootVGA = "1"
	if g := gpuByAddr(fakeSysfs(t, boot).GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "primary display") {
		t.Errorf("boot display must be refused: %q", g.Reason)
	}

	shared := slices.Clone(referenceHost)
	shared[2].group, shared[3].group = "17", "17" // same group as the Wi-Fi card
	if g := gpuByAddr(fakeSysfs(t, shared).GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "0000:08:00.0") {
		t.Errorf("shared group must name the foreign device: %q", g.Reason)
	}

	monitor := slices.Clone(referenceHost)
	monitor[2].monitor = true
	if g := gpuByAddr(fakeSysfs(t, monitor).GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "monitor") {
		t.Errorf("GPU driving a monitor must be refused: %q", g.Reason)
	}

	usb := append(slices.Clone(referenceHost), fakeDev{"0000:03:00.2", "0c0330", "1002", "73a6", "xhci_hcd", "14", "", false})
	if g := gpuByAddr(fakeSysfs(t, usb).GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "0000:03:00.2") {
		t.Errorf("a USB function in the slot must be refused: %q", g.Reason)
	}

	audioGroup := slices.Clone(referenceHost)
	audioGroup[3].group = "17" // audio function shares the Wi-Fi card's group
	if g := gpuByAddr(fakeSysfs(t, audioGroup).GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "0000:03:00.1") {
		t.Errorf("every function's group must be checked: %q", g.Reason)
	}

	twin := append(slices.Clone(referenceHost),
		fakeDev{"0000:05:00.0", "030000", "1002", "73ff", "amdgpu", "15", "0", false})
	if g := gpuByAddr(fakeSysfs(t, twin).GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "same PCI ID") {
		t.Errorf("an identical second card must be refused: %q", g.Reason)
	}

	alone := []fakeDev{referenceHost[2], referenceHost[3]}
	if g := gpuByAddr(fakeSysfs(t, alone).GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "host's display") {
		t.Errorf("the host's only GPU must be refused: %q", g.Reason)
	}

	h := fakeSysfs(t, referenceHost)
	os.RemoveAll(filepath.Join(h.SysRoot, "kernel/iommu_groups"))
	if g := gpuByAddr(h.GPUs(), "0000:03:00.0"); g.Eligible || !strings.Contains(g.Reason, "IOMMU") {
		t.Errorf("IOMMU off must be refused: %q", g.Reason)
	}
}

func TestReserveAndRelease(t *testing.T) {
	h := fakeSysfs(t, referenceHost)
	rx := gpuByAddr(h.GPUs(), "0000:03:00.0")
	conf := filepath.Join(h.EtcRoot, vfioModprobe)

	h.Run = func(name string, args ...string) error {
		if name != "pkexec" || args[2] != reserveScript || args[3] != "sh" || args[4] != "1002:73ff,1002:ab28" || args[5] != "update-initramfs" {
			t.Fatalf("unexpected command: %s %q", name, args)
		}
		write(t, conf, "options vfio-pci ids="+args[4]+" disable_vga=1\n")
		return nil
	}
	if err := h.ReserveGPU(rx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reserveScript, "blacklist") {
		t.Fatal("the reservation must never blacklist a driver (the host iGPU uses amdgpu too)")
	}

	h.Run = func(string, ...string) error { return exitErr(126) }
	if err := h.ReleaseGPU(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("126 must be cancelled: %v", err)
	}
	h.Run = func(name string, args ...string) error { os.Remove(conf); return nil }
	if err := h.ReleaseGPU(); err != nil || h.ReservedIDs() != "" {
		t.Fatalf("release: %v", err)
	}

	bad := rx
	bad.Funcs = []PCIFunc{{"0000:03:00.0", "1002:73ff;reboot", "amdgpu"}}
	h.Run = func(string, ...string) error { t.Fatal("must not elevate with an invalid ID"); return nil }
	if err := h.ReserveGPU(bad); err == nil {
		t.Fatal("invalid ID must be refused")
	}
	if err := h.ReserveGPU(gpuByAddr(h.GPUs(), "0000:75:00.0")); err == nil {
		t.Fatal("the iGPU must never be reservable")
	}
}

func TestGPUReady(t *testing.T) {
	c := testDefaults()
	c.GPU, c.RAMMiB = "0000:03:00.0", 16384
	devs := slices.Clone(referenceHost)
	if _, err := fakeSysfs(t, devs).GPUReady(c, 1<<40); err == nil || !strings.Contains(err.Error(), "reserve the GPU") {
		t.Fatalf("amdgpu-bound GPU must be refused: %v", err)
	}
	devs[2].driver, devs[3].driver = "vfio-pci", "vfio-pci"
	h := fakeSysfs(t, devs)
	if _, err := h.GPUReady(c, 8<<30); err == nil || !strings.Contains(err.Error(), "16384 MiB") {
		t.Fatalf("low memlock must be refused: %v", err)
	}
	old := c
	old.Version = "big-sur"
	if _, err := h.GPUReady(old, ^uint64(0)); err == nil || !strings.Contains(err.Error(), "monterey") {
		t.Fatalf("RX 6600 on Big Sur must be refused: %v", err)
	}
	funcs, err := h.GPUReady(c, ^uint64(0))
	if err != nil || !slices.Equal(funcs, []string{"0000:03:00.0", "0000:03:00.1"}) {
		t.Fatalf("ready GPU: %v %v", funcs, err)
	}
	os.Chmod(filepath.Join(h.DevVFIO, "14"), 0)
	if os.Geteuid() != 0 {
		if _, err := h.GPUReady(c, ^uint64(0)); err == nil || !strings.Contains(err.Error(), "kvm group") {
			t.Fatalf("unreadable /dev/vfio node must be refused: %v", err)
		}
	}
}

// The real shell must reject hostile IDs before touching anything.
func TestReserveScriptRejectsInjection(t *testing.T) {
	for _, bad := range []string{"", "1002:73ff;reboot", "$(id)", "1002:73ff ab28", "../x"} {
		err := exec.Command("/bin/sh", "-c", reserveScript, "sh", bad).Run()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 {
			t.Errorf("%q: expected exit 2, got %v", bad, err)
		}
	}
}

func TestBuildArgsWithGPU(t *testing.T) {
	args := BuildArgs(testDefaults(), NewWorkspace("/vm"), "/run/q.sock", false, []string{"0000:03:00.0", "0000:03:00.1"})
	if !slices.Contains(args, "vmware-svga") {
		t.Fatal("vmware-svga must stay: the QEMU window needs a graphical console for input")
	}
	if argAfter(args, "-device", "vfio-pci,host=0000:03:00.0,multifunction=on,x-no-kvm-intx=on") == "" ||
		argAfter(args, "-device", "vfio-pci,host=0000:03:00.1") == "" {
		t.Fatalf("vfio args missing: %q", args)
	}
	if !slices.Contains(BuildArgs(testDefaults(), NewWorkspace("/vm"), "/run/q.sock", false, nil), "vmware-svga") {
		t.Fatal("without a GPU the emulated display stays")
	}
	c := testDefaults()
	c.GPU = "03:00.0"
	var fe *FieldError
	if err := c.Validate(); !errors.As(err, &fe) || fe.Field != "gpu" {
		t.Fatalf("short PCI address must be refused: %v", err)
	}
}

// A GT 710 (GK208, 10de:128b) in slot 05:00, bound to vfio-pci, next to the host's RX 6600.
func TestKeplerGPU(t *testing.T) {
	devs := append(slices.Clone(referenceHost),
		fakeDev{"0000:05:00.0", "030000", "10de", "128b", "vfio-pci", "21", "0", false},
		fakeDev{"0000:05:00.1", "040300", "10de", "0e0f", "vfio-pci", "21", "", false})
	h := fakeSysfs(t, devs)
	g := gpuByAddr(h.GPUs(), "0000:05:00.0")
	if g.Compat != CompatKepler || !g.Eligible {
		t.Fatalf("a Kepler card can be passed through: %+v", g)
	}
	if compatOf("10de", "1380") != CompatUnsupported || compatOf("10de", "0f02") != CompatUnsupported {
		t.Fatal("Maxwell (GTX 750 Ti) and Fermi (GT 730 GF108) are not Kepler")
	}
	c := testDefaults()
	c.GPU, c.RAMMiB = "0000:05:00.0", 8192
	for v, want := range map[string]string{"big-sur": "", "catalina": "", "sequoia": "root-patch mode", "tahoe": "no driver"} {
		c.Version, c.RootPatch = v, false
		_, err := h.GPUReady(c, ^uint64(0))
		if (want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("%s without root-patch mode: %v", v, err)
		}
	}
	c.Version, c.RootPatch = "sequoia", true
	if funcs, err := h.GPUReady(c, ^uint64(0)); err != nil || len(funcs) != 2 {
		t.Fatalf("Sequoia with root-patch mode: %v %v", funcs, err)
	}
	if !strings.Contains(reserveScript, "softdep nouveau pre: vfio-pci") || !strings.Contains(reserveScript, "softdep nvidia pre: vfio-pci") {
		t.Error("vfio-pci must claim an NVIDIA card before nouveau or nvidia")
	}
}

func TestPatchKextGPU(t *testing.T) {
	isolateXDG(t, t.TempDir())
	host := slices.Clone(referenceHost)
	host[2].device = "73df" // RX 6700 XT
	reason := func() (bool, string) {
		g := gpuByAddr(fakeSysfs(t, host).GPUs(), "0000:03:00.0")
		return g.Eligible, g.Reason
	}
	if ok, r := reason(); ok || !strings.Contains(r, "settings.yaml") {
		t.Fatalf("without kexts the reason names settings.yaml: %v %q", ok, r)
	}
	kextsYAML(t, "kexts:\n  - url: https://example.com/a.zip\n")
	if ok, r := reason(); !ok {
		t.Fatalf("with a kext listed it is eligible: %q", r)
	}
	kextsYAML(t, "kexts: [")
	if ok, r := reason(); ok || !strings.Contains(r, "settings.yaml") {
		t.Fatalf("a broken file is the reason: %v %q", ok, r)
	}

	kextsYAML(t, "kexts:\n  - url: https://example.com/a.zip\n")
	c := testDefaults()
	c.GPU, c.Version = "0000:03:00.0", "sequoia"
	c.Extras = &ExtraKexts{} // the start's snapshot, taken before settings.yaml was edited
	if _, err := fakeSysfs(t, host).GPUReady(c, 1<<40); err == nil || !strings.Contains(err.Error(), "settings.yaml") {
		t.Fatalf("a start without kexts refuses the GPU: %v", err)
	}
}

// Rocky Linux rebuilds every kernel's initramfs with dracut; another distribution changes nothing
// (task 4.4).
func TestReserveInitramfsPerDistribution(t *testing.T) {
	h := fakeSysfs(t, referenceHost)
	rx := gpuByAddr(h.GPUs(), "0000:03:00.0")
	conf := filepath.Join(h.EtcRoot, vfioModprobe)
	write(t, filepath.Join(h.EtcRoot, "os-release"), "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=\"10.0\"\n")
	var tools []string
	h.Run = func(name string, args ...string) error {
		tools = append(tools, args[len(args)-1])
		if args[2] == reserveScript {
			write(t, conf, "options vfio-pci ids="+args[4]+" disable_vga=1\n")
		} else {
			os.Remove(conf)
		}
		return nil
	}
	if err := h.ReserveGPU(rx); err != nil {
		t.Fatal(err)
	}
	if err := h.ReleaseGPU(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(tools, " ") != "dracut dracut" {
		t.Fatalf("initramfs tools: %v", tools)
	}
	for _, sc := range []string{reserveScript, releaseScript} {
		if !strings.Contains(sc, "dracut) rebuild() { dracut --regenerate-all --force; }") || !strings.Contains(sc, `echo "invalid initramfs tool" >&2; exit 2`) {
			t.Fatal("the scripts must rebuild with dracut on Rocky and refuse another tool")
		}
	}
	write(t, filepath.Join(h.EtcRoot, "os-release"), "ID=arch\n")
	h.Run = func(string, ...string) error { t.Fatal("must not elevate on an unsupported distribution"); return nil }
	if err := h.ReserveGPU(rx); err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("unsupported distribution: %v", err)
	}
}

// While a VM with the GPU runs, another VM with that GPU and Release are refused, naming it; a VM
// without passthrough starts as usual (task 4.4).
func TestGPUSingleUse(t *testing.T) {
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	mk := func(name, gpu string) VMEntry {
		c := testDefaults()
		c.Name, c.GPU = name, gpu
		c.Workspace = filepath.Join(t.TempDir(), name)
		c.SSHPort = 0
		write(t, filepath.Join(c.Workspace, DiskFile), "")
		e, err := CreateVM(c)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	a, b, _ := mk("GPU-A", "0000:03:00.0"), mk("GPU-B", "0000:03:00.0"), mk("Plain", "")
	running := map[string]bool{a.ID: true}
	old := findRunFn
	findRunFn = func(e VMEntry) (RunInfo, bool) { return RunInfo{PID: 1}, running[e.ID] }
	t.Cleanup(func() { findRunFn = old })
	if err := (&VM{ID: b.ID}).Start(b.Config); err == nil || !strings.Contains(err.Error(), "GPU-A, which is running") {
		t.Fatalf("second VM with the GPU: %v", err)
	}
	if name, ok := gpuRunner("0000:03:00.0", a.ID); ok {
		t.Fatalf("a VM is not its own GPU's other user: %s", name)
	}
	h := fakeSysfs(t, referenceHost)
	h.Run = func(string, ...string) error { t.Fatal("must not elevate while the GPU is in use"); return nil }
	if err := h.ReleaseGPU(); err == nil || !strings.Contains(err.Error(), "GPU-A is running") {
		t.Fatalf("release while in use: %v", err)
	}
	running = map[string]bool{}
	if _, ok := gpuRunner("", ""); ok {
		t.Fatal("no VM runs")
	}
}

// The reservation scripts for real, as root in a throwaway container (MKG_ROOT_SCRIPTS=1, never
// on a host): dracut rebuilds on success, and a failed rebuild removes the files and rebuilds again.
func TestReserveScriptsAsRoot(t *testing.T) {
	if os.Getenv("MKG_ROOT_SCRIPTS") != "1" || os.Getuid() != 0 {
		t.Skip("only as root in a throwaway container")
	}
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	write(t, filepath.Join(bin, "dracut"), "#!/bin/sh\necho \"dracut $*\" >> "+calls+"\n[ ! -e "+bin+"/fail ] || { rm "+bin+"/fail; exit 1; }\n")
	os.Chmod(filepath.Join(bin, "dracut"), 0o755)
	os.MkdirAll("/run/lock", 0o755)
	os.MkdirAll("/etc/modprobe.d", 0o755)
	os.MkdirAll("/etc/udev/rules.d", 0o755)
	sh := func(script string, args ...string) error {
		c := exec.Command("/bin/sh", append([]string{"-c", script, "sh"}, args...)...)
		c.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
		out, err := c.CombinedOutput()
		t.Logf("%s", out)
		return err
	}
	conf := "/etc/modprobe.d/macos-kvm-gui-vfio.conf"
	if err := sh(reserveScript, "1002:73ff,1002:ab28", "dracut"); err != nil || !exists(conf) {
		t.Fatalf("reserve: %v", err)
	}
	if err := sh(releaseScript, "dracut"); err != nil || exists(conf) {
		t.Fatalf("release: %v", err)
	}
	write(t, filepath.Join(bin, "fail"), "")
	if err := sh(reserveScript, "1002:73ff", "dracut"); err == nil || exists(conf) || exists("/etc/udev/rules.d/99-macos-kvm-gui-vfio.rules") {
		t.Fatalf("a failed rebuild must roll back: %v", err)
	}
	if got := readFile(t, calls); strings.Count(got, "dracut --regenerate-all --force") != 4 {
		t.Fatalf("dracut calls:\n%s", got)
	}
	if err := sh(reserveScript, "1002:73ff", "rm -rf /"); err == nil {
		t.Fatal("another initramfs tool was accepted")
	}
}

// While one GPU start or release holds the GPU lock, another is refused (Codex group 4 review).
func TestGPULock(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	unlock, err := gpuLock()
	if err != nil {
		t.Fatal(err)
	}
	h := fakeSysfs(t, referenceHost)
	h.Run = func(string, ...string) error { t.Fatal("must not elevate while a GPU start runs"); return nil }
	if err := h.ReleaseGPU(); !errors.Is(err, ErrBusy) {
		t.Fatalf("release during a start: %v", err)
	}
	unlock()
	if u, err := gpuLock(); err != nil {
		t.Fatal(err)
	} else {
		u()
	}
}

// A QEMU passing the GPU through is found even when no VM in the list started it (--config).
func TestGPUProcess(t *testing.T) {
	proc := t.TempDir()
	fakeProc(t, proc, "900", "qemu-system-x86", "", "qemu-system-x86_64", "-device", "vfio-pci,host=0000:03:00.0,multifunction=on")
	if pid, ok := gpuProcess(proc, "0000:03:00.0"); !ok || pid != 900 {
		t.Fatalf("by address: %d %v", pid, ok)
	}
	if _, ok := gpuProcess(proc, ""); !ok {
		t.Fatal("any GPU")
	}
	if _, ok := gpuProcess(proc, "0000:04:00.0"); ok {
		t.Fatal("another address")
	}
}
