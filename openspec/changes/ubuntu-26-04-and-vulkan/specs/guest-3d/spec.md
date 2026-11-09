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

#### Scenario: Renderer fails
- **WHEN** a VM with Vulkan runs on a host that has what Venus needs but virglrenderer fails on the chosen GPU (Venus starts when the guest's driver first uses it, after the start returned)
- **THEN** QEMU's error is kept in the VM's log, and `vm start` prints it when QEMU exits

#### Scenario: Debian 13
- **WHEN** the user opens Settings › Display of a Linux VM with 3D on Debian 13, whose virglrenderer has no render server and whose Vulkan loader has no device filter
- **THEN** Vulkan is unavailable and the reasons name both

#### Scenario: QEMU without Venus
- **WHEN** a VM with Vulkan on starts on a host whose QEMU's 3D device has no Venus support
- **THEN** QEMU is not started and the message names Venus and the Display setting

#### Scenario: Without 3D
- **WHEN** the user turns 3D acceleration off for a VM with Vulkan on
- **THEN** Vulkan is turned off too and saved so

### Requirement: Host GPU choice
A VM with 3D acceleration SHALL draw on the host GPU it names by PCI address, or on the automatic choice (the first GPU with a Mesa driver, else the first GPU) when it names none. The GPUs offered SHALL be those with a render node, each with its name and driver. A start whose chosen GPU is absent or not accessible SHALL be refused, naming the GPU and the Display setting.

#### Scenario: QEMU's own window
- **WHEN** a VM with 3D and a chosen GPU uses QEMU's own window
- **THEN** its OpenGL draws on the desktop's GPU, Vulkan is not offered (the window cannot show another GPU's Vulkan output), choosing the window turns Vulkan off, and Settings says the GPU choice applies in the app's screen

#### Scenario: Passthrough GPU
- **WHEN** a GPU is reserved for passthrough (bound to `vfio-pci`)
- **THEN** it is not offered as a host GPU for 3D

#### Scenario: Choosing the NVIDIA GPU
- **WHEN** the user chooses the RTX 4070 for a VM with 3D and Vulkan and starts it
- **THEN** OpenGL draws on the RTX 4070's render node and the guest's only Vulkan device is the RTX 4070

#### Scenario: Inherited selector
- **WHEN** kvm-gui runs with `MESA_VK_DEVICE_SELECT` or a Vulkan loader filter in its environment and starts a VM with Vulkan
- **THEN** QEMU's environment has only the filter for the VM's GPU, and the guest's Vulkan device is that GPU

#### Scenario: Two identical GPUs
- **WHEN** the host has two GPUs with the same vendor and device IDs and the VM names the second by its PCI address
- **THEN** OpenGL draws on the second GPU, the guest's Vulkan lists both identical GPUs, and Settings says why

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
