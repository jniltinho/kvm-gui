# Spec Delta

## Purpose

Lets the app create and run ordinary KVM virtual machines (Linux, Windows and other systems installed from an ISO) next to macOS VMs, with sensible per-OS device defaults.

## ADDED Requirements

### Requirement: Guest types
Every VM SHALL have a guest type: macOS, Linux, Windows or Other, and Linux and Windows VMs SHALL also have an OS profile. VMs that existed before this change SHALL be macOS VMs. The macOS-only behavior (OpenCore, recovery and installer media, SMBIOS identity, root-patch mode, Reims vGPU, ReimsVgpu.kext, extra kexts) SHALL apply only to macOS VMs.

#### Scenario: Existing VM after upgrade
- **WHEN** the database from before this change is opened
- **THEN** every VM is a macOS VM and boots as before

#### Scenario: Linux VM boot
- **WHEN** a Linux VM starts
- **THEN** QEMU gets no OpenCore disk, no SMBIOS identity and no Apple-specific device or argument

### Requirement: OS profiles
Each OS profile SHALL define the default firmware, disk bus, network model, graphics, sound, input and CPU features. The New VM wizard SHALL apply them, and the user SHALL be able to change each one in Settings.

#### Scenario: Windows 11 defaults
- **WHEN** a VM is created with the Windows 11 profile
- **THEN** it gets UEFI with Secure Boot, a TPM 2.0, a SATA disk, an e1000e network card and Hyper-V enlightenments

#### Scenario: Linux defaults
- **WHEN** a VM is created with a Linux profile
- **THEN** it gets UEFI, a virtio disk, a virtio network card and virtio graphics

### Requirement: Firmware
Linux, Windows and Other VMs SHALL boot with UEFI or legacy BIOS, as configured. A UEFI VM SHALL keep its own persistent UEFI variables file in its folder, created once from the host firmware's variables template that matches the firmware code it boots.

#### Scenario: BIOS VM
- **WHEN** an Other VM set to BIOS starts
- **THEN** it boots with legacy BIOS and has no UEFI variables file

### Requirement: Secure Boot
Secure Boot SHALL require UEFI. A VM with Secure Boot SHALL boot the host's Secure Boot firmware code with variables created from the template with Microsoft keys enrolled, and SHALL NOT start when either file is missing. Turning Secure Boot on or off for an existing VM SHALL replace its UEFI variables from the matching template only after the user confirms that boot entries stored in them are lost.

#### Scenario: Secure Boot firmware missing
- **WHEN** a VM with Secure Boot starts on a host without the Secure Boot firmware
- **THEN** QEMU is not started and the error names the missing file and the package that provides it

#### Scenario: Enforced in the guest
- **WHEN** a Linux VM with Secure Boot boots a signed distribution and then an unsigned EFI program
- **THEN** `mokutil --sb-state` in the guest reports Secure Boot enabled and the unsigned program is refused

#### Scenario: BIOS with Secure Boot
- **WHEN** the user sets firmware BIOS on a VM with Secure Boot
- **THEN** the settings refuse it and say Secure Boot needs UEFI

### Requirement: TPM
A VM with a TPM SHALL get a TPM 2.0 emulated by `swtpm`, with its state in the VM folder readable only by the user, started with the VM and reachable only through a socket in the run's private directory.

#### Scenario: Windows 11 setup
- **WHEN** Windows 11 setup checks the hardware in a VM with a TPM and Secure Boot
- **THEN** it finds a TPM 2.0 and Secure Boot and proceeds

#### Scenario: swtpm missing
- **WHEN** a VM with a TPM starts and `swtpm` is not installed
- **THEN** QEMU is not started and the error says to install `swtpm`

#### Scenario: State privacy
- **WHEN** another user on the host lists the VM folder
- **THEN** the TPM state directory and its files are not readable by them

### Requirement: TPM lifecycle
QEMU SHALL start only after the TPM socket is ready, within a deadline. If QEMU fails to start or exits, `swtpm` SHALL be stopped and reaped, and the VM SHALL count as running until `swtpm` has exited and its state is written, so no snapshot, clone or move reads a half-written state.

