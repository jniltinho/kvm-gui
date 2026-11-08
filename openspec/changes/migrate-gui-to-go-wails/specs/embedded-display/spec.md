# Spec Delta

## MODIFIED Requirements

### Requirement: Private display export
The system SHALL, when the Display setting is "In the app", start QEMU without its own window and with VNC on a Unix socket inside the private, user-only runtime directory of that VM run, and SHALL never open a VNC TCP port. A Reims vGPU VM SHALL always get that VNC socket next to Reims vGPU's own window, whatever its Display setting.

#### Scenario: Default start
- **WHEN** the VM starts with the default settings
- **THEN** QEMU has `-display none` and `-vnc unix:<private dir>/vnc.sock`, and no QEMU window opens

#### Scenario: QEMU window fallback
- **WHEN** the Display setting is "QEMU window" on a VM with VMware SVGA or ordinary-guest graphics
- **THEN** QEMU starts with its own window and without VNC, as before

#### Scenario: Reims vGPU with QEMU window
- **WHEN** a Reims vGPU VM with Display "QEMU window" starts
- **THEN** QEMU has `-display none` and the VNC socket, and Reims vGPU's window shows the guest

### Requirement: In-app screen
The system SHALL show the VM's screen in the VM's own window while the VM runs, updated as the guest redraws, scaled to fit the window without distortion, and SHALL show a placeholder when the VM is stopped. The Manager's Details SHALL show a smaller Preview of the same screen for every VM whose display is exported to the app; a VM with Display set to QEMU window and VMware SVGA graphics SHALL show no Preview and say the screen is in QEMU's window. A Reims vGPU VM SHALL always have its Preview, whatever its Display setting.

#### Scenario: VM running
- **WHEN** the VM is running and the guest draws its screen
- **THEN** the VM window shows the same image the guest shows, and it follows changes of the guest's resolution

#### Scenario: VM stops
- **WHEN** QEMU exits
- **THEN** the screen is replaced by a placeholder and the viewer releases its connection

### Requirement: Separate window
Each running VM's screen SHALL be in its own resizable window, separate from the Manager. Closing it SHALL NOT stop the VM unless the user chooses so in the close dialog, and Show SHALL open it again.

#### Scenario: Detach and close
- **WHEN** the user closes a VM window choosing to continue in the background and then picks Show
- **THEN** the VM kept running and its window opens again with the current screen

### Requirement: Full screen and host key
The system SHALL offer a full screen view of the VM window. Ctrl+Alt+F SHALL be the host key: it is never sent to the guest, the Ctrl and Alt presses already sent are released, and it toggles full screen.

#### Scenario: Enter and leave full screen
- **WHEN** the user picks View › Full Screen and then presses Ctrl+Alt+F
- **THEN** the screen fills the monitor, then returns to a normal window, and the guest never receives the F key

#### Scenario: Back to the tab
- **WHEN** the VM window is not in full screen and the user presses Ctrl+Alt+F
- **THEN** the VM window enters full screen and the guest never receives the F key
