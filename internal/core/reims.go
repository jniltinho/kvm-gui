package core

import (
	"errors"
	"os"
	"path/filepath"
)

// Reims vGPU (github.com/steelbrain/reims-vgpu) is a paravirtual GPU for macOS guests that
// renders through the host's Vulkan driver. It runs on its own QEMU build, which the app
// installs from a prebuilt package or builds in Docker, but never embeds (docs/reims-vgpu.md).

// ReimsDir is ~/.local/share/kvm-gui/reims-ctl, reims-ctl's data folder for kvm-gui: qemu/<build>
// and qemu/current. Builds made by macos-kvm-gui stay in the old Workspace's .reims.
func ReimsDir() (string, error) { return filepath.Join(DataDir(), "reims-ctl"), nil }

// ReimsQEMU finds the Reims QEMU: reims_qemu in settings.yaml (or MKG_REIMS_QEMU), the app's
// own install in <Workspace>/.reims, then an upstream reims-ctl install. A relative reims_qemu
// is relative to settings.yaml's folder, a relative MKG_REIMS_QEMU to the current directory:
// the VM runs QEMU from its own folder, so the path must be absolute by then.
func ReimsQEMU() (string, error) {
	if p := Setting("MKG_REIMS_QEMU"); p != "" {
		_, env := os.LookupEnv("MKG_REIMS_QEMU")
		if _, kenv := os.LookupEnv("KVM_GUI_REIMS_QEMU"); kenv {
			env = true
		}
		if !env && !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(SettingsFile()), p)
		}
		p, _ = filepath.Abs(p) // the env's path, from the current directory
		if _, err := os.Stat(p); err != nil {
			return "", errors.New("reims_qemu in " + SettingsFile() + ": " + err.Error())
		}
		return p, nil
	}
	var tried []string
	if dir, err := ReimsDir(); err == nil {
		tried = append(tried, filepath.Join(dir, "qemu", "current", "bin", "qemu-system-x86_64"))
	}
	if old := legacyWorkspaceDir(); old != "" {
		tried = append(tried, filepath.Join(old, ".reims", "qemu", "current", "bin", "qemu-system-x86_64"))
	}
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	tried = append(tried, filepath.Join(data, "reims-ctl", "qemu", "current", "bin", "qemu-system-x86_64"))
	for _, p := range tried {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", errors.New("Reims vGPU is not installed (looked for " + tried[0] + "): install it with Install Reims vGPU or `kvm-gui reims install`")
}
