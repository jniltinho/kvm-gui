<script setup lang="ts">
// Machine › New: guided (one step per section) or expert (every section on one page). Finish checks every
// section and opens the first one with a problem; the core validates again before anything is created.
// A macOS VM can also be created by reims-ctl (Reims vGPU), whose output streams in as it works.
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { Events } from "@wailsio/runtime";
import { CreateVM, DetectISO, GetCatalog, PickFile, PickFolder, type Catalog, type Guest, type NewVM } from "./api";
import Dropdown, { type Option } from "./Dropdown.vue";
import GuestIcon from "./GuestIcon.vue";
import Modal from "./Modal.vue";
import { GUESTS, guestLabel, label } from "./labels";

const props = defineProps<{ names: string[] }>();
const emit = defineEmits<{ close: []; created: [id: string, notice: string] }>();

const STEPS = [["os", "Name and operating system"], ["hw", "Hardware"], ["disk", "Hard disk"], ["sum", "Summary"]] as const;
type Step = (typeof STEPS)[number][0];
const step = ref<Step>("os");
const expert = ref(false);
const cat = ref<Catalog | null>(null);
const loadError = ref("");
const busy = ref(false);
const detect = ref("");
const problems = ref<Record<string, string>>({});
const serverError = ref("");
const output = ref<string[]>([]); // reims-ctl's lines while it creates the VM

const n = reactive<NewVM>({
  name: "", folder: "", guest: "macos", version: "sequoia", profile: "ubuntu", iso: "", graphics: "vmware-svga",
  ramMiB: 8192, vcpus: 4, firmware: "uefi", secureBoot: false, tpm: false, disk: "new", diskGiB: 80, diskPath: "",
  backend: "", offline: false,
});
const mac = computed(() => n.guest === "macos");
const reims = computed(() => mac.value && n.backend === "reims-ctl");
const profile = computed(() => cat.value?.profiles.find((p) => p.id === n.profile));
const minRam = computed(() => (mac.value ? cat.value?.minMacRamMiB : profile.value?.minRamMiB) ?? 256);
const maxRam = computed(() => cat.value?.hostMemMiB || undefined);

const guestOptions: Option<Guest>[] = GUESTS.map(([id, l]) => [id, l]);
const versionOptions = computed<Option<string>[]>(() => (cat.value?.versions ?? []).map((v) => [v.id, `macOS ${v.name}`]));
const profileOptions = computed<Option<string>[]>(() => (cat.value?.profiles ?? []).filter((p) => p.guest === n.guest).map((p) => [p.id, p.name]));
const graphicsOptions = computed<Option<string>[]>(() => (mac.value ? ["vmware-svga", "reims-vgpu"] : ["virtio-vga", "vga"]).map((g) => [g, label(g)]));
// 3D acceleration, as in Settings: virtio-vga's virgl variant, not for Windows.
const controller = computed({
  get: () => (n.graphics === "virtio-vga-gl" ? "virtio-vga" : n.graphics),
  set: (g: string) => { n.graphics = g === "virtio-vga" && n.graphics === "virtio-vga-gl" ? "virtio-vga-gl" : g; },
});
const can3d = computed(() => !mac.value && n.guest !== "windows" && controller.value === "virtio-vga" && cat.value?.render3D?.can3D !== false);
const accel3d = computed({ get: () => n.graphics === "virtio-vga-gl", set: (on: boolean) => { n.graphics = on ? "virtio-vga-gl" : "virtio-vga"; } });
// macOS boots only with a power-of-two core count (cores of two threads): offer those.
const macCPUs = computed<Option<number>[]>(() => [1, 2, 4, 8, 16, 32].filter((x) => x <= Math.max(2, cat.value?.hostThreads ?? 32)).map((x) => [x, `${x}`]));
const firmwareOptions: Option<string>[] = [["uefi", "UEFI"], ["bios", "Legacy BIOS"]];
const defaultFolder = computed(() => `${cat.value?.workspace ?? "~/KVM-VMs"}/${(n.name.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")) || "vm"}`);

