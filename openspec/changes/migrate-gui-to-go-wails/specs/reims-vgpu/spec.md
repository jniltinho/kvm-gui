# Spec Delta

## MODIFIED Requirements

### Requirement: Display
A `reims-vgpu` VM's screen SHALL be Reims vGPU's own window (QEMU with `-display none`, `REIMS_VGPU_WINDOW` not set to 0), whatever the VM's Display setting. When the GUI started the run and can embed it (see the `screen-embedding` capability), that window SHALL be shown inside the VM window. Otherwise it SHALL stay on the desktop and the VM window SHALL say that the screen is in Reims vGPU's window. In both cases the app's VNC socket SHALL still be served: the Preview and the MCP server's screen capture and input SHALL keep working, for runs started by the GUI, the CLI or the MCP server.

#### Scenario: Start from the app
- **WHEN** a `reims-vgpu` VM starts with Normal from the GUI on X11
- **THEN** the guest shows inside the VM window, and the Preview updates

#### Scenario: Start from the app without embedding
- **WHEN** a `reims-vgpu` VM starts from the GUI on a session without X11 or XWayland
- **THEN** a Reims vGPU window shows the guest, the Preview updates, and the VM window says the screen is in that window

#### Scenario: Started by the MCP server
- **WHEN** an MCP client starts a `reims-vgpu` VM and calls `screen_capture`
- **THEN** the capture shows the guest, and the Manager shows the VM running with its Preview, without embedding it

### Requirement: Reims QEMU build
The app SHALL NOT embed or ship Reims vGPU; it installs it only when the user asks (Package install, Docker build). A VM with `reims-vgpu` SHALL run the `qemu-system-x86_64` of the user's Reims install: `reims_qemu` in `settings.yaml` when set, otherwise `~/.local/share/kvm-gui/reims-ctl/qemu/current/bin/qemu-system-x86_64` (under `$XDG_DATA_HOME`), otherwise `<old Workspace>/.reims/qemu/current/bin/qemu-system-x86_64` from an install made by `macos-kvm-gui`. Other VMs SHALL keep using the system QEMU.

#### Scenario: Not installed
- **WHEN** a VM with `reims-vgpu` starts and no Reims QEMU build is found
- **THEN** it does not start, and the error names the paths searched and points to Install Reims vGPU

### Requirement: Package install
The app SHALL embed a manifest naming the reims release tag and each package's SHA-256, and install the package for the host's platform (Ubuntu 24.04, Ubuntu 26.04, Debian 13, Rocky Linux 10) from **Install Reims vGPU** and `reims install`: download over HTTPS, verify the SHA-256, unpack its QEMU, `reims-ctl` and `reims-util` together into a staged build folder `~/.local/share/kvm-gui/reims-ctl/qemu/<build>` (under `$XDG_DATA_HOME`; no path may leave it, no links outside it), then activate it under the same rules as a build; `~/.local/share/kvm-gui/bin/` SHALL hold only links through `current`. A host without a package SHALL get an error offering the Docker build.

#### Scenario: Install on Ubuntu 24.04
- **WHEN** the user installs on an Ubuntu 24.04 host
- **THEN** the 24.04 package is downloaded and verified, `~/.local/share/kvm-gui/reims-ctl/qemu/current/bin/qemu-system-x86_64` lists `apple-gfx-pci`, and `~/.local/share/kvm-gui/bin/reims-ctl doctor` runs with kvm-gui's `reims-ctl` folders

#### Scenario: Tampered download
- **WHEN** the downloaded package does not match the manifest's SHA-256
- **THEN** nothing is unpacked and `current` is unchanged

#### Scenario: Unsupported release
- **WHEN** the host is a distribution without a package, such as Fedora
- **THEN** the install stops naming the supported platforms and the Docker build

