# Spec Delta

## Purpose

Moving Linux, Windows and Other VMs between kvm-gui and VirtualBox (or another OVF tool) as OVF appliances: an `.ova` archive, or an `.ovf` descriptor with its files. kvm-gui's own VM bundles stay a separate operation (they carry what OVF cannot: macOS, snapshots, TPM state).

## ADDED Requirements

### Requirement: Import an appliance
The system SHALL import an `.ova` (a tar with an OVF 1.0 or 2.0 descriptor, its disk images and optionally a manifest), or an `.ovf` with the files it references beside it, as one new x86 Linux, Windows or Other VM with a name unique in the VM list. It SHALL convert every hard disk to qcow2 in the new VM's folder and SHALL never start the VM.

#### Scenario: VirtualBox Ubuntu appliance
- **WHEN** the user imports an `.ova` that VirtualBox 7 exported from an Ubuntu 64-bit VM with 4096 MB, 2 processors, EFI, one SATA disk and one NAT adapter
- **THEN** a Linux VM with the Ubuntu profile, 4096 MiB, 2 vCPUs, UEFI firmware, one qcow2 SATA disk with the same contents and one NAT adapter with a new MAC address appears in the list, powered off

#### Scenario: ARM appliance
- **WHEN** the appliance's operating system is an ARM one (for example VirtualBox's `Ubuntu_arm64`)
- **THEN** the import is refused: kvm-gui runs x86 guests

### Requirement: Imported settings and identity
An import SHALL map the appliance's name, operating system, memory, processors, firmware (BIOS or UEFI), Secure Boot, hard disks with their controller type (SATA, NVMe, or virtio where the appliance says so; IDE and SCSI become SATA with a note), optical drives and network adapters to the VM's settings, and SHALL give the VM a new identity: new MAC addresses and its own SSH port.

#### Scenario: Two imports of one appliance
- **WHEN** the same appliance is imported twice
- **THEN** the two VMs have different MAC addresses and SSH ports

#### Scenario: Name already in use
- **WHEN** the appliance's VM name is already used by another VM
- **THEN** the imported VM gets a free name derived from it ("Ubuntu (imported)", as bundle imports do) and the existing VM is unchanged

### Requirement: Unmapped settings are reported
What an appliance describes and kvm-gui cannot represent (more processors or memory than the host has, an unknown OS, a controller kvm-gui lacks, floppy drives, extra monitors, USB filters, shared folders, snapshots, TPM or NVRAM state) SHALL be listed before the import, with what kvm-gui uses instead. An unknown OS SHALL become an Other VM, or Linux or Windows when the descriptor's OS family says so, never a guessed profile.

#### Scenario: Unknown operating system
- **WHEN** the appliance's VirtualBox OS type is not in kvm-gui's table and its standard OS id is a Linux one
- **THEN** the planned VM is a generic Linux VM and the preview notes the original OS type

### Requirement: Required sections are understood
A section or device the descriptor marks as required that kvm-gui does not understand SHALL refuse the import, naming it.

#### Scenario: Unknown required section
- **WHEN** the descriptor has an unknown section with `ovf:required="true"`
- **THEN** the import is refused naming that section

### Requirement: Preview before import
Before an import creates anything, the system SHALL show the VM it would create and each setting it cannot keep, and SHALL check that VM's settings as a save would.

#### Scenario: Preview before import
- **WHEN** the user picks an appliance in the GUI, or runs the CLI import with `--dry-run`
- **THEN** the system shows the VM it would create (name, guest type and profile, memory, processors, firmware, disks with their sizes and controllers, network adapters) and each setting it cannot keep, and creates nothing

### Requirement: Bridged adapters on import
A network adapter that the appliance attaches to a host interface (bridged) SHALL be imported as NAT, with a note, unless the user picks one of this host's bridges for it in the preview (GUI) or with `--bridge` (CLI) or `bridge` (MCP).

#### Scenario: Bridged VirtualBox adapter
- **WHEN** a VirtualBox appliance's adapter is bridged to `enp5s0` and the user picks no bridge
- **THEN** the imported adapter is NAT and the preview says the bridged attachment to `enp5s0` was replaced

### Requirement: Untrusted appliances
An appliance SHALL be treated as untrusted input and refused, before any file of the new VM is created, when a member path is absolute, contains `/` or `..`, or is not a plain file; when a member appears twice; when the descriptor is missing, not first in an `.ova`, malformed or over 4 MiB; when a referenced file is missing; when there are more than 64 members; or when a disk image is not VMDK, qcow2 or raw.

#### Scenario: Path outside the appliance
- **WHEN** an `.ova` holds a member named `../../.bashrc`
- **THEN** the import is refused naming that member, and nothing is written

### Requirement: Disk images are self-contained
Each disk image SHALL be opened only with its declared format, never probed, and its header SHALL be checked before any tool opens it, refusing an image that refers to anything outside itself: a backing file, an external data file, or a VMDK whose extents are in other files (only single-file VMDK subtypes are accepted). A disk whose real virtual size differs from the size the descriptor declares, or exceeds 16 TiB, SHALL be refused.

#### Scenario: Backing file pointing into the host
- **WHEN** an appliance's qcow2 disk has a backing file `/etc/shadow`
- **THEN** the import is refused naming the disk, before any conversion runs

#### Scenario: Raw disk with another format's header
- **WHEN** a disk declared raw holds a qcow2 header pointing at a backing file
- **THEN** the disk is read as raw data, never as qcow2, so no other file is opened

