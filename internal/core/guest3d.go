package core

// 3D acceleration of Linux and Other guests on a host GPU, without passthrough (OpenSpec
// ubuntu-26-04-and-vulkan, design D3-D6): OpenGL through virgl, and Vulkan through Venus, drawn on
// the GPU the VM names by PCI address or on the automatic choice.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// RenderGPU is a host GPU with a render node, which can draw a 3D guest.
type RenderGPU struct {
	Addr   string `json:"addr"`   // PCI address, 0000:0c:00.0
	ID     string `json:"id"`     // vendor:device, 1002:73df
	Name   string `json:"name"`   // as lspci names it
	Driver string `json:"driver"` // kernel driver: amdgpu, nvidia...
	Node   string `json:"node"`   // /dev/dri/renderD128
	Vulkan bool   `json:"vulkan"` // a Vulkan driver (ICD) for it is installed
	Twin   bool   `json:"twin"`   // another GPU has the same vendor:device: Vulkan cannot tell them apart
}

// mesaDrivers draw virgl best: the automatic choice takes the first GPU driven by one.
var mesaDrivers = []string{"amdgpu", "radeon", "i915", "xe", "nouveau"}

// vulkanICDs are the ICD manifest names (prefixes) of each kernel driver's Vulkan driver.
var vulkanICDs = map[string][]string{
	"amdgpu": {"radeon_icd"}, "radeon": {"radeon_icd"},
	"i915": {"intel_icd", "intel_hasvk_icd"}, "xe": {"intel_icd"},
	"nouveau": {"nouveau_icd"}, "nvidia": {"nvidia_icd"},
}

func (h Host) devDRI() string {
	if h.DevDRI != "" {
		return h.DevDRI
	}
	return "/dev/dri"
}

func (h Host) etcRoot() string {
	if h.EtcRoot != "" {
		return h.EtcRoot
	}
	return "/etc"
}

// RenderGPUs lists the host's GPUs that have a render node (a GPU bound to vfio-pci for passthrough
// has none), by render node.
func (h Host) RenderGPUs() []RenderGPU {
	nodes, _ := filepath.Glob(filepath.Join(h.SysRoot, "class/drm/renderD*"))
	slices.Sort(nodes)
	var out []RenderGPU
	for _, n := range nodes {
		dev, err := filepath.EvalSymlinks(filepath.Join(n, "device"))
		if err != nil {
			continue
		}
		addr := filepath.Base(dev)
		if !pciAddrRe.MatchString(addr) {
			continue
		}
		f := h.pciFunc(addr)
		g := RenderGPU{Addr: addr, ID: f.ID, Driver: f.Driver, Node: filepath.Join(h.devDRI(), filepath.Base(n))}
		g.Name = h.pciName(addr, f.ID)
		g.Vulkan = h.hasICD(f.Driver)
		out = append(out, g)
	}
	for i := range out {
		for j := range out {
			if i != j && out[i].ID == out[j].ID {
				out[i].Twin = true
			}
		}
	}
	return out
}

// hasICD reports whether a Vulkan ICD manifest for a kernel driver is installed.
func (h Host) hasICD(driver string) bool {
	for _, dir := range []string{filepath.Join(h.ShareRoot, "vulkan/icd.d"), filepath.Join(h.etcRoot(), "vulkan/icd.d")} {
		for _, prefix := range vulkanICDs[driver] {
			if m, _ := filepath.Glob(filepath.Join(dir, prefix+"*.json")); len(m) > 0 {
				return true
			}
		}
	}
	return false
}

// AutoRenderGPU is the automatic choice: the first GPU with a Mesa driver, which virglrenderer is
// tested on most, else the first one.
func AutoRenderGPU(gpus []RenderGPU) (RenderGPU, bool) {
	for _, g := range gpus {
		if slices.Contains(mesaDrivers, g.Driver) {
			return g, true
		}
	}
	if len(gpus) > 0 {
		return gpus[0], true
	}
	return RenderGPU{}, false
}

