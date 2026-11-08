package core

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Firmware is a matching pair of host OVMF files: the code and the template of the variables a
// new VM copies once. Only a pair from the same row of firmwarePairs is ever used together.
type Firmware struct {
	Code, Vars string
	Secure     bool
	Package    string // the package that provides it, for messages
}

// firmwarePairs lists the host OVMF builds, by distribution layout: Ubuntu and Debian's ovmf,
// then Rocky Linux and Fedora's edk2-ovmf. Secure pairs have Microsoft's keys enrolled.
var firmwarePairs = []Firmware{
	{Code: "/usr/share/OVMF/OVMF_CODE_4M.secboot.fd", Vars: "/usr/share/OVMF/OVMF_VARS_4M.ms.fd", Secure: true, Package: "ovmf"},
	{Code: "/usr/share/OVMF/OVMF_CODE_4M.fd", Vars: "/usr/share/OVMF/OVMF_VARS_4M.fd", Package: "ovmf"},
	{Code: "/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd", Vars: "/usr/share/edk2/ovmf/OVMF_VARS.secboot.fd", Secure: true, Package: "edk2-ovmf"},
	{Code: "/usr/share/edk2/ovmf/OVMF_CODE.fd", Vars: "/usr/share/edk2/ovmf/OVMF_VARS.fd", Package: "edk2-ovmf"},
}

// HostFirmware finds the host's OVMF pair under root ("/" on a real host).
func HostFirmware(root string, secure bool) (Firmware, error) {
	var want []string
	for _, f := range firmwarePairs {
		if f.Secure != secure {
			continue
		}
		if exists(filepath.Join(root, f.Code)) && exists(filepath.Join(root, f.Vars)) {
			return Firmware{Code: filepath.Join(root, f.Code), Vars: filepath.Join(root, f.Vars), Secure: secure, Package: f.Package}, nil
		}
		want = append(want, f.Code)
	}
	kind := "UEFI firmware (OVMF)"
	if secure {
		kind = "Secure Boot UEFI firmware with Microsoft keys"
	}
	return Firmware{}, fmt.Errorf("no %s on this host: looked for %s; install the ovmf package (Ubuntu, Debian) or edk2-ovmf (Rocky Linux)",
		kind, strings.Join(want, ", "))
}

// varsKind names the template a VM's variables were made from.
func varsKind(secure bool) string {
	if secure {
		return "secure"
	}
	return "plain"
}

// ErrVarsTemplate: the VM's UEFI variables were made for the other Secure Boot setting.
var ErrVarsTemplate = errors.New("Secure Boot was changed after the UEFI variables were made: reset them (kvm-gui config reset-nvram; boot entries stored in them are lost)")

// ensureVars makes the VM's UEFI variables from the template once and never overwrites them. A VM
// whose variables came from the other kind of template is refused until ResetVars.
func ensureVars(dir string, fw Firmware) error {
	vars := filepath.Join(dir, GuestVarsFile)
	kindFile := filepath.Join(dir, GuestVarsKind)
	if exists(vars) {
		// A missing marker (an interrupted first start) is refused too: plain variables must
		// never pass for Secure Boot ones.
		k, err := os.ReadFile(kindFile)
		if err != nil || strings.TrimSpace(string(k)) != varsKind(fw.Secure) {
			return ErrVarsTemplate
		}
		return nil
	}
	// The marker first: variables without one are refused, never taken for the other kind.
	if err := writeFileAtomic(kindFile, []byte(varsKind(fw.Secure)+"\n"), 0o600); err != nil {
		return err
	}
	return copyFileAtomic(fw.Vars, vars, 0o600)
}

// ResetVars replaces a powered-off VM's UEFI variables with a fresh copy of the template that
// matches its Secure Boot setting. Boot entries stored in the old ones are lost.
func ResetVars(e VMEntry) error {
	c := e.Config
	if c.MacOS() || c.Firmware != FirmwareUEFI {
		return errors.New("only UEFI Linux, Windows and Other VMs have resettable variables")
	}
	dir, err := c.WorkspaceDir()
	if err != nil {
		return err
	}
	fw, err := HostFirmware("/", c.SecureBoot)
	if err != nil {
		return err
	}
	unlock, err := NewWorkspace(dir).Lock() // the lock a start takes: no start meanwhile
	if err != nil {
		return err
	}
	defer unlock()
	if _, running := FindRun(e); running {
		return errors.New("power the VM off first")
	}
	// Replaced in place (write and rename), so a failure leaves the old variables: the marker
	// first, which refuses a start until both match.
	if err := writeFileAtomic(filepath.Join(dir, GuestVarsKind), []byte(varsKind(fw.Secure)+"\n"), 0o600); err != nil {
		return err
	}
	return copyFileAtomic(fw.Vars, filepath.Join(dir, GuestVarsFile), 0o600)
}

func copyFileAtomic(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // on disk before it replaces anything
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return err
	}
	return syncDir(filepath.Dir(dst))
}

// swtpm timing: QEMU starts only once the socket answers; after QEMU exits, swtpm gets a moment
// to write its state before it is killed.
var (
	swtpmReady = 5 * time.Second
	swtpmExit  = 10 * time.Second
	swtpmBin   = "swtpm"
)

// startTPM runs swtpm for a VM: state in <VM folder>/tpm (0700, files 0600 through umask 077),
// control socket in the run's private directory. stop waits for swtpm to exit (killing it after
// swtpmExit) and must run on every exit path, before the VM's lock is released.
func startTPM(dir, runDir string) (sock string, stop func(), err error) {
	bin, err := exec.LookPath(swtpmBin)
	if err != nil {
		return "", nil, errors.New("this VM has a TPM and swtpm is not installed: install the swtpm package")
	}
	state := filepath.Join(dir, GuestTPMDir)
	if err := os.MkdirAll(state, 0o700); err != nil {
		return "", nil, err
	}
	if err := os.Chmod(state, 0o700); err != nil {
		return "", nil, err
	}
	sock = filepath.Join(runDir, "swtpm.sock")
	old := syscall.Umask(0o077)
	cmd := exec.Command(bin, "socket", "--tpm2", "--tpmstate", "dir="+state+",mode=0600",
		"--ctrl", "type=unixio,path="+sock, "--terminate")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = cmd.Start()
	syscall.Umask(old)
	if err != nil {
		return "", nil, fmt.Errorf("could not start swtpm: %w", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	stop = func() {
		select {
		case <-exited:
		case <-time.After(swtpmExit):
			cmd.Process.Kill()
			<-exited
		}
	}
	// Ready once the socket exists. Never connect to test it: with --terminate, swtpm exits when
	// its control connection closes, so a probe would end it before QEMU connects.
	for deadline := time.Now().Add(swtpmReady); ; {
		if st, err := os.Stat(sock); err == nil && st.Mode()&os.ModeSocket != 0 {
			return sock, stop, nil
		}
		select {
		case <-exited:
			return "", nil, errors.New("swtpm exited before its socket was ready")
		default:
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			<-exited
			return "", nil, errors.New("swtpm's socket was not ready in time")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
