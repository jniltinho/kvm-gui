# Linux, Windows and other guests

[Documentation index](README.md)

Next to macOS VMs, kvm-gui creates and runs ordinary KVM VMs from an installer ISO. Each VM has a
guest type (`linux`, `windows`, `other`) and an OS profile with device defaults modeled on
virt-manager's (libosinfo):

| Profile | Firmware | Disk | Network | Graphics | Extras |
|---|---|---|---|---|---|
| `ubuntu`, `debian`, `fedora`, `rocky`, `arch`, `opensuse`, `linux` | UEFI | virtio | virtio-net-pci | virtio-vga (3D optional) | virtio-rng |
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

## Display: resolution, 3D acceleration and Vulkan

**Resolution** (Settings › Display, `config set resolution=2560x1440`) is the screen size the guest
starts with, given to it as its display's preferred mode (EDID): HD (1280×720) to 4K UHD
(3840×2160), ultrawide sizes included. The guest can still change it in its own display settings.

**Enable 3D acceleration** (Settings › Display, under the Paravirtualized controller;
`config set graphics=virtio-vga-gl`) gives Linux and Other guests virtio-gpu with virgl: the
guest's OpenGL is drawn by a GPU of the host, with no passthrough, so the host keeps the GPU and
its monitors. The screen stays in the app (QEMU's `egl-headless` display feeds the VNC socket) or,
with Screen set to QEMU's window, in a GTK window drawn with OpenGL. It needs QEMU's OpenGL modules
(`qemu-system-modules-opengl` on Ubuntu and Debian) and access to `/dev/dri/renderD*` (the desktop
user has it; otherwise the `render` group). Windows has no virgl driver, so the box is off for
Windows VMs. Rocky Linux 10's QEMU has no 3D device, so the box is off there, with the reason.

**Enable Vulkan (Venus)** (under 3D acceleration; `config set vulkan=true`) runs the guest's Vulkan
on the host GPU's own Vulkan driver: games and apps that need Vulkan, with no passthrough. The
guest needs Mesa's virtio Vulkan driver (`mesa-vulkan-drivers`; Ubuntu 24.04 and later guests have
it). `vulkaninfo` in the guest then lists `Virtio-GPU Venus (<the host GPU>)`. The host needs QEMU
with Venus, virglrenderer's render server (`virgl_render_server`) and a Vulkan loader that can
filter devices. Ubuntu 26.04 has all three. Debian 13 lacks the render server and the loader filter,
so Vulkan is off there, with the reasons. The VM's memory is shared with the host's renderer (a
memfd), and turning 3D off turns Vulkan off.

**Host GPU** (under 3D acceleration; `config set render-gpu=0000:0c:00.0`, or `auto`) picks the GPU
that draws, by PCI address (`kvm-gui gpu list` shows each GPU's render node and whether it has a
Vulkan driver). Automatic takes the first GPU with a Mesa driver (AMD, Intel, nouveau), else the
first one. With Vulkan on, the guest sees only that GPU's Vulkan device. Two GPUs of the same
model are the exception: both reach the guest's Vulkan, since the filter works by vendor and device
ID, and OpenGL still draws on the chosen one. In QEMU's own window, OpenGL draws on the desktop's
GPU and Vulkan is not offered (that window cannot show another GPU's Vulkan output, and QEMU crashed
there in testing): Vulkan runs in the app's screen, and choosing QEMU's window turns it off. A VM whose GPU is gone (removed, or reserved for
passthrough) does not start until you choose another one.

## Import a VirtualBox appliance

