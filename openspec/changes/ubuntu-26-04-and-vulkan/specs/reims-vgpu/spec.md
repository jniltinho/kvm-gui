# Spec Delta

## MODIFIED Requirements

### Requirement: Package install
The app SHALL embed a manifest naming the reims release tag and each package's SHA-256, and install the package for the host's platform (Ubuntu 26.04, Debian 13, Rocky Linux 10) from **Install Reims vGPU** and `reims install`: download over HTTPS, verify the SHA-256, unpack its QEMU, `reims-ctl` and `reims-util` together into a staged build folder `~/.local/share/kvm-gui/reims-ctl/qemu/<build>` (under `$XDG_DATA_HOME`; no path may leave it, no links outside it), then activate it under the same rules as a build; `~/.local/share/kvm-gui/bin/` SHALL hold only links through `current`. A host without a package, Ubuntu 24.04 among them, SHALL get an error naming the supported platforms.

#### Scenario: Install on Ubuntu 26.04
- **WHEN** the user installs on an Ubuntu 26.04 host
- **THEN** the 26.04 package is downloaded and verified, `~/.local/share/kvm-gui/reims-ctl/qemu/current/bin/qemu-system-x86_64` lists `apple-gfx-pci`, and `~/.local/share/kvm-gui/bin/reims-ctl doctor` runs with kvm-gui's `reims-ctl` folders

#### Scenario: Install on Ubuntu 24.04
- **WHEN** the user installs on an Ubuntu 24.04 host
- **THEN** nothing is downloaded, and the error names the supported platforms and v0.15.0 as the last release with a 24.04 package

#### Scenario: Tampered download
- **WHEN** the downloaded package does not match the manifest's SHA-256
- **THEN** nothing is unpacked and `current` is unchanged

#### Scenario: Unsupported release
- **WHEN** the host is a distribution without a package, such as Fedora
- **THEN** the install stops naming the supported platforms

### Requirement: Docker build
The app SHALL embed a build recipe and build upstream reims-vgpu at the pinned commit with upstream's `scripts/install-ctl.sh` inside an Ubuntu 26.04 container, from **Build Reims vGPU** and `reims build [--release 26.04]`, showing progress, producing QEMU, `reims-ctl` and `reims-util`. On a host that is not Ubuntu 26.04 the build SHALL stop before starting Docker, naming Ubuntu 26.04 and, on Debian 13 and Rocky Linux 10, the prebuilt package install. Docker missing or unusable SHALL stop the build with the fix.

#### Scenario: Build on Ubuntu 26.04
- **WHEN** the user builds on an Ubuntu 26.04 host with Docker
- **THEN** `~/.local/share/kvm-gui/reims-ctl/qemu/current/bin/qemu-system-x86_64` exists, runs on the host, and lists `apple-gfx-pci`, and `~/.local/share/kvm-gui/bin/reims-ctl` runs

#### Scenario: Build on Ubuntu 24.04
- **WHEN** the user builds on an Ubuntu 24.04 host and no release is given
- **THEN** the build stops before starting Docker and names the supported platforms

#### Scenario: Unsupported host release
- **WHEN** the host is Ubuntu 22.04 and no release is given
- **THEN** the build stops before starting Docker and names the supported platforms

### Requirement: Prebuilt packages
This repository SHALL publish, on a release tagged `reims-<commit>`, one package per supported platform (`ubuntu26.04`, `debian13`, `rocky10`) named `reims-qemu-<commit>-<platform>-x86_64.tar.gz` with a `.sha256` file, built from upstream at that commit in a container of that platform and stripped, each holding QEMU, `reims-ctl` and `reims-util`.

#### Scenario: Release
- **WHEN** a `reims-<commit>` tag is pushed
- **THEN** the release holds the three packages and their SHA-256 files, and each package's three programs run in a clean container of its platform
