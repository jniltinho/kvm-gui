# Spec Delta

## MODIFIED Requirements

### Requirement: Settings file
The app SHALL keep user settings in `settings.yaml` in its user config folder (`~/.config/kvm-gui/settings.yaml`, under `$XDG_CONFIG_HOME`), with the keys `theme`, `assets_dir`, `kexts` and `disable`. When the file does not exist, the first read SHALL create it from a default embedded in the app that sets nothing. An unknown key or invalid YAML SHALL be reported with the file's path.

#### Scenario: First start
- **WHEN** the app reads its settings and `settings.yaml` does not exist
- **THEN** the file is created with the commented default, and behavior is the same as with no settings

#### Scenario: Unknown key
- **WHEN** `settings.yaml` has a key the app does not know
- **THEN** reading the kexts fails with an error naming the file and the key

### Requirement: Settings precedence
For `theme` and `assets_dir` the precedence SHALL be: environment variable (`KVM_GUI_THEME` or `MKG_THEME`, `KVM_GUI_ASSETS_DIR` or `MKG_ASSETS_DIR`), then `settings.yaml`, then the older `.env` file.

#### Scenario: Precedence
- **WHEN** `.env` has `MKG_THEME=dark`, `settings.yaml` has `theme: light` and the environment has `MKG_THEME=system`
- **THEN** the theme is `system`; without the environment variable it is `light`; with neither it is `dark`

### Requirement: Download and cache
Before a VM starts from the GUI, the CLI (`vm start`) or the MCP server, the app SHALL download every listed archive that is not cached yet into a cache shared by every VM, `~/.cache/kvm-gui/kexts/extra/` (under `$XDG_CACHE_HOME`), keyed by the full SHA-256 of the item (`url`, `sha256`, `load`). A cache hit SHALL count only when the stored item equals the requested one. A failure SHALL leave nothing cached for that item and stop the start with an error naming its URL.

#### Scenario: First start with a new item
- **WHEN** a VM starts and an item is not cached
- **THEN** the archive is downloaded once, and later starts reuse the cache without network access

#### Scenario: Hash mismatch
- **WHEN** the downloaded archive does not match the item's `sha256`
- **THEN** the VM does not start, the error shows the expected and actual hashes, and nothing is cached

#### Scenario: Changed item
- **WHEN** the user changes an item's `url`, `sha256` or `load`
- **THEN** the new item is downloaded, and the old item's kexts are no longer loaded

#### Scenario: Slow download from the MCP server
- **WHEN** an MCP client starts a VM whose item takes longer than the server's start timeout to download
- **THEN** the download finishes before the VM process is launched, and the timeout applies only to the VM showing up
