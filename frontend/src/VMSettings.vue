<script setup lang="ts">
// Machine › Settings (spec vm-manager-ui › Settings pages): one page per device group, the choices of the
// VM's guest type only, read-only fields while it runs, and OK validated by the core (a FieldError opens
// its page, marks the field and focuses it; nothing is saved). A reims-ctl VM changes only its processors,
// memory, resolution and SSH port (the core hands them to reims-ctl), plus the app's own name and description.
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { GetCatalog, PickFile, PickFolder, SaveVM, type Catalog, type Config, type RenderGPU, type VMInfo } from "./api";
import { askConfirm } from "./ask";
import Dropdown, { type Option } from "./Dropdown.vue";
import Modal from "./Modal.vue";
import TabBar from "./TabBar.vue";
import { base, graphicsOf, guestLabel, isMac, label, size } from "./labels";

const props = defineProps<{ vm: VMInfo; page: string }>();
const emit = defineEmits<{ close: []; saved: [] }>();

const PAGES = [
  ["general", "General", "general"], ["system", "System", "system"], ["display", "Display", "display"],
  ["storage", "Storage", "storage"], ["audio", "Audio", "audio"], ["network", "Network", "network"],
  ["serial", "Serial Ports", "serial"], ["usb", "USB", "usb"], ["shared", "Shared Folders", "folder"],
] as const;
const page = ref<string>(props.page);
const c = ref<Config>(JSON.parse(JSON.stringify(props.vm.config)));
const cat = ref<Catalog | null>(null);
const mac = computed(() => isMac(c.value));
const running = computed(() => props.vm.running);
const reims = computed(() => props.vm.config.backend === "reims-ctl");
const fixed = computed(() => running.value || reims.value); // what only the app's own VMs change, powered off
const problem = ref<{ field: string; error: string } | null>(null);
const busy = ref(false);
const nic = ref(0);
const heading = ref<HTMLElement>();

const PAGE_OF = (f: string) =>
  /^(name|workspace|guest|profile|version|description)$/.test(f) ? "general"
  : /^(ram|cores|vcpus|firmware|secure_boot|tpm|boot_order)$/.test(f) ? "system"
  : /^(graphics|resolution|display|gpu|root_patch)$/.test(f) ? "display"
  : /^(disks|disk\d+|optical\d+)$/.test(f) ? "storage"
  : /^(networks|nic\d|ssh_port)$/.test(f) ? "network"
  : f === "serial" ? "serial" : f === "usb" ? "usb" : f === "shared_folders" ? "shared" : "general";
const isBad = (...f: string[]) => !!problem.value && f.includes(problem.value.field);
const bad = (...f: string[]) => (isBad(...f) ? "bad-field" : "");
// A field with the problem: marked, and described by the message at the top of the page.
const inv = (...f: string[]) => (isBad(...f) ? { class: "bad-field", "aria-invalid": "true", "aria-describedby": "s-err" } : {});

// Processors: vCPUs as cores of two threads when even (as the wizard and core.Recommended do).
const vcpus = computed({
  get: () => c.value.vcpus,
  set: (v: number) => { c.value.vcpus = v; c.value.cores = v >= 2 && v % 2 === 0 ? v / 2 : Math.max(1, v); },
});
const macCPUs: Option<number>[] = [1, 2, 4, 8, 16, 32].map((x) => [x, `${x}`]);
// Base memory: the core's minimum for the guest, up to the host's memory.
const minRam = computed(() => (mac.value ? cat.value?.minMacRamMiB : cat.value?.profiles.find((p) => p.id === c.value.profile)?.minRamMiB) ?? 256);
const maxRam = computed(() => cat.value?.hostMemMiB || 65536);
const versionOptions = computed<Option<string>[]>(() => (cat.value?.versions ?? []).map((v) => [v.id, `macOS ${v.name}`]));
const profileOptions = computed<Option<string>[]>(() => (cat.value?.profiles ?? []).map((p, i, all) =>
  [p.id, `${guestLabel(p.guest)}: ${p.name}`, { sep: i > 0 && all[i - 1].guest !== p.guest }]));
