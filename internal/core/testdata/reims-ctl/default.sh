#!/bin/sh
# mac: the QEMU command reims-ctl 0.1.0 runs for this VM.
# Its window opens under -display none; QMP is at the -qmp socket.
unset REIMS_VGPU_WINDOW
export REIMS_VGPU_LOG='warn,reims_vgpu_core::device::lifecycle=info'
exec /r/bin/qemu-system-x86_64 \
  -name mac \
  -machine 'q35,memory-backend=ram,dump-guest-core=off' \
  -object 'memory-backend-memfd,id=ram,size=8192M,share=on' \
  -accel kvm \
  -m 8192M \
  -cpu 'Skylake-Client-noTSX-IBRS,kvm=on,vendor=GenuineIntel,+invtsc,vmware-cpuid-freq=on,+ssse3,+sse4.2,+popcnt,+avx,+aes,+xsave,+xsaveopt,check' \
  -smp '4,sockets=1,cores=4,threads=1' \
  -global 'ICH9-LPC.acpi-pci-hotplug-with-bridge-support=off' \
  -global 'ICH9-LPC.disable_s3=1' \
  -device 'pcie-root-port,bus=pcie.0,addr=0x1,slot=1,x-speed=16,x-width=32' \
  -device 'isa-applesmc,osk=ourhardworkbythesewordsguardedpleasedontsteal(c)AppleComputerInc' \
  -drive 'if=pflash,format=raw,readonly=on,file=/tmp/h/vms/my mac/OVMF_CODE.fd' \
  -drive 'if=pflash,format=raw,file=/tmp/h/vms/my mac/OVMF_VARS.fd' \
  -smbios 'type=2' \
  -device 'ich9-ahci,id=sata,addr=0x2' \
  -drive 'id=opencore,if=none,format=qcow2,file=/tmp/h/vms/my mac/opencore.qcow2' \
  -device 'ide-hd,bus=sata.2,drive=opencore,bootindex=1' \
  -drive 'id=disk,if=none,format=qcow2,file=/tmp/h/vms/my mac/disk.qcow2' \
  -device 'ide-hd,bus=sata.3,drive=disk,rotation_rate=1' \
  -drive 'id=installer,if=none,format=raw,snapshot=on,file=/tmp/h/inst.img' \
  -device 'ide-hd,bus=sata.4,drive=installer' \
  -netdev 'user,id=net0,ipv6=off,hostfwd=tcp:127.0.0.1:2299-:22' \
  -device 'virtio-net-pci,netdev=net0,id=net0,mac=52:54:00:12:34:56,addr=0x3' \
  -vga none \
  -device 'apple-gfx-pci,id=gfx,bus=pcie.0,addr=0x4,displays=1,resolution=1920x1080' \
  -device 'apple-gfx-iosurface-pci,id=iosfc,gpu=gfx,bus=pcie.0,addr=0x6' \
  -device 'apple-videotoolbox-pci,id=video,bus=pcie.0,addr=0x8' \
  -device 'apple-m2scaler-pci,id=scaler,bus=pcie.0,addr=0x9' \
  -device 'qemu-xhci,id=xhci,addr=0x5,conditional-intr-mapping=on' \
  -device 'usb-kbd,id=kbd,bus=xhci.0' \
  -device 'usb-tablet,id=tablet,bus=xhci.0' \
  -audiodev 'pipewire,id=snd0' \
  -device 'virtio-sound-pci,id=audio,audiodev=snd0,addr=0x7' \
  -device 'ich9-intel-hda,id=hda,addr=0x1b' \
  -device 'hda-output,bus=hda.0,audiodev=snd0' \
  -display none \
  -serial none \
  -monitor none \
  -qmp 'unix:/tmp/h/vms/my mac/qmp.sock,server=on,wait=off' \
  -pidfile '/tmp/h/vms/my mac/qemu.pid'
