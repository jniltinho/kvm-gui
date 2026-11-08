# Spec Delta

## ADDED Requirements

### Requirement: Passthrough with several running VMs
GPU passthrough SHALL stay a macOS VM option, and at most one GPU SHALL be reserved at a time. While a VM with a passed-through GPU runs, starting another VM with that GPU SHALL be refused, and Release SHALL be refused, naming the running VM. VMs without passthrough SHALL start as usual next to it.

#### Scenario: Second VM with the same GPU
- **WHEN** a macOS VM with the RX 6700 XT passed through is running and the user starts another VM set to the same GPU
- **THEN** the second VM does not start and the message names the running VM

#### Scenario: Linux VM next to it
- **WHEN** a macOS VM with a passed-through GPU is running and the user starts a Linux VM without passthrough
- **THEN** both run

### Requirement: Initramfs per distribution
Reserve and Release SHALL rebuild the initramfs with `update-initramfs` on Ubuntu and Debian and with `dracut` on Rocky Linux 10, keeping the same rollback on failure.

#### Scenario: Reserve on Rocky Linux
- **WHEN** the user reserves an eligible GPU on Rocky Linux 10
- **THEN** the reservation files are written, `dracut` rebuilds the initramfs of every installed kernel, and a failure removes the files and rebuilds again