function setProfile(id: string) {
  const p = cat.value?.profiles.find((x) => x.id === id);
  if (!p) return;
  c.value.profile = p.id;
  c.value.guest = p.guest;
  if (p.guest !== "linux") c.value.shared_folders = [];
}
const graphicsOptions = computed<Option<string>[]>(() => (mac.value ? ["vmware-svga", "reims-vgpu"] : ["virtio-vga", "vga"]).map((g) => [g, label(g)]));
// 3D acceleration is virtio-vga's virgl variant (virtio-vga-gl), shown as VirtualBox shows it: a box
// under the controller. Windows has no virgl driver.
const graphics = computed({
  get: () => (graphicsOf(c.value) === "virtio-vga-gl" ? "virtio-vga" : graphicsOf(c.value)),
  set: (g: string) => {
    c.value.graphics = g === "vmware-svga" ? "" : g === "virtio-vga" && accel3d.value ? "virtio-vga-gl" : g;
    if (c.value.graphics !== "virtio-vga-gl") c.value.vulkan = false; // Vulkan runs on 3D acceleration
  },
});
const can3d = computed(() => !mac.value && c.value.guest !== "windows" && graphics.value === "virtio-vga" && cat.value?.render3D?.can3D !== false);
const accel3d = computed({
  get: () => c.value.graphics === "virtio-vga-gl",
  set: (on: boolean) => {
    c.value.graphics = on ? "virtio-vga-gl" : "virtio-vga";
    if (!on) c.value.vulkan = false; // Vulkan runs on 3D acceleration
  },
});
const hint3d = computed(() => c.value.guest === "windows" ? "Not for Windows: it has no driver for it."
  : graphics.value !== "virtio-vga" ? "Needs the Paravirtualized (virtio-vga) controller."
    : cat.value?.render3D?.can3D === false ? `Not on this host: ${cat.value.render3D.reason3D}.`
      : "The guest's OpenGL drawn by the host's GPU (virgl), with no passthrough: smoother desktops and video in Linux.");
// Vulkan (Venus) and the host GPU that draws 3D (OpenSpec ubuntu-26-04-and-vulkan).
const canVulkan = computed(() => accel3d.value && !!cat.value?.render3D?.canVulkan && c.value.display !== "window");
const vulkan = computed({ get: () => !!c.value.vulkan, set: (on: boolean) => { c.value.vulkan = on; } });
// lspci's "Vendor Inc. Chip [Marketing name] (rev xx)" as "Vendor Marketing name".
const GPU_VENDORS: Record<string, string> = { "1002": "AMD", "10de": "NVIDIA", "8086": "Intel" };
function shortGPU(g: RenderGPU): string {
  const n = g.name.replace(/\s*\(rev [0-9a-f]+\)$/, "");
  const last = [...n.matchAll(/\[([^\]]+)\]/g)].pop()?.[1];
  const vendor = GPU_VENDORS[g.id.split(":")[0]];
  return last && vendor && !last.includes("/ATI") ? `${vendor} ${last}` : n;
}
const autoGPU = computed(() => cat.value?.renderGPUs.find((g) => g.addr === cat.value?.autoRenderGPU));
const gpuOptions = computed<Option<string>[]>(() => {
  const gpus = cat.value?.renderGPUs ?? [];
  const opts: Option<string>[] = [["", autoGPU.value ? `Automatic (${shortGPU(autoGPU.value)})` : "Automatic"]];
  for (const g of gpus) opts.push([g.addr, `${shortGPU(g)} · ${g.driver}${g.vulkan ? "" : " · no Vulkan driver"}`]);
  const saved = c.value.render_gpu;
  if (saved && !gpus.some((g) => g.addr === saved)) opts.push([saved, `${saved} (not on this host)`]);
  return opts;
});
const renderGPU = computed({ get: () => c.value.render_gpu ?? "", set: (v: string) => { c.value.render_gpu = v || undefined; } });
const chosenGPU = computed(() => (c.value.render_gpu ? cat.value?.renderGPUs.find((g) => g.addr === c.value.render_gpu) : autoGPU.value));
const RES_NAMES: Record<string, string> = {
  "1280x720": "HD", "1280x800": "WXGA", "1366x768": "HD laptop", "1440x900": "WXGA+", "1600x900": "HD+",
  "1680x1050": "WSXGA+", "1920x1080": "Full HD", "1920x1200": "WUXGA", "2560x1080": "Ultrawide Full HD",
  "2560x1440": "QHD", "2560x1600": "WQXGA", "3440x1440": "Ultrawide QHD", "3840x2160": "4K UHD",
};
// macOS on Reims vGPU: the sizes it supports; other guests: the common monitor sizes.
const resolutionOptions = computed<Option<string>[]>(() => ((mac.value ? cat.value?.resolutions : cat.value?.guestResolutions) ?? [])
  .map((r) => [r, RES_NAMES[r] ? `${RES_NAMES[r]} (${r.replace("x", "×")})` : r.replace("x", "×")]));
