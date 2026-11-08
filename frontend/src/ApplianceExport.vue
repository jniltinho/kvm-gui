<script setup lang="ts">
// File › Export Appliance…: the selected powered-off Linux, Windows or Other VM as an .ova that VirtualBox imports.
// What the appliance does not carry is listed first; a VM with a TPM needs the BitLocker note acknowledged, and an
// existing file is replaced only after a confirmation (OpenSpec add-ova-import-export).
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { ApplianceExportNotes, CancelAppliance, ExportAppliance, PickSaveAppliance, type VM } from "./api";
import { askConfirm } from "./ask";
import Modal from "./Modal.vue";

const props = defineProps<{ vm: VM; tpm: boolean }>();
const emit = defineEmits<{ close: []; exported: [notice: string] }>();
const path = ref("");
const notes = ref<string[]>([]);
const acceptTPM = ref(false);
const error = ref("");
const busy = ref(false);
const done = ref(0);
const total = ref(0);
const pct = computed(() => (total.value ? Math.round((done.value * 100) / total.value) : 0));
const ready = computed(() => !!path.value.trim() && (!props.tpm || acceptTPM.value));

let off: (() => void) | undefined;
onMounted(async () => {
  try { notes.value = await ApplianceExportNotes(props.vm.id); } catch (e) { error.value = String(e); }
  off = Events.On("appliance-progress", (ev) => { const p = ev.data as { done: number; total: number }; done.value = p.done; total.value = p.total; });
});
onBeforeUnmount(() => off?.());
async function choose() {
  const p = await PickSaveAppliance(props.vm.name).catch(() => "");
  if (p) path.value = p.toLowerCase().endsWith(".ova") ? p : p + ".ova";
}
async function run(overwrite = false) {
  busy.value = true;
  error.value = "";
  done.value = total.value = 0;
  try {
    const r = await ExportAppliance(props.vm.id, path.value.trim(), overwrite, acceptTPM.value);
    if (r.error) {
      if (!overwrite && r.error.includes("already exists")) {
        busy.value = false;
        if (await askConfirm("Export appliance", `${path.value} already exists. Replace it?`, "Replace")) await run(true);
        return;
      }
      error.value = r.error;
      return;
    }
    emit("exported", `Exported ${props.vm.name} to ${path.value}.`);
  } catch (e) { error.value = String(e); } finally { busy.value = false; }
}
</script>

<template>
  <Modal :title="`Export ${vm.name} as an appliance`" :width="680" :busy="busy" @close="emit('close')">
    <div class="ea">
      <p class="muted">An .ova that VirtualBox imports: the VM's settings and its disks, with a manifest. The VM must stay powered off while it is written.</p>
      <div class="row">
        <input v-model="path" class="in grow mono" aria-label="Appliance file" placeholder="Where to write the .ova" spellcheck="false" :disabled="busy" />
        <button type="button" class="btn" :disabled="busy" @click="choose">Choose…</button>
      </div>
      <p v-if="error" class="note bad" role="alert">{{ error }}</p>
      <section v-if="notes.length" class="grp" aria-labelledby="ea-notes"><h2 id="ea-notes" class="grp-h">Not carried</h2><div class="grp-b">
        <ul class="list"><li v-for="n in notes" :key="n">{{ n }}</li></ul>
      </div></section>
      <label v-if="tpm" class="chk"><input v-model="acceptTPM" type="checkbox" :disabled="busy" />
        I have the BitLocker recovery key (or BitLocker is suspended): secrets sealed to this VM's TPM will not open on the other side.</label>
      <div v-if="busy" class="row" role="status">
        <progress :value="pct" max="100" class="grow" aria-label="Export progress" />
        <span class="muted">{{ pct }}%</span>
      </div>
    </div>
    <template #footer>
      <button v-if="busy" type="button" class="btn" @click="CancelAppliance">Cancel export</button>
      <button v-else type="button" class="btn primary" :disabled="!ready" @click="run()">Export</button>
      <button type="button" class="btn" :disabled="busy" @click="emit('close')">Close</button>
    </template>
  </Modal>
</template>

<style scoped>
.ea { padding: 10px 12px; display: flex; flex-direction: column; gap: 8px; }
.ea p { margin: 0; }
.list { margin: 0; padding-left: 18px; }
progress { height: 14px; }
</style>
