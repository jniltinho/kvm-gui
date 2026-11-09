<script setup lang="ts">
// Manager › Details: the selected VM's settings by section, with the screen Preview. A section's title
// opens that Settings page (vm-manager-ui › Machine tools).
import { computed } from "vue";
import type { VMInfo } from "./api";
import { base, graphicsOf, guestLabel, isMac, label, size } from "./labels";
import Screen from "./Screen.vue";

const props = defineProps<{ vm: VMInfo }>();
const emit = defineEmits<{ open: [page: string] }>();

// The app has the screen unless QEMU draws it in its own window; Reims vGPU always serves it to the app too.
const previewable = computed(() => props.vm.config.graphics === "reims-vgpu" || props.vm.config.display !== "window");

type Section = { page: string; title: string; icon: string; rows: [string, string][] };

const sections = computed<Section[]>(() => {
  const v = props.vm;
  const c = v.config;
  const mac = isMac(c);
  const threads = c.cores ? c.vcpus / c.cores : 1;
  const storage: [string, string][] = v.disks.map((d, i) => [
    mac ? `SATA Port ${i}` : `Disk ${i + 1} (${label(d.bus)})`,
    d.missing ? `${base(d.path)} — missing` : `${base(d.path)} (${d.format}, ${size(d.sizeBytes)}, ${size(d.usedBytes)} used)`,
  ]);
  if (c.backend === "reims-ctl") storage.push(["Boot disk", "As reims-ctl sets it up"]);
  else if (mac) {
    if (v.recovery) storage.push(["SATA Port 1", `${base(v.recovery)} (${c.version} recovery)`]);
    storage.push(["Boot disk", "OpenCore (built in, read-only)"]);
  } else {
    (c.optical ?? []).forEach((o, i) => storage.push([`Optical drive ${i + 1}`, o ? base(o) : "Empty"]));
    if (!v.disks.length && !(c.optical ?? []).length) storage.push(["Devices", "None"]);
  }
  const nics = (c.networks ?? []).map((n, i): [string, string] =>
    [`Adapter ${i + 1}`, n.enabled ? `${label(n.model)} (${label(n.mode)}${n.mode === "bridged" && n.bridge ? ", " + n.bridge : ""})` : "Disabled"]);
  if (!nics.length) nics.push(["Adapters", "None"]);
  nics.push(["Port forwarding", `SSH 127.0.0.1:${c.ssh_port} to guest port 22`]);
  const shares = c.shared_folders ?? [];
  return [
    { page: "general", title: "General", icon: "general", rows: [
      ["Name", v.name], ["Operating system", v.os], ["Guest type", guestLabel(v.guest)], ["Folder", c.workspace],
      ...(c.backend === "reims-ctl" ? [["Managed by", "reims-ctl"]] as [string, string][] : [])] },
    { page: "system", title: "System", icon: "system", rows: [
      ["Base memory", `${c.ram_mib} MiB`],
      ["Processors", `${c.vcpus} (${c.cores} cores × ${threads} threads)`],
      ["Firmware", mac ? "UEFI (OVMF) with OpenCore" : `${label(c.firmware)}${c.secure_boot ? ", Secure Boot" : ""}`],
      ...(mac ? [] : [["TPM", c.tpm ? "TPM 2.0 (swtpm)" : "None"], ["Boot order", c.boot_order === "disk" ? "Hard disk first" : c.boot_order === "cdrom" ? "Optical drive first" : "Optical drive first while a medium is in"]] as [string, string][]),
      ["Acceleration", "KVM, Q35 chipset"]] },
    { page: "display", title: "Display", icon: "display", rows: [
      ["Graphics controller", label(graphicsOf(c))],
      ...(c.graphics === "reims-vgpu" ? [["Resolution", c.resolution || "1920x1080"]] as [string, string][] : []),
      ...(c.graphics === "virtio-vga-gl" ? [["Vulkan", c.vulkan ? "Venus" : "Off"], ["Host GPU", c.render_gpu || "Automatic"]] as [string, string][] : []),
      ["Screen", c.display === "window" ? "QEMU's own window" : "Inside the app"],
      ...(mac && c.gpu ? [["GPU passthrough", c.gpu]] as [string, string][] : [])] },
    { page: "storage", title: "Storage", icon: "storage", rows: storage },
    { page: "audio", title: "Audio", icon: "audio", rows: [["Controller", "Intel HD Audio (ICH9), duplex"]] },
    { page: "network", title: "Network", icon: "network", rows: nics },
    { page: "serial", title: "Serial Ports", icon: "serial", rows: [["Port 1",
      mac ? "Not available for macOS VMs" : c.serial === "file" ? "Log file serial.log in the VM folder" : c.serial === "socket" ? "Socket in the run directory" : "Disconnected"]] },
    { page: "usb", title: "USB", icon: "usb", rows: [
      ["Controller", mac ? "USB 3 (xHCI) and USB 2 (EHCI)" : "USB 3 (xHCI)"],
      ["Devices", (c.usb_devices ?? []).map((u) => `${u.vendor}:${u.product}`).join(", ") || "Tablet and keyboard only"]] },
    { page: "shared", title: "Shared Folders", icon: "folder", rows: v.guest !== "linux"
      ? [["Shared folders", "Linux VMs only"]]
      : shares.length ? shares.map((s): [string, string] => [s.tag, `${s.path}${s.read_only ? " (read-only)" : ""}`]) : [["Shared folders", "None"]] },
    { page: "general", title: "Description", icon: "description", rows: [["", c.description || "None"]] },
  ];
});
</script>

