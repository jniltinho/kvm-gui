# Spec Delta

## Purpose

Lets the user save the state of a powered-off VM and return to it later, as a tree of named snapshots like VirtualBox's Snapshots tool.

## ADDED Requirements

### Requirement: Take a snapshot
The system SHALL take a named snapshot, with an optional description, of a powered-off VM. A snapshot SHALL hold every hard disk of the VM, its UEFI variables, its TPM state and its hardware settings. Taking a snapshot while any QEMU has one of the VM's disks open SHALL be refused with a message that the VM must be powered off.

#### Scenario: Powered-off VM
- **WHEN** the user takes snapshot "Before update" of a powered-off Linux VM
- **THEN** the snapshot appears in the tree with its name, date and description, under the previous current snapshot

#### Scenario: Running VM
- **WHEN** the user tries to take a snapshot of a running VM
- **THEN** nothing is changed and the message says to power the VM off first

### Requirement: Snapshot eligibility
A VM SHALL be eligible for snapshots only when it is not a `reims-ctl` VM, every hard disk is qcow2, and no other VM, or another VM's snapshot, references any of them. A VM with a raw hard disk SHALL be refused, naming each raw disk and offering conversion to qcow2 in the Media manager. Install media (ISO images and macOS `BaseSystem.img`) SHALL NOT be part of a snapshot and SHALL NOT make a VM ineligible.

#### Scenario: Raw disk
- **WHEN** the user takes a snapshot of a VM whose second disk is raw
- **THEN** nothing is changed and the message names the raw disk and points to conversion

#### Scenario: Disk shared with another VM
- **WHEN** the user takes a snapshot of VM A while stopped VM B also uses A's disk
- **THEN** nothing is changed and the message names B

#### Scenario: macOS VM with recovery media
- **WHEN** the user takes a snapshot of a powered-off macOS VM with `BaseSystem.img` present
- **THEN** the snapshot is taken from `mac_hdd_ng.img`, the UEFI variables and the settings, and `BaseSystem.img` is left as it is

### Requirement: Snapshot tree
Each VM SHALL have its own snapshot tree. The Snapshots tool SHALL show it with "Current state" under the snapshot the VM was last restored to or taken from, and SHALL show each snapshot's name, date and description. Two VMs SHALL never share snapshots.

#### Scenario: Branch
- **WHEN** the user restores snapshot A, then takes snapshot C, while snapshot B was taken after A
- **THEN** B and C are both children of A and "Current state" is under C

#### Scenario: Two VMs
- **WHEN** VMs X and Y both have snapshots named "Clean"
- **THEN** each VM's tree shows only its own "Clean", and restoring one never changes the other VM

### Requirement: Restore a snapshot
The system SHALL restore a snapshot of a powered-off VM: its disks, UEFI variables, TPM state and hardware settings return to the snapshot's contents, and disks attached after the snapshot are detached without deleting their files. It SHALL warn that the current state is lost unless saved as a snapshot first, and SHALL offer to take that snapshot.

#### Scenario: Restore
- **WHEN** the user restores "Before update" and confirms without saving the current state
- **THEN** the next boot shows the guest as it was when "Before update" was taken, with the settings it had then

### Requirement: Restore is all or nothing
Take, restore and delete SHALL each either complete for every part of the snapshot or leave the VM and its snapshots exactly as before. An interrupted operation (crash, power loss, killed process) SHALL be completed or rolled back the next time the app, CLI or MCP server opens the VM, before anything else uses it; a delete SHALL always be completed once it has started removing data.

#### Scenario: Second disk fails
- **WHEN** restoring a snapshot fails on the VM's second disk after the first disk was restored
- **THEN** the first disk, the UEFI variables, the TPM state and the settings are back to their state before the restore, and the error names the failing disk

#### Scenario: Delete fails on the second disk
- **WHEN** deleting a snapshot fails after its tag was removed from the first disk
- **THEN** reopening the VM finishes the delete, and it never shows a snapshot that cannot restore

#### Scenario: Killed during restore
- **WHEN** the process is killed while copying the TPM state during a restore and the app is opened again
- **THEN** before the VM can start, the restore is either finished or rolled back, and the VM boots consistently

### Requirement: Delete a snapshot
The system SHALL delete a snapshot of a powered-off VM after confirmation; its children SHALL become children of its parent, and the other snapshots and the current state SHALL stay unchanged.

#### Scenario: Delete a middle snapshot
- **WHEN** the user deletes snapshot B whose parent is A and whose child is C
- **THEN** C becomes a child of A, and A, C and the current state still restore and boot as before

### Requirement: Snapshots in the CLI and MCP server
The CLI and the MCP server SHALL list, take, restore and delete snapshots with the same rules as the GUI.

#### Scenario: CLI list
- **WHEN** a user runs `kvm-gui snapshot list "Ubuntu"`
- **THEN** it prints the snapshot tree with names, dates and the current state
