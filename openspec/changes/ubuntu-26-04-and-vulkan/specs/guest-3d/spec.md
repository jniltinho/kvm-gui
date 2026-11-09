# Spec Delta

## ADDED Requirements

### Requirement: OpenGL acceleration
A Linux or Other VM SHALL be able to turn on 3D acceleration: virtio-gpu with virgl, the guest's OpenGL drawn by a GPU of the host without passthrough, shown in the app's screen or in QEMU's own window. Windows and macOS VMs SHALL NOT offer it. A host whose QEMU lacks the 3D graphics device SHALL show the option as unavailable with the reason, and a VM that has it set SHALL NOT start there, naming the missing device and the Display setting.

#### Scenario: Linux with 3D
- **WHEN** a Linux VM with 3D acceleration starts on Ubuntu 26.04 with a GPU
- **THEN** the guest's OpenGL renderer is virgl on the host GPU, and its screen shows in the app

#### Scenario: Host without the device
- **WHEN** the user opens Settings › Display of a Linux VM on Rocky Linux 10, whose QEMU has no `virtio-vga-gl`
- **THEN** 3D acceleration is unavailable and says the host's QEMU lacks the 3D graphics device

### Requirement: Vulkan acceleration
A Linux or Other VM with 3D acceleration SHALL be able to turn on Vulkan (Venus), so the guest's Vulkan runs on the host GPU's Vulkan driver through virtio-gpu. It SHALL be offered only with 3D acceleration on and where the host's QEMU supports Venus, else shown unavailable with the reason. A VM with Vulkan SHALL NOT start on a host that cannot run it, naming what is missing.

#### Scenario: Vulkan in a Linux guest
- **WHEN** a Linux VM with a Mesa that has the virtio Vulkan driver starts with Vulkan on, on Ubuntu 26.04
- **THEN** `vulkaninfo` in the guest lists a Venus device on the chosen host GPU, and a Vulkan program renders in the app's screen

#### Scenario: Renderer fails at start
- **WHEN** a VM with Vulkan starts on a host that has what Venus needs but virglrenderer fails to initialize it on the chosen GPU
- **THEN** the start reports QEMU's error message and the VM's log keeps it

#### Scenario: Debian 13 without the render server
- **WHEN** the user opens Settings › Display of a Linux VM with 3D on Debian 13, whose virglrenderer has no render server
- **THEN** Vulkan is unavailable and the reason names the missing render server

#### Scenario: QEMU without Venus
- **WHEN** a VM with Vulkan on starts on a host whose QEMU's 3D device has no Venus support
- **THEN** QEMU is not started and the message names Venus and the Display setting

#### Scenario: Without 3D
- **WHEN** the user turns 3D acceleration off for a VM with Vulkan on
- **THEN** Vulkan is turned off too and saved so

### Requirement: Host GPU choice
A VM with 3D acceleration SHALL draw on the host GPU it names by PCI address, or on the automatic choice (the first GPU with a Mesa driver, else the first GPU) when it names none. The GPUs offered SHALL be those with a render node, each with its name and driver. A start whose chosen GPU is absent or not accessible SHALL be refused, naming the GPU and the Display setting.

#### Scenario: QEMU's own window
- **WHEN** a VM with 3D, Vulkan and a chosen GPU uses QEMU's own window
- **THEN** its Vulkan runs on the chosen GPU, its OpenGL on the desktop's GPU, and Settings says the choice applies to Vulkan only there

#### Scenario: Passthrough GPU
- **WHEN** a GPU is reserved for passthrough (bound to `vfio-pci`)
- **THEN** it is not offered as a host GPU for 3D

#### Scenario: Choosing the NVIDIA GPU
- **WHEN** the user chooses the RTX 4070 for a VM with 3D and Vulkan and starts it
- **THEN** OpenGL draws on the RTX 4070's render node and the guest's only Vulkan device is the RTX 4070

#### Scenario: Selection layer missing
- **WHEN** a VM with Vulkan names a GPU and the host lacks Mesa's Vulkan device-select layer, or the environment disables it
- **THEN** the start is refused naming the missing layer and its package (an inherited variable that disables it is removed from QEMU's environment, so the selection still holds)

#### Scenario: Two identical GPUs
- **WHEN** the host has two GPUs with the same vendor and device IDs and the VM names the second by its PCI address
- **THEN** OpenGL and the guest's only Vulkan device are the second GPU

#### Scenario: Renumbered render nodes
- **WHEN** after a reboot the chosen GPU's render node is `renderD129` instead of `renderD128`
- **THEN** the VM still draws on the GPU it names

#### Scenario: GPU removed
- **WHEN** a VM names a GPU whose PCI address the host no longer has
- **THEN** it does not start, and the message names the GPU and points to Settings › Display

#### Scenario: Automatic
- **WHEN** a VM with 3D names no GPU
- **THEN** it draws on the automatic choice, and Settings shows which GPU that is

### Requirement: 3D settings in every interface
Settings › Display SHALL show 3D acceleration, Vulkan and the host GPU together, each unavailable one with its reason. The command line (`config set`) and the MCP server (`vm_config_set`) SHALL set the same three settings with the same validation, and `gpu list` SHALL say which GPUs can draw 3D.

#### Scenario: Command line
- **WHEN** the user runs `kvm-gui config set vulkan=true` on a Linux VM without 3D acceleration
- **THEN** the change is refused, saying Vulkan needs 3D acceleration
