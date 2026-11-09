package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemQEMU is the host's own QEMU: qemu-system-x86_64 on Ubuntu and Debian, /usr/libexec/qemu-kvm
// on Rocky Linux and the RHEL family, which ship no qemu-system-x86_64.
func SystemQEMU() string {
	if p, err := exec.LookPath("qemu-system-x86_64"); err == nil {
		return p
	}
	if exists("/usr/libexec/qemu-kvm") {
		return "/usr/libexec/qemu-kvm"
	}
	return "qemu-system-x86_64"
}

var (
	deviceHelpMu sync.Mutex
	deviceHelp   = map[string]string{} // QEMU binary → its -device help output
)

// checkDevices refuses a start whose arguments name a device the QEMU binary does not have (Rocky's
// qemu-kvm has no vmware-svga or virtio-9p, for example), naming the device and the setting.
func checkDevices(qemu string, args []string) error {
	deviceHelpMu.Lock()
	help, ok := deviceHelp[qemu]
	deviceHelpMu.Unlock()
	if !ok {
		out, err := exec.Command(qemu, "-device", "help").CombinedOutput()
		if err != nil {
			return nil // QEMU reports its own error when it starts
		}
		help = string(out)
		deviceHelpMu.Lock()
		deviceHelp[qemu] = help
		deviceHelpMu.Unlock()
	}
	for i, a := range args {
		name := ""
		switch {
		case a == "-device" && i+1 < len(args):
			name, _, _ = strings.Cut(args[i+1], ",")
		case a == "-virtfs":
			name = "virtio-9p-pci"
		default:
			continue
		}
		if !strings.Contains(help, `"`+name+`"`) {
			return fmt.Errorf("%s has no %s device, which the VM's %s setting needs: choose another one in Settings, or use a QEMU that has it",
				qemu, name, deviceSetting(name))
		}
	}
	return nil
}

func deviceSetting(dev string) string {
	switch {
	case dev == "virtio-vga-gl":
		return "Graphics (3D needs QEMU's OpenGL modules: qemu-system-modules-opengl on Ubuntu and Debian)"
	case dev == "vmware-svga" || dev == "VGA" || dev == "virtio-vga" || strings.HasPrefix(dev, "apple-gfx"):
		return "Graphics"
	case dev == "virtio-9p-pci":
		return "Shared Folders"
	case strings.HasPrefix(dev, "tpm"):
		return "TPM"
	case dev == "usb-host":
		return "USB"
	case strings.Contains(dev, "net") || dev == "vmxnet3" || strings.HasPrefix(dev, "e1000"):
		return "Network"
	case strings.HasPrefix(dev, "isa-applesmc"):
		return "macOS guest"
	}
	return "Storage or System"
}

