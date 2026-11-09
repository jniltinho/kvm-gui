package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Vulkan and the host GPU (OpenSpec ubuntu-26-04-and-vulkan, task 3.1): Vulkan needs 3D
// acceleration, turning 3D off turns Vulkan off, the GPU is a PCI address or automatic, and none of
// it applies to macOS or Windows.
func TestVulkanSettings(t *testing.T) {
	c, err := NewGuest("ubuntu", "Lab", "")
	if err != nil {
		t.Fatal(err)
	}
	c.SSHPort = 2222
	set := func(kv string) error {
		k, v, _ := strings.Cut(kv, "=")
		if err := ConfigKeys[k](&c, v); err != nil {
			return err
		}
		return c.Validate()
	}
	if err := set("vulkan=true"); err == nil || !strings.Contains(err.Error(), "3D acceleration") {
		t.Fatalf("vulkan without 3D: %v", err)
	}
	c.Vulkan = false
	for _, kv := range []string{"graphics=" + GraphicsVirtioGL, "vulkan=true", "render-gpu=0000:0C:00.0"} {
		if err := set(kv); err != nil {
			t.Fatalf("%s: %v", kv, err)
		}
	}
	if !c.Vulkan || c.RenderGPU != "0000:0c:00.0" {
		t.Fatalf("set: %+v", c)
	}
	if err := set("render-gpu=card0"); err == nil || !strings.Contains(err.Error(), "PCI address") {
		t.Fatalf("a bad GPU address: %v", err)
	}
	if err := set("render-gpu=auto"); err != nil || c.RenderGPU != "" {
		t.Fatalf("auto: %v %q", err, c.RenderGPU)
	}
	if err := set("graphics=" + GraphicsVirtio); err != nil || c.Vulkan {
		t.Fatalf("3D off must turn Vulkan off: %v %+v", err, c.Vulkan)
	}

	w, err := NewGuest("windows11", "Win", "")
	if err != nil {
		t.Fatal(err)
	}
	w.Vulkan, w.SSHPort = true, 2222
	if err := w.Validate(); err == nil {
		t.Error("Vulkan on Windows was accepted")
	}
	m := Defaults()
	m.Vulkan = true
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "Linux and Other") {
		t.Errorf("Vulkan on macOS: %v", err)
	}
	for _, k := range []string{"vulkan", "render-gpu"} {
		found := false
		for _, a := range AgentConfigKeys {
			found = found || a == k
		}
		if !found {
			t.Errorf("agents cannot set %s", k)
		}
	}
}

// fake3DHost is a host with three render nodes: two identical RX 6700 XTs (renderD128 at 0c:00.0,
// renderD130 at 0d:00.0) and an RTX 4070 (renderD129 at 01:00.0); only RADV is installed.
func fake3DHost(t *testing.T) Host {
	t.Helper()
	root := t.TempDir()
	h := Host{SysRoot: filepath.Join(root, "sys"), ShareRoot: filepath.Join(root, "share"), EtcRoot: filepath.Join(root, "etc"),
		FirmwareRoot: filepath.Join(root, "fs"), DevDRI: filepath.Join(root, "dri"),
		Output: func(string, ...string) ([]byte, error) { return nil, errors.New("no lspci") }}
	for _, g := range []struct{ node, addr, vendor, device, driver string }{
		{"renderD128", "0000:0c:00.0", "0x1002", "0x73df", "amdgpu"},
		{"renderD129", "0000:01:00.0", "0x10de", "0x2786", "nvidia"},
		{"renderD130", "0000:0d:00.0", "0x1002", "0x73df", "amdgpu"},
	} {
		dev := filepath.Join(h.SysRoot, "devices/pci0000:00", g.addr)
		write(t, filepath.Join(dev, "vendor"), g.vendor+"\n")
		write(t, filepath.Join(dev, "device"), g.device+"\n")
		os.MkdirAll(filepath.Join(h.SysRoot, "bus/drivers", g.driver), 0o755)
		os.Symlink(filepath.Join(h.SysRoot, "bus/drivers", g.driver), filepath.Join(dev, "driver"))
		os.MkdirAll(filepath.Join(h.SysRoot, "bus/pci/devices"), 0o755)
		os.Symlink(dev, filepath.Join(h.SysRoot, "bus/pci/devices", g.addr))
		os.MkdirAll(filepath.Join(h.SysRoot, "class/drm", g.node), 0o755)
		os.Symlink(dev, filepath.Join(h.SysRoot, "class/drm", g.node, "device"))
		write(t, filepath.Join(h.DevDRI, g.node), "")
	}
	write(t, filepath.Join(h.ShareRoot, "vulkan/icd.d/radeon_icd.x86_64.json"), "{}")
	return h
}

