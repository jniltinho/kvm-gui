<script setup lang="ts">
// File › Import Appliance…: an .ova or .ovf (as VirtualBox exports it) as a new Linux, Windows or Other VM. The
// preview shows the VM it would create and every setting it cannot keep before anything is written; name, folder
// and a host bridge for bridged adapters can be changed there (OpenSpec add-ova-import-export).
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { CancelAppliance, GetCatalog, ImportAppliance, PickFile, PickFolder, PlanAppliance, type ImportPreview } from "./api";
import Dropdown, { type Option } from "./Dropdown.vue";
import Modal from "./Modal.vue";
import { guestLabel } from "./labels";

const emit = defineEmits<{ close: []; imported: [id: string, notice: string] }>();
const path = ref("");
const name = ref("");
const folder = ref("");
const bridge = ref("");
const bridges = ref<string[]>([]);
const preview = ref<ImportPreview | null>(null);
const error = ref("");
const busy = ref(false);
const done = ref(0);
const total = ref(0);
const bridgeOptions = computed<Option<string>[]>(() => [["", "NAT (bridged adapters become NAT)"], ...bridges.value.map((b) => [b, `Bridge ${b}`] as Option<string>)]);
const pct = computed(() => (total.value ? Math.round((done.value * 100) / total.value) : 0));

async function plan() {
  error.value = "";
  try {
    preview.value = await PlanAppliance(path.value, { name: name.value, folder: folder.value, bridge: bridge.value });
    if (!name.value) name.value = preview.value.config.name;
    if (!folder.value) folder.value = preview.value.folder;
  } catch (e) {
    preview.value = null;
    error.value = String(e);
  }
}
async function choose() {
  const p = await PickFile("Import appliance (.ova or .ovf)", "ova").catch(() => "");
  if (!p) return;
  path.value = p;
  name.value = folder.value = "";
  await plan();
}
async function browse() {
  const p = await PickFolder("Folder for the imported VM").catch(() => "");
  if (p) { folder.value = p + "/" + (name.value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-") || "vm"); await plan(); }
}
let off: (() => void) | undefined;
onMounted(async () => {
  try { bridges.value = (await GetCatalog()).bridges ?? []; } catch { /* NAT only */ }
  off = Events.On("appliance-progress", (ev) => { const p = ev.data as { done: number; total: number }; done.value = p.done; total.value = p.total; });
});
onBeforeUnmount(() => off?.());
async function run() {
  busy.value = true;
  error.value = "";
  done.value = total.value = 0;
  try {
    const r = await ImportAppliance(path.value, { name: name.value, folder: folder.value, bridge: bridge.value });
    if (r.error && !r.id) { error.value = r.error; return; }
    emit("imported", r.id ?? "", r.error ? `Imported, but: ${r.error}` : `Imported ${name.value}.`);
  } catch (e) { error.value = String(e); } finally { busy.value = false; }
}
</script>

<template>
  <Modal title="Import appliance" :width="720" :busy="busy" @close="emit('close')">
    <div class="ia">
      <div class="row">
        <input v-model="path" class="in grow mono" aria-label="Appliance file" placeholder="An .ova or .ovf file" spellcheck="false" :disabled="busy" @change="plan" />
        <button type="button" class="btn" :disabled="busy" @click="choose">Choose…</button>
      </div>
      <p v-if="error" class="note bad" role="alert">{{ error }}</p>
      <template v-if="preview">
        <div class="form">
          <label class="lbl" for="ia-name">Name</label>
          <input id="ia-name" v-model="name" class="in" spellcheck="false" :disabled="busy" @change="plan" />
          <label class="lbl" for="ia-folder">Folder</label>
          <div class="row"><input id="ia-folder" v-model="folder" class="in grow mono" spellcheck="false" :disabled="busy" @change="plan" />
            <button type="button" class="btn" :disabled="busy" @click="browse">Browse…</button></div>
          <span class="lbl">Bridged adapters</span>
          <Dropdown v-model="bridge" :options="bridgeOptions" :width="320" aria-label="Bridged adapters" :disabled="busy" @update:model-value="plan" />
        </div>
        <section class="grp" aria-labelledby="ia-vm"><h2 id="ia-vm" class="grp-h">The VM it creates</h2><div class="grp-b">
          <p>{{ guestLabel(preview.config.guest) }}, {{ preview.config.profile }} profile · {{ preview.config.ram_mib }} MiB · {{ preview.config.vcpus }} vCPUs ·
            {{ preview.config.firmware === "uefi" ? "UEFI" : "BIOS" }}{{ preview.config.secure_boot ? " with Secure Boot" : "" }}{{ preview.config.tpm ? " · TPM 2.0" : "" }}</p>
          <ul class="list"><li v-for="d in preview.disks" :key="d" class="mono">{{ d }}</li></ul>
        </div></section>
        <section v-if="preview.notes.length" class="grp" aria-labelledby="ia-notes"><h2 id="ia-notes" class="grp-h">Not kept, or changed</h2><div class="grp-b">
          <ul class="list"><li v-for="n in preview.notes" :key="n">{{ n }}</li></ul>
        </div></section>
      </template>
      <p v-else-if="!error" class="muted">Choose an appliance: kvm-gui shows the VM it would create and what it cannot keep before anything is written. macOS appliances are not supported (move macOS VMs as kvm-gui bundles).</p>
      <div v-if="busy" class="row" role="status">
        <progress :value="pct" max="100" class="grow" aria-label="Import progress" />
        <span class="muted">{{ pct }}%</span>
      </div>
    </div>
    <template #footer>
      <button v-if="busy" type="button" class="btn" @click="CancelAppliance">Cancel import</button>
      <button v-else type="button" class="btn primary" :disabled="!preview" @click="run">Import</button>
      <button type="button" class="btn" :disabled="busy" @click="emit('close')">Close</button>
    </template>
  </Modal>
</template>

<style scoped>
.ia { padding: 10px 12px; display: flex; flex-direction: column; gap: 8px; }
.ia p { margin: 0; }
.list { margin: 0; padding-left: 18px; }
progress { height: 14px; }
</style>
