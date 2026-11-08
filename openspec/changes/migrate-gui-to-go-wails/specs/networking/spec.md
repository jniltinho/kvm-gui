# Spec Delta

## MODIFIED Requirements

### Requirement: Adapters
The system SHALL support up to four adapters per VM, each enabled or disabled, attached to NAT or to a host bridge, with a MAC address and an adapter type: `virtio-net-pci`, `vmxnet3` or `e1000-82545em` for macOS VMs, and `virtio-net-pci`, `e1000e` or `e1000` for Linux, Windows and Other VMs. It SHALL start QEMU with one network device per enabled adapter.

#### Scenario: Second adapter on a bridge
- **WHEN** adapter 1 is NAT and adapter 2 is bridged to `br0` with type `vmxnet3`
- **THEN** QEMU gets a user-mode netdev for adapter 1 and a bridge netdev on `br0` with a `vmxnet3` device for adapter 2

#### Scenario: Type of another guest type
- **WHEN** the user sets type `vmxnet3` on a Windows VM through `config set`
- **THEN** the change is refused with the types allowed for Windows VMs

### Requirement: Adapter type defaults
The system SHALL default new adapters of macOS VMs to `vmxnet3` through Catalina and `virtio-net-pci` from Big Sur, and SHALL warn in the settings about a type the VM's macOS version has no built-in driver for. Other guest types SHALL default to their OS profile's type, and Windows VMs set to `virtio-net-pci` SHALL be warned that the guest needs the virtio drivers.

#### Scenario: Intel adapter on Sonoma
- **WHEN** the user picks Intel PRO/1000 MT Desktop for a Sonoma VM
- **THEN** the settings warn that macOS Sonoma has no driver for it, and still allow saving

#### Scenario: Windows 11 defaults
- **WHEN** a Windows 11 VM is created
- **THEN** its first adapter is NAT with type `e1000e`