// The host GPUs, the automatic choice, a choice by PCI address that survives renumbered render
// nodes, and identical GPUs marked (task 3.2).
func TestRenderGPUs(t *testing.T) {
	h := fake3DHost(t)
	gpus := h.RenderGPUs()
	if len(gpus) != 3 {
		t.Fatalf("%+v", gpus)
	}
	for _, g := range gpus {
		amd := g.Driver == "amdgpu"
		if g.Vulkan != amd || g.Twin != amd || (amd && g.ID != "1002:73df") {
			t.Errorf("%+v", g)
		}
	}
	if g, _ := AutoRenderGPU(gpus); g.Addr != "0000:0c:00.0" {
		t.Errorf("automatic: %+v", g)
	}
	c := Config{Graphics: GraphicsVirtioGL, RenderGPU: "0000:01:00.0"}
	if g, err := h.RenderGPUFor(c); err != nil || !strings.HasSuffix(g.Node, "renderD129") || g.Vulkan {
		t.Errorf("chosen NVIDIA: %+v %v", g, err)
	}
	// After a reboot the NVIDIA GPU is renderD128: the VM still names it.
	os.Rename(filepath.Join(h.SysRoot, "class/drm/renderD129"), filepath.Join(h.SysRoot, "class/drm/renderD131"))
	if g, err := h.RenderGPUFor(c); err != nil || !strings.HasSuffix(g.Node, "renderD131") {
		t.Errorf("renumbered: %+v %v", g, err)
	}
	c.RenderGPU = "0000:42:00.0"
	if _, err := h.RenderGPUFor(c); err == nil || !strings.Contains(err.Error(), "0000:42:00.0") || !strings.Contains(err.Error(), "Settings › Display") {
		t.Errorf("a missing GPU: %v", err)
	}
	if _, err := (Host{SysRoot: t.TempDir()}).RenderGPUFor(Config{}); err == nil {
		t.Error("a host without render nodes gave a GPU")
	}
}

// What the probe says on Ubuntu 26.04, Debian 13 and Rocky Linux 10 (design D6), and what a start
// refuses (task 3.2).
func TestGuest3DSupportAndStartChecks(t *testing.T) {
	help := []byte("virtio-vga-gl options:\n  blob=<bool>\n  hostmem=<size>\n  venus=<bool>\n")
	for name, c := range map[string]struct {
		help             []byte
		server, filters  bool
		can3D, canVulkan bool
		reasons          []string
	}{
		"ubuntu26.04": {help, true, true, true, true, nil},
		"debian13":    {help, false, false, true, false, []string{"render server", "Vulkan loader"}},
		"oldqemu":     {[]byte("blob=<bool>\n"), true, true, true, false, []string{"no Venus"}},
		"rocky10":     {nil, true, true, false, false, []string{"needs 3D"}},
	} {
		h := fake3DHost(t)
		h.Output = func(string, ...string) ([]byte, error) {
			if c.help == nil {
				return []byte("qemu-kvm: -device virtio-vga-gl,help: Device 'virtio-vga-gl' not found"), errors.New("exit status 1")
			}
			return c.help, nil
		}
		if c.server {
			write(t, filepath.Join(h.FirmwareRoot, "usr/libexec/virgl_render_server"), "")
		}
		loader := "libvulkan"
		if c.filters {
			loader += " VK_LOADER_DEVICE_ID_FILTER"
		}
		write(t, filepath.Join(h.FirmwareRoot, "usr/lib/x86_64-linux-gnu/libvulkan.so.1"), loader)
		s := h.Guest3DSupport("/qemu-" + name)
		if s.Can3D != c.can3D || s.CanVulkan != c.canVulkan || len(s.VulkanReasons) != len(c.reasons) {
			t.Errorf("%s: %+v", name, s)
		}
		for i, r := range c.reasons {
			if i < len(s.VulkanReasons) && !strings.Contains(s.VulkanReasons[i], r) {
				t.Errorf("%s: reason %q, want %q", name, s.VulkanReasons[i], r)
			}
		}
		vm := Config{Graphics: GraphicsVirtioGL, Vulkan: true}
		err := h.check3D(vm, "/qemu-"+name)
		if (err == nil) != c.canVulkan || (err != nil && !strings.Contains(err.Error(), "Settings › Display")) {
			t.Errorf("%s: Vulkan start: %v", name, err)
		}
	}
	h := fake3DHost(t)
	h.Output = func(string, ...string) ([]byte, error) { return help, nil }
	write(t, filepath.Join(h.FirmwareRoot, "usr/libexec/virgl_render_server"), "")
	write(t, filepath.Join(h.FirmwareRoot, "usr/lib/x86_64-linux-gnu/libvulkan.so.1"), "VK_LOADER_DEVICE_ID_FILTER")
	if err := h.check3D(Config{Graphics: GraphicsVirtioGL, Vulkan: true, RenderGPU: "0000:01:00.0"}, "/qemu-u"); err == nil || !strings.Contains(err.Error(), "no Vulkan driver") {
		t.Errorf("Vulkan on a GPU without an ICD: %v", err)
	}
	if err := h.check3D(Config{Graphics: GraphicsVirtioGL, RenderGPU: "0000:42:00.0"}, "/qemu-u"); err == nil || !strings.Contains(err.Error(), "0000:42:00.0") {
		t.Errorf("a missing GPU: %v", err)
	}
	if os.Geteuid() != 0 {
		os.Chmod(filepath.Join(h.DevDRI, "renderD128"), 0)
		if err := h.check3D(Config{Graphics: GraphicsVirtioGL}, "/qemu-u"); err == nil || !strings.Contains(err.Error(), "render group") {
			t.Errorf("an inaccessible node in the app's screen: %v", err)
		}
		if err := h.check3D(Config{Graphics: GraphicsVirtioGL, Display: DisplayWindow}, "/qemu-u"); err != nil {
			t.Errorf("QEMU's window does not use the node for OpenGL: %v", err)
		}
		if err := h.check3D(Config{Graphics: GraphicsVirtioGL, Display: DisplayWindow, Vulkan: true}, "/qemu-u"); err == nil {
			t.Error("Vulkan in QEMU's window still needs the node")
		}
	}
	if err := h.check3D(Config{Graphics: GraphicsVirtio}, "/qemu-u"); err != nil {
		t.Errorf("2D: %v", err)
	}
}