// CreateGuestVM is `vm create --guest`: c (from NewGuest) is stored, its folder made and every new
// disk in it created with qemu-img; the VM is selected. A failure leaves no folder or row behind.
func CreateGuestVM(ctx context.Context, c Config) (VMEntry, error) {
	if c.MacOS() {
		return VMEntry{}, errors.New("CreateGuestVM is for Linux, Windows and Other VMs")
	}
	if c.Workspace == "" {
		c.Workspace = VMFolder(c.Name)
	}
	dir, err := c.WorkspaceDir()
	if err != nil {
		return VMEntry{}, err
	}
	if err := requireEmpty(dir); err != nil {
		return VMEntry{}, err
	}
	for _, iso := range c.Optical {
		if iso != "" && !exists(iso) {
			return VMEntry{}, &FieldError{"optical0", iso + " does not exist"}
		}
	}
	e, err := CreateVM(c)
	if err != nil {
		return e, err
	}
	created := !exists(dir)
	undo := func() {
		DeleteVM(e, false)
		if created {
			os.RemoveAll(dir)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil { // the guest's disk and TPM state are private
		undo()
		return e, err
	}
	for i, d := range e.Config.Disks {
		path, err := e.Config.DiskPath(d)
		if err != nil || exists(path) {
			continue
		}
		size := c.DiskGiB
		if i > 0 || size <= 0 {
			size = 32
		}
		if err := runStream(ctx, "", nil, "qemu-img", "create", "-f", d.Format, path, strconv.Itoa(size)+"G"); err != nil {
			undo()
			return e, fmt.Errorf("creating %s: %w", d.Path, err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			undo()
			return e, err
		}
	}
	if err := SelectVM(e.ID); err != nil {
		return e, err
	}
	return e, nil
}

// startGuest is Start for a Linux, Windows or Other VM.
func (v *VM) startGuest(c Config) error {
	dir, _ := c.WorkspaceDir()
	if !exists(dir) {
		return fmt.Errorf("VM folder %s does not exist", dir)
	}
	for _, d := range c.Disks {
		path, err := c.DiskPath(d)
		if err != nil {
			return err
		}
		if !exists(path) {
			return fmt.Errorf("disk %s does not exist: attach another one in Settings › Storage", path)
		}
	}
	for _, iso := range c.Optical {
		if iso != "" && !exists(iso) {
			return fmt.Errorf("the ISO %s does not exist: eject it or choose another one in Settings › Storage", iso)
		}
	}
	if r, ok := v.findRun(c); ok {
		return fmt.Errorf("this VM's disk is already in use by QEMU (pid %d): shut that VM down first", r.PID)
	}
	if err := checkBridges(c, "/sys/class/net", "/etc/qemu/bridge.conf", BridgeHelper()); err != nil {
		return err
	}
	if err := checkSSHPort(c); err != nil {
		return err
	}
	if err := checkUSBDevices(c, "/sys/bus/usb/devices", "/dev/bus/usb"); err != nil {
		return err
	}
	var fw Firmware
	if c.Firmware == FirmwareUEFI {
		var err error
		if fw, err = HostFirmware("/", c.SecureBoot); err != nil {
			return err
		}
	}
	w := NewWorkspace(dir)
	unlock, err := v.lockRun(w, c)
	if err != nil {
		return err
	}
	if c.Firmware == FirmwareUEFI {
		if err := ensureVars(dir, fw); err != nil {
			unlock()
			return err
		}
	}
	qmpDir, err := v.newRunDir()
	if err != nil {
		unlock()
		return err
	}
	sock := filepath.Join(qmpDir, "qmp.sock")
	tpmSock, stopTPM := "", func() {}
	if c.TPM {
		if tpmSock, stopTPM, err = startTPM(dir, qmpDir); err != nil {
			os.RemoveAll(qmpDir)
			unlock()
			return err
		}
	}
	args, err := guestArgs(c, dir, fw, sock, tpmSock)
	qemu := SystemQEMU()
	if err == nil {
		err = checkDevices(qemu, args)
	}
	if err == nil {
		err = SystemHost().check3D(c, qemu)
	}
	var env []string
	if err == nil && c.Vulkan {
		g, _ := renderGPU(c)
		env = vulkanEnv(os.Environ(), g)
	}
	if err != nil {
		stopTPMNow(stopTPM)
		os.RemoveAll(qmpDir)
		unlock()
		return err
	}
	v.log(qemu + " " + strings.Join(args, " "))
	return v.launch(launchSpec{
		qemu: qemu, args: args, dir: dir, env: env, sock: sock, qmpDir: qmpDir, vnc: c.InApp(),
		// swtpm ends when QEMU disconnects (--terminate); the VM's lock is released only after
		// it has written its state, so a snapshot or a clone never reads a half-written TPM.
		afterExit: func(time.Duration) { stopTPM() },
		unlock:    unlock,
	})
}

// stopTPMNow ends swtpm when QEMU never started: it would wait for a client forever.
func stopTPMNow(stop func()) {
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(swtpmExit + time.Second):
	}
}

// checkUSBDevices refuses a passed-through USB device that is not plugged in, or whose device node
// the user cannot open: the app never changes host permissions.
func checkUSBDevices(c Config, sysRoot, devRoot string) error {
	for _, u := range c.USBDevices {
		node, err := usbNode(sysRoot, devRoot, u)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(node, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("USB device %s:%s (%s) cannot be opened for reading and writing by this user: grant access on the host (a udev rule), the app does not change permissions", u.Vendor, u.Product, node)
		}
		f.Close()
	}
	return nil
}

// usbNode finds the /dev/bus/usb node of the first device with u's vendor and product ids.
func usbNode(sysRoot, devRoot string, u USBDevice) (string, error) {
	entries, _ := os.ReadDir(sysRoot)
	for _, e := range entries {
		base := filepath.Join(sysRoot, e.Name())
		vendor, _ := os.ReadFile(filepath.Join(base, "idVendor"))
		product, _ := os.ReadFile(filepath.Join(base, "idProduct"))
		if !strings.EqualFold(strings.TrimSpace(string(vendor)), u.Vendor) || !strings.EqualFold(strings.TrimSpace(string(product)), u.Product) {
			continue
		}
		bus, _ := os.ReadFile(filepath.Join(base, "busnum"))
		dev, _ := os.ReadFile(filepath.Join(base, "devnum"))
		b, _ := strconv.Atoi(strings.TrimSpace(string(bus)))
		d, _ := strconv.Atoi(strings.TrimSpace(string(dev)))
		return filepath.Join(devRoot, fmt.Sprintf("%03d", b), fmt.Sprintf("%03d", d)), nil
	}
	return "", fmt.Errorf("USB device %s:%s is not plugged in", u.Vendor, u.Product)
}

// GuestOptions are `vm create --guest` and MCP vm_create's choices for a Linux, Windows or Other
// VM; nil and zero fields keep the profile's defaults.
type GuestOptions struct {
	Profile, ISO, Folder, Firmware string
	DiskGiB, SSHPort               int
	SecureBoot, TPM                *bool
}

// CreateGuest creates a Linux, Windows or Other VM with the profile's defaults and the options,
// recommended CPU and RAM for this host (never below the profile's), its folder and disk.
func CreateGuest(ctx context.Context, name string, o GuestOptions) (VMEntry, error) {
	if o.ISO != "" {
		abs, err := filepath.Abs(o.ISO)
		if err != nil {
			return VMEntry{}, err
		}
		o.ISO = abs
	}
	c, err := NewGuest(o.Profile, name, o.ISO)
	if err != nil {
		return VMEntry{}, err
	}
	p, _ := ProfileByID(o.Profile)
	rec := Recommended(c, runtime.NumCPU(), SystemHost().MemTotalMiB())
	c.RAMMiB, c.VCPUs, c.Cores = max(rec.RAMMiB, p.RAMMiB), rec.VCPUs, rec.Cores
	c.Workspace, c.SSHPort = o.Folder, o.SSHPort
	if o.DiskGiB > 0 {
		c.DiskGiB = o.DiskGiB
	}
	if o.Firmware != "" {
		c.Firmware = o.Firmware
		if c.Firmware == FirmwareBIOS && o.SecureBoot == nil {
			c.SecureBoot = false
		}
	}
	if o.SecureBoot != nil {
		c.SecureBoot = *o.SecureBoot
	}
	if o.TPM != nil {
		c.TPM = *o.TPM
	}
	return CreateGuestVM(ctx, c)
}