// Defaults follow the guest type and profile (core.Recommended and the profile, as core.CreateGuest picks them).
// A field the user changed since the last defaults keeps its value, unless the new guest type cannot take it.
type Hardware = Partial<Pick<NewVM, "graphics" | "firmware" | "secureBoot" | "tpm" | "ramMiB" | "vcpus" | "diskGiB" | "disk">>;
let applied: Hardware = {};
const kept = ref(false);
function defaults(): Hardware | null {
  const c = cat.value;
  if (!c) return null;
  if (mac.value) return { graphics: reims.value ? "reims-vgpu" : "vmware-svga", ramMiB: c.recRamMiB, vcpus: c.recVcpus, diskGiB: c.macDiskGiB, disk: "new" };
  const p = profile.value;
  if (!p) return null;
  return { graphics: p.graphics, firmware: p.firmware, secureBoot: p.secureBoot, tpm: p.tpm,
    ramMiB: Math.min(Math.max(c.recRamMiB, p.ramMiB), c.hostMemMiB || Infinity), vcpus: c.recVcpus, diskGiB: p.diskGiB };
}
function applyDefaults() {
  const d = defaults();
  if (!d) return;
  const fits: Record<string, boolean> = {
    graphics: graphicsOptions.value.some((o) => o[0] === controller.value) && !(reims.value && n.graphics !== "reims-vgpu") && !(n.graphics === "virtio-vga-gl" && n.guest === "windows"),
    vcpus: !mac.value || macCPUs.value.some((o) => o[0] === n.vcpus),
    disk: !mac.value || n.disk === "new",
  };
  kept.value = false;
  for (const [k, v] of Object.entries(d) as [keyof Hardware, never][]) {
    const edited = k in applied && n[k] !== applied[k];
    if (edited && fits[k] !== false) { kept.value = true; continue; }
    n[k] = v;
  }
  applied = d;
}
function setGuest(g: Guest) {
  n.guest = g;
  if (g !== "macos" && profile.value?.guest !== g) n.profile = cat.value?.profiles.find((p) => p.guest === g)?.id ?? "";
  applyDefaults();
}
watch(() => n.profile, applyDefaults);
watch(() => n.backend, applyDefaults);

async function browseISO() {
  const p = await PickFile("Choose an installer ISO", "iso").catch(() => "");
  if (p) { n.iso = p; await detectISO(); }
}
async function browseFolder() {
  const p = await PickFolder("Choose the VM folder").catch(() => "");
  if (p) n.folder = p;
}
async function browseDisk() {
  const p = await PickFile("Choose an existing disk image", "disk").catch(() => "");
  if (p) n.diskPath = p;
}
// ISO detection (kvm-guests › ISO detection): propose the type and profile, keep the choice when unknown.
async function detectISO() {
  detect.value = "";
  if (!n.iso.trim()) return;
  try {
    const d = await DetectISO(n.iso.trim());
    if (d.profile && d.guest) {
      setGuest(d.guest);
      n.profile = d.profile;
      if (!n.name.trim()) n.name = d.name;
      await nextTick(); // the profile watcher has applied its defaults
      detect.value = `Detected ${cat.value?.profiles.find((p) => p.id === d.profile)?.name ?? d.profile} (volume label ${d.label}).`
        + (kept.value ? " The hardware you changed is kept; the rest follows the profile." : "");
    } else {
      detect.value = d.label ? `Could not detect the system from the volume label ${d.label}: choose the type and profile.` : "This file is not an ISO 9660 image, or it cannot be read.";
    }
  } catch (e) { detect.value = String(e); }
}

