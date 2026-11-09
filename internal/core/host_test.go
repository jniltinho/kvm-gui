package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeHost builds fake /proc, /sys and /etc in a temp directory.
func fakeHost(t *testing.T, cpuFlags, msrs string, procs ...string) Host {
	root := t.TempDir()
	h := Host{
		ProcRoot: filepath.Join(root, "proc"),
		SysRoot:  filepath.Join(root, "sys"),
		EtcRoot:  filepath.Join(root, "etc"),
		DevKVM:   filepath.Join(root, "kvm"),
		LookPath: func(s string) (string, error) { return "/usr/bin/" + s, nil },
		Output: func(string, ...string) ([]byte, error) {
			return []byte("QEMU emulator version 10.2.1 (Debian 1:10.2.1+ds-1ubuntu3.3)\nname \"apple-gfx-pci\", bus PCI"), nil
		},
		ShareRoot:  filepath.Join(root, "share"),
		DevUdmabuf: filepath.Join(root, "udmabuf"),
	}
	write(t, filepath.Join(h.ShareRoot, "vulkan/icd.d/nvidia_icd.json"), "{}")
	write(t, filepath.Join(h.ProcRoot, "sys/vm/max_map_count"), "2097152\n")
	reims := filepath.Join(root, "reims-qemu")
	write(t, reims, "")
	t.Setenv("MKG_REIMS_QEMU", reims)
	write(t, filepath.Join(h.ProcRoot, "cpuinfo"), "processor : 0\nflags\t\t: fpu "+cpuFlags+"\n")
	if msrs != "" {
		write(t, h.msrsPath(), msrs+"\n")
	}
	for i, p := range procs {
		write(t, filepath.Join(h.ProcRoot, string(rune('1'+i)), "comm"), p+"\n")
	}
	write(t, h.DevKVM, "")
	write(t, filepath.Join(h.EtcRoot, "os-release"), "ID=ubuntu\nVERSION_ID=\"26.04\"\nPRETTY_NAME=\"Ubuntu 26.04.1 LTS\"\n")
	return h
}

func byName(cs []Check) map[string]Check {
	m := map[string]Check{}
	for _, c := range cs {
		m[c.Name] = c
	}
	return m
}

func TestChecksReadyHost(t *testing.T) {
	h := fakeHost(t, "svm avx2 sse4_1", "Y", "bash")
	for _, c := range h.Checks("sonoma") {
		if c.Status != OK {
			t.Errorf("%s: %v (%s)", c.Name, c.Status, c.Detail)
		}
	}
}

func TestChecksProblems(t *testing.T) {
	h := fakeHost(t, "fpu", "N", "VBoxHeadless")
	h.LookPath = func(s string) (string, error) {
		if s == "qemu-img" {
			return "", exec.ErrNotFound
		}
		return s, nil
	}
	h.Output = func(string, ...string) ([]byte, error) { return []byte("QEMU emulator version 6.2.0"), nil }
	os.Chmod(h.DevKVM, 0)
	m := byName(h.Checks("sonoma"))
	want := map[string]Status{
		"CPU virtualization":          Fail,
		"qemu-img":                    Fail,
		"QEMU version":                Fail,
		"CPU compatible with version": Fail,
		"kvm ignore_msrs":             Fail,
		"Other hypervisors":           Warn,
	}
	if os.Geteuid() != 0 {
		want["/dev/kvm"] = Fail
	}
	for name, st := range want {
		if m[name].Status != st {
			t.Errorf("%s: expected %v, got %v (%s)", name, st, m[name].Status, m[name].Detail)
		}
	}
	if !strings.Contains(m["qemu-img"].Hint, "qemu-utils") {
		t.Errorf("qemu-img hint must name the package: %q", m["qemu-img"].Hint)
	}
	if !strings.Contains(m["/dev/kvm"].Hint, "kvm") {
		t.Errorf("/dev/kvm hint: %q", m["/dev/kvm"].Hint)
	}
}

