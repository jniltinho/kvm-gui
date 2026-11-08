<script setup lang="ts">
// Machine › New: guided (one step per section) or expert (every section on one page). Finish checks every
// section and opens the first one with a problem; the core validates again before anything is created.
import { computed, onMounted, reactive, ref, watch } from "vue";
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
const busy = ref(false);
const detect = ref("");
const problems = ref<Record<string, string>>({});
const serverError = ref("");

const n = reactive<NewVM>({
  name: "", folder: "", guest: "macos", version: "sequoia", profile: "ubuntu", iso: "", graphics: "vmware-svga",
  ramMiB: 8192, vcpus: 4, firmware: "uefi", secureBoot: false, tpm: false, disk: "new", diskGiB: 80, diskPath: "",
});
const mac = computed(() => n.guest === "macos");
const profile = computed(() => cat.value?.profiles.find((p) => p.id === n.profile));
const minRam = computed(() => (mac.value ? cat.value?.minMacRamMiB : profile.value?.minRamMiB) ?? 256);

// Defaults follow the guest type and profile (core.Recommended and the profile, as core.CreateGuest picks them).
function applyDefaults() {
  const c = cat.value;
  if (!c) return;
  if (mac.value) {
    Object.assign(n, { graphics: "vmware-svga", ramMiB: c.recRamMiB, vcpus: c.recVcpus, diskGiB: c.macDiskGiB, disk: "new" });
    return;
  }
  const p = profile.value;
  if (!p) return;
  Object.assign(n, { graphics: p.graphics, firmware: p.firmware, secureBoot: p.secureBoot, tpm: p.tpm,
    ramMiB: Math.min(Math.max(c.recRamMiB, p.ramMiB), c.hostMemMiB || Infinity), vcpus: c.recVcpus, diskGiB: p.diskGiB });
}
function setGuest(g: Guest) {
  n.guest = g;
  if (g !== "macos" && profile.value?.guest !== g) n.profile = cat.value?.profiles.find((p) => p.guest === g)?.id ?? "";
  applyDefaults();
}
watch(() => n.profile, applyDefaults);

const guestOptions: Option<Guest>[] = GUESTS.map(([id, l]) => [id, l]);
const versionOptions = computed<Option<string>[]>(() => (cat.value?.versions ?? []).map((v) => [v.id, `macOS ${v.name}`]));
const profileOptions = computed<Option<string>[]>(() => (cat.value?.profiles ?? []).filter((p) => p.guest === n.guest).map((p) => [p.id, p.name]));
const graphicsOptions = computed<Option<string>[]>(() => (mac.value ? ["vmware-svga", "reims-vgpu"] : ["virtio-vga", "vga"]).map((g) => [g, label(g)]));
// macOS boots only with a power-of-two core count (cores of two threads): offer those.
const macCPUs = computed<Option<number>[]>(() => [1, 2, 4, 8, 16, 32].filter((x) => x <= Math.max(2, cat.value?.hostThreads ?? 32)).map((x) => [x, `${x}`]));
const firmwareOptions: Option<string>[] = [["uefi", "UEFI"], ["bios", "Legacy BIOS"]];
const defaultFolder = computed(() => `${cat.value?.workspace ?? "~/KVM-VMs"}/${(n.name.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")) || "vm"}`);

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
      detect.value = `Detected ${cat.value?.profiles.find((p) => p.id === d.profile)?.name ?? d.profile} (volume label ${d.label}).`;
    } else {
      detect.value = d.label ? `Could not detect the system from the volume label ${d.label}: choose the type and profile.` : "This file is not an ISO 9660 image, or it cannot be read.";
    }
  } catch (e) { detect.value = String(e); }
}