// Which section a field belongs to, for Finish and for the core's FieldError.
const SECTION: Record<string, Step> = {
  name: "os", folder: "os", workspace: "os", guest: "os", version: "os", profile: "os", iso: "os", optical0: "os",
  backend: "os", reims_ctl_name: "os",
  ram: "hw", vcpus: "hw", cores: "hw", graphics: "hw", firmware: "hw", secure_boot: "hw", tpm: "hw",
  diskGiB: "disk", diskPath: "disk", disk0: "disk", disks: "disk",
};
function check(): Record<string, string> {
  const p: Record<string, string> = {};
  const name = n.name.trim();
  if (!name) p.name = "Give the VM a name.";
  else if (props.names.some((x) => x.toLowerCase() === name.toLowerCase())) p.name = "A VM with this name already exists.";
  if (n.folder && !n.folder.startsWith("/") && !n.folder.startsWith("~")) p.folder = "Use an absolute folder.";
  if (n.iso && !n.iso.startsWith("/")) p.iso = "Use an absolute path to the ISO.";
  if (!mac.value && !profile.value) p.profile = "Choose an OS profile.";
  if (!Number.isInteger(n.ramMiB) || n.ramMiB < minRam.value || (maxRam.value && n.ramMiB > maxRam.value))
    p.ram = `Use ${minRam.value} to ${maxRam.value ?? "?"} MiB.`;
  if (!Number.isInteger(n.vcpus) || n.vcpus < 1) p.vcpus = "At least 1 CPU.";
  if (!mac.value && n.secureBoot && n.firmware !== "uefi") p.secure_boot = "Secure Boot needs UEFI firmware.";
  if ((reims.value || (!mac.value && n.disk === "new")) && (!Number.isInteger(n.diskGiB) || n.diskGiB < 1)) p.diskGiB = "At least 1 GiB.";
  if (!mac.value && n.disk === "existing" && !n.diskPath.startsWith("/")) p.diskPath = "Choose an existing disk image.";
  return p;
}
// The attributes of a field with a problem: marked, and described by its message (wz-err-<id>).
function inv(id: string, ...fields: string[]) {
  return [id, ...fields].some((f) => problems.value[f])
    ? { class: "bad-field", "aria-invalid": "true", "aria-describedby": `wz-err-${id}` } : {};
}
const msg = (...f: string[]) => f.map((x) => problems.value[x]).find(Boolean);
function firstProblem(p: Record<string, string>) {
  const f = Object.keys(p)[0];
  if (!f) return;
  step.value = SECTION[f] ?? "os";
  setTimeout(() => document.querySelector<HTMLElement>(".wz-page [aria-invalid='true']")?.focus());
}

let off: (() => void) | undefined;
async function finish() {
  problems.value = check();
  serverError.value = "";
  if (Object.keys(problems.value).length) return firstProblem(problems.value);
  busy.value = true;
  output.value = [];
  off = Events.On("reims-create", (ev) => {
    output.value.push(String(ev.data));
    nextTick(() => { const o = document.querySelector(".wz-out"); if (o) o.scrollTop = o.scrollHeight; });
  });
  try {
    const r = await CreateVM({ ...n, name: n.name.trim(), iso: n.iso.trim(), folder: n.folder.trim(),
      disk: mac.value ? "new" : n.disk, backend: reims.value ? "reims-ctl" : "" });
    if (r.error && !r.id) {
      if (r.field) { problems.value = { [r.field]: r.error }; firstProblem(problems.value); }
      serverError.value = r.error;
      return;
    }
    emit("created", r.id ?? "", r.error ? `Created, but: ${r.error}` : r.notice ?? "");
  } catch (e) { serverError.value = String(e); } finally { busy.value = false; off?.(); }
}
const stepIndex = computed(() => STEPS.findIndex((s) => s[0] === step.value));
function next() {
  const p = check();
  const here = Object.fromEntries(Object.entries(p).filter(([f]) => (SECTION[f] ?? "os") === step.value));
  problems.value = here;
  if (Object.keys(here).length) return firstProblem(here);
  step.value = STEPS[stepIndex.value + 1][0];
}
const shown = (s: Step) => expert.value ? s !== "sum" : step.value === s;
// A new step: the focus goes to its heading (an invalid field takes it instead, see firstProblem).
watch(step, () => nextTick(() => document.querySelector<HTMLElement>(".wz-page .grp-h")?.focus()));