// RenderGPUFor is the GPU a 3D VM draws on: the one it names, or the automatic choice.
func (h Host) RenderGPUFor(c Config) (RenderGPU, error) {
	gpus := h.RenderGPUs()
	if c.RenderGPU == "" {
		if g, ok := AutoRenderGPU(gpus); ok {
			return g, nil
		}
		return RenderGPU{}, errors.New("3D acceleration needs a GPU on the host and there is none (/dev/dri/renderD*): turn 3D acceleration off in Settings › Display")
	}
	for _, g := range gpus {
		if g.Addr == c.RenderGPU {
			return g, nil
		}
	}
	return RenderGPU{}, fmt.Errorf("the host GPU this VM draws on (%s) is not here, or has no render node (reserved for passthrough?): choose another one in Settings › Display", c.RenderGPU)
}

// renderGPU is RenderGPUFor on this host (tests replace it).
var renderGPU = func(c Config) (RenderGPU, error) { return SystemHost().RenderGPUFor(c) }

// Guest3D says whether this host can run 3D (OpenGL) and Vulkan guests, and why not.
type Guest3D struct {
	Can3D         bool     `json:"can3D"`
	Reason3D      string   `json:"reason3D,omitempty"`
	CanVulkan     bool     `json:"canVulkan"`
	VulkanReasons []string `json:"vulkanReasons,omitempty"`
}

var (
	gl3DHelpMu sync.Mutex
	gl3DHelp   = map[string]string{} // QEMU binary → its virtio-vga-gl properties ("" = no such device)
)

// Guest3DSupport probes the host (design D6): QEMU's 3D device and its Venus property,
// virglrenderer's render server, which QEMU requests with Venus, and a Vulkan loader with the
// device filters that limit a guest to its GPU. It is static: a renderer that still fails at
// start shows QEMU's own error.
func (h Host) Guest3DSupport(qemu string) Guest3D {
	gl3DHelpMu.Lock()
	help, ok := gl3DHelp[qemu]
	gl3DHelpMu.Unlock()
	if !ok {
		out, err := h.Output(qemu, "-device", "virtio-vga-gl,help")
		if err == nil && bytes.Contains(out, []byte("blob")) {
			help = string(out)
		}
		gl3DHelpMu.Lock()
		gl3DHelp[qemu] = help
		gl3DHelpMu.Unlock()
	}
	var s Guest3D
	if help == "" {
		s.Reason3D = "this host's QEMU has no 3D graphics device (virtio-vga-gl); on Ubuntu and Debian it comes with qemu-system-modules-opengl"
		s.VulkanReasons = []string{"Vulkan needs 3D acceleration"}
		return s
	}
	s.Can3D = true
	if !strings.Contains(help, "venus=") {
		s.VulkanReasons = append(s.VulkanReasons, "this host's QEMU has no Venus support")
	}
	if !h.hasRenderServer() {
		s.VulkanReasons = append(s.VulkanReasons, "virglrenderer's render server (virgl_render_server) is not installed")
	}
	if !h.loaderHasFilters() {
		s.VulkanReasons = append(s.VulkanReasons, "the Vulkan loader (libvulkan1) is too old to limit a guest to one GPU")
	}
	s.CanVulkan = len(s.VulkanReasons) == 0
	return s
}

func (h Host) root(p string) string { return filepath.Join(h.FirmwareRoot, p) }

func (h Host) hasRenderServer() bool {
	for _, p := range []string{"usr/libexec/virgl_render_server", "usr/lib/virglrenderer/virgl_render_server"} {
		if exists(h.root(p)) {
			return true
		}
	}
	return false
}

