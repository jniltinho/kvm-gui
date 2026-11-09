# Proposal

## Why

The reference host moved to Ubuntu 26.04 (QEMU 10.2.1, virglrenderer 1.2 with Venus, Mesa 26), and the user wants it to be the oldest Ubuntu kvm-gui supports. A newer QEMU also brings guest Vulkan: on QEMU 10.0 and later, virtio-gpu's Venus lets a Linux guest use the host GPU's Vulkan driver without passthrough. Today a Linux guest has only OpenGL (virgl), drawn on whichever Mesa GPU kvm-gui finds first, and the host has three GPUs (RX 6700 XT, RTX 4070, the Ryzen iGPU).

## What Changes

- **BREAKING** Ubuntu 24.04 is no longer a supported host. The supported hosts are Ubuntu 26.04, Debian 13 and Rocky Linux 10. The `.deb` targets Ubuntu 26.04 and Debian 13, the `.rpm` targets Rocky Linux 10, and CI, the clean-distribution checks, the GNOME checks and the Reims vGPU packages drop 24.04. The build image stays on Ubuntu 24.04 only because its glibc 2.39 is the oldest of the three hosts (Rocky Linux 10's).
- **BREAKING** The system QEMU must be 10.0 or later (Debian 13 has 10.0, Rocky Linux 10 has 10.1, Ubuntu 26.04 has 10.2). The host checks fail below that and name the supported hosts, and an unsupported distribution or release shows a warning.
- Linux and Other VMs with 3D acceleration can turn on **Vulkan (Venus)**: the guest's Vulkan runs on the host GPU through virtio-gpu (`venus`, `blob`, `hostmem`, guest memory on a shared memfd). It is offered only where the host's QEMU and virglrenderer can do it; elsewhere, such as Rocky Linux 10, whose QEMU has no `virtio-vga-gl`, 3D and Vulkan show as unavailable with the reason.
- A VM with 3D acceleration can choose the **host GPU** that draws it (and runs its Vulkan), or keep the automatic choice. The choice is stored by the GPU's PCI address, so it survives render nodes being renumbered. A start whose GPU is gone is refused, naming the GPU and the setting.
- Settings › Display, the CLI (`config set`) and the MCP server (`vm_config_set`) expose both options; `gpu list` shows which GPUs can draw 3D.

## Capabilities

### New Capabilities
- `guest-3d`: 3D acceleration of Linux and Other guests on a host GPU without passthrough: OpenGL (virgl), Vulkan (Venus), the host GPU choice, what the host must have, and how each surface shows it.

### Modified Capabilities
- `host-preflight`: the QEMU minimum becomes 10.0, a warning for unsupported host distributions, and Set up host's package lists without Ubuntu 24.04.
- `desktop-app`: the supported distributions become Ubuntu 26.04, Debian 13 and Rocky Linux 10.
- `reims-vgpu`: packages, install and Docker builds for Ubuntu 26.04, Debian 13 and Rocky Linux 10, without Ubuntu 24.04.

## Impact

- Code: `internal/core` (`guest.go` arguments and `RenderNode`, `vm.go` `runArgs`, `guest_run.go` start checks, `host.go` checks and setup packages, `reimspkg.go`, the GPU listing used by `gpu list`), `internal/gui` (catalog of host GPUs, Settings bindings), `frontend/src/VMSettings.vue`, `cmd` (`config set` keys, `gpu list`), `internal/mcp` (`vm_config_set` keys).
- Packaging and checks: `scripts/check-dist.sh` (no `ubuntu24.04`; the `macos-kvm-gui` 0.13.0 upgrade check moves to Ubuntu 26.04), `scripts/check-gnome.sh` (the X11 embedding check moves to Debian 13, whose GNOME 48 still has an X11 session; Ubuntu 26.04 stays Wayland only), `scripts/package.sh`, `.github/workflows/ci.yml`, `reims-release.yml`, `reims-upstream.yml`, `reims/Dockerfile`, `reims/manifest.json`.
- Docs: `README.md`, `AGENTS.md` (reference host: Ubuntu 26.04, KDE Plasma on Wayland), `docs/getting-started.md`, `docs/guests.md`, `docs/cli.md`, `docs/mcp.md`, `docs/reims-vgpu.md`, `docs/troubleshooting.md`, `docs/development.md`, the release skill.
- Users on Ubuntu 24.04 keep v0.15.0. The first release with this change says so.
