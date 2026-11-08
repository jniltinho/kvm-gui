# Parity with VirtualBox's Manager

kvm-gui matches the *workflows* of VirtualBox 7's Manager and VM windows, with its own look. VirtualBox
(GPLv3) and virt-manager (GPLv2+) are functional references only: no code, UI files, strings, layouts or
icons are copied (design D2; kvm-gui is BSD-3-Clause).

- **Reference** names the VirtualBox source in `VirtualBox/virtualbox` under
  `src/VBox/Frontends/VirtualBox/src/` (shortened to `…/`), the virt-manager file (`virtManager/…`) where
  it adds to it, and the local capture in `docs/prints/virtualbox/` (git-ignored, `vb-NN`).
- **kvm-gui** names the screen and its capture in `docs/prints/gui/` (git-ignored, `gui-NN`).
- **Status**: *covered* (same workflow), *adapted* (how), *not applicable* (why) or *open* (the task
  that closes it). The Wails GUI is not released as the default GUI while a row is open (spec
  `vm-manager-ui` › Parity matrix, task 7.10).

## Manager window

| VirtualBox element | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| File menu | `…/manager/UIVirtualBoxManager.cpp`; vb-45 | File: Preferences, Media manager, Exit; gui-23 | adapted: Import/Export appliance, Network manager, Cloud and Extension packs are not here (rows below) |
| Machine menu | `…/manager/UIVirtualBoxManager.cpp`; vb-46 | Machine: New, Add, Settings, Remove, Start (Normal/Headless), Show, Shut down, Show log, Refresh; gui-23 | adapted: Clone, Move, Export to OCI, Group, Sort, Create shortcut open (Clone 7.7, Move 5.4); Show disabled until the VM window (6.3) |
| Help menu | `…/manager/UIVirtualBoxManager.cpp` | Help › About kvm-gui | adapted: no online manual, update check or bug-report links (offline app) |
| Toolbar: New | `…/manager/UIVirtualBoxManager.cpp`; vb-01 | New (Ctrl+N); gui-01 | covered |
| Toolbar: Add | idem | Add dialog: a VM settings file (`vm.json` or an export) through `core.ImportSettings`, or a reims-ctl VM not in the list yet (`core.ReimsCtlCandidates`, `core.AddReimsCtlVM`) | adapted: kvm-gui has no `.vbox`; its settings file plays that role |
| Toolbar: Settings | idem | Settings (Ctrl+S) | covered |
| Toolbar: Discard | idem | Remove…: removes the VM from the list, optionally deleting the files the app created (an explicit choice in its confirmation) | adapted: kvm-gui keeps no saved state to discard, so the toolbar names it Remove…, as the Machine menu does |
| Toolbar: Start (Normal, Headless, Detachable) | idem | Start, and Normal or Headless from its ▼ (`core.StartDetached`, a GUI run that outlives the window; Normal lets Reims vGPU open its X11 window); Headless disabled for Reims vGPU with the reason; gui-24 | adapted: the VM window that Normal opens arrives with task 6.3; Detachable is not applicable (every start detaches) |
| Toolbar: Show | idem | Show | open: task 6.3 |
| Toolbar: Shut down / Power off | `…/manager/UIVirtualBoxManager.cpp` (Close submenu) | Shut down sends the ACPI power button over the run's control socket | adapted: Power off and Save state live in the VM window's close dialog (task 7.6) |
| VM list: name, OS icon, state | `…/manager/chooser/UIChooserItemMachine.cpp`; vb-01 | name, guest-type icon, OS, state, memory · CPUs · graphics; refreshed every 2 s; gui-01 | covered |
| VM list groups | `…/manager/chooser/` | — | not applicable: one flat list sorted by name; the VM count stays small on a workstation |
| Welcome pane | `…/manager/UIVirtualBoxManagerWidget.cpp` (home) | Welcome with New and Add, Settings/Remove/Start disabled; gui-00 | covered |
| Tools: Details | `…/manager/details/UIDetailsGenerator.cpp`; vb-10 | Details with General, System, Display, Storage, Audio, Network, Serial Ports, USB, Shared Folders, Description; a section title opens its Settings page; gui-01 | covered |
| Details: Preview | `…/manager/details/UIDetailsElements.cpp` (preview) | Preview box with the VM name | open: live screen with the screen stream (task 6.3) |
| Tools: Snapshots | `…/snapshots/UISnapshotPane.cpp`; vb-11; `virtManager/details/snapshots.py` | Snapshots tool with Current state, Take/Restore/Delete/Properties buttons; gui-08 | open: buttons disabled until the core's snapshots (tasks 5.1, 5.5) |
| Tools: Logs | `…/logviewer/UIVMLogViewerWidget.cpp`; vb-12 | Logs: the newest run log of the VM, its last 256 KiB, Refresh; gui-09 | adapted: one QEMU output log per run instead of VBox.log rotation; the GUI's detached runs write `gui-start.log` there |
| Tools: Activity | `…/activity/vmactivity/UIVMActivityMonitor.cpp`; vb-13 | — | open: task 7.7 |
| Tools: File manager | `…/guestctrl/UIFileManager.cpp`; vb-14 | — | not applicable: needs guest additions; Linux guests use Shared Folders and SSH (port forwarding) |
| Status bar | — | selected VM and state, last action, VM count, version | adapted: VirtualBox's Manager has none; kept from the previous app |
| Global tools: Media, Network, Cloud, Extensions | `…/manager/tools/` | File › Media manager; gui-22 | adapted: Network manager not applicable (NAT and host bridges need no host-only networks); Cloud and Extensions not applicable |

