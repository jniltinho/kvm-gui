# Spec Delta

## Purpose

Where kvm-gui keeps its files on the user's machine, following the XDG Base Directory layout Linux distributions use, and how data from `macos-kvm-gui` moves there.

## ADDED Requirements

### Requirement: XDG directories
The application SHALL keep its files in the XDG base directories, each with a `kvm-gui` folder: configuration in `$XDG_CONFIG_HOME` (default `~/.config/kvm-gui`), data in `$XDG_DATA_HOME` (`~/.local/share/kvm-gui`), re-downloadable files in `$XDG_CACHE_HOME` (`~/.cache/kvm-gui`), logs in `$XDG_STATE_HOME` (`~/.local/state/kvm-gui`) and sockets and locks in `$XDG_RUNTIME_DIR/kvm-gui`. A relative or empty XDG variable SHALL fall back to its default.

#### Scenario: Default layout
- **WHEN** `kvm-gui` runs for the first time for a user with no XDG variables set
- **THEN** `settings.yaml` is in `~/.config/kvm-gui/`, the database `kvm-gui.db` in `~/.local/share/kvm-gui/`, and nothing is written outside the XDG folders and the VM folders

#### Scenario: Custom XDG_CONFIG_HOME
- **WHEN** `XDG_CONFIG_HOME` is `/srv/me/config`
- **THEN** `settings.yaml` is read from `/srv/me/config/kvm-gui/`

### Requirement: What goes where
Configuration SHALL hold `settings.yaml`, the older `.env` and `reims-ctl`'s configuration; data SHALL hold the database, the installed Reims vGPU builds with `reims-ctl` and `reims-util`, and `reims-ctl`'s installers; cache SHALL hold full installers, downloaded kexts and build caches; state SHALL hold the QEMU logs of each VM's runs.

#### Scenario: Clearing the cache
- **WHEN** the user deletes `~/.cache/kvm-gui`
- **THEN** every VM still starts, and installers and kexts are downloaded again when needed

### Requirement: VM folders
New VMs SHALL be created in the Workspace, by default `~/KVM-VMs`, one folder per VM. The Workspace SHALL hold only VM folders.

#### Scenario: New VM
- **WHEN** a user with the default Workspace creates "Ubuntu 26.04"
- **THEN** its folder is `~/KVM-VMs/ubuntu-26-04`

### Requirement: reims-ctl inside the app's folders
The application SHALL run `reims-ctl` with its configuration, data and cache inside kvm-gui's own folders (`~/.config/kvm-gui/reims-ctl`, `~/.local/share/kvm-gui/reims-ctl`, `~/.cache/kvm-gui/reims-ctl`), so a separate `reims-ctl` install of the user is never changed.

#### Scenario: User's own reims-ctl
- **WHEN** the user also has upstream's `reims-ctl` installed with VMs in `~/.config/reims-ctl`
- **THEN** creating and starting VMs in kvm-gui leaves `~/.config/reims-ctl` and `~/.local/share/reims-ctl` unchanged, and Add still lists those VMs to register

### Requirement: Environment variables
The application SHALL read `KVM_GUI_THEME`, `KVM_GUI_ASSETS_DIR` and `KVM_GUI_REIMS_QEMU`, and SHALL still read `MKG_THEME`, `MKG_ASSETS_DIR` and `MKG_REIMS_QEMU` when the new name is not set.

#### Scenario: Old variable
- **WHEN** only `MKG_THEME=light` is set
- **THEN** the theme is light

### Requirement: Migration from macos-kvm-gui
On first start, when the new database does not exist and `~/.config/macos-kvm-gui/` has one, the application SHALL copy `settings.yaml`, `.env` and `known_hosts` to its configuration folder and a consistent copy of the database to its data folder, then migrate that copy. Relative paths in the copied settings SHALL be rewritten as absolute paths resolved against the old folder. The old folder SHALL be left unchanged, and existing VM folders SHALL stay where they are.

#### Scenario: Upgrade
- **WHEN** a user of `macos-kvm-gui` with VMs in `~/macOS-VMs` starts `kvm-gui`
- **THEN** the same VMs are listed with their settings and boot with the same NVRAM, identity and disks from `~/macOS-VMs`, and `~/.config/macos-kvm-gui/` is unchanged

#### Scenario: Relative QEMU override
- **WHEN** the old `settings.yaml` has `reims_qemu: reims/bin/qemu-system-x86_64`
- **THEN** the new `settings.yaml` has the absolute path under `~/.config/macos-kvm-gui/` and the same executable runs

#### Scenario: VM in an OSX-KVM checkout
- **WHEN** a VM registered by `macos-kvm-gui` lives in `~/OSX-KVM` (its files under `~/OSX-KVM/macos-kvm-gui/`)
- **THEN** after the upgrade it is listed and boots from `~/OSX-KVM` as before, the checkout's own files are untouched, and Move can later move its files into `~/KVM-VMs`

#### Scenario: Older binary afterwards
- **WHEN** the user runs the previous `macos-kvm-gui` after the upgrade
- **THEN** it opens its own unchanged database and does not see changes made in `kvm-gui`

### Requirement: Older shared files still used
After the migration, installers, kexts and Reims vGPU builds that `macos-kvm-gui` kept in the old Workspace (`.installers`, `.kexts`, `.reims`) SHALL still be used where the new folders lack them, and SHALL never be moved or deleted by the application.

#### Scenario: Cached Sequoia installer
- **WHEN** a Sequoia installer is in `~/macOS-VMs/.installers` and not in `~/.cache/kvm-gui/installers`
- **THEN** an offline install uses it without downloading it again

### Requirement: New VMs after the upgrade
After the migration, a Workspace the user had set to a folder other than `~/macOS-VMs` SHALL be kept; otherwise the Workspace SHALL become `~/KVM-VMs`.

#### Scenario: Default Workspace before
- **WHEN** the user had the default Workspace `~/macOS-VMs` and creates a VM after the upgrade
- **THEN** the new VM's folder is in `~/KVM-VMs`, and the old VMs stay in `~/macOS-VMs`
