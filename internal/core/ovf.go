package core

// OVF appliances (.ova, .ovf), as VirtualBox and other tools write them: the descriptor is read
// into a neutral appliance model (design D1 of add-ova-import-export), which import maps to a
// Config and export builds from one. Elements are matched by namespace URI, so OVF 1.0 and 2.0
// (and any prefixes) read the same (D2). The descriptor is untrusted input.

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"regexp"
	"strconv"
	"strings"
)

// OVF namespaces.
const (
	nsOVF1  = "http://schemas.dmtf.org/ovf/envelope/1"
	nsOVF2  = "http://schemas.dmtf.org/ovf/envelope/2"
	nsCIM   = "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/"
	nsRASD  = nsCIM + "CIM_ResourceAllocationSettingData"
	nsVSSD  = nsCIM + "CIM_VirtualSystemSettingData"
	nsSASD  = nsCIM + "CIM_StorageAllocationSettingData"
	nsEPASD = nsCIM + "CIM_EthernetPortAllocationSettingData"
	nsVBox  = "http://www.virtualbox.org/ovf/machine"
	nsVMW   = "http://www.vmware.com/schema/ovf"
)

// maxDescriptor bounds the descriptor read (spec: refused over 4 MiB).
const maxDescriptor = 4 << 20

// appliance is one VM of an OVF descriptor, independent of the XML.
type appliance struct {
	Name, Description string
	OSID              int    // DMTF CIM OperatingSystem id (0 when absent)
	OSType            string // VirtualBox's OS type, "" when absent
	CPUs              int
	MemoryBytes       uint64
	Firmware          string // "efi", "bios" or "" (not said)
	SecureBoot        bool
	Controllers       []ovfController
	Disks             []ovfDisk
	Opticals          int
	NICs              []ovfNIC
	Files             []ovfFile // References, in order
	Notes             []string  // what the descriptor has that kvm-gui does not keep
	FromVirtualBox    bool      // a vbox:Machine section was read
}

// ovfController is a disk controller: Kind "ide", "sata", "scsi", "nvme" or "virtio-scsi".
type ovfController struct {
	InstanceID, Kind, Sub string
}

// ovfDisk is a hard disk: its file, its format ("vmdk", "qcow2", "raw"), the capacity the
// descriptor declares in bytes, and the controller (InstanceID) it sits on.
type ovfDisk struct {
	DiskID, FileRef, Href, Format string
	Capacity, Unit                uint64 // bytes; Unit is the declared capacity's unit
	Controller                    string
	Address                       int
}

// ovfNIC is a network adapter: Attachment "nat", "bridged" or another kind, the adapter type as
// the descriptor names it, and the host interface for a bridged one.
type ovfNIC struct {
	Attachment, Adapter, HostIF string
}

// ovfFile is a file the descriptor references.
type ovfFile struct {
	ID, Href string
	Size     int64 // ovf:size, -1 when absent
}

// --- a small namespace-aware element tree -------------------------------------------------

type xnode struct {
	Name  xml.Name
	Attr  []xml.Attr
	Kids  []*xnode
	Text  string
	depth int
}

func (n *xnode) attr(space, local string) (string, bool) {
	for _, a := range n.Attr {
		if a.Name.Local == local && (a.Name.Space == space || space == "*") {
			return a.Value, true
		}
	}
	return "", false
}

// ovfAttr is an attribute in the envelope's namespace (1 or 2).
func (n *xnode) ovfAttr(local string) string {
	for _, a := range n.Attr {
		if a.Name.Local == local && (a.Name.Space == nsOVF1 || a.Name.Space == nsOVF2) {
			return a.Value
		}
	}
	return ""
}

func (n *xnode) child(space, local string) *xnode {
	for _, k := range n.Kids {
		if k.Name.Local == local && k.Name.Space == space {
			return k
		}
	}
	return nil
}

func (n *xnode) children(space, local string) []*xnode {
	var out []*xnode
	for _, k := range n.Kids {
		if k.Name.Local == local && k.Name.Space == space {
			out = append(out, k)
		}
	}
	return out
}

