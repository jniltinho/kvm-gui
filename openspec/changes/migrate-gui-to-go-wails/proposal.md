# Proposal

## Why

The repository has two desktop apps for one job: the Go/Fyne app (`internal/gui`) and the Rust/egui `reims-gui`. Both are hand-drawn widget toolkits, so every VirtualBox-style screen costs bespoke layout code, twice, and the Go app still needs a Rust static library to embed Reims vGPU's window. The author's goXterm project has already moved the same way (Go + Wails v3 + Vue 3 + Tailwind, one binary that is desktop, CLI and MCP; Rust archived), and the repository is now `kvm-gui`: the next step is one Go app, with a web UI that reaches VirtualBox's Manager parity, that creates both macOS VMs (with Reims vGPU) and ordinary KVM VMs (Linux, Windows, other).

## What Changes

- **New desktop GUI in Go + Wails v3** (Vue 3, Vite Plus `vp`, Tailwind), following goXterm's layout: Go at the repository root, `internal/gui` the only package that imports Wails, the SPA in `frontend/` embedded in the binary. The Fyne GUI is replaced when the parity gate passes.
- **VirtualBox Manager parity** (workflow-equivalent, own look): Manager window with menus, toolbar, VM list and Details / Snapshots / Logs tools; New VM wizard (guided and expert); Settings pages (General, System, Display, Storage, Audio, Network, Serial Ports, USB, Shared Folders); Preferences; Media manager; Clone; Export/Import; a VM window per running VM with Machine / View / Input / Devices menus, status bar and the close dialog. VirtualBox (`VirtualBox/virtualbox`), virt-manager and webvirtmgr are **references for behavior and device defaults only**: no code, icons, strings or layouts are copied from the GPL projects.
- **Ordinary KVM guests**: Linux, Windows 10/11 and Other guest types next to macOS, with per-OS device profiles (virtio or emulated disk/NIC, display, UEFI or BIOS, Secure Boot and TPM 2.0 through `swtpm` for Windows 11), ISO install media, offline guest-type detection from the ISO, and the same CLI and MCP coverage as macOS VMs.
- **Several VMs at once**: VMs run as detached processes (as the MCP server already starts them), so closing the Manager leaves them running and reopening it finds them. **BREAKING** for the rule "the desktop app runs at most one VM at a time".
- **Snapshots** (offline, stopped VM): take, restore, delete, shown as a tree with "Current state".
- **Screen**: the VM's VNC frames, decoded in Go, are drawn on a `<canvas>` over an authenticated loopback stream; Reims vGPU's window is embedded in the VM window through a **pure-Go X11** implementation, with the existing fallback to its own window. No cgo, no Rust.
- **Rename to `kvm-gui`**: binary, desktop entry and window title; it behaves the same when invoked as `macos-kvm-gui` and reads the same data (`~/.config/macos-kvm-gui/`, database, `settings.yaml`, `MKG_*` variables, host files under `/etc`).
- **Release artifacts**: an AppImage, a plain `linux-amd64` binary, a `.deb` (Ubuntu 24.04, Ubuntu 26.04, Debian 13) and an `.rpm` (Rocky Linux 10), with `SHA256SUMS`. The `kvm-gui` packages replace `macos-kvm-gui` on upgrade and install a `macos-kvm-gui` command link. **BREAKING**: no more `.tar.gz`; Fedora is no longer a target.
- **Local database**: unchanged, one SQLite file through the pure-Go `modernc.org/sqlite` driver (no cgo, no SQLite library on the host), migrated to schema 6 with an automatic backup.
- **Rust archived**: `reims-gui/` moves read-only to `archive/rust/reims-gui/`; its CI and release workflows are disabled at once; the Go build stops linking `rg-x11-ffi`. **BREAKING**: no further `reims-gui-v*` releases. The only Rust left is upstream Reims vGPU's QEMU, packaged from its own folder `reims/` (Dockerfiles, manifest, packaging script) by the existing `reims-<commit>` releases (today `reims-c099dd5`).
- **macOS VMs on Reims vGPU through upstream `reims-ctl`**: new Reims VMs are created (installer from Apple, OpenCore image, disk) by `reims-ctl`, and the app runs the QEMU command `reims-ctl command` prints, adding its own VNC and QMP sockets for that run only; Add registers VMs made in `reims-ctl`. The app's own OpenCore path stays for VMware SVGA, GPU passthrough, older versions and existing VMs. Reims packages are rebuilt from the newest `steelbrain/reims-vgpu` commit and now carry `reims-ctl` and `reims-util`. `steelbrain/reims-vgpu` is the base reference in the docs.
- **Files in the XDG layout**: configuration in `~/.config/kvm-gui`, database and Reims builds in `~/.local/share/kvm-gui`, downloads in `~/.cache/kvm-gui`, logs in `~/.local/state/kvm-gui`, new VMs in `~/KVM-VMs`. A first start copies `~/.config/macos-kvm-gui` (leaving it untouched); VMs in `~/macOS-VMs` and `~/OSX-KVM` stay where they are. `reims-ctl` runs with its folders inside kvm-gui's.
- **One binary, Cobra**: GUI, CLI and MCP server are one Cobra command tree with flags; the Fyne GUI is deleted at once, not kept in between.
- **Skins as in goXterm**: `frontend/src/themes/` with the same registry and one CSS file per skin: Light, Dark, Windows XP, macOS, macOS Dark, Mac OS Retro and System, with a check that every skin defines the same tokens.
- **Docker only**: every build, test and package runs in one Docker image; the host needs only Docker and make. After each milestone, `make dist` writes the artifacts to `dist/`.
- The in-progress change `embed-reims-window-go` (Fyne + Rust FFI) is superseded; its requirements move into this change's `screen-embedding` capability.

