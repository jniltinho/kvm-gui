# Spec Delta

## MODIFIED Requirements

### Requirement: Local MCP server
The system SHALL provide `kvm-gui mcp`, an MCP server over standard input and output only that writes nothing but MCP messages to standard output, and SHALL NOT open any network listener. Started through a `macos-kvm-gui` link, it SHALL behave the same.

#### Scenario: Agent registration
- **WHEN** an agent starts `kvm-gui mcp` and lists its tools
- **THEN** it gets the VM, screen, input, snapshot, installer and host tools with their input schemas

#### Scenario: Old registration
- **WHEN** an agent configured with `macos-kvm-gui mcp` starts it through a link to `kvm-gui`
- **THEN** it gets the same tools as with `kvm-gui mcp`

### Requirement: Run ownership and discovery
The system SHALL let one process own a running VM and, at the same time, the desktop GUI and one other process (the MCP server or the CLI) attach to it, each through the run's VNC socket and a QMP socket of its own. It SHALL find a run of the app only through the run record in this user's private run directory that names the VM and the QEMU process (pid and start time), never through a shared disk alone, using its sockets only inside those directories, and SHALL NOT stop a VM when an attached process or the MCP server exits.

#### Scenario: VM started in the app
- **WHEN** a VM runs in the app with Display: In the app and an agent calls `screen_capture` for it
- **THEN** the agent gets the current guest screen as a PNG image and the app keeps showing it

#### Scenario: VM started by the agent
- **WHEN** an agent starts a VM with `vm_start` and then the agent exits
- **THEN** the VM keeps running and the app can show it

#### Scenario: Socket outside a private directory
- **WHEN** a QEMU process opens the VM's disk but its sockets are in a directory another user owns or that is not 0700
- **THEN** no tool connects to them

#### Scenario: GUI and agent together
- **WHEN** the GUI shows a running VM in its VM window and an agent calls `vm_status` and then `vm_shutdown`
- **THEN** the agent gets the status and the shutdown request reaches the guest, and the GUI keeps its connection and shows the VM stopping

#### Scenario: Shared disk does not grant control
- **WHEN** VM A runs, stopped VM B references A's second disk, and an agent calls `vm_shutdown` for B
- **THEN** the tool says B is not running (its disk is in use by A) and A keeps running

### Requirement: Screen and input
The system SHALL return the guest screen as PNG image content in guest coordinates (with the scale when reduced) only after a complete frame, SHALL send key chords and pointer events serialized per run with pauses between events, and SHALL refuse text the run's keyboard layout table does not cover before sending any key.

#### Scenario: Menu bar from the keyboard
- **WHEN** an agent sends `ctrl+f2`, `right` four times, `down`, `t` and `return` while the recovery shows its window
- **THEN** the recovery opens Utilities › Terminal

#### Scenario: Untypeable text
- **WHEN** the run's keyboard layout is Dvorak and an agent calls `type_text` with `sh install`
- **THEN** the tool fails with a message and no key is sent

#### Scenario: VM in its own window
- **WHEN** a VMware SVGA VM runs with Display: In a window and an agent calls `screen_capture`
- **THEN** the tool fails saying screen and input need Display: In the app; for a Reims vGPU VM, whose VNC socket is always served, the capture works