#### Scenario: QEMU fails to start
- **WHEN** QEMU exits at once because of a bad argument in a VM with a TPM
- **THEN** no `swtpm` process is left and the next start works

#### Scenario: Restart keeps the TPM
- **WHEN** a Windows 11 VM shuts down and starts again
- **THEN** the guest sees the same TPM, with its keys

### Requirement: Disks and optical drives
A Linux, Windows or Other VM SHALL have zero or more qcow2 or raw hard disks and zero or more optical drives on the configured bus; a VM without a hard disk boots from its optical drive (a live ISO). Disk images SHALL be created in the VM folder unless the user picks an existing image. An optical medium SHALL be attached read-only and SHALL be changeable or ejected while the VM runs.

#### Scenario: Eject after install
- **WHEN** the user ejects the installer ISO from the running VM
- **THEN** the guest sees an empty drive and the next boot starts from the hard disk

### Requirement: Boot order
A Linux, Windows or Other VM SHALL boot from the devices in its configured order. A new VM with an installer ISO SHALL boot from the ISO first while the hard disk is empty.

#### Scenario: First boot
- **WHEN** a new VM with an ISO starts for the first time
- **THEN** it boots the ISO's installer

### Requirement: ISO detection
When the user selects an ISO in the New VM wizard, the system SHALL read its volume label offline and, for known Linux distributions and Windows releases, SHALL propose the guest type and OS profile. It SHALL leave the choice to the user when the label is unknown.

#### Scenario: Ubuntu ISO
- **WHEN** the user selects an Ubuntu Desktop ISO
- **THEN** the wizard proposes Linux and the Ubuntu profile, and the name field suggests Ubuntu

#### Scenario: Unknown ISO
- **WHEN** the ISO's label matches no known system
- **THEN** the wizard keeps the type the user chose and says it could not detect the system

### Requirement: Display and input for ordinary guests
Linux, Windows and Other VMs SHALL use the same private display export and in-app screen as macOS VMs, with an absolute pointer device so the guest pointer follows the host pointer without capture.

#### Scenario: Pointer without capture
- **WHEN** the user moves the mouse over a running Linux VM's screen
- **THEN** the guest pointer follows without the pointer being captured

### Requirement: Shared folders
A Linux VM SHALL be able to have host folders shared with it, read-only or read-write, each with a mount tag. Other guest types SHALL NOT offer shared folders.

#### Scenario: Mount in the guest
- **WHEN** a Linux VM has the host folder `~/share` shared with tag `share`
- **THEN** the guest can mount tag `share` and see the folder's files

### Requirement: Serial port
A VM SHALL be able to have one serial port connected to a log file in the VM folder or to a socket in the run's private directory, with nothing connected by default.

#### Scenario: Serial log
- **WHEN** a Linux VM with the serial port connected to a file boots with a serial console
- **THEN** the file holds the guest's console output

### Requirement: USB devices
A VM SHALL have a USB 3 controller with the absolute pointer and keyboard, and SHALL be able to pass through host USB devices the user picks by vendor and product. The app SHALL NOT change host device permissions; a device the user cannot open SHALL be refused with the path and the reason.

#### Scenario: Device without permission
- **WHEN** the user attaches a USB device whose device node is not writable by the user
- **THEN** the device is not attached and the message names the device node and says permissions must be granted on the host

### Requirement: Ordinary guests in the CLI and MCP server
The CLI and the MCP server SHALL create, configure, start, stop and list Linux, Windows and Other VMs with the guest type, OS profile, ISO, disk size, firmware, Secure Boot and TPM, with the same validation and safety rules as the GUI.

#### Scenario: Create from the CLI
- **WHEN** a user runs `kvm-gui vm create --guest linux --profile ubuntu --iso ubuntu.iso --disk 40G "Ubuntu"`
- **THEN** the VM is created with the Ubuntu profile, a 40 GiB disk and the ISO attached, and it appears in the GUI

#### Scenario: Create from MCP
- **WHEN** an MCP client calls `vm_create` with `guest: windows` and `profile: windows11`
- **THEN** the VM is created with Secure Boot and a TPM, and `vm_config_get` reports them