// A Vulkan VM's command line and QEMU's environment (task 3.3, design D5).
func TestVulkanCommandLineAndEnv(t *testing.T) {
	defer func(f func(Config) (RenderGPU, error)) { renderGPU = f }(renderGPU)
	renderGPU = func(Config) (RenderGPU, error) {
		return RenderGPU{Node: "/dev/dri/renderD129", ID: "10de:2786", Vulkan: true}, nil
	}
	ub := testGuest(t, "ubuntu")
	ub.Graphics, ub.Vulkan, ub.RAMMiB = GraphicsVirtioGL, true, 4096
	s := argPairs(must(guestArgs(ub, ub.Workspace, Firmware{}, "/run/x/qmp.sock", "")))
	for _, want := range []string{"-machine q35,memory-backend=ram0", "-m 4096", "-object memory-backend-memfd,id=ram0,size=4096M,share=on",
		"-device virtio-vga-gl,edid=on,xres=1920,yres=1080,blob=true,venus=true,hostmem=4G", "-display egl-headless,rendernode=/dev/dri/renderD129"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in %s", want, s)
		}
	}
	ub.Vulkan = false
	if s := argPairs(must(guestArgs(ub, ub.Workspace, Firmware{}, "/run/x/qmp.sock", ""))); strings.Contains(s, "memfd") || strings.Contains(s, "venus") || strings.Contains(s, "memory-backend") {
		t.Errorf("OpenGL only: %s", s)
	}

	t.Setenv("DRI_PRIME", "1")
	base := []string{"HOME=/home/u", "XDG_RUNTIME_DIR=/run/user/1000", "WAYLAND_DISPLAY=wayland-0", "DRI_PRIME=pci-0000_0c_00_0",
		"MESA_VK_DEVICE_SELECT=1002:73df!", "VK_LOADER_VENDOR_ID_FILTER=0x1002", "VK_ICD_FILENAMES=/x.json"}
	kept := append([]string(nil), base...)
	nv := vulkanEnv(base, RenderGPU{ID: "10de:2786"})
	amd := vulkanEnv(base, RenderGPU{ID: "1002:73df"})
	want := "HOME=/home/u XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-0 VK_LOADER_VENDOR_ID_FILTER=0x10de VK_LOADER_DEVICE_ID_FILTER=0x2786"
	if strings.Join(nv, " ") != want {
		t.Errorf("NVIDIA env: %v", nv)
	}
	if !strings.Contains(strings.Join(amd, " "), "VK_LOADER_VENDOR_ID_FILTER=0x1002 VK_LOADER_DEVICE_ID_FILTER=0x73df") {
		t.Errorf("AMD env: %v", amd)
	}
	if strings.Join(base, "\x00") != strings.Join(kept, "\x00") || os.Getenv("DRI_PRIME") != "1" {
		t.Error("the parent's environment changed")
	}
}

// Vulkan and the host GPU survive the VM list (they live in the guest settings' JSON column).
func TestVulkanSettingsSaved(t *testing.T) {
	isolateXDG(t, t.TempDir())
	c := testGuest(t, "ubuntu")
	c.Optical = []string{""}
	c.Graphics, c.Vulkan, c.RenderGPU = GraphicsVirtioGL, true, "0000:0c:00.0"
	e, err := CreateVM(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reload(e)
	if err != nil || !got.Config.Vulkan || got.Config.RenderGPU != "0000:0c:00.0" {
		t.Fatalf("reloaded %+v %v", got.Config, err)
	}
}