const summary = computed<[string, string][]>(() => [
  ["Name", n.name.trim()], ["Folder", n.folder.trim() || defaultFolder.value],
  ["Guest type", guestLabel(n.guest)],
  mac.value ? ["macOS version", versionOptions.value.find((o) => o[0] === n.version)?.[1] ?? n.version] : ["OS profile", profile.value?.name ?? ""],
  ...(reims.value ? [["Created by", `reims-ctl, from ${n.offline ? "the offline installer" : "the recovery image"}`]] as [string, string][] : []),
  ...(mac.value ? [] : [["Installer ISO", n.iso || "None"]] as [string, string][]),
  ["Memory", `${n.ramMiB} MiB`], ["Processors", `${n.vcpus}`], ["Graphics", label(n.graphics)],
  ...(mac.value ? [] : [["Firmware", `${label(n.firmware)}${n.secureBoot ? ", Secure Boot" : ""}${n.tpm ? ", TPM 2.0" : ""}`]] as [string, string][]),
  ["Hard disk", reims.value ? `New ${n.diskGiB} GiB (reims-ctl)` : mac.value ? `New ${cat.value?.macDiskGiB ?? 80} GiB qcow2 (mac_hdd_ng.img), or the folder's own` : n.disk === "new" ? `New ${n.diskGiB} GiB qcow2` : n.disk === "existing" ? n.diskPath : "None (boot the ISO)"],
]);

async function load() {
  loadError.value = "";
  try { cat.value = await GetCatalog(); applyDefaults(); } catch (e) { loadError.value = String(e); return; }
  await nextTick();
  document.getElementById("wz-name")?.focus();
}
onMounted(load);
onBeforeUnmount(() => off?.());
</script>

