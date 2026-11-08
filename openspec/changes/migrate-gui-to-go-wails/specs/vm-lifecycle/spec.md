# Spec Delta

## MODIFIED Requirements

### Requirement: Start the VM
The system SHALL start a macOS VM of the app's own pipeline with QEMU's KVM, q35 machine, OpenCore in snapshot mode, OVMF firmware with persistent UEFI variables, the macOS disk, NAT networking and SSH forwarded only from `127.0.0.1:<port>` to guest port 22. Linux, Windows and Other VMs SHALL start as the `kvm-guests` capability specifies and `reims-ctl` VMs as the `reims-vgpu` capability specifies, with the same SSH forward rule.

#### Scenario: Start with install media
- **WHEN** the user clicks "Start" and `BaseSystem.img` exists
- **THEN** QEMU starts with the install media attached and the VM window opens

#### Scenario: Start without install media
- **WHEN** the user clicks "Start" and `BaseSystem.img` does not exist
- **THEN** QEMU starts with only the macOS disk

#### Scenario: Missing disk
- **WHEN** the user tries to start and `mac_hdd_ng.img` does not exist
- **THEN** the system does not start QEMU and advises creating the disk

#### Scenario: SSH restricted to the host
- **WHEN** the VM is running with port 2222
- **THEN** port 2222 accepts connections only on `127.0.0.1`

### Requirement: QEMU monitoring
The system SHALL display the VM state ("Stopped", "Running", "Paused" or "Paused (error)") and the QEMU output, and SHALL translate known failures into clear messages. A VM started from the desktop app SHALL run in its own detached process: closing the Manager or a VM window SHALL NOT stop it. `vm start` in a terminal SHALL keep running in the foreground until QEMU exits, as before.

#### Scenario: KVM busy
- **WHEN** QEMU exits with `failed to initialize kvm: Device or resource busy`
- **THEN** the state returns to "Stopped" and the system reports that another hypervisor is using virtualization

#### Scenario: Disk error
- **WHEN** QEMU pauses the guest due to a disk I/O error, for example lack of space on the host
- **THEN** the state becomes "Paused (error)" with the error message, even with the process alive

#### Scenario: Normal exit
- **WHEN** the guest shuts down and QEMU exits with code 0
- **THEN** the state returns to "Stopped" with no error message

#### Scenario: Closing the app with the VM running
- **WHEN** the user closes the Manager with a VM running
- **THEN** the Manager closes, the VM keeps running, and the Manager shows it Running with its last output when opened again

#### Scenario: Foreground CLI start
- **WHEN** a user runs `kvm-gui vm start "Ubuntu"` in a terminal
- **THEN** the command stays in the foreground until QEMU exits and returns QEMU's result

### Requirement: UEFI variable persistence
A macOS VM of the app's own pipeline SHALL create, on first start, its own copy of the UEFI variables file from `OVMF_VARS-1920x1080.fd` and SHALL reuse it on later starts, never overwriting it. A UEFI Linux, Windows or Other VM SHALL create its copy once from the host template that matches its firmware, and SHALL replace it only when the user confirms turning Secure Boot on or off.

#### Scenario: Second start
- **WHEN** the VM has been started once and is started again
- **THEN** QEMU uses the same UEFI variables copy, with changes made by the guest preserved

#### Scenario: Secure Boot toggled
- **WHEN** the user turns Secure Boot on for a powered-off Linux VM and confirms
- **THEN** its UEFI variables are replaced from the Microsoft-keys template, and the next start boots with Secure Boot
