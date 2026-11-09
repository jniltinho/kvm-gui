# Spec Delta

## MODIFIED Requirements

### Requirement: Supported distributions
The `.deb` SHALL install on Ubuntu 26.04 and Debian 13, and the `.rpm` on Rocky Linux 10. The AppImage and the plain binary SHALL run on the same three distributions once the GTK 4 and WebKitGTK 6.0 runtime libraries are installed; the documentation SHALL list, for each distribution, the one command that installs them and the VM tools (QEMU, OVMF, `swtpm`). Ubuntu 24.04 is no longer supported: the documentation SHALL say so and name v0.15.0 as the last release for it.

#### Scenario: Clean distribution check
- **WHEN** the `.deb` is installed with apt in a clean Ubuntu 26.04 or Debian 13 container, the `.rpm` with dnf in a clean Rocky Linux 10 container, or the documented libraries are installed and the AppImage and plain binary are run
- **THEN** each runs `--self-test`, renders the Manager window under a virtual display and exits 0

#### Scenario: Upgrade from macos-kvm-gui
- **WHEN** the last `macos-kvm-gui` package is installed with a VM in its list in a clean Ubuntu 26.04 or Rocky Linux 10 container, and the `kvm-gui` package is installed over it
- **THEN** `macos-kvm-gui` is replaced, both command names run kvm-gui, and the VM is in kvm-gui's list
