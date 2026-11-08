# Spec Delta

## Purpose

The screens and workflows of the desktop GUI, at workflow parity with VirtualBox's Manager and VM windows: what each screen offers and how the user reaches every VM operation.

## ADDED Requirements

### Requirement: Manager window
The Manager window SHALL have File, Machine and Help menus, a toolbar (New, Add, Settings, Discard, Start with Normal and Headless choices, Show, Shut down), the VM list with each VM's name, guest type icon and state, and a tool area for the selected VM. With no VM it SHALL show a welcome pane with New and Add.

#### Scenario: First start
- **WHEN** the GUI opens with no VM in the database
- **THEN** the welcome pane shows with New and Add, and Settings, Discard and Start are disabled

#### Scenario: State in the list
- **WHEN** a VM is running and another is stopped
- **THEN** the list shows Running for the first and Powered Off for the second, updated without a refresh

### Requirement: Start modes
Start SHALL offer Normal and Headless. Normal SHALL start the VM and open its VM window; Headless SHALL start it without opening one, and Show SHALL open it later. A VM whose screen is not in the app (Reims vGPU's own window, or Display set to QEMU window) SHALL start its own window in both modes, and its VM window SHALL say where the screen is; Headless SHALL be disabled for it, saying why.

#### Scenario: Headless Linux VM
- **WHEN** the user starts a Linux VM with Headless and later picks Show
- **THEN** no window opens at start, the Preview updates, and Show opens the VM window with the current screen

#### Scenario: Reims vGPU VM
- **WHEN** the user opens the Start menu for a Reims vGPU VM
- **THEN** Headless is disabled with a note that Reims vGPU always opens its own window

### Requirement: Machine tools
For the selected VM the Manager SHALL offer three tools: Details (sections General, System, Display, Storage, Audio, Network, Serial Ports, USB, Shared Folders, Description, with the screen Preview), Snapshots and Logs (the QEMU output of the current or last run). Clicking a Details section SHALL open that Settings page.

#### Scenario: Section to Settings
- **WHEN** the user clicks the Storage section in Details
- **THEN** Settings opens on the Storage page for that VM

### Requirement: Settings pages
VM Settings SHALL have the pages General, System, Display, Storage, Audio, Network, Serial Ports, USB and Shared Folders, SHALL validate on OK and point to the page with the problem, and SHALL disable fields that cannot change while the VM runs, saying why.

#### Scenario: Invalid value
- **WHEN** the user sets memory above the host's RAM and clicks OK
- **THEN** the dialog stays open on System, marks the memory field and saves nothing

#### Scenario: Running VM
- **WHEN** Settings opens for a running VM
- **THEN** System, Storage controllers and the guest type are read-only with a note that the VM must be powered off

### Requirement: Pages follow the guest type
Settings and Details SHALL show only the choices that apply to the VM's guest type: macOS VMs keep VMware SVGA or Reims vGPU graphics, GPU passthrough and root-patch mode; Linux, Windows and Other VMs get their own graphics, disk bus, network model, firmware, Secure Boot and TPM choices.

#### Scenario: Windows VM display
- **WHEN** Settings › Display opens for a Windows VM
- **THEN** Reims vGPU and VMware SVGA are not offered and the Windows graphics choices are

### Requirement: VM window
Starting a VM with Normal, or Show on a running VM, SHALL open one window for that VM with its screen, the menus Machine (Settings, Take Snapshot, Pause, Reset, ACPI Shutdown, Close), View (Full Screen, Scaled Mode, Take Screenshot), Input (Insert Ctrl+Alt+Del, Release Keys), Devices (Optical Drive, Network cable), Help, and a status bar with disk, optical, network, USB activity and the host key.

#### Scenario: Two VMs on screen
- **WHEN** two VMs are started with Normal
- **THEN** each has its own window, with its own name in the title

#### Scenario: Change the ISO while running
- **WHEN** the user picks Devices › Optical Drive › Choose a disk file and selects an ISO
- **THEN** the guest sees the new medium without a restart

### Requirement: Close dialog
Closing a VM window SHALL ask what to do: continue running in the background, send the shutdown signal, or power off; power off SHALL ask for confirmation. Closing the Manager SHALL NOT stop any VM.

#### Scenario: Continue in background
- **WHEN** the user closes a VM window and picks continue running in the background
- **THEN** the window closes, the VM keeps running and Show reopens its window

### Requirement: Pause and reset
The VM window SHALL pause and resume the guest and reset it after confirmation, and the Manager SHALL show Paused while it is paused.

#### Scenario: Pause
- **WHEN** the user picks Machine › Pause
- **THEN** the guest stops running, the screen keeps its last image and the list shows Paused

### Requirement: Screenshot
View › Take Screenshot SHALL save the current guest screen as a PNG file the user chooses.

#### Scenario: Save a screenshot
- **WHEN** the user takes a screenshot of a running VM and picks a file
- **THEN** the file is a PNG of the guest screen at the guest's resolution

### Requirement: Preferences
File › Preferences SHALL have General (default Workspace), Input (host key display), Display (interface scale) and Interface (theme), applied without a restart.

#### Scenario: Interface scale
- **WHEN** the user sets the interface scale to 150 %
- **THEN** every open window redraws at that scale and the choice is kept for the next start

### Requirement: Media manager
File › Media manager SHALL list the hard disk and optical images known to the VMs with size, format, location, and the VMs and snapshots using them. It SHALL create, copy and grow a hard disk image and convert raw to qcow2. Growing SHALL refuse an image with snapshots, and no operation SHALL shrink an image.

#### Scenario: Disk in use
- **WHEN** the user tries to grow a disk attached to a running VM
- **THEN** the change is refused with the VM's name

#### Scenario: Grow a disk
- **WHEN** the user grows a powered-off Linux VM's 40 GiB qcow2 disk without snapshots to 60 GiB
- **THEN** the image's virtual size is 60 GiB, the guest's data is intact, and the dialog says the guest's partition must be extended inside the guest

### Requirement: Remove media
Removing an image from the Media manager SHALL be a separate choice from deleting its file: remove forgets it, delete also deletes the file after confirmation. Both SHALL be refused while any VM or snapshot uses the image, naming them.

#### Scenario: Image used by a snapshot
- **WHEN** the user deletes a disk image that a snapshot of a VM still uses
- **THEN** nothing is deleted and the message names the VM and the snapshot

### Requirement: Activity
The Manager SHALL show, for each running VM, CPU use (the QEMU process's CPU time per second), memory in use (the QEMU process's resident memory) and disk read and write throughput (the guest's block statistics), refreshed at least every 2 seconds.

#### Scenario: Two busy VMs
- **WHEN** VM A runs a CPU-bound workload and VM B a disk-bound one
- **THEN** within 2 seconds A shows high CPU use and B shows disk throughput, each on its own row

### Requirement: Parity matrix
The repository SHALL keep a parity matrix listing each VirtualBox Manager, wizard, Settings page and VM-window function with its status here: covered, adapted (with how) or not applicable (with why). The GUI SHALL NOT be released as the default GUI while a row is open.

#### Scenario: Release gate
- **WHEN** a release of the Wails GUI is prepared
- **THEN** every row of the parity matrix is covered, adapted or not applicable, each with evidence or a reason