<template>
  <Modal :title="expert ? 'New VM (expert mode)' : 'New VM'" :width="780" :height="580" :busy="busy" @close="emit('close')">
    <div v-if="loadError" class="wz-fail">
      <p class="note bad" role="alert">The wizard could not read this host's choices: {{ loadError }}</p>
      <button type="button" class="btn" @click="load">Retry</button>
    </div>
    <div v-else-if="cat" class="wz">
      <ol v-if="!expert" class="wz-steps" aria-label="Steps">
        <li v-for="([id, title], i) in STEPS" :key="id" :class="{ on: step === id, done: i < stepIndex }" :aria-current="step === id ? 'step' : undefined">
          <span class="wz-n">{{ i + 1 }}</span> {{ title }}</li>
      </ol>
      <div class="wz-page">
        <p v-if="serverError" class="note bad" role="alert">{{ serverError }}</p>

        <section v-if="shown('os')" class="grp" aria-labelledby="wz-h-os">
          <h2 id="wz-h-os" class="grp-h" tabindex="-1">Name and operating system</h2>
          <div class="grp-b form">
            <label for="wz-name">Name</label>
            <div class="row"><input id="wz-name" v-model="n.name" data-field="name" class="in grow" v-bind="inv('name')" spellcheck="false" autofocus />
              <GuestIcon :guest="n.guest" :size="28" /></div>
            <span v-if="msg('name')" id="wz-err-name" class="span2 bad">{{ msg("name") }}</span>
            <label for="wz-folder">Folder</label>
            <div class="row"><input id="wz-folder" v-model="n.folder" data-field="folder" class="in grow" v-bind="inv('folder', 'workspace')" :placeholder="defaultFolder" spellcheck="false" />
              <button type="button" class="btn small" aria-label="Browse for the VM folder" @click="browseFolder">Browse…</button></div>
            <span v-if="msg('folder', 'workspace')" id="wz-err-folder" class="span2 bad">{{ msg("folder", "workspace") }}</span>
            <template v-if="!reims">
              <label for="wz-iso">Installer ISO</label>
              <div class="row"><input id="wz-iso" v-model="n.iso" data-field="iso" class="in grow" v-bind="inv('iso', 'optical0')" placeholder="None" spellcheck="false" @change="detectISO" />
                <button type="button" class="btn small" aria-label="Browse for an installer ISO" @click="browseISO">Browse…</button></div>
            </template>
            <span v-if="detect || msg('iso', 'optical0')" id="wz-err-iso" class="span2" :class="msg('iso', 'optical0') ? 'bad' : 'muted'" role="status">{{ msg("iso", "optical0") || detect }}</span>
            <span class="lbl">Guest type</span>
            <Dropdown :model-value="n.guest" :options="guestOptions" :width="180" aria-label="Guest type" data-field="guest" v-bind="inv('guest')" @update:model-value="setGuest" />
            <span v-if="msg('guest')" id="wz-err-guest" class="span2 bad">{{ msg("guest") }}</span>
            <template v-if="mac">
              <span class="lbl">macOS version</span>
              <Dropdown v-model="n.version" :options="versionOptions" :width="260" aria-label="macOS version" data-field="version" v-bind="inv('version')" />
              <span v-if="msg('version')" id="wz-err-version" class="span2 bad">{{ msg("version") }}</span>
              <span></span>
              <label class="chk"><input v-model="n.backend" type="checkbox" true-value="reims-ctl" false-value="" v-bind="inv('backend', 'reims_ctl_name')" /> Create with reims-ctl (Reims vGPU)</label>
              <span v-if="msg('backend', 'reims_ctl_name')" id="wz-err-backend" class="span2 bad">{{ msg("backend", "reims_ctl_name") }}</span>
              <template v-if="reims">
                <span></span>
                <label class="chk"><input v-model="n.offline" type="checkbox" /> Use the offline installer (13 to 20 GB) instead of the recovery image</label>
                <p class="span2 muted">reims-ctl downloads macOS from Apple and keeps the VM; afterwards the app changes only its processors, memory, resolution and SSH port.</p>
              </template>
              <p v-else class="span2 muted">macOS VMs boot with OpenCore. The install media are downloaded from Apple's recovery servers when you first start the VM.</p>
            </template>
            <template v-else>
              <span class="lbl">OS profile</span>
              <Dropdown v-model="n.profile" :options="profileOptions" :width="260" aria-label="OS profile" data-field="profile" v-bind="inv('profile')" />
              <span v-if="msg('profile')" id="wz-err-profile" class="span2 bad">{{ msg("profile") }}</span>
              <p v-if="profile" class="span2 muted">Profile defaults: {{ label(profile.firmware) }}{{ profile.secureBoot ? " with Secure Boot" : "" }}{{ profile.tpm ? ", TPM 2.0" : "" }}, {{ label(profile.diskBus) }} disk, {{ label(profile.nic) }} network, {{ label(profile.graphics) }}.</p>
            </template>
          </div>
        </section>

        <section v-if="shown('hw')" class="grp" aria-labelledby="wz-h-hw">
          <h2 id="wz-h-hw" class="grp-h" tabindex="-1">Hardware</h2>
          <div class="grp-b form">
            <label for="wz-ram">Base memory</label>
            <div class="row"><input id="wz-ram" v-model.number="n.ramMiB" data-field="ram" type="number" :min="minRam" :max="maxRam" step="512" class="in" v-bind="inv('ram')" /> MiB
              <span id="wz-ram-range" class="muted">{{ minRam }} to {{ maxRam ?? "?" }} MiB (the host has {{ cat.hostMemMiB }} MiB)</span></div>
            <span v-if="msg('ram')" id="wz-err-ram" class="span2 bad">{{ msg("ram") }}</span>
            <label :for="mac ? undefined : 'wz-cpu'" :class="{ lbl: mac }">Processors</label>
            <div class="row">
              <Dropdown v-if="mac" v-model="n.vcpus" :options="macCPUs" :width="90" aria-label="Processors" data-field="vcpus" v-bind="inv('vcpus', 'cores')" />
              <input v-else id="wz-cpu" v-model.number="n.vcpus" data-field="vcpus" type="number" min="1" :max="cat.hostThreads" class="in" v-bind="inv('vcpus', 'cores')" />
              <span class="muted">host: {{ cat.hostThreads }} threads{{ mac ? "; macOS needs a power-of-two core count" : "" }}</span></div>
            <span v-if="msg('vcpus', 'cores')" id="wz-err-vcpus" class="span2 bad">{{ msg("vcpus", "cores") }}</span>
            <span class="lbl">Graphics</span>
            <Dropdown v-model="controller" :options="graphicsOptions" :width="240" aria-label="Graphics" data-field="graphics" :disabled="reims" v-bind="inv('graphics')" />
            <template v-if="!mac">
              <span class="lbl"></span>
              <label class="chk" :class="{ off: !can3d && !accel3d }"><input v-model="accel3d" type="checkbox" :disabled="!can3d && !accel3d" /> Enable 3D acceleration (virgl: the host's GPU draws the guest's OpenGL)</label>
              <span v-if="cat.render3D && !cat.render3D.can3D && n.guest !== 'windows'" class="span2 muted">3D acceleration is not available on this host: {{ cat.render3D.reason3D }}.</span>
            </template>
            <span v-if="msg('graphics')" id="wz-err-graphics" class="span2 bad">{{ msg("graphics") }}</span>
            <div v-if="n.graphics === 'reims-vgpu' && cat.reimsMissing" class="span2 note warn">
              Reims vGPU is not installed on this host. {{ reims ? "reims-ctl comes with it: install it to create this VM." : "You can create the VM now; install Reims vGPU before its first start." }}
              <details class="tech"><summary>Details</summary><p>{{ cat.reimsMissing }}</p></details>
            </div>
            <template v-if="!mac">
              <span class="lbl">Firmware</span>
              <Dropdown v-model="n.firmware" :options="firmwareOptions" :width="160" aria-label="Firmware" data-field="firmware" v-bind="inv('firmware')" />
              <span></span>
              <label class="chk" :class="{ off: n.firmware !== 'uefi' }"><input v-model="n.secureBoot" type="checkbox" data-field="secure_boot" :disabled="n.firmware !== 'uefi'" v-bind="inv('secure_boot')" /> Secure Boot (Microsoft keys enrolled)</label>
              <span v-if="msg('secure_boot')" id="wz-err-secure_boot" class="span2 bad">{{ msg("secure_boot") }}</span>
              <span></span>
              <label class="chk"><input v-model="n.tpm" type="checkbox" data-field="tpm" /> TPM 2.0 (swtpm)</label>
            </template>
          </div>
        </section>

        <section v-if="shown('disk')" class="grp" aria-labelledby="wz-h-disk">
          <h2 id="wz-h-disk" class="grp-h" tabindex="-1">Hard disk</h2>
          <div v-if="reims" class="grp-b form">
            <label for="wz-size">Size</label>
            <div class="row"><input id="wz-size" v-model.number="n.diskGiB" data-field="diskGiB" type="number" min="1" class="in" v-bind="inv('diskGiB')" /> GiB <span class="muted">created by reims-ctl in the VM folder</span></div>
            <span v-if="msg('diskGiB')" id="wz-err-diskGiB" class="span2 bad">{{ msg("diskGiB") }}</span>
          </div>
          <div v-else-if="mac" class="grp-b">
            <p class="muted">A new {{ cat.macDiskGiB }} GiB qcow2 disk (mac_hdd_ng.img) is created in the VM folder; it grows as macOS writes to it. A folder that already has one keeps it.</p>
          </div>
          <div v-else class="grp-b form" role="radiogroup" aria-labelledby="wz-h-disk">
            <span></span>
            <label class="chk"><input v-model="n.disk" type="radio" value="new" name="wz-disk" /> Create a new disk</label>
            <label for="wz-size">Size</label>
            <div class="row"><input id="wz-size" v-model.number="n.diskGiB" data-field="diskGiB" type="number" min="1" class="in" v-bind="inv('diskGiB')" :disabled="n.disk !== 'new'" /> GiB <span class="muted">qcow2, grows as the guest writes</span></div>
            <span v-if="msg('diskGiB')" id="wz-err-diskGiB" class="span2 bad">{{ msg("diskGiB") }}</span>
            <span></span>
            <label class="chk"><input v-model="n.disk" type="radio" value="existing" name="wz-disk" /> Use an existing disk image</label>
            <label for="wz-dpath">Image</label>
            <div class="row"><input id="wz-dpath" v-model="n.diskPath" data-field="diskPath" class="in grow" v-bind="inv('diskPath', 'disk0', 'disks')" :disabled="n.disk !== 'existing'" spellcheck="false" />
              <button type="button" class="btn small" aria-label="Browse for a disk image" :disabled="n.disk !== 'existing'" @click="browseDisk">Browse…</button></div>
            <span v-if="msg('diskPath', 'disk0', 'disks')" id="wz-err-diskPath" class="span2 bad">{{ msg("diskPath", "disk0", "disks") }}</span>
            <span></span>
            <label class="chk"><input v-model="n.disk" type="radio" value="none" name="wz-disk" /> No hard disk (boot a live ISO)</label>
          </div>
        </section>

        <section v-if="!expert && step === 'sum'" class="grp" aria-labelledby="wz-h-sum">
          <h2 id="wz-h-sum" class="grp-h" tabindex="-1">Summary</h2>
          <dl class="grp-b sum"><template v-for="[k, v] in summary" :key="k"><dt>{{ k }}</dt><dd>{{ v }}</dd></template></dl>
        </section>

        <section v-if="output.length" class="grp" aria-labelledby="wz-h-out">
          <h2 id="wz-h-out" class="grp-h">reims-ctl</h2>
          <pre class="grp-b wz-out mono" tabindex="0" aria-label="reims-ctl output">{{ output.join("\n") }}</pre>
          <span class="sr" aria-live="polite">{{ output[output.length - 1] }}</span>
        </section>
      </div>
    </div>
    <p v-else class="muted" style="padding: 12px" role="status">Loading…</p>
    <template #footer>
      <button type="button" class="btn" :aria-pressed="expert" :disabled="busy || !cat" @click="expert = !expert; problems = {}">{{ expert ? "Guided mode" : "Expert mode" }}</button>
      <span class="grow"></span>
      <span v-if="busy" class="muted" role="status">{{ reims ? "reims-ctl is creating the VM…" : "Creating…" }}</span>
      <button v-if="!expert" type="button" class="btn" :disabled="stepIndex === 0 || busy" @click="step = STEPS[stepIndex - 1][0]">Back</button>
      <button v-if="!expert && step !== 'sum'" type="button" class="btn primary" :disabled="busy || !cat" @click="next">Next</button>
      <button type="button" class="btn" :class="{ primary: expert || step === 'sum' }" :disabled="busy || !cat" @click="finish">Finish</button>
      <button type="button" class="btn" :disabled="busy" @click="emit('close')">Cancel</button>
    </template>
  </Modal>
