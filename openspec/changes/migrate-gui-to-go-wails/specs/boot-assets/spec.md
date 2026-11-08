# Spec Delta

## MODIFIED Requirements

### Requirement: Runtime asset directory
The system SHALL use, instead of the embedded files, a directory named by `KVM_GUI_ASSETS_DIR` or `MKG_ASSETS_DIR` (from the environment, `settings.yaml`'s `assets_dir`, or `~/.config/kvm-gui/.env`) with the same layout as the embedded files, and SHALL refuse it when any file of the embedded set is missing.

#### Scenario: Newer OpenCore without rebuilding
- **WHEN** `MKG_ASSETS_DIR` points to a complete directory
- **THEN** the VM boots with those files and the app shows their versions with the directory as the source

#### Scenario: Incomplete directory
- **WHEN** `MKG_ASSETS_DIR` points to a directory without `files/EFI/OC/OpenCore.efi`
- **THEN** the VM does not start and the app names the missing file
