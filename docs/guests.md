# Linux, Windows and other guests

[Documentation index](README.md)

Next to macOS VMs, kvm-gui creates and runs ordinary KVM VMs from an installer ISO. Each VM has a
guest type (`linux`, `windows`, `other`) and an OS profile with device defaults modeled on
virt-manager's (libosinfo):

| Profile | Firmware | Disk | Network | Graphics | Extras |
|---|---|---|---|---|---|
| `ubuntu`, `debian`, `fedora`, `rocky`, `arch`, `opensuse`, `linux` | UEFI | virtio | virtio-net-pci | virtio-vga | virtio-rng |
| `windows10` | UEFI | SATA | e1000e | VGA | Hyper-V enlightenments |
| `windows11` | UEFI + Secure Boot | SATA | e1000e | VGA | TPM 2.0, Hyper-V enlightenments |
| `other` | BIOS | SATA | e1000 | VGA | |

Windows uses emulated disk and network by default so its setup needs no driver disc; switch to
virtio and attach a virtio-win ISO as a second drive if you want the paravirtual devices.

## Create

```sh
kvm-gui vm create Ubuntu --guest linux --profile ubuntu --iso ~/ISOs/ubuntu-24.04.2-live-server-amd64.iso --disk 40
kvm-gui vm create "Windows 11" --guest windows --profile windows11 --iso ~/ISOs/Win11_22H2_x64v2.iso
kvm-gui vm start --vm Ubuntu
```

The MCP server's `vm_create` takes the same choices (`guest`, `profile`, `iso`, `disk_gib`,
`firmware`, `secure_boot`, `tpm`). The new VM's folder is `~/KVM-VMs/<name>` with its disk
(`disk0.qcow2`); it boots the ISO first while one is in the drive (`config set iso=` ejects it).

**ISO detection.** The wizard reads the ISO's volume label offline and proposes the profile:
Ubuntu, Kubuntu and other flavours, Debian, Fedora, Rocky/RHEL/AlmaLinux, Arch, openSUSE, and
Windows. Windows 10 and Windows 11 media share the label `CCCOMA_X64FRE_*`, so they get Windows 11
and you pick Windows 10 when it is that; Windows Server media (`SSS_X64FRE_*`) get Windows 10.

## Firmware, Secure Boot and TPM

- UEFI VMs boot the host's OVMF (`ovmf` on Ubuntu and Debian, `edk2-ovmf` on Rocky Linux); the VM
  keeps its own UEFI variables in `OVMF_VARS.fd`, made once from the matching template.
- Secure Boot uses the firmware with Microsoft's keys. After turning it on or off, reset the
  variables (boot entries stored in them are lost): `kvm-gui config reset-nvram --yes`.
- A TPM 2.0 is emulated by `swtpm`, with its state in the VM's `tpm/` folder (readable only by
  you). Install `swtpm` (`sudo apt install swtpm` or `sudo dnf install swtpm`).
- `kvm-gui check` reports both as Warnings when they are missing.

## Other settings

`config set` keys for these VMs: `firmware`, `secure-boot`, `tpm`, `boot-order` (`cdrom`, `disk`),
`serial` (`file` writes `serial.log` in the VM folder, `socket` a Unix socket in the run's private
folder), `description` and `iso`. Linux VMs can share host folders over 9p (tag-mounted in the
guest); USB devices can be passed through by vendor and product id when your user can open their
device node (kvm-gui never changes host permissions). A device the host's QEMU lacks (for example
Rocky Linux's `qemu-kvm` has no `vmware-svga` and no 9p) stops the start with the setting to change.

## Validated

On the reference host (Ubuntu 24.04, KVM), in the build image with `/dev/kvm`
(`go test -tags integration -run RealGuest ./internal/core`):

- Ubuntu Server 24.04.2 with Secure Boot on: the signed shim and GRUB start and the installer
  reaches its language screen.
- Secure Boot is enforced: an unsigned EFI program runs with Secure Boot off and is refused with
  it on ("Access Denied").
- Windows 11 22H2 with Secure Boot and a TPM: setup passes its hardware check and shows the
  license terms.
