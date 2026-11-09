# Spec Delta

## ADDED Requirements

### Requirement: Supported host
The checks SHALL report the host's distribution: OK on Ubuntu 26.04 or later, Debian 13 or later and Rocky Linux 10 or later, and a Warning on any other distribution or older release (Ubuntu 24.04 among them), naming the supported hosts and the last kvm-gui release that supported Ubuntu 24.04.

#### Scenario: Ubuntu 24.04
- **WHEN** the checks run on Ubuntu 24.04
- **THEN** the host check shows Warning, names Ubuntu 26.04, Debian 13 and Rocky Linux 10, and says v0.15.0 was the last release for Ubuntu 24.04

#### Scenario: Ubuntu 26.04
- **WHEN** the checks run on Ubuntu 26.04
- **THEN** the host check is OK

## MODIFIED Requirements

### Requirement: Prerequisite checks
The system SHALL check and display, each item with status OK, Warning, Failure or Inconclusive and a fix hint: VT-x/AMD-V on the CPU, read and write access to `/dev/kvm`, the system QEMU and `qemu-img`, the system QEMU's version (10.0 or later) and the kvm module's `ignore_msrs` value.

#### Scenario: Host ready
- **WHEN** all prerequisites are present and `ignore_msrs` is `Y`
- **THEN** all checks show as OK

#### Scenario: User without KVM access
- **WHEN** `/dev/kvm` exists but the user cannot open it for reading and writing
- **THEN** the KVM check shows as Failure, with the hint to add the user to the `kvm` group

#### Scenario: Missing tool
- **WHEN** `qemu-img` is not found in `PATH`
- **THEN** the corresponding check shows as Failure and names the package to install

#### Scenario: Old QEMU
- **WHEN** the system QEMU reports a version lower than 10.0, such as Ubuntu 24.04's 8.2.2
- **THEN** the QEMU check shows as Failure, states the minimum version and names the supported hosts

### Requirement: Package manager per distribution
Set up host SHALL install the missing packages with `apt-get` on Ubuntu 26.04 and Debian 13 and with `dnf` on Rocky Linux 10, using that distribution's package names, and SHALL change nothing on any other distribution or release, listing the packages it needs instead.

#### Scenario: Rocky Linux
- **WHEN** Set up host runs on Rocky Linux 10 without `qemu-img`
- **THEN** it installs the package that provides it with `dnf` and reruns the checks

#### Scenario: Unsupported distribution
- **WHEN** Set up host runs on a distribution other than the three supported ones, or on Ubuntu 24.04
- **THEN** nothing is installed and the app lists the packages to install by hand
