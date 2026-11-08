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
The system SHALL show the VM's screen while the VM runs, updated as the guest redraws, scaled to fit without distortion, in the Manager's Screen tab (the selected VM; a Normal start from the Manager opens that tab) and in the VM's own window, and SHALL show a placeholder when the VM is stopped. The Manager's Details SHALL show a smaller Preview of the same screen for every VM whose display is exported to the app; a VM with Display set to QEMU window and VMware SVGA graphics SHALL show no Preview and say the screen is in QEMU's window. A Reims vGPU VM SHALL always have its Preview, whatever its Display setting.

#### Scenario: VM running
- **WHEN** the VM is running and the guest draws its screen
- **THEN** the Screen tab and the VM window show the same image the guest shows, and they follow changes of the guest's resolution

#### Scenario: VM stops
- **WHEN** QEMU exits
- **THEN** the screen is replaced by a placeholder and the viewer releases its connection

### Requirement: Separate window
Each running VM's screen SHALL also be available in its own resizable window, separate from the Manager, opened by Show or by `kvm-gui --show <vm>` (in the running GUI when one is open). Closing it SHALL NOT stop the VM unless the user chooses so in the close dialog, and Show SHALL open it again.

#### Scenario: Show from the command line
- **WHEN** the GUI is open and the user runs `kvm-gui --show "<name>"` for a running VM
- **THEN** that GUI opens the VM's window, or brings it to the front, and no second GUI stays running

#### Scenario: Detach and close
- **WHEN** the user closes a VM window choosing to continue in the background and then picks Show
- **THEN** the VM kept running and its window opens again with the current screen

### Requirement: Full screen and host key
The system SHALL offer a full screen view of the VM window. Right Ctrl SHALL be the host key: it is never sent to the guest, and it gives the guest the keyboard (a click on the screen does too) or takes it back, releasing every key and button the guest got. Ctrl+Alt+F SHALL toggle full screen: the F is never sent to the guest and the Ctrl and Alt presses already sent are released.

#### Scenario: Enter and leave full screen
- **WHEN** the user picks View › Full Screen and then presses Ctrl+Alt+F
- **THEN** the screen fills the monitor, then returns to a normal window, and the guest never receives the F key

#### Scenario: Back to the tab
- **WHEN** the VM window is not in full screen and the user presses Ctrl+Alt+F
- **THEN** the VM window enters full screen and the guest never receives the F key

#### Scenario: Keyboard back from the guest
- **WHEN** the guest has the keyboard and the user presses Right Ctrl
- **THEN** keys go to the app again, the guest got no Right Ctrl, and no key or button stays pressed in the guest
