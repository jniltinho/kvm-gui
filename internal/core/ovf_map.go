package core

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// osEntry is a row of the OS table (design D5): a kvm-gui profile, the VirtualBox OS type and the
// DMTF CIM OperatingSystem id written on export, and the VirtualBox type prefixes read on import.
type osEntry struct {
	Profile, OSType string
	CIM             int
	Aliases         []string // VirtualBox OS type prefixes, lower case
}

// osTable: the CIM ids follow DMTF's CIM_OperatingSystem.OSType values (Ubuntu 64-Bit 94,
// Debian 64-Bit 96, RedHat Enterprise Linux 64-Bit 80, Linux 64-Bit 101, Microsoft Windows 10
// 64-bit 120, Microsoft Windows 11 64-bit 122, Other 64-Bit 102); task 3.0 checks which ones
// VirtualBox 7.2 maps back to the same OS type.
var osTable = []osEntry{
	{"ubuntu", "Ubuntu_64", 94, []string{"ubuntu", "kubuntu", "lubuntu", "xubuntu"}},
	{"debian", "Debian_64", 96, []string{"debian"}},
	{"fedora", "Fedora_64", 101, []string{"fedora"}},
	{"rocky", "RedHat_64", 80, []string{"redhat", "oracle", "rocky", "alma", "centos"}},
	{"arch", "ArchLinux_64", 101, []string{"archlinux"}},
	{"opensuse", "OpenSUSE_64", 101, []string{"opensuse", "suse"}},
	{"windows11", "Windows11_64", 122, []string{"windows11", "windows2022", "windows2025"}},
	{"windows10", "Windows10_64", 120, []string{"windows10", "windows2016", "windows2019"}},
	{"linux", "Linux_64", 101, []string{"linux", "gentoo", "mandriva", "turbolinux", "xandros"}},
	{"other", "Other_64", 102, []string{"other"}},
}

// osForProfile is the export row of a profile ("other" for an unknown one).
func osForProfile(profile string) osEntry {
	for _, e := range osTable {
		if e.Profile == profile {
			return e
		}
	}
	return osTable[len(osTable)-1]
}

// CIM OperatingSystem ids by family, for appliances without VirtualBox's OS type.
var (
	cimMacOS   = []int{2}
	cimLinux   = []int{36, 79, 80, 82, 83, 84, 85, 87, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99, 100, 101, 106, 107, 108, 109}
	cimWindows = []int{16, 17, 18, 19, 58, 63, 67, 69, 70, 71, 72, 73, 74, 75, 76, 77, 103, 105, 111, 112, 113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123}
)

// profileForAppliance picks the kvm-gui profile of an appliance's OS (D5): VirtualBox's OS type by
// its bounded aliases, else the CIM id (exact Ubuntu, Debian and RHEL ids, else its family),
// else Other. note says what was assumed; macOS and ARM appliances are refused.
func profileForAppliance(a appliance) (profile, note string, err error) {
	t := strings.ToLower(a.OSType)
	if strings.Contains(t, "arm") || strings.Contains(t, "aarch64") {
		return "", "", fmt.Errorf("the appliance's OS is %s, an ARM one: kvm-gui runs x86 guests", a.OSType)
	}
	if strings.HasPrefix(t, "macos") || strings.HasPrefix(t, "mac os") || slices.Contains(cimMacOS, a.OSID) {
		return "", "", errors.New("the appliance is a macOS VM: macOS VMs move between kvm-gui hosts as kvm-gui bundles, not as appliances")
	}
	if t != "" {
		for _, e := range osTable {
			for _, p := range e.Aliases {
				if strings.HasPrefix(t, p) {
					return e.Profile, "", nil
				}
			}
		}
		if strings.HasPrefix(t, "windows") || strings.HasPrefix(t, "win") {
			return "windows10", fmt.Sprintf("OS type %s has no profile of its own: imported with the Windows 10 one", a.OSType), nil
		}
	}
	switch {
	case a.OSID == 93 || a.OSID == 94:
		return "ubuntu", "", nil
	case a.OSID == 95 || a.OSID == 96:
		return "debian", "", nil
	case a.OSID == 79 || a.OSID == 80 || a.OSID == 106 || a.OSID == 107 || a.OSID == 108 || a.OSID == 109:
		return "rocky", "", nil
	case a.OSID == 122 || a.OSID == 123:
		return "windows11", "", nil
	case slices.Contains(cimLinux, a.OSID):
		return "linux", note2(a, "generic Linux"), nil
	case slices.Contains(cimWindows, a.OSID):
		return "windows10", note2(a, "Windows 10"), nil
	}
	return "other", note2(a, "Other"), nil
}

