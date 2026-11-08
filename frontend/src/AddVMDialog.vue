<script setup lang="ts">
// Machine › Add: an existing VM from its settings file (vm.json or an export), or a reims-ctl VM that is not in
// the list yet (kvm-gui's reims-ctl folders and the user's own reims-ctl).
import { onMounted, ref } from "vue";
import { AddReimsCtl, AddVM, PickFile, ReimsCtlCandidates, type ReimsCandidate, type Result } from "./api";
import Modal from "./Modal.vue";

const emit = defineEmits<{ close: []; added: [id: string, notice: string] }>();
const cands = ref<ReimsCandidate[] | null>(null);
const listError = ref("");
const pick = ref(-1);
const busy = ref(false);
const error = ref("");

async function load() {
  listError.value = "";
  cands.value = null;
  try { cands.value = (await ReimsCtlCandidates()) ?? []; } catch (e) { listError.value = String(e); }
}
async function run(fn: () => Promise<Result>) {
  busy.value = true;
  error.value = "";
  try {
    const r = await fn();
    if (r.error && !r.id) { error.value = r.error; return; }
    emit("added", r.id ?? "", r.error ? `Added, but: ${r.error}` : r.notice ?? "");
  } catch (e) { error.value = String(e); } finally { busy.value = false; }
}
async function fromFile() {
  const path = await PickFile("Add a VM from its settings file (vm.json or an export)", "json").catch(() => "");
  if (path) await run(() => AddVM(path));
}
function fromReims() {
  const c = cands.value?.[pick.value];
  if (c) void run(() => AddReimsCtl(c.name, c.config_dir));
}
onMounted(load);
</script>

<template>
  <Modal title="Add VM" :width="640" :busy="busy" @close="emit('close')">
    <div class="add">
      <p v-if="error" class="note bad" role="alert">{{ error }}</p>
      <section class="grp" aria-labelledby="add-h-file"><h2 id="add-h-file" class="grp-h">From a settings file</h2><div class="grp-b">
        <p class="muted">A VM's vm.json in its folder, or a settings file exported from kvm-gui.</p>
        <div><button type="button" class="btn" :disabled="busy" @click="fromFile">Choose settings file…</button></div>
      </div></section>
      <section class="grp" aria-labelledby="add-h-reims"><h2 id="add-h-reims" class="grp-h">From reims-ctl</h2><div class="grp-b">
        <template v-if="listError">
          <p class="muted">reims-ctl's VMs cannot be listed. Install Reims vGPU (QEMU with reims-ctl) to add them.</p>
          <details class="tech"><summary>Details</summary><p>{{ listError }}</p></details>
          <div><button type="button" class="btn small" @click="load">Retry</button></div>
        </template>
        <p v-else-if="!cands" class="muted" role="status">Looking for reims-ctl VMs…</p>
        <p v-else-if="!cands.length" class="muted">Every reims-ctl VM is already in the list.</p>
        <div v-else class="cands" role="radiogroup" aria-labelledby="add-h-reims">
          <label v-for="(c, i) in cands" :key="c.config_dir + c.name" class="cand">
            <input v-model="pick" type="radio" name="add-reims" :value="i" />
            <span class="cand-t"><span><b>{{ c.name }}</b> <span class="muted">({{ c.state || "stopped" }})</span></span>
              <span class="mono muted">{{ c.dir }}</span><span class="muted">reims-ctl folder: {{ c.config_dir }}</span></span>
          </label>
        </div>
      </div></section>
    </div>
    <template #footer>
      <span v-if="busy" class="muted" role="status">Adding…</span>
      <button type="button" class="btn primary" :disabled="busy || !cands?.[pick]" @click="fromReims">Add reims-ctl VM</button>
      <button type="button" class="btn" :disabled="busy" @click="emit('close')">Cancel</button>
    </template>
  </Modal>
</template>

<style scoped>
.add { padding: 10px 12px; display: flex; flex-direction: column; }
.add p { margin: 0; }
.cands { display: flex; flex-direction: column; gap: 4px; max-height: 220px; overflow-y: auto; }
.cand { display: flex; align-items: flex-start; gap: 8px; padding: 4px; cursor: pointer; }
.cand:hover { background: var(--hover); }
.cand input { margin-top: 3px; }
.cand-t { display: flex; flex-direction: column; min-width: 0; }
</style>