func (h Host) loaderHasFilters() bool {
	for _, dir := range []string{"usr/lib/x86_64-linux-gnu", "usr/lib64", "usr/lib"} {
		b, err := os.ReadFile(h.root(filepath.Join(dir, "libvulkan.so.1")))
		if err == nil {
			return bytes.Contains(b, []byte("VK_LOADER_DEVICE_ID_FILTER"))
		}
	}
	return false
}

// check3D refuses a 3D start this host cannot run, naming what is missing and Settings › Display.
func (h Host) check3D(c Config, qemu string) error {
	if c.Graphics != GraphicsVirtioGL {
		return nil
	}
	s := h.Guest3DSupport(qemu)
	if !s.Can3D {
		return errors.New(s.Reason3D + ": turn 3D acceleration off in Settings › Display")
	}
	if c.Vulkan && !s.CanVulkan {
		return errors.New("Vulkan (Venus) cannot run on this host: " + strings.Join(s.VulkanReasons, "; ") + ". Turn Vulkan off in Settings › Display")
	}
	g, err := h.RenderGPUFor(c)
	if err != nil {
		return err
	}
	if c.Vulkan && !g.Vulkan {
		return fmt.Errorf("the host GPU %s (%s) has no Vulkan driver installed: install it, or turn Vulkan off in Settings › Display", g.Name, g.Driver)
	}
	if !c.InApp() && !c.Vulkan {
		return nil // QEMU's window draws OpenGL on the desktop's GPU: the render node is not used
	}
	f, err := os.OpenFile(g.Node, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("3D acceleration cannot use the host GPU %s (%v): log in on the desktop, or join the render group (sudo usermod -aG render $USER, then log in again)", g.Name, err)
	}
	f.Close()
	return nil
}

// venusHostmem is the host-visible window a Venus guest maps GPU memory through; it is address
// space, not memory allocated up front (task 1.1 ran with it on both GPUs).
const venusHostmem = "4G"

// venusDevice is what the 3D device gets for Vulkan: blob resources and Venus (design D5).
func venusDevice(c Config) string {
	if !c.Vulkan {
		return ""
	}
	return ",blob=true,venus=true,hostmem=" + venusHostmem
}

// venusMachine puts a Vulkan VM's memory on a shared memfd backend, which blob resources map into
// the host's renderer; it pairs with venusMemory.
func venusMachine(c Config) string {
	if !c.Vulkan {
		return ""
	}
	return ",memory-backend=ram0"
}

// venusMemory is the memfd backend venusMachine names.
func venusMemory(c Config) []string {
	if !c.Vulkan {
		return nil
	}
	return []string{"-object", fmt.Sprintf("memory-backend-memfd,id=ram0,size=%dM,share=on", c.RAMMiB)}
}

// vulkanSelectors are the variables that pick or filter the Vulkan devices QEMU's renderer sees:
// replaced in its environment, never inherited (design D5; Mesa's forcing selectors break Venus).
var vulkanSelectors = []string{"MESA_VK_DEVICE_SELECT", "MESA_VK_DEVICE_SELECT_FORCE_DEFAULT_DEVICE", "DRI_PRIME",
	"VK_LOADER_VENDOR_ID_FILTER", "VK_LOADER_DEVICE_ID_FILTER", "VK_LOADER_DRIVER_ID_FILTER",
	"VK_LOADER_DRIVERS_SELECT", "VK_LOADER_DRIVERS_DISABLE", "VK_ICD_FILENAMES", "VK_DRIVER_FILES"}

// vulkanEnv is QEMU's environment for a Vulkan VM: base without any device selector, with the
// loader's filter for g's vendor and device.
func vulkanEnv(base []string, g RenderGPU) []string {
	var out []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(vulkanSelectors, k) {
			out = append(out, kv)
		}
	}
	vendor, device, _ := strings.Cut(g.ID, ":")
	return append(out, "VK_LOADER_VENDOR_ID_FILTER=0x"+vendor, "VK_LOADER_DEVICE_ID_FILTER=0x"+device)
}