### Requirement: Docker build
The app SHALL embed a build recipe and build upstream reims-vgpu at the pinned commit with upstream's `scripts/install-ctl.sh` inside a container of the host's platform (Ubuntu 24.04, Ubuntu 26.04, Debian 13, Rocky Linux 10), from **Build Reims vGPU** and `reims build [--release <platform>]`, showing progress, producing QEMU, `reims-ctl` and `reims-util`. The platform SHALL default to the host's; another host stops the build naming the supported platforms. Docker missing or unusable SHALL stop the build with the fix.

#### Scenario: Build on Ubuntu 24.04
- **WHEN** the user builds on an Ubuntu 24.04 host with Docker
- **THEN** `~/.local/share/kvm-gui/reims-ctl/qemu/current/bin/qemu-system-x86_64` exists, runs on the host, and lists `apple-gfx-pci`, and `~/.local/share/kvm-gui/bin/reims-ctl` runs

#### Scenario: Unsupported host release
- **WHEN** the host is Ubuntu 22.04 and no release is given
- **THEN** the build stops before starting Docker and names the supported platforms

### Requirement: Build activation
Installed packages and builds SHALL live in kvm-gui's data folder, each build as one versioned folder `reims-ctl/qemu/<build>` holding its QEMU, `reims-ctl` and `reims-util`, with `qemu/current` naming the active one and `bin/reims-ctl` and `bin/reims-util` resolving through `current`; the build cache SHALL be in the cache folder and nothing SHALL be installed elsewhere on the host. A build SHALL be activated only when, on the host, the libraries of all three programs resolve, `reims-ctl` runs and QEMU's device list has `apple-gfx-pci`; otherwise `current` SHALL stay as it was, with the error listing what is missing. A build for another platform SHALL stay staged.

#### Scenario: Missing host library
- **WHEN** the staged QEMU needs a library the host lacks
- **THEN** `current` is unchanged, the active QEMU, `reims-ctl` and `reims-util` are unchanged, and the error names the library and the package of the host's distribution that provides it

### Requirement: Prebuilt packages
This repository SHALL publish, on a release tagged `reims-<commit>`, one package per supported platform (`ubuntu24.04`, `ubuntu26.04`, `debian13`, `rocky10`) named `reims-qemu-<commit>-<platform>-x86_64.tar.gz` with a `.sha256` file, built from upstream at that commit in a container of that platform and stripped, each holding QEMU, `reims-ctl` and `reims-util`.

#### Scenario: Release
- **WHEN** a `reims-<commit>` tag is pushed
- **THEN** the release holds the four packages and their SHA-256 files, and each package's three programs run in a clean container of its platform