## Capabilities

### New Capabilities

- `file-layout`: where the app keeps configuration, data, cache, logs, runtime files and VMs (XDG, `~/KVM-VMs`), `reims-ctl`'s folders inside them, the variable names, and the migration from `macos-kvm-gui`.

- `desktop-app`: the Wails application itself: one `kvm-gui` binary (GUI, CLI, MCP), embedded SPA, Docker-only build, AppImage, binary, `.deb` and `.rpm` artifacts with their licenses, the local SQLite database, supported distributions, data compatibility and database backup, themes and HiDPI, accessibility for agent-driven checks, the loopback stream's security, release checks, and the Go-only repository (Rust GUI archived, Rust CI disabled).
- `vm-manager-ui`: the VirtualBox-parity screens and workflows: Manager window, tools, Settings pages, Preferences, Media manager, VM window menus and close dialog, Activity, the parity matrix and gate.
- `kvm-guests`: Linux, Windows and Other guests: guest types and OS profiles, firmware, Secure Boot, TPM, disks and optical media, ISO detection, and their CLI/MCP use.
- `vm-snapshots`: offline snapshots of a VM's disks, NVRAM and TPM state.
- `screen-embedding`: Reims vGPU's window inside the VM window on X11, carried over from `embed-reims-window-go` and re-targeted to the Wails VM window.

### Modified Capabilities

- `vm-management`: the theme preference offers the six skins and System; several running VMs; the New VM wizard covers every guest type; adds Clone, disk images in use by file identity, and the files a VM owns for move, export, import and delete.
- `vm-lifecycle`: Start and UEFI variable persistence cover macOS and point to `kvm-guests` for other guests; closing the app no longer stops or blocks on a running VM (detached runs), while `vm start` in a terminal stays in the foreground.
- `networking`: adapter types and defaults per guest type (`e1000e`, `e1000` for ordinary guests).
- `host-preflight`: Set up host with `dnf` on Rocky Linux 10; checks for `swtpm` and Secure Boot firmware.
- `gpu-passthrough`: one passthrough VM at a time next to other running VMs; `dracut` on Rocky Linux 10.
- `embedded-display`: the screen lives in a VM window per running VM (VirtualBox style) with a close dialog, instead of a Manager tab plus an optional separate window.
- `reims-vgpu`: Display targets the VM window; packages install into `reims-ctl`'s layout and carry `reims-ctl`; adds the packaging folder, the latest upstream build, macOS VMs created and run through `reims-ctl`, Add for existing `reims-ctl` VMs, and the existing pipeline for other macOS VMs.
- `mcp-server`: the server is `kvm-gui mcp` (also through a `macos-kvm-gui` link); the GUI and one other process attach to a run at the same time, each with its own QMP socket.
- `reims-gui`: all requirements removed; the Rust app is archived.
- `full-installer`, `boot-assets`: installers cached in `~/.cache/kvm-gui/installers`; the runtime asset directory read from `KVM_GUI_ASSETS_DIR` and `~/.config/kvm-gui`.

## Impact

- **Code**: new `internal/gui` (Wails services), `frontend/` (Vue SPA), `build/` (Docker image, desktop file, icons); `internal/core` gains guest types, OS profiles, `swtpm`, ISO detection, snapshots, clone, schema 6; `internal/embed` becomes pure-Go X11 (`github.com/jezek/xgb`, BSD-3-Clause); `cmd/` and `internal/mcp` learn guest types; the Fyne GUI is deleted at cutover.
- **Moved**: `reims-gui/` → `archive/rust/reims-gui/`; `internal/core/reims/` and `scripts/reims-package.sh` → `reims/`.
- **CI/release**: `reims-gui-ci.yml` and `reims-gui-release.yml` removed and a Rust-free `ci.yml` on push and PR added at once; `go-release.yml` publishes the AppImage, the binary, the `.deb`, the `.rpm` and `SHA256SUMS` from a green commit and marks them latest; `reims-release.yml` reads `reims/`.
- **Dependencies**: Wails v3, Vue 3, Tailwind, Vite Plus (Docker only); on the host, GTK 4 and WebKitGTK 6.0 runtime libraries, QEMU, OVMF and `swtpm` from the distribution; `fyne.io/fyne/v2` dropped. glibc floor becomes 2.39 (Ubuntu 24.04 build host).
- **Docs**: README, AGENTS.md, `docs/` (architecture, development, getting started, Reims vGPU), a new VirtualBox parity matrix, screenshots from the real app; `THIRD_PARTY_NOTICES.md` gains the frontend and Go dependencies.
