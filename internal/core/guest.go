package core

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Guest types. A VM with an empty Guest is a macOS VM (every VM saved before guest types existed).
const (
	GuestMacOS   = "macos"
	GuestLinux   = "linux"
	GuestWindows = "windows"
	GuestOther   = "other"
)

// Firmware for Linux, Windows and Other VMs (macOS always boots OVMF with OpenCore).
const (
	FirmwareUEFI = "uefi"
	FirmwareBIOS = "bios"
)

// Graphics for Linux, Windows and Other VMs.
const (
	GraphicsVirtio = "virtio-vga"
	GraphicsVGA    = "vga"
)

// Disk buses for Linux, Windows and Other VMs.
const (
	BusVirtio = "virtio"
	BusSATA   = "sata"
	BusNVMe   = "nvme"
)

// Disk is a hard disk image of a Linux, Windows or Other VM. A relative Path is inside the VM's
// folder (the VM owns it); an absolute one may be anywhere (the VM only uses it).
type Disk struct {
	Path   string `json:"path"`
	Format string `json:"format"` // qcow2 or raw
	Bus    string `json:"bus"`    // BusVirtio, BusSATA or BusNVMe
}

// SharedFolder is a host folder shared with a Linux guest over 9p, mounted by its tag.
type SharedFolder struct {
	Path     string `json:"path"`
	Tag      string `json:"tag"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// USBDevice is a host USB device passed through, by vendor and product id (hex, 4 digits).
type USBDevice struct {
	Vendor  string `json:"vendor"`
	Product string `json:"product"`
}

// Serial port modes.
const (
	SerialFile   = "file"   // <VM folder>/serial.log
	SerialSocket = "socket" // a Unix socket in the run's private directory
)

// OSProfile holds a guest's device defaults, modeled on virt-manager/libosinfo's.
type OSProfile struct {
	ID, Name, Guest   string
	Firmware          string
	SecureBoot, TPM   bool
	DiskBus, NIC      string
	Graphics          string
	HyperV, RNG       bool
	ISOLabels         []*regexp.Regexp // volume labels of the system's install media
	DefaultName       string
	MinRAMMiB, RAMMiB int
	DiskGiB           int
}

func labels(res ...string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, r := range res {
		out = append(out, regexp.MustCompile(`(?i)`+r))
	}
	return out
}

func linuxProfile(id, name string, iso ...string) OSProfile {
	return OSProfile{ID: id, Name: name, Guest: GuestLinux, Firmware: FirmwareUEFI, DiskBus: BusVirtio,
		NIC: "virtio-net-pci", Graphics: GraphicsVirtio, RNG: true, ISOLabels: labels(iso...), DefaultName: name,
		MinRAMMiB: 1024, RAMMiB: 4096, DiskGiB: 40}
}

// Profiles are the OS profiles of Linux, Windows and Other VMs.
var Profiles = []OSProfile{
	linuxProfile("ubuntu", "Ubuntu", `ubuntu`), // Ubuntu, Ubuntu-Server, Kubuntu, Lubuntu
	linuxProfile("debian", "Debian", `^Debian`, `^d-live`),
	linuxProfile("fedora", "Fedora", `^Fedora`),
	linuxProfile("rocky", "Rocky Linux", `^Rocky`, `^RHEL`, `^AlmaLinux`),
	linuxProfile("arch", "Arch Linux", `^ARCH_`),
	linuxProfile("opensuse", "openSUSE", `^openSUSE`),
	linuxProfile("linux", "Linux"),
	// Windows 10 and 11 media share the CCCOMA_X64FRE label: an ISO with it gets Windows 11, and the
	// user picks Windows 10 when it is that. Windows Server media (SSS_) get the Windows 10 profile.
	{ID: "windows10", Name: "Windows 10", Guest: GuestWindows, Firmware: FirmwareUEFI, DiskBus: BusSATA, NIC: "e1000e",
		Graphics: GraphicsVGA, HyperV: true, ISOLabels: labels(`^SSS_X64FRE`), DefaultName: "Windows 10",
		MinRAMMiB: 2048, RAMMiB: 4096, DiskGiB: 64},
	{ID: "windows11", Name: "Windows 11", Guest: GuestWindows, Firmware: FirmwareUEFI, SecureBoot: true, TPM: true,
		DiskBus: BusSATA, NIC: "e1000e", Graphics: GraphicsVGA, HyperV: true,
		ISOLabels: labels(`^CCCOMA_X64FRE`, `^CPBA_X64FRE`, `^CCSA_X64FRE`), DefaultName: "Windows 11",
		MinRAMMiB: 4096, RAMMiB: 8192, DiskGiB: 80},
	{ID: "other", Name: "Other", Guest: GuestOther, Firmware: FirmwareBIOS, DiskBus: BusSATA, NIC: "e1000",
		Graphics: GraphicsVGA, DefaultName: "Other", MinRAMMiB: 256, RAMMiB: 2048, DiskGiB: 32},
}

// ProfileByID returns the OS profile id.
func ProfileByID(id string) (OSProfile, bool) {
	for _, p := range Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return OSProfile{}, false
}

// GuestNICModels are the adapter types offered to Linux, Windows and Other VMs.
var GuestNICModels = []string{"virtio-net-pci", "e1000e", "e1000"}

// MacOS reports whether the VM is a macOS VM (the OpenCore path, or reims-ctl).
func (c Config) MacOS() bool { return c.Guest == "" || c.Guest == GuestMacOS }

// NICModelsFor are the adapter types the VM's guest type accepts.
func (c Config) NICModelsFor() []string {
	if c.MacOS() {
		return NICModels
	}
	return GuestNICModels
}

// NewGuest is a new Linux, Windows or Other VM with the profile's defaults, an ISO (optional) and
// one new disk of the profile's size in the VM folder (created by CreateGuestDisks).
func NewGuest(profile, name, iso string) (Config, error) {
	p, ok := ProfileByID(profile)
	if !ok {
		return Config{}, fmt.Errorf("unknown OS profile %q (expected one of %s)", profile, strings.Join(ProfileIDs(), ", "))
	}
	c := Defaults()
	c.Guest, c.Profile, c.Version = p.Guest, p.ID, ""
	c.Name = name
	if c.Name == "" {
		c.Name = p.DefaultName
	}
	c.Firmware, c.SecureBoot, c.TPM, c.Graphics = p.Firmware, p.SecureBoot, p.TPM, p.Graphics
	c.RAMMiB, c.Cores, c.VCPUs = p.RAMMiB, 2, 2
	c.Disks = []Disk{{Path: "disk0.qcow2", Format: "qcow2", Bus: p.DiskBus}}
	c.DiskGiB = p.DiskGiB
	if iso != "" {
		c.Optical = []string{iso}
	}
	c.Networks = []NIC{{Enabled: true, Mode: NetNAT, Model: p.NIC, MAC: RandomMAC()}}
	return c, nil
}

// ProfileIDs lists the profile ids.
func ProfileIDs() []string {
	var ids []string
	for _, p := range Profiles {
		ids = append(ids, p.ID)
	}
	return ids
}

var (
	usbIDRe = regexp.MustCompile(`^[0-9a-fA-F]{4}$`)
	tagRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,31}$`)
)