// Which section a field belongs to, for Finish and for the core's FieldError.
const SECTION: Record<string, Step> = {
  name: "os", folder: "os", workspace: "os", guest: "os", version: "os", profile: "os", iso: "os", optical0: "os",
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
  if (!Number.isInteger(n.ramMiB) || n.ramMiB < minRam.value) p.ram = `At least ${minRam.value} MiB.`;
  else if (cat.value?.hostMemMiB && n.ramMiB > cat.value.hostMemMiB) p.ram = `The host has ${cat.value.hostMemMiB} MiB.`;
  if (!Number.isInteger(n.vcpus) || n.vcpus < 1) p.vcpus = "At least 1 CPU.";
  if (!mac.value && n.secureBoot && n.firmware !== "uefi") p.secure_boot = "Secure Boot needs UEFI firmware.";
  if (!mac.value && n.disk === "new" && (!Number.isInteger(n.diskGiB) || n.diskGiB < 1)) p.diskGiB = "At least 1 GiB.";
  if (!mac.value && n.disk === "existing" && !n.diskPath.startsWith("/")) p.diskPath = "Choose an existing disk image.";
  return p;
}
const bad = (f: string) => (problems.value[f] ? "bad-field" : "");
function firstProblem(p: Record<string, string>) {
  const f = Object.keys(p)[0];
  if (!f) return;
  step.value = SECTION[f] ?? "os";
  setTimeout(() => document.querySelector<HTMLElement>(`[data-field="${f}"]`)?.focus());
}

