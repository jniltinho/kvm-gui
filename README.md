<p align="center">
  <img src="internal/assets/logo/logo.svg" width="112" alt="kvm-gui logo">
</p>

<h1 align="center">kvm-gui</h1>

<p align="center">
  <b>Create and run macOS and other VMs on Linux KVM, from one binary.</b><br>
  OpenCore and OVMF are built in; Reims vGPU gives macOS Metal through the host's Vulkan driver.
</p>

<p align="center">
  <a href="https://github.com/jniltinho/kvm-gui/actions/workflows/ci.yml"><img src="https://github.com/jniltinho/kvm-gui/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/jniltinho/kvm-gui/releases/latest"><img src="https://img.shields.io/github/v/release/jniltinho/kvm-gui?color=blue" alt="Latest release"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.27-blue" alt="Go 1.27"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/jniltinho/kvm-gui?color=blue" alt="License"></a>
</p>

> **For home lab use.** macOS is Apple's software, and its license only allows it on Apple-branded hardware. Make sure your use complies with Apple's macOS license terms. This project is not affiliated with Apple.

## What it is

One binary: a desktop GUI in Go + [Wails](https://wails.io) (Vue 3, Vite Plus, Tailwind) with VirtualBox-like workflows, the [command line](docs/cli.md) and an [MCP server](docs/mcp.md) for AI agents.

- **macOS VMs**, with OpenCore and OVMF built in, or on [Reims vGPU](https://github.com/steelbrain/reims-vgpu) through upstream's `reims-ctl`.
- **Linux, Windows and Other VMs**: OS profiles, ISO detection, UEFI with Secure Boot, TPM 2.0 through swtpm.
- **Snapshots** (offline, journaled), **full clones**, and **bundles** to move a VM between hosts.
- VMs started from the GUI or by an agent keep running when the app closes.

The work is planned in the OpenSpec change [`migrate-gui-to-go-wails`](openspec/changes/migrate-gui-to-go-wails/proposal.md); the Rust app `reims-gui` is archived, read-only, in [`archive/rust/reims-gui`](archive/rust/reims-gui/ARCHIVED.md), and its old builds in the [`archive`](https://github.com/jniltinho/kvm-gui/releases/tag/archive) release.

## Install

**Status:** the Wails GUI is on `main` and not released yet; until its first release, build it with `make dist` (below). The latest release, [v0.13.0](https://github.com/jniltinho/kvm-gui/releases/tag/v0.13.0), is the previous `macos-kvm-gui` (command line and Fyne GUI). The archived Rust app's releases (`reims-gui` 0.1.5 and 0.1.6) are kept, for history only, in the [`archive`](https://github.com/jniltinho/kvm-gui/releases/tag/archive) release.

Each kvm-gui release has five files: an AppImage, a plain binary, a `.deb`, an `.rpm` and `SHA256SUMS`. Check them first:

```sh
sha256sum -c --ignore-missing SHA256SUMS
```

| Distribution | Install |
|---|---|
| Ubuntu 24.04, 26.04, Debian 13 | `sudo apt install ./kvm-gui_X.Y.Z_amd64.deb` |
| Rocky Linux 10 | `sudo dnf install epel-release && sudo dnf install ./kvm-gui-X.Y.Z-1.x86_64.rpm` (WebKitGTK 6 comes from EPEL) |
| AppImage or binary, Ubuntu/Debian | `sudo apt install libgtk-4-1 libwebkitgtk-6.0-4 qemu-system-x86 qemu-utils ovmf pkexec`, then `chmod +x` and run the file |
| AppImage or binary, Rocky Linux 10 | `sudo dnf install epel-release && sudo dnf install gtk4 webkitgtk6.0 qemu-kvm qemu-img edk2-ovmf polkit`, then `chmod +x` and run the file |

The packages replace `macos-kvm-gui` and keep `macos-kvm-gui` as a command name. Each install command above runs in a clean container of its distribution on every CI run (`scripts/check-dist.sh`).

**Upgrading from `macos-kvm-gui`.** The first start copies the old settings and VM list from `~/.config/macos-kvm-gui` to `~/.config/kvm-gui` and `~/.local/share/kvm-gui`, and leaves the old folder untouched; VMs stay in their folders. To be safe, back it up first: `tar czf ~/macos-kvm-gui-backup.tgz -C ~/.config macos-kvm-gui`. To roll back, reinstall the old package (`sudo apt install ./macos-kvm-gui_0.13.0_amd64.deb`): it reads its own untouched folder, so VMs created or changed after the upgrade are not in its list.

## Reims vGPU

[steelbrain/reims-vgpu](https://github.com/steelbrain/reims-vgpu) is the base of kvm-gui's Reims support: the paravirtual GPU, its QEMU build and `reims-ctl`. Its README and `docs/qemu.md` are the source of truth for the GPU and the guest's setup. This repository packages upstream's QEMU (with `reims-ctl` and `reims-util`) for Ubuntu 24.04, Ubuntu 26.04, Debian 13 and Rocky Linux 10 from [`reims/`](reims/README.md), on `reims-<commit>` releases. More in [docs/reims-vgpu.md](docs/reims-vgpu.md).

## Build

Everything builds and runs in one Docker image; the host needs only Docker and make.

```sh
make image      # the build image (build/linux.Dockerfile)
make test       # gofmt, go vet, go test -race, every needed tool required
make openspec   # openspec validate --all --strict
make self-test  # the GUI opens and loads the VM list, under Xvfb
make dist       # dist/: the AppImage, the binary, the .deb, the .rpm and SHA256SUMS
scripts/check-dist.sh ubuntu24.04 all   # the artifacts on a clean distribution (also ubuntu26.04, debian13, rocky10)
```

## Documentation

[Overview](docs/overview.md) · [Getting started](docs/getting-started.md) · [Command line](docs/cli.md) · [MCP server](docs/mcp.md) · [Architecture](docs/architecture.md) · [GPU passthrough](docs/gpu-passthrough.md) · [Reims vGPU](docs/reims-vgpu.md) · [Troubleshooting](docs/troubleshooting.md) · [Development](docs/development.md) · [References](docs/references.md)

## License

BSD-3-Clause ([LICENSE](LICENSE)). The embedded boot files keep their own licenses, and Reims vGPU's separately published packages theirs ([THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)). Built on the work of [OSX-KVM](https://github.com/kholia/OSX-KVM), [acidanthera](https://github.com/acidanthera), [TianoCore](https://github.com/tianocore/edk2), [Dortania](https://dortania.github.io/) and [steelbrain/reims-vgpu](https://github.com/steelbrain/reims-vgpu); see [references](docs/references.md).