// validateGuest checks a Linux, Windows or Other VM.
func (c Config) validateGuest() error {
	p, ok := ProfileByID(c.Profile)
	if !ok || p.Guest != c.Guest {
		return &FieldError{"profile", fmt.Sprintf("unknown %s profile %q", c.Guest, c.Profile)}
	}
	if c.RAMMiB < p.MinRAMMiB {
		return &FieldError{"ram", fmt.Sprintf("%s needs at least %d MiB", p.Name, p.MinRAMMiB)}
	}
	if c.Cores < 1 || c.VCPUs < 1 || c.VCPUs%c.Cores != 0 {
		return &FieldError{"vcpus", "must be a multiple of the number of cores"}
	}
	switch {
	case c.Reims():
		return &FieldError{"graphics", "Reims vGPU is for macOS VMs"}
	case c.GPU != "":
		return &FieldError{"gpu", "GPU passthrough is for macOS VMs"}
	case c.RootPatch:
		return &FieldError{"root_patch", "root-patch mode is for macOS VMs"}
	}
	if c.Graphics != GraphicsVirtio && c.Graphics != GraphicsVGA {
		return &FieldError{"graphics", "expected " + GraphicsVirtio + " or " + GraphicsVGA}
	}
	if c.Firmware != FirmwareUEFI && c.Firmware != FirmwareBIOS {
		return &FieldError{"firmware", "expected uefi or bios"}
	}
	if c.SecureBoot && c.Firmware != FirmwareUEFI {
		return &FieldError{"secure_boot", "Secure Boot needs UEFI firmware"}
	}
	for i, d := range c.Disks {
		f := fmt.Sprintf("disk%d", i)
		if d.Path == "" || (d.Format != "qcow2" && d.Format != "raw") {
			return &FieldError{f, "needs a path and a format (qcow2 or raw)"}
		}
		if !filepath.IsAbs(d.Path) && (filepath.Base(d.Path) != d.Path || !filepath.IsLocal(d.Path)) {
			// A subfolder could be a link to elsewhere, and delete --purge would then remove that image.
			return &FieldError{f, "a relative disk path is a file name in the VM folder: use an absolute path for an image elsewhere"}
		}
		if d.Bus != BusVirtio && d.Bus != BusSATA && d.Bus != BusNVMe {
			return &FieldError{f, "bus must be virtio, sata or nvme"}
		}
	}
	if n := c.sataPorts(); n > 6 {
		return &FieldError{"disks", fmt.Sprintf("%d SATA devices: the controller has 6 ports", n)}
	}
	for i, iso := range c.Optical {
		if iso != "" && !filepath.IsAbs(iso) {
			return &FieldError{fmt.Sprintf("optical%d", i), "use an absolute path to the ISO"}
		}
	}
	if c.BootOrder != "" && c.BootOrder != "cdrom" && c.BootOrder != "disk" {
		return &FieldError{"boot_order", "expected cdrom or disk"}
	}
	if c.Serial != "" && c.Serial != SerialFile && c.Serial != SerialSocket {
		return &FieldError{"serial", "expected file or socket"}
	}
	if len(c.SharedFolders) > 0 && c.Guest != GuestLinux {
		return &FieldError{"shared_folders", "shared folders are for Linux VMs"}
	}
	tags := map[string]bool{}
	for _, s := range c.SharedFolders {
		if !filepath.IsAbs(s.Path) || !tagRe.MatchString(s.Tag) || tags[s.Tag] {
			return &FieldError{"shared_folders", "each share needs an absolute folder and a unique tag of letters, digits, . _ -"}
		}
		tags[s.Tag] = true
	}
	for _, u := range c.USBDevices {
		if !usbIDRe.MatchString(u.Vendor) || !usbIDRe.MatchString(u.Product) {
			return &FieldError{"usb", "vendor and product are 4 hex digits, as lsusb shows them"}
		}
	}
	if c.SSHPort < 1024 || c.SSHPort > 65535 {
		return &FieldError{"ssh_port", "use a port between 1024 and 65535"}
	}
	return validateNICs(c.Adapters(), GuestNICModels)
}

