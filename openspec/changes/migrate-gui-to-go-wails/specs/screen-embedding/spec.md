# Spec Delta

## Purpose

How the desktop GUI shows Reims vGPU's own accelerated window inside the app on X11 (the Manager's Screen tab or the VM's window), and how it falls back to that window on the desktop when it cannot.

## ADDED Requirements

### Requirement: When the window is embedded
The GUI SHALL embed a Reims vGPU VM's window, in the Manager's Screen tab or in the VM's window (one at a time), only when the run was started from the GUI, the GUI runs in an X11 session (never on Wayland, XWayland included), and no other running GUI instance holds that run. Otherwise the guest SHALL stay in Reims vGPU's own window and the app SHALL say where the screen is. The GUI SHALL never take a window of a run started by the CLI or MCP server.

#### Scenario: Started from the GUI on X11
- **WHEN** a Reims vGPU VM starts with Normal from the GUI on X11
- **THEN** within 30 seconds macOS shows inside the Manager's Screen tab and no separate Reims vGPU window remains on the desktop

#### Scenario: Started by the CLI
- **WHEN** `kvm-gui vm start` starts the VM while the GUI is open
- **THEN** the guest stays in Reims vGPU's own window, Show says so, and the Preview updates

#### Scenario: Wayland desktop
- **WHEN** the GUI starts a Reims vGPU VM on a Wayland desktop with XWayland
- **THEN** the guest stays in Reims vGPU's own window and the VM window says so

### Requirement: Run ownership survives the GUI
A run started from the GUI SHALL stay a GUI run after that GUI exits: a later GUI of the same user SHALL be able to embed it, after checking that the run is the same QEMU process that was started (not a reused process id) and that no live GUI instance holds it.

#### Scenario: Stale run record
- **WHEN** a GUI run's QEMU has exited and a new, unrelated process got its process id
- **THEN** the GUI does not treat the new process as that run and embeds nothing

### Requirement: Geometry follows the VM window
The embedded window SHALL cover exactly the screen area of the VM window, in physical pixels at the window's scale, and SHALL follow it when the window moves, resizes, changes monitor or scale, or enters full screen, and when the guest changes its resolution.

#### Scenario: Resize and HiDPI
- **WHEN** the VM window is resized, or moved to a monitor with a different scale
- **THEN** the embedded window's position and size match the screen area within one frame, with no gap or overlap

### Requirement: Hidden under overlays
While a dialog, menu or other popup of the GUI covers the screen area, or the VM window is minimized, the embedded window SHALL be hidden, so the GUI is never drawn under the guest and clicks never reach the guest. It SHALL reappear when the area is uncovered.

#### Scenario: Menu over the screen
- **WHEN** the Devices menu opens over the embedded guest
- **THEN** the whole menu is visible and usable, and the guest shows again when it closes

### Requirement: Keyboard to the guest
A click on the embedded guest, or the pointer entering it while the VM window is active, SHALL give the guest the keyboard. Ctrl+Alt+Super SHALL release the guest's capture, as in Reims vGPU's own window.

#### Scenario: Typing in the guest
- **WHEN** the user clicks the embedded guest and types
- **THEN** the keys reach macOS

### Requirement: Keyboard back to the GUI
A click on the GUI's own controls SHALL give the keyboard back to the GUI until the pointer enters the guest again or the guest is clicked. The guest SHALL never take the keyboard while another application is in front, including from focus events queued before the switch.

#### Scenario: Another application in front
- **WHEN** the user switches to another application right after clicking the guest
- **THEN** that application keeps the keyboard

### Requirement: Release before the parent goes away
Before a VM window that holds an embedded guest is closed, recreated or destroyed, and before the GUI exits normally or on SIGTERM, SIGINT or SIGHUP, the GUI SHALL put the guest window back on the desktop. If the GUI dies without doing so, the window SHALL still go back to the desktop instead of being destroyed, also under a reparenting window manager (whose frame of the GUI's window goes away with it). The VM SHALL keep running.

#### Scenario: Close one of two VM windows
- **WHEN** two Reims vGPU VMs are embedded and the user closes one VM window choosing to continue in the background
- **THEN** that guest's window is back on the desktop, both QEMUs keep running, the other VM window is unchanged, and Show embeds the first again

#### Scenario: Signal with a VM on screen
- **WHEN** the GUI gets SIGTERM while a Reims vGPU guest is embedded
- **THEN** the window is back on the desktop before the GUI exits, the VM keeps running, and the X server and desktop stay up

#### Scenario: GUI killed
- **WHEN** the GUI is killed with SIGKILL while a guest is embedded
- **THEN** the guest window is back on the desktop and the VM keeps running

#### Scenario: Reopen the GUI
- **WHEN** the GUI is opened again and the user picks Show on that VM
- **THEN** its window is embedded in the VM window again

### Requirement: Fallback when embedding fails
If the window cannot be found or embedded within 30 seconds, the VM SHALL keep running in its own window and the VM's log SHALL say why.

#### Scenario: Embedding fails
- **WHEN** the Reims vGPU window does not appear within 30 seconds
- **THEN** the VM keeps running in its own window and the Logs tool says embedding was skipped and why

### Requirement: Stop and restart
When QEMU exits, the embedding SHALL be released and the VM window SHALL show its stopped placeholder, and a later start SHALL embed again. A window lookup still running for an earlier run SHALL never embed a window into a later one.

#### Scenario: Stop during the lookup
- **WHEN** the VM is stopped and started again before the first run's window lookup finishes
- **THEN** only the second run's window is embedded, and nothing is left over from the first