</template>

<style scoped>
.wz { display: flex; flex: 1; min-height: 0; }
.wz-fail { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; padding: 12px; }
.wz-steps { flex: none; width: 200px; margin: 0; padding: 12px 8px; list-style: none; background: var(--surface); border-right: 1px solid var(--border-subtle); display: flex; flex-direction: column; gap: 4px; }
.wz-steps li { display: flex; align-items: center; gap: 8px; padding: 5px 6px; color: var(--text-muted); }
.wz-steps li.on { color: var(--text); font-weight: 700; background: var(--hover); }
.wz-steps li.done { color: var(--text); }
.wz-n { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px; flex: none; border: 1px solid var(--border); border-radius: 50%; font-size: 11px; }
.wz-steps li.on .wz-n { background: var(--primary); border-color: var(--primary); color: var(--primary-text); }
.wz-page { flex: 1; min-width: 0; overflow-y: auto; padding: 12px; }
.span2 { grid-column: 2; }
p.span2 { margin: 0; }
.sum { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 4px 16px; margin: 0; }
.sum dt { color: var(--text-muted); }
.sum dd { margin: 0; overflow-wrap: anywhere; }
.wz-out { max-height: 160px; overflow: auto; margin: 0; white-space: pre-wrap; font-size: 11px; background: var(--surface); }
</style>