## New VM wizard

| VirtualBox step or field | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| Guided and expert modes | `…/wizards/newvm/UIWizardNewVM.cpp`; vb-07 | Guided steps (Name and OS, Hardware, Hard disk, Summary) and Expert mode on one page; gui-03…07 | covered |
| Name, folder | `…/wizards/newvm/UIWizardNewVMNameOSTypePage.cpp`; vb-07 | Name (duplicate refused), Folder (default `<Workspace>/<name>`); gui-03 | covered |
| ISO image with detection | idem; `virtManager/createvm.py` (libosinfo detection) | Installer ISO with the volume label read offline (`core.DetectProfile`): type, profile and name proposed, unknown labels left to the user; gui-03 | covered |
| OS type / edition / version | idem | Guest type (macOS, Linux, Windows, Other) and macOS version or OS profile | adapted: profiles instead of VirtualBox's OS type list |
| Unattended installation | `…/wizards/newvm/UIWizardNewVMUnattendedPage.cpp` | — | not applicable: installers run by hand in the VM window (macOS from recovery) |
| Hardware: memory, CPUs | `…/wizards/newvm/UIWizardNewVMHardwarePage.cpp`; vb-08 | Base memory and Processors prefilled from `core.Recommended` and the profile; macOS gets only power-of-two core counts; gui-04 | covered |
| Hardware: EFI | idem | Firmware UEFI/BIOS, Secure Boot, TPM 2.0 (Linux, Windows, Other); Graphics per guest type | covered |
| Hard disk: new / existing / none | `…/wizards/newvm/UIWizardNewVMDiskPage.cpp`; vb-09 | New (size, qcow2), Existing (format read from the header), None (live ISO); macOS: a new 80 GiB disk; gui-05 | adapted: VDI/VHD/VMDK and fixed-size images not offered (qcow2, thin) |
| Summary | `…/wizards/newvm/UIWizardNewVMSummaryPage.cpp` | Summary step; gui-06 | covered |
| Finish validation | idem | Finish checks every section and opens the first with a problem; the core validates again | covered |
| Reims vGPU via `reims-ctl` with progress | — (kvm-gui only) | macOS: "Create with reims-ctl (Reims vGPU)" (`core.CreateReimsCtlVM`, reims-ctl's output streamed into the wizard; offline installer option; CPUs, memory, disk size); otherwise Graphics › Reims vGPU, with a note when its QEMU is missing | open: Install Reims vGPU from the app (task 7.3) |

## Clone wizard

| VirtualBox element | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| Name, path, MAC policy | `…/wizards/clonevm/UIWizardCloneVM.cpp`; `virtManager/clone.py` | — | open: tasks 5.3, 7.7 |
| Full / linked clone | `…/wizards/clonevm/UIWizardCloneVMTypePage.cpp` | — | open: full clone only (design D11), tasks 5.3, 7.7 |
| Snapshot mode | `…/wizards/clonevm/UIWizardCloneVMModePage.cpp` | — | open: tasks 5.3, 7.7 |

## Settings

| VirtualBox page | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| Basic / Expert toggle, search | `…/settings/UISettingsDialog.cpp`; vb-31 | — | not applicable: every page shows all its fields |
| General (name, OS, folder, description, encryption) | `…/settings/machine/UIMachineSettingsGeneral.cpp`; vb-30 | Name, guest type, macOS version or OS profile, folder, description; gui-10 | adapted: encryption and clipboard/drag-and-drop not applicable (no guest additions) |
| System: motherboard, memory, boot order, chipset, TPM, EFI, Secure Boot | `…/settings/machine/UIMachineSettingsSystem.cpp`; vb-31 | Base memory (refused above the host's RAM), Processors, Firmware, Secure Boot (replaces the UEFI variables after confirmation), TPM, Boot order; macOS shows OpenCore; gui-11, gui-19 | covered |
| System: processor, acceleration | idem | Processors with cores × threads; KVM and Q35 shown | adapted: PAE, nested VT-x and paravirtualization interface fixed by the profile |
| Display: screen, graphics controller, 3D | `…/settings/machine/UIMachineSettingsDisplay.cpp`; vb-32 | Graphics controller per guest type (macOS: VMware SVGA or Reims vGPU; others: virtio-vga or VGA), Reims resolution, screen inside the app or QEMU's window, GPU passthrough and root-patch mode for macOS; gui-12 | adapted: video memory and monitor count are QEMU device defaults |
| Display: remote display, recording | idem | — | not applicable: the screen is a private loopback stream; screenshots in the VM window (7.6) |
| Storage: controllers, disks, optical | `…/settings/machine/UIMachineSettingsStorage.cpp`; vb-33; `virtManager/addhardware.py` | Disks with bus (virtio, SATA, NVMe), attach existing / detach, optical drives choose / eject / add / remove; macOS disk and recovery shown; gui-13 | adapted: new disks and resizing in the Media manager (open, 7.7) |
| Audio | `…/settings/machine/UIMachineSettingsAudio.cpp`; vb-34 | Intel HD Audio (ICH9) shown; gui-14 | adapted: no settings in the core yet |
| Network: adapters 1–4 | `…/settings/machine/UIMachineSettingsNetwork.cpp`; vb-35 | Adapter 1–4: enable, NAT or bridged (host bridges listed), adapter type per guest type, MAC with New; SSH port forwarding; gui-15 | adapted: host-only, internal and NAT network not applicable (no libvirt or host networks) |
| Serial ports | `…/settings/machine/UIMachineSettingsSerial.cpp`; vb-36 | Port 1: disconnected, log file or socket (Linux, Windows, Other); gui-16 | covered |
| USB | `…/settings/machine/UIMachineSettingsUSB.cpp`; vb-37 | xHCI controller; passthrough by vendor:product (Linux, Windows, Other); gui-17 | adapted: device filters typed as ids; a host device picker needs a core listing (open, 7.4) |
| Shared folders | `…/settings/machine/UIMachineSettingsSF.cpp`; vb-38 | Linux: host folder, mount tag, read-only; gui-18 | covered |
| User interface (menus, status bar, mini toolbar) | `…/settings/machine/UIMachineSettingsInterface.cpp`; vb-39 | — | not applicable: the VM window's menus are fixed |
| Validation that opens the failing page | `…/settings/UISettingsDialog.cpp` | OK validates through `core.SaveVM`; a FieldError opens its page and marks the field; nothing saved; gui-19 | covered |
| Read-only while running | `…/settings/UISettingsDialogSpecific.cpp` | General's type, System, Display adapter and Storage read-only with a note | covered |
| reims-ctl VMs | — (kvm-gui only) | Only processors, memory, resolution and SSH port (to `reims-ctl set`), plus the app's name and description; the rest read-only with a note; GPU passthrough and snapshots disabled with the reason | covered |

## Preferences

| VirtualBox page | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| General: default machine folder | `…/settings/global/UIGlobalSettingsGeneral.cpp`; vb-40 | General › Workspace (Browse fills the field); gui-20 | covered |
| Input: host key and shortcuts | `…/settings/global/UIGlobalSettingsInput.cpp` | Input › host key Ctrl+Alt+F shown | adapted: fixed host key, no shortcut editor |
| Display: scale factor, maximum guest size | `…/settings/global/UIGlobalSettingsDisplay.cpp` | Display › Interface scale 100–200 % | adapted: guest size follows the window |
| Interface (theme) | `…/settings/global/UIGlobalSettingsInterface.cpp` | Interface › Skin (six skins and System); gui-21 | covered |
| OK / Apply / Cancel | `…/settings/UISettingsDialog.cpp` | One policy for every page: OK or Apply applies without a restart; closing with changes not applied asks | covered |
| Language, Update, Proxy, Network, Extensions | `…/settings/global/` | — | not applicable: English only, offline app, no extension packs |

## Media manager

| VirtualBox element | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| Hard disks and optical discs lists with size, format, location, attached VMs | `…/medium/UIMediumManager.cpp` | File › Media manager: both lists with name, location, virtual size, used on host, format and the VMs using each, all shown in the table; gui-22 | covered (read-only, said on the window) |
| Create, copy, resize, convert, remove, release | idem | — (the window says it is read-only for now) | open: task 7.7 (grow only, raw to qcow2, remove versus delete with refusals) |
| Floppy images | idem | — | not applicable: no floppy drive |

## Import and export

| VirtualBox element | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| Import appliance | `…/wizards/importappliance/UIWizardImportApp.cpp`; vb-41 | File › Import Appliance…: `.ova`/`.ovf` (OVF 1.0 and 2.0), a preview of the VM it creates with every setting it cannot keep, name, folder and a host bridge, progress with Cancel; `vm import-ova`, MCP `vm_import_ova` (OpenSpec `add-ova-import-export`) | adapted: Linux, Windows and Other VMs (macOS VMs move as bundles); VirtualBox's appliance-settings table is the preview. Evidence: a VirtualBox 7.2.10 `.ova` imported with an identical disk; GUI flow under Xvfb (`docs/screenshots/import-appliance.png`) |
| Export appliance | `…/wizards/exportappliance/UIWizardExportApp.cpp` | File › Export Appliance… for a powered-off VM: the notes on what is not carried, the TPM/BitLocker acknowledgment, overwrite confirmation, progress with Cancel; `vm export-ova`, MCP `vm_export_ova` | adapted: OVF 1.0 with a SHA-256 manifest and streamOptimized disks only (no OCI, OPC or format choice). Evidence: Ubuntu and Windows 11 VMs imported by VirtualBox 7.2.10 with OS type, EFI, CPUs, memory, disks (identical), NAT and TPM; GUI flow under Xvfb (`docs/screenshots/export-appliance.png`) |
| Bundle export and import (kvm-gui's own) | — | `vm export --full`, `vm import` (macOS, snapshots and TPM state included) | open: GUI dialogs, task 7.7 of `migrate-gui-to-go-wails` |

## VM window

| VirtualBox menu item | Reference | kvm-gui | Status |
| --- | --- | --- | --- |
| Machine › Settings, Take Snapshot, Pause, Reset, ACPI Shutdown, Close | `…/runtime/UIMachineLogic.cpp`; `virtManager/vmwindow.py` | — | open: task 7.6 (snapshots 5.1) |
| View › Full Screen, Scaled Mode, Take Screenshot | `…/runtime/UIMachineLogic.cpp` | — | open: tasks 6.2, 7.6 |
| View › Seamless, Adjust window size, virtual screens | idem | — | not applicable: no guest additions |
| Input › Insert Ctrl+Alt+Del, keyboard and mouse release | idem | — | open: task 7.6 |
| Devices › Optical drive (choose, eject), Network cable | idem | — | open: task 7.6 (core QMP change/eject exists, 3.8) |
| Devices › USB, Shared folders, Drag and drop, Clipboard, Guest Additions | idem | — | not applicable: USB devices are set in Settings; the rest needs guest additions |
| Close dialog: background, shutdown, power off | `…/runtime/UIVMCloseDialog.cpp` | — | open: task 7.6 |
| Status bar: disk, optical, network, USB, host key | `…/runtime/UIIndicatorsPool.cpp` | — | open: task 7.6 |