const DISPLAY: Option<string>[] = [["app", "Inside the app"], ["window", "QEMU's own window"]];
const display = computed({
  get: () => c.value.display || "app",
  set: (v: string) => {
    c.value.display = v === "app" ? "" : v;
    if (v === "window") c.value.vulkan = false; // Vulkan runs in the app's screen only
  },
});
const FIRMWARE: Option<string>[] = [["uefi", "UEFI"], ["bios", "Legacy BIOS"]];
const BOOT: Option<string>[] = [["", "Optical drive first while a medium is in"], ["cdrom", "Optical drive first"], ["disk", "Hard disk first"]];
const BUSES: Option<string>[] = [["virtio", label("virtio")], ["sata", label("sata")], ["nvme", label("nvme")]];
const SERIAL: Option<string>[] = [["", "Disconnected"], ["file", "Log file (serial.log in the VM folder)"], ["socket", "Socket in the run directory"]];
const MODES: Option<string>[] = [["nat", "NAT"], ["bridged", "Bridged"]];
const nicModels = computed<Option<string>[]>(() => ((mac.value ? cat.value?.nicModels : cat.value?.guestNicModels) ?? []).map((m) => [m, label(m)]));
const ROOT_PATCH_VERSIONS = ["monterey", "ventura", "sonoma", "sequoia"];
const NICS = [["0", "Adapter 1", "network"], ["1", "Adapter 2", "network"], ["2", "Adapter 3", "network"], ["3", "Adapter 4", "network"]] as const;
const nicTab = computed({ get: () => String(nic.value) as (typeof NICS)[number][0], set: (v: string) => { nic.value = Number(v); } });

// Network adapters 1–4; a missing one is added disabled with a fresh MAC (as core.Config.NIC does).
function randomMAC() {
  const b = crypto.getRandomValues(new Uint8Array(6));
  b[0] = (b[0] & 0xfc) | 0x02;
  return [...b].map((x) => x.toString(16).padStart(2, "0")).join(":");
}
watch(nic, (i) => {
  const nets = (c.value.networks ??= []);
  while (nets.length <= i) nets.push({ enabled: false, mode: "nat", model: mac.value ? "vmxnet3" : "virtio-net-pci", mac: randomMAC() });
}, { immediate: true });
const adapter = computed(() => c.value.networks![nic.value]);
function dropTrailingDisabled() {
  const nets = c.value.networks ?? [];
  while (nets.length && !nets[nets.length - 1].enabled && nets.length > (props.vm.config.networks?.length ?? 0)) nets.pop();
}

async function addDisk() {
  const p = await PickFile("Attach an existing disk image", "disk").catch(() => "");
  if (!p) return;
  const fmt = /\.qcow2$/i.test(p) ? "qcow2" : "raw";
  (c.value.disks ??= []).push({ path: p, format: fmt, bus: cat.value?.profiles.find((x) => x.id === c.value.profile)?.diskBus ?? "sata" });
}
async function chooseISO(i: number) {
  const p = await PickFile("Choose a disc image", "iso").catch(() => "");
  if (p) (c.value.optical ??= [])[i] = p;
}
async function addShare() {
  const p = await PickFolder("Choose a host folder to share").catch(() => "");
  if (!p) return;
  const tag = base(p).replace(/[^A-Za-z0-9_.-]/g, "_").slice(0, 31) || "share";
  (c.value.shared_folders ??= []).push({ path: p, tag, read_only: true });
}

async function ok() {
  problem.value = null;
  dropTrailingDisabled();
  let reset = false;
  const was = props.vm.config;
  if (!mac.value && props.vm.hasVars && !running.value && !!c.value.secure_boot !== !!was.secure_boot && c.value.firmware === "uefi") {
    if (!(await askConfirm("Secure Boot", "Changing Secure Boot replaces the VM's UEFI variables with a fresh copy. Boot entries stored in them are lost. Continue?", "Replace"))) return;
    reset = true;
  }
  busy.value = true;
  try {
    const r = await SaveVM(props.vm.id, c.value, reset);
    if (r.error) {
      problem.value = { field: r.field ?? "", error: r.error };
      if (r.field) page.value = PAGE_OF(r.field);
      const m = /^nic(\d)$/.exec(r.field ?? "");
      if (m) nic.value = Number(m[1]) - 1;
      setTimeout(() => (document.querySelector<HTMLElement>(".st-page [aria-invalid='true']") ?? heading.value)?.focus());
      return;
    }
    emit("saved");
  } catch (e) { problem.value = { field: "", error: String(e) }; } finally { busy.value = false; }
}

