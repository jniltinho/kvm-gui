# Spec Delta

## MODIFIED Requirements

### Requirement: VM list
The system SHALL keep the VMs and app preferences in a local SQLite database and SHALL import an older `config.json` once. The desktop app SHALL run several VMs at once, each VM at most once, and SHALL keep them running when the Manager closes.

#### Scenario: Upgrade from a single VM
- **WHEN** the app starts with a `config.json` and no database
- **THEN** that VM appears in the list with the same settings and `config.json` is renamed to `config.json.migrated`

#### Scenario: Two VMs running
- **WHEN** a macOS VM is running and the user starts a Linux VM
- **THEN** both run, each with its own SSH port, and both show Running in the list

#### Scenario: Manager closed
- **WHEN** the user closes the Manager while two VMs run
- **THEN** both VMs keep running and the Manager shows them Running when it opens again

### Requirement: New VM wizard
The system SHALL create a VM through one wizard, guided or expert like VirtualBox: name, folder, guest type, OS profile or macOS version, and installer ISO for ordinary guests; hardware (memory and CPU prefilled with the host recommendation, firmware, display); and hard disk (new with size, existing image, or none; download now for macOS). On Finish it SHALL validate every section, open the first with a problem, and otherwise create the folder and disk and attach the ISO or download the recovery when asked.

#### Scenario: Duplicate name
- **WHEN** the name is already used by another VM and the user clicks Finish
- **THEN** the wizard opens the name section, says so and creates nothing

#### Scenario: Linux from an ISO
- **WHEN** the user picks Linux, selects an ISO and accepts the defaults
- **THEN** the VM is created with a new disk and the ISO attached, and its first start boots the ISO

### Requirement: Workspace with one folder per VM
The system SHALL keep an app-wide Workspace (default `~/KVM-VMs`) and SHALL give each new or imported VM the folder `<Workspace>/<name>` unless another folder is chosen. Changing the Workspace SHALL NOT move existing VMs.

#### Scenario: New VM folder
- **WHEN** the Workspace is `~/KVM-VMs` and the user creates "Tahoe lab"
- **THEN** its folder is `~/KVM-VMs/tahoe-lab`

### Requirement: Theme preference
The system SHALL offer the skins Light, Dark, Windows XP, macOS, macOS Dark, Mac OS Retro and System, SHALL apply the choice at once to every window including the toolbar, and SHALL let `KVM_GUI_THEME` or `MKG_THEME` override it.

#### Scenario: Switch to Light
- **WHEN** the user picks Light in File › Preferences
- **THEN** the window, toolbar band and separators turn light without a restart

## ADDED Requirements

### Requirement: Clone a VM
The system SHALL make a full clone of a powered-off VM, other than a `reims-ctl` VM, with a new name and folder, copying every attached hard disk (images outside its folder too, which stay unchanged) into the clone's folder, with its UEFI variables, TPM state and settings, and giving it new MAC addresses, a free SSH port and, for macOS, a new SMBIOS identity. A failed clone SHALL leave nothing behind.

#### Scenario: Clone a macOS VM
- **WHEN** the user clones a powered-off macOS VM with the defaults
- **THEN** the clone boots the same macOS with a different serial number, MAC address and SSH port

#### Scenario: Clone an encrypted Windows VM
- **WHEN** the user clones a powered-off Windows 11 VM with BitLocker on, keeping the default TPM choice
- **THEN** the clone boots to the Windows desktop without asking for the recovery key

#### Scenario: Disk full
- **WHEN** the copy fails because the disk is full
- **THEN** the clone's folder is removed and no VM is added

### Requirement: Clone options
The Clone dialog SHALL let the user keep the source's MAC addresses, and start the clone with a new TPM after a warning that disk encryption bound to the TPM (BitLocker) will ask for its recovery key.

#### Scenario: New TPM
- **WHEN** the user picks a new TPM for the clone of a Windows 11 VM
- **THEN** the dialog warns about BitLocker's recovery key before the clone starts, and the clone's TPM state is new

### Requirement: Disk images in use
The system SHALL identify a disk image by its file identity (device and inode), not its path, and SHALL treat a VM as busy while any QEMU process, including one started outside the app, has any of its hard disk images open. A shared image SHALL mark the VM busy, never make another VM's run its own.

#### Scenario: Shared image through a symbolic link
- **WHEN** VM A runs from `/data/disk.qcow2` and VM B's second disk is a symbolic link to the same file
- **THEN** starting VM B is refused and the message names VM A

#### Scenario: External QEMU on a secondary disk
- **WHEN** a QEMU started outside the app has a Linux VM's second disk open
- **THEN** the VM shows as running outside the app and its snapshot, move and delete are refused

### Requirement: Locks on disk images
Every operation on disk images SHALL lock each image it touches by its file identity and recheck its use under the lock. Start, snapshot, restore, clone, move, export, delete and Media manager changes SHALL be refused for an image another running VM or process has open, naming it.

#### Scenario: Two starts at once
- **WHEN** VM A and VM B share an image and both are started at the same moment
- **THEN** exactly one starts and the other is refused naming the first

### Requirement: Files a VM owns
For every guest type the system SHALL know which files a VM owns (disks created in its folder, UEFI variables, TPM state, snapshot data, identity, boot files) and which it only uses (images outside its folder, ISO files). Move, export, import and delete SHALL handle every owned file, SHALL never move or delete a used-only file nor a file another VM or snapshot references, and SHALL keep importing bundles exported by earlier versions.

#### Scenario: Delete with an external disk
- **WHEN** the user deletes a Linux VM with its files, and one disk is an image in another folder
- **THEN** the VM's folder with its owned files is removed and the external image stays

#### Scenario: Export and import an ordinary guest
- **WHEN** a powered-off Windows 11 VM with snapshots is exported and imported on another host
- **THEN** it boots with the same disks, UEFI variables, TPM state and snapshot tree, and external images it used are listed as missing for the user to attach

#### Scenario: Disk referenced by another VM
- **WHEN** the user deletes VM A with its files and stopped VM B uses one of A's disks
- **THEN** the delete is refused naming B, and nothing is deleted

#### Scenario: Old bundle
- **WHEN** a `.tar` exported by `macos-kvm-gui` before this change is imported
- **THEN** it imports as a macOS VM exactly as before, with the same refusal of unknown names and `..` paths

### Requirement: Private TPM state in bundles
Export SHALL write a bundle that holds TPM state readable only by the user, and import SHALL create the TPM directory 0700 and its files 0600 whatever the archive says.

#### Scenario: TPM state in a bundle
- **WHEN** a Windows 11 VM is exported and imported
- **THEN** the bundle file is readable only by the user and the imported TPM directory is 0700 with 0600 files, whatever the archive says