#### Scenario: Flat VMDK with an external extent
- **WHEN** an appliance's VMDK is a descriptor whose extent is `../../home/user/disk.img`
- **THEN** the import is refused naming the disk

### Requirement: Imports are all or nothing
An import SHALL check, before extracting, that the destination's file system has room for the extracted images, and before each conversion, that it has room for the converted disk as measured from the image; a manifest digest that does not match, or a failure or cancellation at any step, SHALL leave no folder, file or VM entry behind.

#### Scenario: Not enough space
- **WHEN** the appliance's images need more space than the workspace's file system has free
- **THEN** the import is refused before anything is extracted, saying how much space it needs

#### Scenario: Corrupted disk
- **WHEN** the manifest's digest of a disk image or of the descriptor does not match the archive's
- **THEN** the import is refused naming the file, and the partly extracted or converted files and the VM folder are removed

### Requirement: macOS appliances
The system SHALL refuse to export a macOS VM, and SHALL refuse to import an appliance whose operating system is macOS, saying that macOS VMs move between kvm-gui hosts as kvm-gui bundles.

#### Scenario: Export a macOS VM
- **WHEN** the user asks to export a macOS VM as an appliance
- **THEN** the export is refused with that reason and no file is written

### Requirement: Export an appliance
The system SHALL export a powered-off Linux, Windows or Other VM, with each disk read in its own format (qcow2 or raw), as one `.ova` holding first an OVF 1.0 descriptor, then every hard disk as a streamOptimized VMDK, then a manifest with a digest of the descriptor and of each disk. The VM SHALL stay locked against starting, and its disks against other VMs' starts, while the disks are read. A running VM SHALL be refused.

#### Scenario: VirtualBox imports and boots it
- **WHEN** a Linux VM with UEFI, 8192 MiB, 4 vCPUs, one disk and one NAT adapter is exported and the file is imported by VirtualBox 7.2
- **THEN** VirtualBox's VM has the matching OS type, 8192 MB, 4 CPUs, EFI enabled, one hard disk and one NAT adapter, and the guest boots in it

#### Scenario: Running VM
- **WHEN** the user exports a VM that is running
- **THEN** the export is refused with "shut the VM down first"

#### Scenario: Shared disk in use
- **WHEN** a disk of the exported VM is also another VM's disk and that VM is running
- **THEN** the export is refused, saying which VM has the disk open

### Requirement: What the exported descriptor says
The descriptor SHALL carry the VM's name and description, its operating system as both the standard OS identifier and VirtualBox's OS type, its memory, processors, firmware and Secure Boot in a form VirtualBox 7.2 applies on import (a setting VirtualBox cannot take is noted before the export), its disk controllers and disks, an empty optical drive for each of the VM's optical drives, and its network adapters with their attachment and adapter type.

#### Scenario: Windows 11 VM
- **WHEN** a Windows 11 VM with UEFI is exported and imported by VirtualBox 7.2
- **THEN** VirtualBox's VM has the Windows 11 64-bit OS type and EFI enabled

### Requirement: What an export leaves behind
An export SHALL write each disk's current state only, not the VM's snapshots, its TPM state or its UEFI variables, and SHALL say so before it starts. When the VM has a TPM, the export SHALL proceed only after the user acknowledges that secrets sealed to it (for example a BitLocker key) will not open on the other side, so its recovery key or a suspended protection is needed.

#### Scenario: VM with snapshots
- **WHEN** the user exports a VM that has snapshots
- **THEN** the export notes that its snapshots are not included and writes the current state

#### Scenario: Windows VM with a TPM
- **WHEN** the user exports a Windows 11 VM that has a TPM without acknowledging the TPM note (`--accept-tpm-loss` on the command line, `accept_tpm_loss: true` in MCP, a checkbox in the GUI)
- **THEN** the export is refused, explaining the BitLocker recovery key

### Requirement: Existing files are kept
An export SHALL NOT replace an existing destination file, even one created while it ran, unless the user confirms it in the GUI, passes `--force` on the command line, or passes `overwrite: true` to the MCP tool, which is then annotated as destructive. The destination SHALL never be a file the VM uses (its disks, wherever they are, or its ISOs), and a failed or cancelled export SHALL leave no partial file at the destination.

#### Scenario: Export over an existing file
- **WHEN** the CLI export's destination exists and `--force` is not given
- **THEN** the export is refused and the existing file is unchanged

#### Scenario: File created during the export
- **WHEN** another program creates the destination file while the disks are converted, and no overwrite was allowed
- **THEN** the export ends with an error, the other program's file is unchanged and no temporary file remains

### Requirement: Where import and export are offered
Import and export SHALL be available from the command line (`vm import-ova <file> [--name] [--folder] [--bridge] [--dry-run]`, `vm export-ova <file> [--force] [--accept-tpm-loss]`), the MCP server (`vm_import_ova`, `vm_export_ova`) and the GUI (File › Import Appliance… with its preview, File › Export Appliance… for the selected VM), each with progress for the disk conversion and a way to cancel. These are separate from kvm-gui's own VM bundle export and import.

#### Scenario: Import from the GUI
- **WHEN** the user picks File › Import Appliance…, chooses an `.ova` and confirms the preview
- **THEN** a progress dialog follows the disk conversion, and the new VM is selected in the list when it ends