// The page list is a vertical tablist: ↑/↓/Home/End move the focus and the page (the focus stays on the list);
// a click opens the page and moves the focus to its heading.
function navKey(e: KeyboardEvent) {
  const i = PAGES.findIndex((p) => p[0] === page.value);
  const next = { ArrowDown: i + 1, ArrowUp: i - 1, Home: 0, End: PAGES.length - 1 }[e.key];
  if (next === undefined || next < 0 || next >= PAGES.length) return;
  e.preventDefault();
  page.value = PAGES[next][0];
  (e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>("[role=tab]")[next]?.focus();
}
function open(id: string) {
  page.value = id;
  nextTick(() => heading.value?.focus());
}
const title = computed(() => PAGES.find((p) => p[0] === page.value)?.[1] ?? "");
onMounted(async () => { try { cat.value = await GetCatalog(); } catch (e) { problem.value = { field: "", error: String(e) }; } });
</script>

<template>
  <Modal :title="`${vm.name} — Settings`" :width="860" :height="600" :busy="busy" @close="emit('close')">
    <div class="st">
      <div class="st-nav" role="tablist" aria-orientation="vertical" aria-label="Settings pages" @keydown="navKey">
        <button v-for="[id, l, ic] in PAGES" :id="`s-tab-${id}`" :key="id" type="button" role="tab" class="st-tab" :class="{ on: page === id }"
          :aria-selected="page === id" aria-controls="s-page" :tabindex="page === id ? 0 : -1" @click="open(id)">
          <img :src="`./icons/${ic}.svg`" alt="" /> {{ l }}</button>
      </div>
      <div id="s-page" class="st-page" role="tabpanel" :aria-labelledby="`s-tab-${page}`">
        <h2 ref="heading" class="st-h" tabindex="-1">{{ title }}</h2>
        <p v-if="problem" id="s-err" class="note bad" role="alert">{{ problem.error }}</p>
        <p v-if="reims && !['audio'].includes(page)" class="note">
          reims-ctl keeps this VM: here you can change its processors, memory, resolution and SSH port (and its name and description in the app). The rest is shown as reims-ctl has it.</p>
        <p v-if="running && ['general', 'system', 'display', 'storage'].includes(page)" class="note warn">
          {{ vm.name }} is running: the guest type, System, Display adapter and storage cannot change until it is powered off.</p>

        <!-- General -->
        <template v-if="page === 'general'">
          <div class="form">
            <label for="s-name">Name</label>
            <input id="s-name" v-model="c.name" class="in" v-bind="inv('name')" spellcheck="false" />
            <span class="lbl">Guest type</span>
            <span>{{ guestLabel(vm.guest) }}</span>
            <template v-if="mac">
              <span class="lbl">macOS version</span>
              <span v-if="reims">{{ versionOptions.find((o) => o[0] === c.version)?.[1] ?? "Chosen in reims-ctl" }}</span>
              <Dropdown v-else v-model="c.version" :options="versionOptions" :width="260" aria-label="macOS version" :disabled="running" v-bind="inv('version')" />
            </template>
            <template v-else>
              <span class="lbl">OS profile</span>
              <Dropdown :model-value="c.profile ?? ''" :options="profileOptions" :width="260" aria-label="OS profile" :disabled="running" v-bind="inv('profile', 'guest')" @update:model-value="setProfile" />
            </template>
            <span class="lbl">Folder</span>
            <span class="mono">{{ c.workspace }}</span>
            <label for="s-desc">Description</label>
            <textarea id="s-desc" v-model="c.description" class="in" rows="6" v-bind="inv('description')"></textarea>
          </div>
        </template>

        <!-- System -->
        <template v-else-if="page === 'system'">
          <div class="form">
            <label for="s-ram">Base memory</label>
            <div class="row">
              <input v-model.number="c.ram_mib" type="range" class="grow" :min="minRam" :max="maxRam" step="256" :disabled="running" aria-label="Base memory slider" />
              <input id="s-ram" v-model.number="c.ram_mib" type="number" class="in" :min="minRam" :max="maxRam" step="256" :disabled="running" aria-describedby="s-ram-range" v-bind="inv('ram')" /> MiB</div>
            <span></span><span id="s-ram-range" class="muted" :class="{ bad: isBad('ram') }">Allowed: {{ minRam }} to {{ maxRam }} MiB (the host has {{ cat?.hostMemMiB ?? "?" }} MiB).</span>
            <label :for="mac ? undefined : 's-cpu'" :class="{ lbl: mac }">Processors</label>
            <div class="row">
              <Dropdown v-if="mac" v-model="vcpus" :options="macCPUs" :width="90" aria-label="Processors" :disabled="running" v-bind="inv('vcpus', 'cores')" />
              <input v-else id="s-cpu" v-model.number="vcpus" type="number" class="in" min="1" :max="cat?.hostThreads" :disabled="running" v-bind="inv('vcpus', 'cores')" />
              <span class="muted">{{ c.cores }} cores × {{ c.cores ? c.vcpus / c.cores : 1 }} threads; the host has {{ cat?.hostThreads ?? "?" }} threads</span></div>
            <template v-if="mac">
              <span class="lbl">Firmware</span><span>UEFI (OVMF) with OpenCore, built in</span>
            </template>
            <template v-else>
              <span class="lbl">Firmware</span>
              <Dropdown v-model="c.firmware" :options="FIRMWARE" :width="160" aria-label="Firmware" :disabled="running" v-bind="inv('firmware')" />
              <span></span>
              <label class="chk" :class="{ off: running || c.firmware !== 'uefi' }"><input v-model="c.secure_boot" type="checkbox" :disabled="running || c.firmware !== 'uefi'" v-bind="inv('secure_boot')" /> Secure Boot (Microsoft keys enrolled)</label>
              <span></span>
              <label class="chk" :class="{ off: running }"><input v-model="c.tpm" type="checkbox" :disabled="running" v-bind="inv('tpm')" /> TPM 2.0 (swtpm)</label>
              <span class="lbl">Boot order</span>
              <Dropdown :model-value="c.boot_order ?? ''" :options="BOOT" :width="300" aria-label="Boot order" :disabled="running" v-bind="inv('boot_order')" @update:model-value="(v: string) => (c.boot_order = v)" />
            </template>
            <span class="lbl">Acceleration</span><span>KVM, Q35 chipset</span>
          </div>
        </template>

        <!-- Display -->
        <template v-else-if="page === 'display'">
          <div class="form">
            <span class="lbl">Graphics controller</span>
            <Dropdown v-model="graphics" :options="graphicsOptions" :width="260" aria-label="Graphics controller" :disabled="fixed" v-bind="inv('graphics')" />
            <template v-if="!mac">
              <span class="lbl">Features</span>
              <div>
                <label class="chk" :class="{ off: fixed || (!can3d && !accel3d) }"><input v-model="accel3d" type="checkbox" :disabled="fixed || (!can3d && !accel3d)" /> Enable 3D acceleration</label>
                <p class="muted">{{ hint3d }}</p>
                <label class="chk" :class="{ off: fixed || (!canVulkan && !vulkan) }"><input v-model="vulkan" type="checkbox" :disabled="fixed || (!canVulkan && !vulkan)" /> Enable Vulkan (Venus)</label>
                <p class="muted">{{ !accel3d ? "Needs 3D acceleration." : display === "window" ? "Not in QEMU's own window, which draws with the desktop's GPU: set Screen to Inside the app." : cat?.render3D?.canVulkan ? "The guest's Vulkan runs on the host GPU's driver: games and apps that need Vulkan. The guest needs Mesa's virtio Vulkan driver (Ubuntu 24.04 or later have it)." : `Not on this host: ${(cat?.render3D?.vulkanReasons ?? []).join("; ")}.` }}</p>
              </div>
              <template v-if="accel3d">
                <span class="lbl">Host GPU</span>
                <Dropdown v-model="renderGPU" :options="gpuOptions" :width="420" aria-label="Host GPU" :disabled="fixed" v-bind="inv('render_gpu')" />
                <p v-if="c.render_gpu && !cat?.renderGPUs.some((g) => g.addr === c.render_gpu)" class="span2 note warn">This GPU is not on this host now: the VM will not start until you choose another one.</p>
                <p v-else-if="vulkan && chosenGPU && !chosenGPU.vulkan" class="span2 note warn">{{ shortGPU(chosenGPU) }} has no Vulkan driver installed on this host.</p>
                <p v-else-if="vulkan && chosenGPU?.twin" class="span2 muted">Another GPU of the same model is on this host: the guest's Vulkan lists both, and OpenGL draws on this one.</p>
                <p v-if="display === 'window'" class="span2 muted">In QEMU's own window, OpenGL draws on the desktop's GPU: this choice applies in the app's screen.</p>
              </template>
            </template>
            <template v-if="c.graphics === 'reims-vgpu' || !mac">
              <span class="lbl">Resolution</span>
              <Dropdown :model-value="c.resolution || '1920x1080'" :options="resolutionOptions" :width="240" aria-label="Resolution" :disabled="running" v-bind="inv('resolution')" @update:model-value="(v: string) => (c.resolution = v)" />
              <p v-if="!mac" class="span2 muted">The screen size the guest starts with (it can still change it in its own display settings).</p>
              <div v-if="c.graphics === 'reims-vgpu' && cat?.reimsMissing" class="span2 note warn">Reims vGPU is not installed on this host: install it before you start this VM.
                <details class="tech"><summary>Details</summary><p>{{ cat.reimsMissing }}</p></details></div>
            </template>
            <span class="lbl">Screen</span>
            <Dropdown v-model="display" :options="DISPLAY" :width="200" aria-label="Screen" :disabled="fixed || c.graphics === 'reims-vgpu'" v-bind="inv('display')" />
            <template v-if="mac">
              <label for="s-gpu">GPU passthrough</label>
              <input id="s-gpu" v-model="c.gpu" class="in" placeholder="None (PCI address such as 0000:03:00.0)" aria-describedby="s-gpu-help" :disabled="fixed || c.graphics === 'reims-vgpu'" v-bind="inv('gpu')" spellcheck="false" />
              <span></span><span id="s-gpu-help" class="muted">{{ reims ? "Not available for VMs that reims-ctl keeps." : c.graphics === "reims-vgpu" ? "Not with Reims vGPU." : "The GPU must be bound to vfio-pci on the host first." }}</span>
              <span></span>
              <label class="chk" :class="{ off: fixed || !ROOT_PATCH_VERSIONS.includes(c.version) }"><input v-model="c.root_patch" type="checkbox" :disabled="fixed || !ROOT_PATCH_VERSIONS.includes(c.version)" v-bind="inv('root_patch')" /> Root-patch mode (experimental, Monterey to Sequoia)</label>
              <span></span><span class="muted">Lowers SIP and loads OpenCore Legacy Patcher's helpers so it can root-patch a passed-through NVIDIA Kepler GPU inside macOS.</span>
            </template>
          </div>
        </template>

        <!-- Storage -->
        <template v-else-if="page === 'storage'">
          <section class="grp"><h3 class="grp-h">Hard disks</h3><div class="grp-b">
            <table class="tbl" aria-label="Hard disks">
              <thead><tr><th>Image</th><th>Format</th><th>Bus</th><th>Size</th><th v-if="!mac"><span class="sr">Actions</span></th></tr></thead>
              <tbody>
                <template v-if="mac">
                  <tr v-for="d in vm.disks" :key="d.path"><td class="mono">{{ d.path }}</td><td>{{ d.format }}</td><td>{{ label(d.bus) }}</td><td>{{ size(d.sizeBytes) }}</td></tr>
                </template>
                <tr v-for="(d, i) in mac ? [] : c.disks ?? []" :key="i" :class="bad('disk' + i)">
                  <td class="mono">{{ d.path }}</td><td>{{ d.format }}</td>
                  <td><Dropdown v-model="d.bus" :options="BUSES" :width="130" :aria-label="`Bus of disk ${i + 1}`" :disabled="running" v-bind="inv('disk' + i)" /></td>
                  <td>{{ size(vm.disks[i]?.path.endsWith(d.path) ? vm.disks[i].sizeBytes : 0) }}</td>
                  <td><button type="button" class="btn small" :aria-label="`Detach disk ${i + 1}`" :disabled="running" @click="c.disks!.splice(i, 1)">Detach</button></td>
                </tr>
                <tr v-if="!mac && !(c.disks ?? []).length"><td colspan="5" class="muted">No hard disk: the VM boots its optical drive.</td></tr>
              </tbody>
            </table>
            <div v-if="!mac" class="row"><button type="button" class="btn small" :disabled="running" v-bind="inv('disks')" @click="addDisk">Attach existing disk…</button>
              <span class="muted">A detached disk's file is kept. Creating and growing disks is not available yet.</span></div>
            <p v-if="mac && !reims" class="muted">macOS VMs keep their disk in the VM folder; OpenCore boots from a built-in read-only disk.</p>
          </div></section>
          <section class="grp"><h3 class="grp-h">Optical drives</h3><div class="grp-b">
            <template v-if="mac"><span>{{ reims ? "Set by reims-ctl" : vm.recovery ? base(vm.recovery) + " (recovery)" : "No install media in the folder" }}</span></template>
            <template v-else>
              <div v-for="(o, i) in c.optical ?? []" :key="i" class="row" :class="bad('optical' + i)">
                <span class="drv">Drive {{ i + 1 }}</span>
                <input v-model="c.optical![i]" class="in grow mono" placeholder="Empty" :aria-label="`Optical drive ${i + 1} image`" :disabled="running" v-bind="inv('optical' + i)" spellcheck="false" />
                <button type="button" class="btn small" :aria-label="`Choose an image for optical drive ${i + 1}`" :disabled="running" @click="chooseISO(i)">Choose…</button>
                <button type="button" class="btn small" :aria-label="`Eject optical drive ${i + 1}`" :disabled="running || !o" @click="c.optical![i] = ''">Eject</button>
                <button type="button" class="btn small" :aria-label="`Remove optical drive ${i + 1}`" :disabled="running" @click="c.optical!.splice(i, 1)">Remove</button>
              </div>
              <div class="row"><button type="button" class="btn small" :disabled="running" @click="(c.optical ??= []).push('')">Add optical drive</button>
                <span v-if="running" class="muted">Power the VM off to change its optical drives.</span></div>
            </template>
          </div></section>
        </template>

        <!-- Audio -->
        <template v-else-if="page === 'audio'">
          <div class="form"><span class="lbl">Controller</span><span>Intel HD Audio (ICH9), duplex</span>
            <span class="lbl">Host driver</span><span>QEMU's default audio backend</span></div>
          <p class="muted">Every VM has this controller; it has no settings yet.</p>
        </template>

        <!-- Network -->
        <template v-else-if="page === 'network'">
          <TabBar v-model="nicTab" :tabs="NICS" size="small" panel="s-nic" aria-label="Adapters" />
          <div id="s-nic" class="form tab-page nic-page" role="tabpanel" :aria-labelledby="`s-nic-tab-${nic}`" :class="bad('nic' + (nic + 1))">
            <span></span>
            <label class="chk" :class="{ off: reims }"><input v-model="adapter.enabled" type="checkbox" :disabled="reims" v-bind="inv('nic' + (nic + 1))" /> Enable network adapter</label>
            <span class="lbl">Attached to</span>
            <Dropdown v-model="adapter.mode" :options="MODES" :width="140" aria-label="Attached to" :disabled="reims || !adapter.enabled" v-bind="inv('nic' + (nic + 1))" />
            <template v-if="adapter.mode === 'bridged'">
              <label for="s-br">Bridge</label>
              <div class="row"><input id="s-br" v-model="adapter.bridge" class="in" list="s-bridges" placeholder="br0" :disabled="reims || !adapter.enabled" spellcheck="false" />
                <datalist id="s-bridges"><option v-for="b in cat?.bridges ?? []" :key="b" :value="b" /></datalist>
                <span class="muted">{{ cat?.bridges.length ? "Host bridges: " + cat.bridges.join(", ") : "No bridge on this host" }}</span></div>
            </template>
            <span class="lbl">Adapter type</span>
            <Dropdown v-model="adapter.model" :options="nicModels" :width="260" aria-label="Adapter type" :disabled="reims || !adapter.enabled" />
            <label for="s-mac">MAC address</label>
            <div class="row"><input id="s-mac" v-model="adapter.mac" class="in mono" :disabled="reims || !adapter.enabled" spellcheck="false" />
              <button type="button" class="btn small" aria-label="New random MAC address" :disabled="reims || !adapter.enabled" @click="adapter.mac = randomMAC()">New</button></div>
          </div>
          <section class="grp" style="margin-top: 12px"><h3 class="grp-h">Port forwarding</h3><div class="grp-b form">
            <label for="s-ssh">SSH on 127.0.0.1</label>
            <div class="row"><input id="s-ssh" v-model.number="c.ssh_port" type="number" min="1024" max="65535" class="in" v-bind="inv('ssh_port')" /> to guest port 22</div>
            <p v-if="vm.sshWarn" class="span2 warn">{{ vm.sshWarn }}</p>
          </div></section>
          <p v-if="running" class="muted">Network changes apply at the next start.</p>
        </template>

        <!-- Serial Ports -->
        <template v-else-if="page === 'serial'">
          <p v-if="mac" class="muted">macOS VMs have no serial port.</p>
          <div v-else class="form">
            <span class="lbl">Port 1</span>
            <Dropdown :model-value="c.serial ?? ''" :options="SERIAL" :width="320" aria-label="Serial port 1" v-bind="inv('serial')" @update:model-value="(v: string) => (c.serial = v)" />
            <span></span><span class="muted">Applies at the next start.</span>
          </div>
        </template>

        <!-- USB -->
        <template v-else-if="page === 'usb'">
          <div class="form"><span class="lbl">Controller</span><span>{{ mac ? "USB 3 (xHCI) and USB 2 (EHCI)" : "USB 3 (xHCI)" }}, with a tablet and a keyboard</span></div>
          <section v-if="!mac" class="grp" style="margin-top: 12px"><h3 class="grp-h">Host devices passed through</h3><div class="grp-b">
            <div v-for="(u, i) in c.usb_devices ?? []" :key="i" class="row">
              <input v-model="u.vendor" class="in mono" style="width: 70px" placeholder="vendor" :aria-label="`Vendor id of device ${i + 1}`" v-bind="inv('usb')" spellcheck="false" /> :
              <input v-model="u.product" class="in mono" style="width: 70px" placeholder="product" :aria-label="`Product id of device ${i + 1}`" v-bind="inv('usb')" spellcheck="false" />
              <button type="button" class="btn small" :aria-label="`Remove USB device ${i + 1}`" @click="c.usb_devices!.splice(i, 1)">Remove</button>
            </div>
            <div class="row"><button type="button" class="btn small" @click="(c.usb_devices ??= []).push({ vendor: '', product: '' })">Add device</button>
              <span class="muted">Vendor and product ids as lsusb shows them. The device node must be writable by you; the app never changes host permissions.</span></div>
          </div></section>
          <p v-else class="muted">USB passthrough is for Linux, Windows and Other VMs.</p>
        </template>

        <!-- Shared Folders -->
        <template v-else-if="page === 'shared'">
          <template v-if="c.guest === 'linux'">
            <table class="tbl" aria-label="Shared folders" :class="bad('shared_folders')">
              <thead><tr><th>Host folder</th><th>Mount tag</th><th>Read-only</th><th><span class="sr">Actions</span></th></tr></thead>
              <tbody>
                <tr v-for="(s, i) in c.shared_folders ?? []" :key="i">
                  <td class="mono">{{ s.path }}</td>
                  <td><input v-model="s.tag" class="in" :aria-label="`Mount tag of ${s.path}`" v-bind="inv('shared_folders')" spellcheck="false" /></td>
                  <td><input v-model="s.read_only" type="checkbox" :aria-label="`${s.path} read-only`" /></td>
                  <td><button type="button" class="btn small" :aria-label="`Remove share ${s.tag}`" @click="c.shared_folders!.splice(i, 1)">Remove</button></td>
                </tr>
                <tr v-if="!(c.shared_folders ?? []).length"><td colspan="4" class="muted">No shared folder.</td></tr>
              </tbody>
            </table>
            <div class="row" style="margin-top: 8px"><button type="button" class="btn small" @click="addShare">Add folder…</button>
              <span class="muted">In the guest: <span class="mono">mount -t 9p -o trans=virtio TAG /mnt</span></span></div>
          </template>
          <p v-else class="muted">Shared folders are for Linux VMs.</p>
        </template>
      </div>
    </div>
    <template #footer>
      <span v-if="busy" class="muted" role="status">Saving…</span>
      <button type="button" class="btn primary" :disabled="busy" @click="ok">OK</button>
      <button type="button" class="btn" :disabled="busy" @click="emit('close')">Cancel</button>
    </template>
  </Modal>
</template>

<style scoped>
.st { display: flex; flex: 1; min-height: 0; }
.st-nav { flex: none; width: 180px; padding: 8px 6px; display: flex; flex-direction: column; gap: 2px; background: var(--surface); border-right: 1px solid var(--border-subtle); overflow-y: auto; }
.st-tab { display: flex; align-items: center; gap: 8px; height: 30px; padding: 0 8px; font: inherit; font-size: 12px; color: var(--text); background: transparent; border: 1px solid transparent; text-align: left; cursor: pointer; border-radius: var(--ctl-radius); }
.st-tab img { width: 20px; height: 20px; }
.st-tab:hover:not(.on) { background: var(--hover); }
.st-tab.on { background: var(--selection); color: var(--selection-text); }
.st-tab:focus-visible { outline-offset: 2px; }
.st-page { flex: 1; min-width: 0; overflow-y: auto; padding: 10px 14px; }
.st-h { margin: 0 0 10px; font-size: 15px; font-weight: 600; }
.span2 { grid-column: 2; }
p.span2 { margin: 0; }
.tbl { width: 100%; border-collapse: collapse; table-layout: auto; }
.tbl th { text-align: left; font-weight: 600; padding: 3px 6px; border-bottom: 1px solid var(--border); }
.tbl td { padding: 3px 6px; border-bottom: 1px solid var(--border-subtle); vertical-align: middle; }
.tbl td.mono { max-width: 300px; }
.tbl.bad-field, tr.bad-field td { background: color-mix(in srgb, var(--danger) 12%, transparent); }
.form.bad-field { outline: 1px solid var(--danger); outline-offset: 4px; }
.nic-page { padding: 10px; }
.drv { width: 56px; flex: none; }
</style>