func TestMemTotal(t *testing.T) {
	h := fakeHost(t, "svm", "Y")
	write(t, filepath.Join(h.ProcRoot, "meminfo"), "MemFree:  100 kB\nMemTotal:       195037184 kB\n")
	if got := h.MemTotalMiB(); got != 190466 {
		t.Fatalf("MemTotalMiB = %d", got)
	}
}

func TestCompatOldVersionAndUnknownProcs(t *testing.T) {
	h := fakeHost(t, "vmx", "Y")
	if c := h.checkCompat(h.cpuFlags(), "monterey"); c.Status != OK {
		t.Errorf("monterey does not require AVX2: %v", c.Status)
	}
	h.ProcRoot = filepath.Join(h.ProcRoot, "does-not-exist")
	if c := h.checkConflict(); c.Status != Unknown {
		t.Errorf("without /proc it must be inconclusive: %v", c.Status)
	}
}

// exitErr produces a real *exec.ExitError with the given code.
func exitErr(code int) error {
	return exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(code)).Run()
}

func TestFixIgnoreMSRs(t *testing.T) {
	h := fakeHost(t, "svm", "N")
	conf := filepath.Join(h.EtcRoot, modprobeFile)

	h.Run = func(name string, args ...string) error {
		if name != "pkexec" || args[2] != fixScript {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		write(t, h.msrsPath(), "Y")
		write(t, conf, "options kvm ignore_msrs=1\n")
		return nil
	}
	if err := h.FixIgnoreMSRs(); err != nil {
		t.Fatalf("expected success: %v", err)
	}

	write(t, h.msrsPath(), "N")
	os.Remove(conf)
	h.Run = func(string, ...string) error { return exitErr(126) }
	if err := h.FixIgnoreMSRs(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("126 must mean cancelled: %v", err)
	}

	h.Run = func(string, ...string) error { write(t, h.msrsPath(), "Y"); return exitErr(1) }
	if err := h.FixIgnoreMSRs(); err == nil || !strings.Contains(err.Error(), "not persist") {
		t.Fatalf("expected partial failure: %v", err)
	}

	write(t, h.msrsPath(), "N")
	h.Run = func(string, ...string) error { return exitErr(127) }
	if err := h.FixIgnoreMSRs(); err == nil || !strings.Contains(err.Error(), "pkexec failed") {
		t.Fatalf("expected pkexec error: %v", err)
	}
}

func TestSetupHost(t *testing.T) {
	h := fakeHost(t, "svm avx2", "N")
	h.LookPath = func(s string) (string, error) {
		if s == "qemu-img" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + s, nil
	}
	h.Run = func(name string, args ...string) error {
		if name != "pkexec" || args[2] != setupScript || args[4] != "nilton" || args[5] != "kvm_amd" || args[6] != "qemu-utils" {
			t.Fatalf("unexpected command: %s %q", name, args[3:])
		}
		write(t, h.msrsPath(), "Y")
		return nil
	}
	msg, err := h.SetupHost("nilton")
	if err != nil || !strings.Contains(msg, "ready") {
		t.Fatalf("setup: %q %v", msg, err)
	}

	os.Chmod(h.DevKVM, 0)
	if os.Geteuid() != 0 {
		if msg, _ := h.SetupHost("nilton"); !strings.Contains(msg, "log out") {
			t.Errorf("new group membership needs a new login: %q", msg)
		}
	}

	h.Run = func(string, ...string) error { return exitErr(126) }
	if _, err := h.SetupHost("nilton"); !errors.Is(err, ErrCancelled) {
		t.Errorf("126 must be cancelled: %v", err)
	}
	h.Run = func(string, ...string) error { t.Fatal("must not elevate"); return nil }
	if _, err := h.SetupHost("bad;user"); err == nil {
		t.Error("invalid user names must be refused before elevation")
	}
	noVirt := fakeHost(t, "fpu", "N")
	noVirt.Run = h.Run
	if _, err := noVirt.SetupHost("nilton"); err == nil || !strings.Contains(err.Error(), "BIOS") {
		t.Errorf("no VT-x/AMD-V: %v", err)
	}
}

// The real shell must reject hostile arguments before running anything.
func TestSetupScriptRejectsInjection(t *testing.T) {
	for _, args := range [][]string{
		{"-o", "kvm_amd", ""},
		{"nilton;reboot", "kvm_amd", ""},
		{"nilton", "kvm_amd;reboot", ""},
		{"nilton", "kvm_amd", "qemu-utils;reboot"},
		{"nilton", "kvm_amd", "$(id)"},
		{"nilton", "kvm_amd", "", "yum;reboot"},
		{"nilton", "kvm_amd", ""},
	} {
		err := exec.Command("/bin/sh", append([]string{"-c", setupScript, "sh"}, args...)...).Run()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 {
			t.Errorf("%q: expected exit 2, got %v", args, err)
		}
	}
}

func TestPkexecCancelled(t *testing.T) {
	kde := fmt.Errorf("%w: Error executing command as another user: Not authorized This incident has been reported.", exitErr(127))
	missing := fmt.Errorf("%w: Error executing command as another user: /x: No such file or directory", exitErr(127))
	cases := []struct {
		err  error
		want bool
	}{
		{exitErr(126), true},
		{kde, true},
		{missing, false},
		{exitErr(1), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := pkexecCancelled(c.err); got != c.want {
			t.Errorf("%v: got %v want %v", c.err, got, c.want)
		}
	}
	h := fakeHost(t, "svm", "N")
	h.Run = func(string, ...string) error { return kde }
	if _, err := h.SetupHost("nilton"); !errors.Is(err, ErrCancelled) {
		t.Errorf("KDE cancel must be reported as cancelled: %v", err)
	}
}

// The package manager and QEMU per distribution, and the guest checks (task 3.9).
func TestHostPerDistribution(t *testing.T) {
	h := fakeHost(t, "svm avx2", "Y")
	for release, want := range map[string]string{
		"ID=ubuntu\nVERSION_ID=\"26.04\"\n":              "apt",
		"ID=debian\nVERSION_ID=\"13\"\n":                 "apt",
		"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n": "dnf",
		"ID=almalinux\nID_LIKE=\"rhel centos fedora\"\n": "dnf",
		"ID=arch\n": "",
		"ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"\n": "",
	} {
		write(t, filepath.Join(h.EtcRoot, "os-release"), release)
		if got := h.PackageManager(); got != want {
			t.Errorf("%q: %q, want %q", release, got, want)
		}
	}
	// Rocky: qemu-kvm in /usr/libexec, packages and hints through dnf.
	write(t, filepath.Join(h.EtcRoot, "os-release"), "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=\"10.0\"\n")
	h.QEMUKVM = filepath.Join(t.TempDir(), "qemu-kvm")
	write(t, h.QEMUKVM, "")
	h.LookPath = func(s string) (string, error) { return "", exec.ErrNotFound }
	h.FirmwareRoot = t.TempDir()
	cs := byName(h.Checks("sonoma"))
	if c := cs["qemu-system-x86_64"]; c.Status != OK || !strings.Contains(c.Detail, h.QEMUKVM) {
		t.Fatalf("Rocky QEMU: %+v", c)
	}
	if c := cs["qemu-img"]; c.Status != Fail || c.Hint != "sudo dnf install qemu-img" {
		t.Fatalf("qemu-img hint: %+v", c)
	}
	if c := cs["swtpm"]; c.Status != Warn || c.Hint != "sudo dnf install swtpm" {
		t.Fatalf("swtpm: %+v", c)
	}
	if c := cs["Secure Boot firmware"]; c.Status != Warn || c.Hint != "sudo dnf install edk2-ovmf" {
		t.Fatalf("firmware: %+v", c)
	}
	var ran []string
	h.Run = func(name string, args ...string) error {
		ran = args
		write(t, h.msrsPath(), "Y")
		return nil
	}
	if _, err := h.SetupHost("nilton"); err != nil || ran[6] != "qemu-img" || ran[7] != "dnf" {
		t.Fatalf("Rocky setup: %v %q", err, ran)
	}
	// Unsupported distribution: nothing runs, with or without missing packages.
	write(t, filepath.Join(h.EtcRoot, "os-release"), "ID=arch\n")
	ran = nil
	if _, err := h.SetupHost("nilton"); err == nil || !strings.Contains(err.Error(), "qemu-utils") || ran != nil {
		t.Fatalf("unsupported: %v %q", err, ran)
	}
	h.LookPath = func(s string) (string, error) { return "/usr/bin/" + s, nil }
	if _, err := h.SetupHost("nilton"); err == nil || !strings.Contains(err.Error(), "nothing was changed") || ran != nil {
		t.Fatalf("unsupported with every tool: %v %q", err, ran)
	}
}

// Supported hosts and the QEMU floor (OpenSpec ubuntu-26-04-and-vulkan, task 2.1): Ubuntu 26.04,
// Debian 13 and Rocky Linux 10 or later are OK, anything else warns; QEMU must be 10.0 or later.
func TestHostSystemAndQEMUFloor(t *testing.T) {
	for osr, want := range map[string]Status{
		"ID=ubuntu\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\n": Warn,
		"ID=ubuntu\nVERSION_ID=\"26.04\"\n":                                     OK,
		"ID=ubuntu\nVERSION_ID=\"26.10\"\n":                                     OK,
		"ID=debian\nVERSION_ID=\"12\"\n":                                        Warn,
		"ID=debian\nVERSION_ID=\"13\"\n":                                        OK,
		"ID=rocky\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=\"10.1\"\n":       OK,
		"ID=almalinux\nVERSION_ID=\"9.6\"\n":                                    Warn,
		"ID=fedora\nVERSION_ID=\"43\"\n":                                        Warn,
		"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\nVERSION_ID=\"22.2\"\n":        Warn,
	} {
		h := fakeHost(t, "svm", "Y")
		write(t, filepath.Join(h.EtcRoot, "os-release"), osr)
		c := byName(h.Checks("sonoma"))["Host system"]
		if c.Status != want {
			t.Errorf("%q: %v (%s), want %v", osr, c.Status, c.Detail, want)
		}
		if want == Warn && (!strings.Contains(c.Hint, SupportedHosts) || !strings.Contains(c.Hint, "v0.15.0")) {
			t.Errorf("%q: hint %q", osr, c.Hint)
		}
	}
	for v, want := range map[string]Status{"8.2.2": Fail, "9.2.0": Fail, "10.0.13": OK, "10.1.0": OK, "10.2.1": OK, "11.0.0": OK} {
		h := fakeHost(t, "svm", "Y")
		h.Output = func(string, ...string) ([]byte, error) { return []byte("QEMU emulator version " + v), nil }
		c := h.checkQEMUVersion()
		if c.Status != want {
			t.Errorf("QEMU %s: %v, want %v", v, c.Status, want)
		}
		if want == Fail && !strings.Contains(c.Hint, "10.0") {
			t.Errorf("QEMU %s: hint %q", v, c.Hint)
		}
	}
}

// Set up host changes nothing on Ubuntu 24.04: its QEMU would fail the version check.
func TestSetupHostRefusesUbuntu2404(t *testing.T) {
	h := fakeHost(t, "svm", "N")
	write(t, filepath.Join(h.EtcRoot, "os-release"), "ID=ubuntu\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\n")
	h.Run = func(name string, args ...string) error { t.Fatalf("ran %s %v", name, args); return nil }
	_, err := h.SetupHost("tester")
	if err == nil || !strings.Contains(err.Error(), SupportedHosts) || !strings.Contains(err.Error(), "Ubuntu 24.04.3 LTS") {
		t.Fatalf("got %v", err)
	}
}
