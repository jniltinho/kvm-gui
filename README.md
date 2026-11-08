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

## Status: moving to one Go app

kvm-gui is becoming one Go application: a desktop GUI in Go + [Wails](https://wails.io) (Vue 3, Vite Plus, Tailwind), with VirtualBox-like workflows, that creates macOS VMs (with [Reims vGPU](https://github.com/steelbrain/reims-vgpu)) and ordinary KVM VMs (Linux, Windows), plus the command line and the MCP server, in one binary. The plan is the OpenSpec change [`migrate-gui-to-go-wails`](openspec/changes/migrate-gui-to-go-wails/proposal.md).

On this branch today:

- **The command line and the MCP server** work: create, configure, start and stop macOS VMs, install macOS, Reims vGPU, GPU passthrough ([command line](docs/cli.md), [MCP server](docs/mcp.md)).
- **The desktop GUI** is being rebuilt in Wails; `kvm-gui gui` says so until it lands. The previous Go/Fyne GUI is gone, and the Rust app `reims-gui` is archived, read-only, in [`archive/rust/reims-gui`](archive/rust/reims-gui/ARCHIVED.md).
- **Releases:** none from this branch yet. The last published apps, `macos-kvm-gui` v0.13.0 and `reims-gui` v0.1.6, stay on the [releases page](https://github.com/jniltinho/kvm-gui/releases) and read the same VMs.

## Reims vGPU

[steelbrain/reims-vgpu](https://github.com/steelbrain/reims-vgpu) is the base of kvm-gui's Reims support: the paravirtual GPU, its QEMU build and `reims-ctl`. Its README and `docs/qemu.md` are the source of truth for the GPU and the guest's setup. This repository packages upstream's QEMU (with `reims-ctl` and `reims-util`) for Ubuntu 24.04, Ubuntu 26.04, Debian 13 and Rocky Linux 10 from [`reims/`](reims/README.md), on `reims-<commit>` releases. More in [docs/reims-vgpu.md](docs/reims-vgpu.md).

## Build

Everything builds and runs in one Docker image; the host needs only Docker and make.

```sh
make image      # the build image (build/linux.Dockerfile)
make test       # gofmt, go vet, go test -race, every needed tool required
make openspec   # openspec validate --all --strict
make dist       # dist/: the .deb, the .rpm and SHA256SUMS
```

## Documentation

[Overview](docs/overview.md) · [Getting started](docs/getting-started.md) · [Command line](docs/cli.md) · [MCP server](docs/mcp.md) · [Architecture](docs/architecture.md) · [GPU passthrough](docs/gpu-passthrough.md) · [Reims vGPU](docs/reims-vgpu.md) · [Troubleshooting](docs/troubleshooting.md) · [Development](docs/development.md) · [References](docs/references.md)

## License

BSD-3-Clause ([LICENSE](LICENSE)). The embedded boot files keep their own licenses, and Reims vGPU's separately published packages theirs ([THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)). Built on the work of [OSX-KVM](https://github.com/kholia/OSX-KVM), [acidanthera](https://github.com/acidanthera), [TianoCore](https://github.com/tianocore/edk2), [Dortania](https://dortania.github.io/) and [steelbrain/reims-vgpu](https://github.com/steelbrain/reims-vgpu); see [references](docs/references.md).