func note2(a appliance, as string) string {
	what := a.OSType
	if what == "" {
		what = fmt.Sprintf("CIM OS id %d", a.OSID)
	}
	return fmt.Sprintf("OS %s has no profile of its own: imported as %s", what, as)
}

// ImportOptions are the user's choices for an import.
type ImportOptions struct {
	Name   string // "" keeps the appliance's name
	Folder string // "" is the workspace's default folder for the name
	Bridge string // a host bridge for the appliance's bridged adapters; "" makes them NAT
}

// plannedDisk is one disk the import converts: the appliance's file and the VM's disk.
type plannedDisk struct {
	Href, Format string
	Capacity     uint64
	Path         string // in the VM folder, e.g. disk0.qcow2
}

// importPlan is the VM an import would create, its disks and the notes for the user.
type importPlan struct {
	Config Config
	Disks  []plannedDisk
	Notes  []string
}

// planImport maps an appliance to a complete Config (design D1): profile, memory and processors
// within the host's, firmware, controllers, optical drives and adapters, with a note for each
// thing that is not kept. hostMiB and hostThreads cap memory and processors (0: no cap).
func planImport(a appliance, o ImportOptions, hostMiB, hostThreads int) (importPlan, error) {
	var p importPlan
	profile, note, err := profileForAppliance(a)
	if err != nil {
		return p, err
	}
	name := strings.TrimSpace(o.Name)
	if name == "" {
		name = strings.TrimSpace(a.Name)
	}
	if name == "" {
		name = "Imported VM"
	}
	c, err := NewGuest(profile, name, "")
	if err != nil {
		return p, err
	}
	notes := append([]string(nil), a.Notes...)
	if note != "" {
		notes = append(notes, note)
	}
	prof, _ := ProfileByID(profile)
	c.Description = a.Description
	c.Workspace = o.Folder

	// Memory and processors, within the host's.
	if a.MemoryBytes > 0 {
		c.RAMMiB = int((a.MemoryBytes + 1<<20 - 1) >> 20)
	}
	if c.RAMMiB < prof.MinRAMMiB {
		notes = append(notes, fmt.Sprintf("memory raised from %d MiB to %s's minimum of %d MiB", c.RAMMiB, prof.Name, prof.MinRAMMiB))
		c.RAMMiB = prof.MinRAMMiB
	}
	if hostMiB > 0 && c.RAMMiB > hostMiB {
		notes = append(notes, fmt.Sprintf("memory lowered from %d MiB to this host's %d MiB", c.RAMMiB, hostMiB))
		c.RAMMiB = hostMiB
	}
	if a.CPUs > 0 {
		c.VCPUs = a.CPUs
	}
	if hostThreads > 0 && c.VCPUs > hostThreads {
		notes = append(notes, fmt.Sprintf("processors lowered from %d to this host's %d", c.VCPUs, hostThreads))
		c.VCPUs = hostThreads
	}
	c.Cores = c.VCPUs // one thread per core: the topology any guest accepts

	// Firmware (D3): VirtualBox's record or VMware's key, else BIOS, the OVF default.
	switch a.Firmware {
	case "efi":
		c.Firmware = FirmwareUEFI
		c.SecureBoot = a.SecureBoot
	case "bios":
		c.Firmware, c.SecureBoot = FirmwareBIOS, false
	default:
		c.Firmware, c.SecureBoot = FirmwareBIOS, false
		notes = append(notes, "the appliance does not say its firmware: BIOS assumed (switch to UEFI in Settings › System if the guest was installed with it)")
	}
	if c.Firmware == FirmwareUEFI {
		notes = append(notes, "UEFI boot entries are not imported: the guest starts from its fallback loader (EFI/BOOT/BOOTX64.EFI)")
	}
	if c.TPM {
		notes = append(notes, "the TPM starts empty: secrets sealed to the old one (a BitLocker key) need their recovery key")
	}

	// Disks: each converted to qcow2 on the controller the appliance uses when kvm-gui has it.
	ctrl := map[string]string{}
	for _, k := range a.Controllers {
		ctrl[k.InstanceID] = k.Kind
	}
	c.Disks = nil
	for i, d := range a.Disks {
		bus := prof.DiskBus
		switch ctrl[d.Controller] {
		case "sata":
			bus = BusSATA
		case "nvme":
			bus = BusNVMe
		case "virtio-scsi": // kvm-gui has no virtio-scsi controller: virtio-blk instead
			bus = BusVirtio
			notes = append(notes, fmt.Sprintf("disk %d was on a virtio-scsi controller: attached as virtio-blk (the guest needs its virtio_blk driver at boot)", i+1))
		case "ide", "scsi":
			bus = BusSATA
			notes = append(notes, fmt.Sprintf("disk %d was on an %s controller: attached as SATA (the guest needs its AHCI driver at boot)", i+1, strings.ToUpper(ctrl[d.Controller])))
		}
		path := fmt.Sprintf("disk%d.qcow2", i)
		c.Disks = append(c.Disks, Disk{Path: path, Format: "qcow2", Bus: bus})
		p.Disks = append(p.Disks, plannedDisk{Href: d.Href, Format: d.Format, Capacity: d.Capacity, Path: path})
	}
	c.DiskGiB = 0
	c.Optical = make([]string, a.Opticals)

	// Adapters (bridged → NAT unless a host bridge was chosen).
	c.Networks = nil
	for i, n := range a.NICs {
		nic := NIC{Enabled: true, Mode: NetNAT, Model: guestNICModel(n.Adapter, prof.NIC), MAC: RandomMAC()}
		switch n.Attachment {
		case "nat":
		case "bridged":
			if o.Bridge != "" {
				nic.Mode, nic.Bridge = NetBridged, o.Bridge
			} else {
				where := ""
				if n.HostIF != "" {
					where = " to " + n.HostIF
				}
				notes = append(notes, fmt.Sprintf("adapter %d was bridged%s: imported as NAT (pick a bridge of this host to keep it bridged)", i+1, where))
			}
		default:
			notes = append(notes, fmt.Sprintf("adapter %d was attached to %s: imported as NAT", i+1, n.Attachment))
		}
		if nic.Model != guestNICModel(n.Adapter, "") && n.Adapter != "" {
			notes = append(notes, fmt.Sprintf("adapter %d's type %s has no equivalent: %s", i+1, n.Adapter, nic.Model))
		}
		c.Networks = append(c.Networks, nic)
	}
	if len(c.Networks) > 4 {
		notes = append(notes, fmt.Sprintf("%d adapters: the first 4 are kept", len(c.Networks)))
		c.Networks = c.Networks[:4]
	}
	p.Config, p.Notes = c, notes
	return p, nil
}

// guestNICModel maps an OVF or VirtualBox adapter type to a guest NIC model; def when unknown.
func guestNICModel(adapter, def string) string {
	a := strings.ToLower(adapter)
	switch {
	case strings.Contains(a, "virtio"):
		return "virtio-net-pci"
	case a == "e1000e" || a == "82574l":
		return "e1000e"
	case a == "e1000" || strings.HasPrefix(a, "8254") || a == "82543gc":
		return "e1000"
	}
	return def
}

// validatePlan checks the planned VM as a save would; its SSH port is picked when it is created,
// so any valid one stands in (as CreateVM does).
func validatePlan(c Config) error {
	if c.SSHPort == 0 {
		c.SSHPort = FirstSSHPort
	}
	return c.Validate()
}