<template>
  <div class="det">
    <div class="det-main">
      <section v-for="s in sections" :key="s.title" class="sec" :aria-label="s.title">
        <h3 class="sec-h"><button type="button" class="sec-btn" :aria-label="`${s.title}: open in Settings`" @click="emit('open', s.page)">
          <img :src="`./icons/${s.icon}.svg`" alt="" /> {{ s.title }}</button></h3>
        <dl class="sec-b">
          <template v-for="([k, val], i) in s.rows" :key="i">
            <dt v-if="k">{{ k }}:</dt><dd :class="{ wide: !k }">{{ val }}</dd>
          </template>
        </dl>
      </section>
    </div>
    <aside class="prev" aria-label="Preview">
      <h3 class="sec-h"><span class="sec-btn static"><img src="/icons/preview.svg" alt="" /> Preview</span></h3>
      <div v-if="vm.running && previewable" class="prev-screen live">
        <Screen :key="vm.id" :vm="vm.id" :interval="500" :label="`Preview of ${vm.name}`" />
      </div>
      <div v-else class="prev-screen" role="img" :aria-label="`Preview of ${vm.name}: ${vm.running ? 'disabled' : vm.state}`">
        <span>{{ vm.running ? "Preview disabled" : vm.name }}</span></div>
      <p v-if="vm.running && !previewable" class="muted prev-note">The screen is in QEMU's own window.</p>
    </aside>
  </div>
</template>

<style scoped>
.det { display: flex; gap: 10px; align-items: flex-start; padding: 8px; min-width: 0; }
.det-main { flex: 1; min-width: 0; }
.sec { margin-bottom: 6px; border: 1px solid var(--border-subtle); background: var(--surface); }
.sec-h { margin: 0; font-size: 12px; background: var(--panel); border-bottom: 1px solid var(--border-subtle); }
.sec-btn { display: flex; align-items: center; gap: 6px; width: 100%; height: 26px; padding: 0 8px; font: inherit; font-weight: 700; color: var(--link); background: transparent; border: none; text-align: left; cursor: pointer; }
.sec-btn:hover { background: var(--hover); text-decoration: underline; }
.sec-btn.static { color: var(--text); cursor: default; }
.sec-btn.static:hover { background: transparent; text-decoration: none; }
.sec-btn img { width: 16px; height: 16px; }
.sec-b { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 3px 14px; margin: 0; padding: 6px 10px 8px; }
.sec-b dt { color: var(--text-muted); }
.sec-b dd { margin: 0; overflow-wrap: anywhere; }
.sec-b dd.wide { grid-column: 1 / -1; white-space: pre-wrap; }
.prev { flex: none; width: clamp(180px, 22%, 300px); border: 1px solid var(--border-subtle); background: var(--surface); }
.prev-screen { margin: 10px; aspect-ratio: 16 / 10; display: flex; align-items: center; justify-content: center; padding: 6px; background: color-mix(in srgb, var(--accent) 22%, var(--surface)); color: var(--text); border: 4px solid var(--border); font-weight: 700; font-size: 15px; text-align: center; overflow-wrap: anywhere; border-radius: var(--ctl-radius); }
.prev-screen.live { padding: 0; background: #000; }
.prev-note { margin: -4px 10px 10px; }
</style>