async function finish() {
  problems.value = check();
  serverError.value = "";
  if (Object.keys(problems.value).length) return firstProblem(problems.value);
  busy.value = true;
  try {
    const r = await CreateVM({ ...n, name: n.name.trim(), iso: n.iso.trim(), folder: n.folder.trim(),
      disk: mac.value ? "new" : n.disk });
    if (r.error) {
      if (r.field) { problems.value = { [r.field]: r.error }; firstProblem(problems.value); }
      serverError.value = r.error;
      return;
    }
    emit("created", r.id ?? "", r.notice ?? "");
  } catch (e) { serverError.value = String(e); } finally { busy.value = false; }
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

const summary = computed<[string, string][]>(() => [
  ["Name", n.name.trim()], ["Folder", n.folder.trim() || defaultFolder.value],
  ["Guest type", guestLabel(n.guest)],
  mac.value ? ["macOS version", versionOptions.value.find((o) => o[0] === n.version)?.[1] ?? n.version] : ["OS profile", profile.value?.name ?? ""],
  ...(mac.value ? [] : [["Installer ISO", n.iso || "None"]] as [string, string][]),
  ["Memory", `${n.ramMiB} MiB`], ["Processors", `${n.vcpus}`], ["Graphics", label(n.graphics)],
  ...(mac.value ? [] : [["Firmware", `${label(n.firmware)}${n.secureBoot ? ", Secure Boot" : ""}${n.tpm ? ", TPM 2.0" : ""}`]] as [string, string][]),
  ["Hard disk", mac.value ? `New ${cat.value?.macDiskGiB ?? 80} GiB qcow2 (mac_hdd_ng.img), or the folder's own` : n.disk === "new" ? `New ${n.diskGiB} GiB qcow2` : n.disk === "existing" ? n.diskPath : "None (boot the ISO)"],
]);

onMounted(async () => {
  try { cat.value = await GetCatalog(); applyDefaults(); } catch (e) { serverError.value = String(e); }
});
</script>

<template>
  <Modal :title="expert ? 'New VM (expert mode)' : 'New VM'" :width="780" :height="580" @close="emit('close')">
    <div v-if="cat" class="wz">
      <ol v-if="!expert" class="wz-steps" aria-label="Steps">
        <li v-for="([id, title], i) in STEPS" :key="id" :class="{ on: step === id, done: i < stepIndex }" :aria-current="step === id ? 'step' : undefined">
          <span class="wz-n">{{ i + 1 }}</span> {{ title }}</li>
      </ol>
      <div class="wz-page">
        <p v-if="serverError" class="note bad" role="alert">{{ serverError }}</p>

        <section v-if="shown('os')" class="grp" aria-label="Name and operating system">
          <div class="grp-h">Name and operating system</div>
          <div class="grp-b form">
            <label for="wz-name">Name</label>
            <div class="row"><input id="wz-name" v-model="n.name" data-field="name" class="in grow" :class="bad('name')" spellcheck="false" autofocus />
              <GuestIcon :guest="n.guest" :size="28" /></div>
            <span v-if="problems.name" class="span2 bad">{{ problems.name }}</span>
            <label for="wz-folder">Folder</label>
            <div class="row"><input id="wz-folder" v-model="n.folder" data-field="folder" class="in grow" :class="bad('folder') || bad('workspace')" :placeholder="defaultFolder" spellcheck="false" />
              <button type="button" class="btn small" aria-label="Browse for the VM folder" @click="browseFolder">Browse…</button></div>
            <span v-if="problems.folder || problems.workspace" class="span2 bad">{{ problems.folder || problems.workspace }}</span>
            <label for="wz-iso">Installer ISO</label>
            <div class="row"><input id="wz-iso" v-model="n.iso" data-field="iso" class="in grow" :class="bad('iso') || bad('optical0')" placeholder="None" spellcheck="false" @change="detectISO" />
              <button type="button" class="btn small" aria-label="Browse for an installer ISO" @click="browseISO">Browse…</button></div>
            <span v-if="detect || problems.iso || problems.optical0" class="span2" :class="problems.iso || problems.optical0 ? 'bad' : 'muted'" role="status">{{ problems.iso || problems.optical0 || detect }}</span>
            <span class="lbl">Guest type</span>
            <Dropdown :model-value="n.guest" :options="guestOptions" :width="180" aria-label="Guest type" data-field="guest" @update:model-value="setGuest" />
            <template v-if="mac">
              <span class="lbl">macOS version</span>
              <Dropdown v-model="n.version" :options="versionOptions" :width="260" aria-label="macOS version" data-field="version" :class="bad('version')" />
              <p class="span2 muted">macOS VMs boot with OpenCore. The install media are downloaded from Apple's recovery servers when you first start the VM.</p>
            </template>
            <template v-else>
              <span class="lbl">OS profile</span>
              <Dropdown v-model="n.profile" :options="profileOptions" :width="260" aria-label="OS profile" data-field="profile" :class="bad('profile')" />
              <span v-if="problems.profile" class="span2 bad">{{ problems.profile }}</span>
              <p v-if="profile" class="span2 muted">Profile defaults: {{ label(profile.firmware) }}{{ profile.secureBoot ? " with Secure Boot" : "" }}{{ profile.tpm ? ", TPM 2.0" : "" }}, {{ label(profile.diskBus) }} disk, {{ label(profile.nic) }} network, {{ label(profile.graphics) }}.</p>
            </template>
          </div>
        </section>

        <section v-if="shown('hw')" class="grp" aria-label="Hardware">
          <div class="grp-h">Hardware</div>
          <div class="grp-b form">
            <label for="wz-ram">Base memory</label>
            <div class="row"><input id="wz-ram" v-model.number="n.ramMiB" data-field="ram" type="number" :min="minRam" :max="cat.hostMemMiB || undefined" step="512" class="in" :class="bad('ram')" /> MiB
              <span class="muted">host: {{ cat.hostMemMiB }} MiB, minimum {{ minRam }} MiB</span></div>
            <span v-if="problems.ram" class="span2 bad">{{ problems.ram }}</span>
            <label :for="mac ? undefined : 'wz-cpu'" :class="{ lbl: mac }">Processors</label>
            <div class="row">
              <Dropdown v-if="mac" v-model="n.vcpus" :options="macCPUs" :width="90" aria-label="Processors" data-field="vcpus" :class="bad('vcpus') || bad('cores')" />
              <input v-else id="wz-cpu" v-model.number="n.vcpus" data-field="vcpus" type="number" min="1" :max="cat.hostThreads" class="in" :class="bad('vcpus') || bad('cores')" />
              <span class="muted">host: {{ cat.hostThreads }} threads{{ mac ? "; macOS needs a power-of-two core count" : "" }}</span></div>
            <span v-if="problems.vcpus || problems.cores" class="span2 bad">{{ problems.vcpus || problems.cores }}</span>
            <span class="lbl">Graphics</span>
            <Dropdown v-model="n.graphics" :options="graphicsOptions" :width="240" aria-label="Graphics" data-field="graphics" :class="bad('graphics')" />
            <p v-if="n.graphics === 'reims-vgpu' && cat.reimsMissing" class="span2 note warn">Reims vGPU's QEMU is not installed: {{ cat.reimsMissing }}. Install it before the first start (Reims vGPU install from the app arrives with task 4.5).</p>
            <template v-if="!mac">
              <span class="lbl">Firmware</span>
              <Dropdown v-model="n.firmware" :options="firmwareOptions" :width="160" aria-label="Firmware" data-field="firmware" :class="bad('firmware')" />
              <span></span>
              <label class="chk" :class="{ off: n.firmware !== 'uefi' }"><input v-model="n.secureBoot" type="checkbox" data-field="secure_boot" :disabled="n.firmware !== 'uefi'" /> Secure Boot (Microsoft keys enrolled)</label>
              <span v-if="problems.secure_boot" class="span2 bad">{{ problems.secure_boot }}</span>
              <span></span>
              <label class="chk"><input v-model="n.tpm" type="checkbox" data-field="tpm" /> TPM 2.0 (swtpm)</label>
            </template>
          </div>
        </section>

        <section v-if="shown('disk')" class="grp" aria-label="Hard disk">
          <div class="grp-h">Hard disk</div>
          <div v-if="mac" class="grp-b">
            <p class="muted">A new {{ cat.macDiskGiB }} GiB qcow2 disk (mac_hdd_ng.img) is created in the VM folder; it grows as macOS writes to it. A folder that already has one keeps it.</p>
          </div>
          <div v-else class="grp-b form" role="radiogroup" aria-label="Hard disk">
            <span></span>
            <label class="chk"><input v-model="n.disk" type="radio" value="new" name="wz-disk" /> Create a new disk</label>
            <label for="wz-size">Size</label>
            <div class="row"><input id="wz-size" v-model.number="n.diskGiB" data-field="diskGiB" type="number" min="1" class="in" :class="bad('diskGiB')" :disabled="n.disk !== 'new'" /> GiB <span class="muted">qcow2, grows as the guest writes</span></div>
            <span v-if="problems.diskGiB" class="span2 bad">{{ problems.diskGiB }}</span>
            <span></span>
            <label class="chk"><input v-model="n.disk" type="radio" value="existing" name="wz-disk" /> Use an existing disk image</label>
            <label for="wz-dpath">Image</label>
            <div class="row"><input id="wz-dpath" v-model="n.diskPath" data-field="diskPath" class="in grow" :class="bad('diskPath')" :disabled="n.disk !== 'existing'" spellcheck="false" />
              <button type="button" class="btn small" aria-label="Browse for a disk image" :disabled="n.disk !== 'existing'" @click="browseDisk">Browse…</button></div>
            <span v-if="problems.diskPath" class="span2 bad">{{ problems.diskPath }}</span>
            <span></span>
            <label class="chk"><input v-model="n.disk" type="radio" value="none" name="wz-disk" /> No hard disk (boot a live ISO)</label>
          </div>
        </section>

        <section v-if="!expert && step === 'sum'" class="grp" aria-label="Summary">
          <div class="grp-h">Summary</div>
          <dl class="grp-b sum"><template v-for="[k, v] in summary" :key="k"><dt>{{ k }}</dt><dd>{{ v }}</dd></template></dl>
        </section>
      </div>
    </div>
    <p v-else-if="!serverError" class="muted" style="padding: 12px">Loading…</p>
    <template #footer>
      <button type="button" class="btn" :aria-pressed="expert" @click="expert = !expert; problems = {}">{{ expert ? "Guided mode" : "Expert mode" }}</button>
      <span class="grow"></span>
      <span v-if="busy" class="muted" role="status">Creating…</span>
      <button v-if="!expert" type="button" class="btn" :disabled="stepIndex === 0 || busy" @click="step = STEPS[stepIndex - 1][0]">Back</button>
      <button v-if="!expert && step !== 'sum'" type="button" class="btn primary" :disabled="busy || !cat" @click="next">Next</button>
      <button type="button" class="btn" :class="{ primary: expert || step === 'sum' }" :disabled="busy || !cat" @click="finish">Finish</button>
      <button type="button" class="btn" :disabled="busy" @click="emit('close')">Cancel</button>
    </template>
  </Modal>
</template>

<style scoped>
.wz { display: flex; flex: 1; min-height: 0; }
.wz-steps { flex: none; width: 200px; margin: 0; padding: 12px 8px; list-style: none; background: var(--surface); border-right: 1px solid var(--border-subtle); display: flex; flex-direction: column; gap: 4px; }
.wz-steps li { display: flex; align-items: center; gap: 8px; padding: 5px 6px; color: var(--text-muted); }
.wz-steps li.on { color: var(--text); font-weight: 700; background: var(--hover); }
.wz-steps li.done { color: var(--text); }
.wz-n { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px; flex: none; border: 1px solid var(--border); border-radius: 50%; font-size: 11px; }
.wz-steps li.on .wz-n { background: var(--accent); border-color: var(--accent); color: var(--selection-text); }
.wz-page { flex: 1; min-width: 0; overflow-y: auto; padding: 12px; }
.span2 { grid-column: 2; }
p.span2 { margin: 0; }
.sum { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 4px 16px; margin: 0; }
.sum dt { color: var(--text-muted); }
.sum dd { margin: 0; overflow-wrap: anywhere; }
</style>