`kvm-gui vm import-ova lab.ova` (or the MCP tool `vm_import_ova`) turns an OVF appliance, as
VirtualBox's File › Export Appliance writes it, into a Linux, Windows or Other VM: the OS type
picks the profile, then memory and processors (within this host's), firmware and Secure Boot
(from VirtualBox's own record in the appliance), disks converted to qcow2 on the controller the
appliance used when kvm-gui has it (SATA, NVMe; IDE and SCSI become SATA, virtio-scsi becomes
virtio-blk, each with a note), empty optical drives and network adapters with new MAC addresses.
Run it with `--dry-run` first: it lists what is not kept, such as bridged adapters (NAT unless
`--bridge` names a host bridge), UEFI boot entries (the guest starts from its fallback loader),
TPM state (a BitLocker volume asks for its recovery key) and floppy drives.

The appliance is treated as untrusted: only plain files with flat names, the descriptor first, a
manifest that must match when present, and disks that stand alone (a VMDK that is one file, a
qcow2 without a backing file) whose real size is the one declared; kvm-gui reads each disk's header
itself before `qemu-img` opens it. A refused or failed import leaves nothing behind.

## Export to VirtualBox

`kvm-gui vm export-ova "Ubuntu lab" ~/ubuntu-lab.ova` (File › Export Appliance…, or the MCP tool
`vm_export_ova`) writes a powered-off Linux, Windows or Other VM as an `.ova` that VirtualBox 7
imports: an OVF 1.0 descriptor with VirtualBox's OS type, the VM's processors, memory, firmware
(EFI included), TPM 2.0 and adapters, its disks as streamOptimized VMDKs (on an NVMe controller
for NVMe disks, on SATA for the others), and a SHA-256 manifest. It does not carry snapshots (only
each disk's current state), TPM contents (a VM with a TPM needs `--accept-tpm-loss`: have the
BitLocker recovery key), UEFI boot entries, or Secure Boot (turn it on again in VirtualBox). A
virtio disk here goes on SATA there, so the guest needs its AHCI driver at boot, which the usual
distributions and Windows have. An existing file is replaced only with `--force`, and never a
file of the VM's own folder.

## Validated

- Appliances with VirtualBox 7.2.10 on the reference host (in an isolated VirtualBox configuration):
  Ubuntu and Windows 11 VMs exported by `vm export-ova` imported with their OS type, EFI, CPUs,
  memory, SATA disk, NAT adapter and TPM, the disks identical to the source; VirtualBox's own
  `.ova` imported by `vm import-ova` with an identical disk.
- Vulkan (Venus) on the reference host (Ubuntu 26.04, QEMU 10.2.1, virglrenderer 1.2.0, Mesa
  26.0.8): an Ubuntu 26.04 guest started by `kvm-gui vm start` with 3D and Vulkan listed only
  `Virtio-GPU Venus (AMD Radeon RX 6700 XT (RADV NAVI22))`, its OpenGL was `virgl (AMD Radeon RX
  6700 XT …)`, and `vkcube` rendered in the app's VNC screen. With the host GPU set to the RTX 4070
  (NVIDIA 580), it showed `Virtio-GPU Venus (NVIDIA GeForce RTX 4070)` and `virgl (NVIDIA GeForce
  RTX 4070/PCIe/SSE2)` instead. A VM naming a GPU the host does not have was refused before QEMU
  started.
- 3D acceleration on the reference host (Ubuntu 24.04, QEMU 8.2.2, virglrenderer 1.0): the
  Kubuntu 24.04.2 live ISO started from `kvm-gui vm start` with `graphics=virtio-vga-gl` showed its
  welcome screen at 1920×1080 over the VNC socket, QEMU drawing on the RX 6700 XT's render node.

On the reference host (Ubuntu 24.04, KVM), in the build image with `/dev/kvm`
(`go test -tags integration -run RealGuest ./internal/core`):

- Ubuntu Server 24.04.2 with Secure Boot on: the signed shim and GRUB start and the installer
  reaches its language screen.
- Secure Boot is enforced: an unsigned EFI program runs with Secure Boot off and is refused with
  it on ("Access Denied").
- Windows 11 22H2 with Secure Boot and a TPM: setup passes its hardware check and shows the
  license terms.
