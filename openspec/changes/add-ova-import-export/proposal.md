# Proposal

## Why

Users move VMs between kvm-gui and VirtualBox (the reference host has VirtualBox 7.2.10 with a dozen VMs, and Vagrant's VirtualBox boxes), and VirtualBox's File › Import/Export Appliance is the way they expect to do it. kvm-gui's own bundles (`.kvmbundle`) only round-trip between kvm-gui hosts, so a VirtualBox VM cannot come in and a kvm-gui VM cannot go out; the VirtualBox parity matrix (`docs/parity-virtualbox.md`, "Import appliance" and "Export appliance") still lists both as open.

## What Changes

- **Import an `.ova`** (OVF 1.0 or 2.0 in a tar, with its disk images and optional manifest) as a new Linux, Windows or Other VM: name, guest type and OS profile, memory, processors, firmware (BIOS or UEFI), disks (converted to qcow2), optical drives, network adapters and graphics mapped to kvm-gui's settings; whatever does not map is reported, never silently guessed. Also an `.ovf` with its files beside it.
- **Export a VM as an `.ova`** that VirtualBox 7.2 imports and boots with the same OS type, memory, processors, firmware, disks (streamOptimized VMDK) and network adapters, plus a manifest; snapshots, TPM state and UEFI variables are not exported, and a VM with a TPM needs an explicit acknowledgment (BitLocker recovery key).
- The archive is untrusted input: member paths, the XML, checksums, disk formats, disks that refer outside themselves (backing files, external extents) and sizes are checked before anything is converted, and a failed import leaves nothing behind.
- Available in the command line (`kvm-gui vm import-ova`, `kvm-gui vm export-ova`), the MCP server (`vm_import_ova`, `vm_export_ova`) and the GUI (File › Import Appliance…, File › Export Appliance…), with progress.
- macOS VMs are out of scope: export refuses them, and an import whose OS is macOS is refused with the reason.

## Capabilities

### New Capabilities

- `vm-appliances`: importing and exporting VMs as OVF appliances (`.ova`, `.ovf`) compatible with VirtualBox, across the core, command line, MCP server and GUI.

### Modified Capabilities

(none: the CLI, MCP and GUI entry points are owned by this capability's requirements)

## Impact

- `internal/core`: a new OVF reader and writer (Go's `encoding/xml`, `archive/tar`, `crypto/sha256`), reusing the bundle code's untrusted-archive checks, `createImported`, `CreateGuestVM` and `qemu-img convert` (`vmdk` in, `qcow2` out; `qcow2` in, streamOptimized `vmdk` out). No new Go module.
- `cmd/` (two `vm` subcommands), `internal/mcp` (two tools), `internal/gui` and `frontend/` (two dialogs and File menu items).
- Docs: `docs/cli.md`, `docs/mcp.md`, `docs/guests.md`, `docs/parity-virtualbox.md` (the two rows closed), README.
- Tests use small generated appliances and hostile images; the VirtualBox checks run on the reference host in an isolated VirtualBox (a throwaway `VBOX_USER_HOME`, its own `VBOX_IPC_SOCKETID` and machine folder, checked empty first), so the user's VirtualBox registry and VMs are never touched; the real import-and-boot round trip runs with the user's OK.
- kvm-gui bundles stay a separate operation with their own dialogs (`migrate-gui-to-go-wails`, task 7.7).
