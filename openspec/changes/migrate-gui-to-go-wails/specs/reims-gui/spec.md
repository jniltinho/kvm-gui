# Spec Delta

## REMOVED Requirements

### Requirement: Docker-only build
**Reason**: The Rust app is archived; it is no longer built or released.
**Migration**: Use `kvm-gui`; its build is the `desktop-app` capability's Docker-only build.

### Requirement: Single binary
**Reason**: The Rust app is archived.
**Migration**: `kvm-gui` is the one binary with GUI, CLI and MCP (`desktop-app` › One binary).

### Requirement: Shared data
**Reason**: The Rust app is archived; there is no second app to share data with.
**Migration**: `kvm-gui` reads the same data in place (`desktop-app` › Same data as before the rename).

### Requirement: Same GUI
**Reason**: The Rust app is archived.
**Migration**: The GUI's screens are defined by the `vm-manager-ui` capability.

### Requirement: Reims display inside the window
**Reason**: The Rust app is archived.
**Migration**: The `screen-embedding` capability embeds Reims vGPU's window in the Go app's VM window.

### Requirement: Screen for VMware SVGA
**Reason**: The Rust app is archived.
**Migration**: VMware SVGA VMs use the in-app screen of the `embedded-display` capability.

### Requirement: Reims-first defaults
**Reason**: The Rust app is archived.
**Migration**: The New VM wizard of `vm-management` proposes Reims vGPU for macOS versions where it is installed and validated.

### Requirement: Feature parity
**Reason**: The Rust app is archived; there is no second app to keep at parity.
**Migration**: None needed; the parity target is now VirtualBox (`vm-manager-ui` › Parity matrix).

### Requirement: License
**Reason**: The Rust app is archived; its license files stay with it under `archive/rust/reims-gui/`.
**Migration**: The Go app's licenses and notices are in `LICENSE` and `THIRD_PARTY_NOTICES.md`.
