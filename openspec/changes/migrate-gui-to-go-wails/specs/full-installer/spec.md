# Spec Delta

## MODIFIED Requirements

### Requirement: Full installer download
The system SHALL find, in Apple's public Software Update catalog over HTTPS, the newest `InstallAssistant.pkg` of the VM's macOS major version for Big Sur 11 and later whose distribution supports the VM's Mac board ID, SHALL download it with progress and resume into a shared cache, and SHALL refuse High Sierra to Catalina with a message that the recovery must be used.

#### Scenario: Sequoia
- **WHEN** the user downloads the full installer for a Sequoia VM
- **THEN** the newest macOS 15 InstallAssistant.pkg is downloaded into `~/.cache/kvm-gui/installers/<build>/`