// sataPorts counts the devices on the SATA controller: SATA disks and every optical drive.
func (c Config) sataPorts() int {
	n := len(c.Optical)
	for _, d := range c.Disks {
		if d.Bus == BusSATA {
			n++
		}
	}
	return n
}

// DiskPath resolves a disk's path: relative paths are inside the VM folder.
func (c Config) DiskPath(d Disk) (string, error) {
	if filepath.IsAbs(d.Path) {
		return filepath.Clean(d.Path), nil
	}
	dir, err := c.WorkspaceDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, d.Path), nil
}

// GuestFiles in a Linux, Windows or Other VM's folder.
const (
	GuestVarsFile  = "OVMF_VARS.fd"   // the VM's UEFI variables, made once from the host's template
	GuestVarsKind  = ".vars-template" // which template the variables came from: "secure" or "plain"
	GuestTPMDir    = "tpm"            // swtpm's state (0700, files 0600)
	GuestSerialLog = "serial.log"
)

// guestArgs are a Linux, Windows or Other VM's QEMU arguments. fw is the host firmware (zero for
// BIOS); tpmSock is swtpm's socket ("" without a TPM).
func guestArgs(c Config, dir string, fw Firmware, qmpSocket, tpmSock string) ([]string, error) {
	p, _ := ProfileByID(c.Profile)
	vmPath := func(name string) string { return qemuPath(filepath.Join(dir, name)) }
	machine := "q35"
	if c.SecureBoot {
		machine += ",smm=on"
	}
	cpu := "host"
	if p.HyperV {
		cpu += ",hv_relaxed,hv_vapic,hv_spinlocks=0x1fff,hv_time"
	}
	name := c.Name
	if name == "" {
		name = p.Name
	}
	args := []string{
		"-name", "guest=" + qemuPath(name), // guest=: a name like "process=x" stays a name
		"-enable-kvm", "-machine", machine, "-cpu", cpu, "-m", strconv.Itoa(c.RAMMiB),
		"-smp", fmt.Sprintf("%d,sockets=1,cores=%d,threads=%d", c.VCPUs, c.Cores, c.VCPUs/c.Cores),
		"-device", "qemu-xhci,id=xhci",
		"-device", "usb-kbd,bus=xhci.0", "-device", "usb-tablet,bus=xhci.0",
	}
	if c.Firmware == FirmwareUEFI {
		if c.SecureBoot {
			args = append(args, "-global", "driver=cfi.pflash01,property=secure,value=on")
		}
		args = append(args,
			"-drive", "if=pflash,format=raw,readonly=on,file="+qemuPath(fw.Code),
			"-drive", "if=pflash,format=raw,file="+vmPath(GuestVarsFile))
	}
	cdFirst := c.BootOrder == "cdrom" || (c.BootOrder == "" && slices.ContainsFunc(c.Optical, func(s string) bool { return s != "" }))
	boot := 0
	bootIndex := func() string { boot++; return ",bootindex=" + strconv.Itoa(boot-1) }
	args = append(args, "-device", "ich9-ahci,id=sata")
	port := 0
	cds := func() {
		for i, iso := range c.Optical {
			// format=raw: an untrusted "ISO" is never probed as qcow2, whose backing file could
			// name a host file.
			drive := fmt.Sprintf("id=cd%d,if=none,media=cdrom,readonly=on,format=raw", i)
			if iso != "" {
				drive += ",file=" + qemuPath(iso)
			}
			args = append(args, "-drive", drive, "-device", fmt.Sprintf("ide-cd,bus=sata.%d,drive=cd%d", port, i)+bootIndex())
			port++
		}
	}
	disks := func() error {
		for i, d := range c.Disks {
			path, err := c.DiskPath(d)
			if err != nil {
				return err
			}
			args = append(args, "-drive", fmt.Sprintf("id=disk%d,if=none,format=%s,file=%s", i, d.Format, qemuPath(path)))
			switch d.Bus {
			case BusVirtio:
				args = append(args, "-device", fmt.Sprintf("virtio-blk-pci,drive=disk%d", i)+bootIndex())
			case BusNVMe:
				args = append(args, "-device", fmt.Sprintf("nvme,serial=disk%d,drive=disk%d", i, i)+bootIndex())
			default:
				args = append(args, "-device", fmt.Sprintf("ide-hd,bus=sata.%d,drive=disk%d", port, i)+bootIndex())
				port++
			}
		}
		return nil
	}
	if cdFirst {
		cds()
		if err := disks(); err != nil {
			return nil, err
		}
	} else {
		if err := disks(); err != nil {
			return nil, err
		}
		cds()
	}
	firstNIC, moreNICs := netArgs(c, BridgeHelper())
	args = append(args, firstNIC...)
	if c.Graphics == GraphicsVirtio {
		args = append(args, "-device", "virtio-vga")
	} else {
		args = append(args, "-device", "VGA")
	}
	args = append(args, "-device", "ich9-intel-hda", "-device", "hda-duplex")
	if p.RNG {
		args = append(args, "-device", "virtio-rng-pci")
	}
	if tpmSock != "" {
		args = append(args, "-chardev", "socket,id=chrtpm,path="+qemuPath(tpmSock),
			"-tpmdev", "emulator,id=tpm0,chardev=chrtpm", "-device", "tpm-crb,tpmdev=tpm0")
	}
	switch c.Serial {
	case SerialFile:
		args = append(args, "-serial", "file:"+vmPath(GuestSerialLog))
	case SerialSocket:
		args = append(args, "-serial", "unix:"+qemuPath(filepath.Join(filepath.Dir(qmpSocket), "serial.sock"))+",server=on,wait=off")
	default:
		args = append(args, "-serial", "none")
	}
	for i, s := range c.SharedFolders {
		fs := fmt.Sprintf("local,id=fs%d,path=%s,mount_tag=%s,security_model=mapped-xattr", i, qemuPath(s.Path), s.Tag)
		if s.ReadOnly {
			fs += ",readonly=on"
		}
		args = append(args, "-virtfs", fs)
	}
	for _, u := range c.USBDevices {
		args = append(args, "-device", "usb-host,bus=xhci.0,vendorid=0x"+strings.ToLower(u.Vendor)+",productid=0x"+strings.ToLower(u.Product))
	}
	args = append(args, moreNICs...)
	return append(args, runArgs(c, qmpSocket)...), nil
}
