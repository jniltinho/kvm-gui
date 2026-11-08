# Spec Delta

## ADDED Requirements

### Requirement: Package manager per distribution
Set up host SHALL install the missing packages with `apt-get` on Ubuntu 24.04, Ubuntu 26.04 and Debian 13 and with `dnf` on Rocky Linux 10, using that distribution's package names, and SHALL change nothing on any other distribution, listing the packages it needs instead.

#### Scenario: Rocky Linux
- **WHEN** Set up host runs on Rocky Linux 10 without `qemu-img`
- **THEN** it installs the package that provides it with `dnf` and reruns the checks

#### Scenario: Unsupported distribution
- **WHEN** Set up host runs on a distribution other than the four supported ones
- **THEN** nothing is installed and the app lists the packages to install by hand

### Requirement: Checks for ordinary guests
The checks SHALL also report, as Warning when missing, `swtpm` (needed by VMs with a TPM) and the Secure Boot UEFI firmware with Microsoft keys (needed by VMs with Secure Boot), each with the package that provides it on the host's distribution.

#### Scenario: No swtpm
- **WHEN** `swtpm` is not in `PATH`
- **THEN** the swtpm check shows Warning, says Windows 11 VMs need it and names the package

### Requirement: QEMU executable per distribution
The system SHALL find the system QEMU by the host's distribution: `qemu-system-x86_64` on Ubuntu and Debian, `/usr/libexec/qemu-kvm` on Rocky Linux 10, and SHALL show which one it uses in the checks.

#### Scenario: Rocky Linux
- **WHEN** the checks run on Rocky Linux 10 with `qemu-kvm` installed
- **THEN** the QEMU check is OK and names `/usr/libexec/qemu-kvm` and its version

### Requirement: Devices the host's QEMU lacks
Before a start, the system SHALL check that the host's QEMU has every device the VM's settings need, and SHALL refuse the start naming the missing device and the setting that needs it.

#### Scenario: VMware SVGA on Rocky Linux
- **WHEN** a VM with VMware SVGA starts on Rocky Linux 10, whose QEMU has no `vmware-svga`
- **THEN** QEMU is not started and the message names `vmware-svga` and the Graphics setting

#### Scenario: Shared folder without 9p
- **WHEN** a Linux VM with a shared folder starts on a host whose QEMU lacks `virtio-9p-pci`
- **THEN** QEMU is not started and the message names the device and the Shared Folders page