// text is a child element's trimmed text, "" when absent.
func (n *xnode) text(space, local string) string {
	if c := n.child(space, local); c != nil {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

func isOVF(space string) bool { return space == nsOVF1 || space == nsOVF2 }

// parseTree reads XML into a tree with bounded depth and size. Go's decoder never resolves
// external entities or DTDs.
func parseTree(b []byte) (*xnode, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	var root *xnode
	var stack []*xnode
	nodes := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the descriptor is not valid XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if nodes++; nodes > 100000 || len(stack) >= 64 {
				return nil, errors.New("the descriptor is too large or too deeply nested")
			}
			n := &xnode{Name: t.Name, Attr: append([]xml.Attr(nil), t.Attr...), depth: len(stack)}
			if len(stack) == 0 {
				if root != nil {
					return nil, errors.New("the descriptor has more than one root element")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.Kids = append(p.Kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		case xml.Directive:
			return nil, errors.New("the descriptor has a DTD or another directive: refusing it")
		}
	}
	if root == nil {
		return nil, errors.New("the descriptor is empty")
	}
	return root, nil
}

// --- reading -----------------------------------------------------------------------------

// readOVF reads an OVF 1.0 or 2.0 descriptor with exactly one VirtualSystem.
func readOVF(b []byte) (appliance, error) {
	var a appliance
	if len(b) > maxDescriptor {
		return a, errors.New("the descriptor is larger than 4 MiB")
	}
	root, err := parseTree(b)
	if err != nil {
		return a, err
	}
	if root.Name.Local != "Envelope" || !isOVF(root.Name.Space) {
		return a, errors.New("not an OVF descriptor (no OVF Envelope)")
	}
	ns := root.Name.Space
	// References
	if refs := root.child(ns, "References"); refs != nil {
		for _, f := range refs.children(ns, "File") {
			if f.ovfAttr("compression") != "" {
				return a, fmt.Errorf("%s is compressed (ovf:compression): not supported", f.ovfAttr("href"))
			}
			if f.ovfAttr("chunkSize") != "" {
				return a, fmt.Errorf("%s is split in chunks (ovf:chunkSize): not supported", f.ovfAttr("href"))
			}
			size := int64(-1)
			if s := f.ovfAttr("size"); s != "" {
				if size, err = strconv.ParseInt(s, 10, 64); err != nil || size < 0 {
					return a, fmt.Errorf("file %s has an invalid size %q", f.ovfAttr("href"), s)
				}
			}
			a.Files = append(a.Files, ovfFile{ID: f.ovfAttr("id"), Href: f.ovfAttr("href"), Size: size})
		}
	}
	if root.child(ns, "VirtualSystemCollection") != nil {
		return a, errors.New("the appliance holds several VMs (VirtualSystemCollection): import them one at a time with VirtualBox, or split it")
	}
	vs := root.children(ns, "VirtualSystem")
	if len(vs) != 1 {
		return a, fmt.Errorf("the appliance must hold exactly one VM, it holds %d", len(vs))
	}
	// Sections of the envelope and of the VM: a required section kvm-gui does not know refuses it.
	disks := map[string]ovfDisk{}
	for _, sec := range append(append([]*xnode(nil), root.Kids...), vs[0].Kids...) {
		switch {
		case isOVF(sec.Name.Space) && sec.Name.Local == "DiskSection":
			for _, d := range sec.children(ns, "Disk") {
				disk, err := readDisk(d, a.Files)
				if err != nil {
					return a, err
				}
				disks[disk.DiskID] = disk
			}
		case isOVF(sec.Name.Space) && (sec.Name.Local == "References" || sec.Name.Local == "VirtualSystem" ||
			sec.Name.Local == "NetworkSection" || sec.Name.Local == "Info" || sec.Name.Local == "Name" ||
			sec.Name.Local == "OperatingSystemSection" || sec.Name.Local == "VirtualHardwareSection" ||
			sec.Name.Local == "AnnotationSection" || sec.Name.Local == "ProductSection" ||
			sec.Name.Local == "StartupSection"):
		case isOVF(sec.Name.Space) && sec.Name.Local == "EulaSection":
			a.Notes = append(a.Notes, "the appliance has a license agreement (EulaSection): read it in the .ovf before you use the VM")
		case sec.Name.Space == nsVBox && sec.Name.Local == "Machine":
		default:
			if req, _ := sec.attr("*", "required"); req == "true" {
				return a, fmt.Errorf("the appliance requires %s, which kvm-gui does not support", sec.Name.Local)
			}
			if strings.HasSuffix(sec.Name.Local, "Section") {
				a.Notes = append(a.Notes, sec.Name.Local+" is not kept")
			}
		}
	}
	v := vs[0]
	a.Name = v.text(ns, "Name")
	if a.Name == "" {
		a.Name = v.ovfAttr("id")
	}
	if ann := v.child(ns, "AnnotationSection"); ann != nil {
		a.Description = ann.text(ns, "Annotation")
	}
	if os := v.child(ns, "OperatingSystemSection"); os != nil {
		a.OSID, _ = strconv.Atoi(os.ovfAttr("id"))
		a.OSType = os.text(nsVBox, "OSType")
	}
	hw := v.child(ns, "VirtualHardwareSection")
	if hw == nil {
		return a, errors.New("the VM has no VirtualHardwareSection")
	}
	if err := readHardware(&a, hw, disks); err != nil {
		return a, err
	}
	if m := v.child(nsVBox, "Machine"); m != nil {
		readVBoxMachine(&a, m)
	}
	return a, nil
}

// readDisk reads a DiskSection Disk: its file, format and declared capacity in bytes.
func readDisk(d *xnode, files []ovfFile) (ovfDisk, error) {
	disk := ovfDisk{DiskID: d.ovfAttr("diskId"), FileRef: d.ovfAttr("fileRef")}
	for _, f := range files {
		if f.ID == disk.FileRef {
			disk.Href = f.Href
		}
	}
	if disk.FileRef != "" && disk.Href == "" {
		return disk, fmt.Errorf("disk %s refers to file %s, which the appliance does not list", disk.DiskID, disk.FileRef)
	}
	if d.ovfAttr("parentRef") != "" {
		return disk, fmt.Errorf("disk %s is a delta of another disk (ovf:parentRef): not supported", disk.DiskID)
	}
	format := d.ovfAttr("format")
	switch {
	case strings.Contains(format, "vmdk"):
		disk.Format = "vmdk"
	case strings.Contains(format, "qcow2"):
		disk.Format = "qcow2"
	case strings.Contains(format, "raw") || strings.HasSuffix(format, "/img"):
		disk.Format = "raw"
	default:
		return disk, fmt.Errorf("disk %s is in format %q: only VMDK, qcow2 and raw are supported", disk.DiskID, format)
	}
	capacity, err := strconv.ParseUint(d.ovfAttr("capacity"), 10, 64)
	if err != nil {
		return disk, fmt.Errorf("disk %s has an invalid capacity %q", disk.DiskID, d.ovfAttr("capacity"))
	}
	unit, err := allocationUnits(d.ovfAttr("capacityAllocationUnits"))
	if err != nil {
		return disk, fmt.Errorf("disk %s: %w", disk.DiskID, err)
	}
	hi, lo := bits.Mul64(capacity, unit)
	if hi != 0 {
		return disk, fmt.Errorf("disk %s has an impossible capacity", disk.DiskID)
	}
	disk.Capacity, disk.Unit = lo, unit
	return disk, nil
}

var unitsRe = regexp.MustCompile(`^byte\s*(?:\*\s*(\d+)\s*\^\s*(\d+))?$`)

// allocationUnits is the byte multiplier of a DMTF programmatic unit such as "byte * 2^30";
// "" is bytes. A few names tools write instead are accepted too.
func allocationUnits(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "":
		return 1, nil
	case "kilobytes", "kb":
		return 1 << 10, nil
	case "megabytes", "mb":
		return 1 << 20, nil
	case "gigabytes", "gb":
		return 1 << 30, nil
	}
	m := unitsRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("unknown allocation unit %q", s)
	}
	if m[1] == "" {
		return 1, nil
	}
	base, _ := strconv.ParseFloat(m[1], 64)
	exp, _ := strconv.ParseFloat(m[2], 64)
	v := math.Pow(base, exp)
	if v < 1 || v > 1<<62 || v != math.Trunc(v) {
		return 0, fmt.Errorf("unsupported allocation unit %q", s)
	}
	return uint64(v), nil
}

// Known rasd ResourceTypes; any other required item refuses the appliance.
const (
	rtOther     = 1
	rtCPU       = 3
	rtMemory    = 4
	rtIDE       = 5
	rtSCSI      = 6
	rtEthernet  = 10
	rtFloppy    = 14
	rtCD        = 15
	rtDVD       = 16
	rtDisk      = 17
	rtStorage   = 20 // other storage: SATA (AHCI), NVMe, virtio-scsi by subtype
	rtUSB       = 23
	rtGraphics  = 24
	rtSound     = 35
	rtLogicDisk = 31
)

// readHardware reads the VM's hardware items: rasd Items, and in OVF 2 also sasd StorageItems
// and epasd EthernetPortItems, each with its own namespace for the fields.
func readHardware(a *appliance, hw *xnode, disks map[string]ovfDisk) error {
	type item struct {
		n  *xnode
		ns string
	}
	var items []item
	for _, k := range hw.Kids {
		switch {
		case isOVF(k.Name.Space) && k.Name.Local == "Item":
			items = append(items, item{k, nsRASD})
		case isOVF(k.Name.Space) && k.Name.Local == "StorageItem":
			items = append(items, item{k, nsSASD})
		case isOVF(k.Name.Space) && k.Name.Local == "EthernetPortItem":
			items = append(items, item{k, nsEPASD})
		case k.Name.Space == nsVMW && k.Name.Local == "Config":
			key, _ := k.attr(nsVMW, "key")
			val, _ := k.attr(nsVMW, "value")
			if key == "firmware" && a.Firmware == "" {
				a.Firmware = strings.ToLower(val)
			}
		}
	}
	for _, it := range items {
		t := func(local string) string { return it.n.text(it.ns, local) }
		rt, _ := strconv.Atoi(t("ResourceType"))
		required := it.n.ovfAttr("required") != "false"
		switch rt {
		case rtCPU:
			n, err := strconv.Atoi(t("VirtualQuantity"))
			if err != nil || n < 1 || n > 1024 {
				return fmt.Errorf("invalid processor count %q", t("VirtualQuantity"))
			}
			a.CPUs = n
		case rtMemory:
			q, err := strconv.ParseUint(t("VirtualQuantity"), 10, 64)
			if err != nil {
				return fmt.Errorf("invalid memory size %q", t("VirtualQuantity"))
			}
			unit, err := allocationUnits(t("AllocationUnits"))
			if t("AllocationUnits") == "" {
				unit, err = 1<<20, nil // MiB when the unit is missing, as VirtualBox reads it
			}
			if err != nil {
				return fmt.Errorf("memory: %w", err)
			}
			hi, lo := bits.Mul64(q, unit)
			if hi != 0 {
				return errors.New("impossible memory size")
			}
			a.MemoryBytes = lo
		case rtIDE, rtSCSI, rtStorage:
			c := ovfController{InstanceID: t("InstanceID"), Sub: t("ResourceSubType")}
			sub := strings.ToLower(c.Sub)
			switch {
			case rt == rtIDE:
				c.Kind = "ide"
			case rt == rtSCSI:
				c.Kind = "scsi"
			case sub == "ahci" || strings.Contains(sub, "sata"):
				c.Kind = "sata"
			case strings.Contains(sub, "nvme"):
				c.Kind = "nvme"
			case strings.Contains(sub, "virtio"):
				c.Kind = "virtio-scsi"
			default:
				c.Kind = "sata"
				a.Notes = append(a.Notes, fmt.Sprintf("storage controller %q is attached as SATA", c.Sub))
			}
			a.Controllers = append(a.Controllers, c)
		case rtDisk, rtLogicDisk:
			res := t("HostResource")
			id := res[strings.LastIndex(res, "/")+1:]
			d, ok := disks[id]
			if !ok {
				return fmt.Errorf("a disk drive refers to %q, which the DiskSection does not list", res)
			}
			d.Controller = t("Parent")
			d.Address, _ = strconv.Atoi(t("AddressOnParent"))
			a.Disks = append(a.Disks, d)
		case rtCD, rtDVD:
			a.Opticals++
		case rtEthernet:
			nic := ovfNIC{Adapter: t("ResourceSubType")}
			switch conn := strings.ToLower(t("Connection")); {
			case strings.Contains(conn, "bridged"):
				nic.Attachment = "bridged"
			case conn == "" || strings.Contains(conn, "nat"):
				nic.Attachment = "nat"
			default:
				nic.Attachment = conn
			}
			a.NICs = append(a.NICs, nic)
		case rtFloppy:
			a.Notes = append(a.Notes, "the floppy drive is not kept")
		case rtUSB, rtSound, rtGraphics, rtOther:
			// a USB controller, sound and graphics are kvm-gui's own (from the profile)
		default:
			if required {
				return fmt.Errorf("the appliance requires a device of type %d (%s), which kvm-gui does not support", rt, t("ElementName"))
			}
			a.Notes = append(a.Notes, fmt.Sprintf("device %q (type %d) is not kept", t("ElementName"), rt))
		}
	}
	// Disks the hardware does not attach are still the VM's.
	attached := map[string]bool{}
	for _, d := range a.Disks {
		attached[d.DiskID] = true
	}
	for id, d := range disks {
		if !attached[id] {
			a.Disks = append(a.Disks, d)
		}
	}
	return nil
}

// readVBoxMachine reads what only VirtualBox's own record says (design D3): firmware, Secure
// Boot, the OS type, and each enabled adapter's type, attachment and bridged interface.
func readVBoxMachine(a *appliance, m *xnode) {
	a.FromVirtualBox = true
	if t, ok := m.attr("", "OSType"); ok && a.OSType == "" {
		a.OSType = t
	}
	var walk func(n *xnode)
	var adapters []ovfNIC
	walk = func(n *xnode) {
		switch n.Name.Local {
		case "Firmware":
			if t, _ := n.attr("", "type"); strings.EqualFold(t, "EFI") || strings.HasPrefix(strings.ToUpper(t), "EFI") {
				a.Firmware = "efi"
			} else {
				a.Firmware = "bios"
			}
		case "SecureBoot":
			if e, _ := n.attr("", "enabled"); e == "true" {
				a.SecureBoot = true
			}
		case "Adapter":
			if e, _ := n.attr("", "enabled"); e != "true" {
				return
			}
			nic := ovfNIC{}
			nic.Adapter, _ = n.attr("", "type")
			for _, k := range n.Kids {
				switch k.Name.Local {
				case "NAT":
					nic.Attachment = "nat"
				case "BridgedInterface":
					nic.Attachment = "bridged"
					nic.HostIF, _ = k.attr("", "name")
				case "HostOnlyInterface":
					nic.Attachment = "host-only"
				case "InternalNetwork":
					nic.Attachment = "internal"
				case "NATNetwork":
					nic.Attachment = "nat-network"
				}
			}
			if nic.Attachment == "" {
				nic.Attachment = "nat"
			}
			adapters = append(adapters, nic)
			return
		}
		for _, k := range n.Kids {
			walk(k)
		}
	}
	walk(m)
	if len(adapters) > 0 {
		a.NICs = adapters // VirtualBox's own record has the attachments the standard items lose
	}
}