### Requirement: Corresponding source
The same release SHALL hold `reims-vgpu-src-<commit>.tar.gz` with everything the packages were built from: upstream at that commit with its `vendor/qemu` and the meson subprojects its build fetches, the vendored Rust crates of every Cargo workspace built (the library, `reims-ctl`, SDL3's source among them), `reims-util` at the revision built with its vendored crates, the build recipes, and the license texts.

#### Scenario: Source archive
- **WHEN** a `reims-<commit>` release is published
- **THEN** it holds the source archive beside the packages, and building the packages from that archive alone, with no network, succeeds

### Requirement: ReimsVgpu.kext download
The app SHALL NOT embed ReimsVgpu.kext. Before a Tahoe VM on `reims-vgpu` started by the app's own pipeline starts (app, `vm start`, MCP `vm_start`), it SHALL download `Contents/Info.plist` and `Contents/MacOS/ReimsVgpu` over HTTPS from steelbrain/reims-util at commit `b302389a957c56fad8e5b3b3b16107932ea9e5e9`, check each against a pinned SHA-256, and cache the kext in `~/.cache/kvm-gui/kexts/reims-util-b302389/ReimsVgpu.kext` only when every file matches. Creating a VM SHALL NOT download it.

#### Scenario: Tampered download
- **WHEN** a downloaded file does not match its pinned SHA-256
- **THEN** the start stops and nothing is cached

## ADDED Requirements

### Requirement: Reims QEMU packaging folder
Every active input that builds and packages Reims vGPU's QEMU (the Dockerfiles, the pinned commit, the manifest of published packages and the packaging script) SHALL live in one top-level `reims/` folder, used by the app, by `reims build` and by the `reims-<commit>` release workflow. Frozen copies under `archive/` SHALL NOT be read by any build or workflow.

#### Scenario: One source of truth
- **WHEN** the pinned Reims commit is changed in `reims/Dockerfile`
- **THEN** the app's Docker build, `reims build` and the release workflow's tag check all use the new commit, and no file outside `reims/` and `archive/` pins a Reims commit

#### Scenario: Existing release
- **WHEN** the app installs Reims vGPU on a supported host
- **THEN** it downloads the package of the `reims-<commit>` release the manifest in `reims/` pins and checks its SHA-256

### Requirement: Latest upstream Reims build
The Reims packages SHALL be built in Docker from the newest commit of `steelbrain/reims-vgpu`'s default branch at the time of the build, with that commit pinned in `reims/`, and SHALL include upstream's `reims-ctl` and `reims-util` next to its QEMU, with the corresponding source published on the same release.

#### Scenario: New pin
- **WHEN** the Reims build is updated
- **THEN** `reims/Dockerfile` pins the newest upstream commit, the `reims-<commit>` release has a package for each supported platform with `qemu-system-x86_64`, `reims-ctl` and `reims-util`, and the source archive

### Requirement: macOS VMs created with reims-ctl
A new macOS VM on Reims vGPU SHALL be created by upstream's `reims-ctl` (installer from Apple, OpenCore image, disk), in the VM's folder in the Workspace, with the name, version, CPUs, memory, disk size, MAC address and SSH port the user chose. The app SHALL show `reims-ctl`'s progress and errors and SHALL register the VM in its own list.

#### Scenario: New Tahoe VM
- **WHEN** the user creates a macOS Tahoe VM on Reims vGPU in the wizard
- **THEN** `reims-ctl create` makes it in `<Workspace>/<name>`, the wizard shows the download progress, and the VM appears in the list with Reims vGPU graphics

#### Scenario: reims-ctl missing
- **WHEN** the user picks Reims vGPU in the wizard and `reims-ctl` is not installed
- **THEN** the wizard offers Install Reims vGPU and creates nothing until it is installed

### Requirement: Running reims-ctl VMs from the app
The app SHALL run a `reims-ctl` VM's QEMU itself, as the child of its own run owner, from the command `reims-ctl command` prints for that VM, adding its VNC and QMP sockets in the run's private directory for that run only, never saving them in `reims-ctl`'s settings. Start, shutdown, force stop, pause and run records SHALL work as for every other VM.

#### Scenario: Start and Preview
- **WHEN** the user starts a `reims-ctl` VM from the Manager
- **THEN** the Preview updates, an MCP `screen_capture` returns its screen, and `reims-ctl command` prints the same command as before the start

#### Scenario: Graceful shutdown never forces
- **WHEN** the user picks Shut down on a running `reims-ctl` VM whose guest ignores the request
- **THEN** the VM keeps running until the user confirms a force stop

#### Scenario: Direct start afterwards
- **WHEN** the VM was last run from the app and the user then runs `reims-ctl start` on it after a reboot
- **THEN** it starts as upstream intends, with no socket path left over from the app

### Requirement: Checked reims-ctl launch
Before starting a `reims-ctl` VM, the app SHALL prepare what upstream's own start prepares (the VM's log folder and log rotation, a private folder for upstream's QMP socket). A command whose form the app does not recognise, or that holds an option that would detach QEMU or collide with the app's sockets, SHALL stop the start, naming it, without changing the VM's `reims-ctl` settings.

#### Scenario: Default command
- **WHEN** a VM just created by `reims-ctl` with default settings starts from the app
- **THEN** it starts, and `reims-ctl list --json` reports it running with QEMU's pid

#### Scenario: Conflicting extra argument
- **WHEN** the user set `--extra-args "-daemonize"` on the VM in `reims-ctl` and starts it from the app
- **THEN** the start stops naming `-daemonize`, and the VM's `reims-ctl` settings are unchanged

#### Scenario: Fresh VM with logs on
- **WHEN** a VM just created by `reims-ctl` with logs enabled starts from the app for the first time after a reboot
- **THEN** it starts, and its serial log is written where `reims-ctl logs --serial` reads it

### Requirement: Settings of reims-ctl VMs
The app SHALL read a `reims-ctl` VM's settings (CPUs, memory, resolution, displays, SSH port, MAC address) from `reims-ctl command` before showing them, before each start and when allocating SSH ports, and SHALL write changes only through `reims-ctl set`. Its SSH port SHALL take part in the port checks and warnings of every other VM.

#### Scenario: Settings change
- **WHEN** the user sets 12 GiB of memory on a powered-off `reims-ctl` VM in Settings
- **THEN** `reims-ctl show` reports 12 GiB and the next start uses it

#### Scenario: Port changed outside the app
- **WHEN** the user changes the VM's SSH port with `reims-ctl set` while the app is open
- **THEN** the app shows the new port before the next start and warns if another VM uses it

### Requirement: Operations of reims-ctl VMs
For a `reims-ctl` VM the app, the CLI and the MCP server SHALL refuse snapshots, clone, export, move and GPU passthrough with a message that `reims-ctl` VMs do not support them yet, and the installer SHALL be inserted or ejected through `reims-ctl` for the next start only.

#### Scenario: Unsupported operation
- **WHEN** the user opens Clone or Export on a `reims-ctl` VM
- **THEN** the action is disabled with a note that `reims-ctl` VMs do not support it yet

#### Scenario: Eject the installer
- **WHEN** the user picks Devices › Eject installer on a running `reims-ctl` VM
- **THEN** the menu says it applies at the next start, and `reims-ctl` no longer lists the installer for the VM

### Requirement: QEMU for reims-ctl VMs
The app SHALL resolve the Reims QEMU once, by the order of Reims QEMU build, and SHALL pass that executable to `reims-ctl` for `create` and `command`, so the checked QEMU is the one that runs.

#### Scenario: Override in settings
- **WHEN** `reims_qemu` in `settings.yaml` names a working build and the installed `current` is broken
- **THEN** the VM is created and runs with the build `reims_qemu` names

### Requirement: Add an existing reims-ctl VM
The Manager's Add SHALL list the VMs that `reims-ctl list --json` reports, from kvm-gui's `reims-ctl` folders and from the user's own, that the app does not have yet, and SHALL register the chosen ones without changing their files.

#### Scenario: VM made in reims-ctl's interface
- **WHEN** the user created a VM in `reims-ctl`'s own interface and picks it in Add
- **THEN** it appears in the Manager and starts from there

### Requirement: Existing macOS VMs keep their pipeline
macOS VMs created before this change, macOS VMs on VMware SVGA, with GPU passthrough or root-patch mode, and versions `reims-ctl` does not support SHALL keep the app's own OpenCore, recovery and installer path. The app's macOS start, boot-file preparation and UEFI variables rules SHALL apply only to these VMs; a `reims-ctl` VM's boot files SHALL be left as `reims-ctl` made them.

#### Scenario: Nilton VM
- **WHEN** the existing Nilton VM (Sequoia, Reims vGPU, created by the app) starts after the upgrade
- **THEN** it boots with the same OpenCore, NVRAM and identity as before, without `reims-ctl`

#### Scenario: reims-ctl boot files untouched
- **WHEN** a `reims-ctl` VM starts from the app
- **THEN** its OpenCore image and UEFI variables are the files `reims-ctl` made, unchanged by the app's preparation
