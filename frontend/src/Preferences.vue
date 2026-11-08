<script setup lang="ts">
// File › Preferences (spec vm-manager-ui › Preferences). One policy for every page: changes wait for Apply or OK,
// then apply at once (no restart) and are kept for the next start; closing with changes not applied asks first.
import { computed, onMounted, reactive, ref } from "vue";
import { GetPrefs, PickFolder, SetScale, SetTheme, SetWorkspace, type Prefs } from "./api";
import { askConfirm } from "./ask";
import Dropdown, { type Option } from "./Dropdown.vue";
import Modal from "./Modal.vue";
import TabBar from "./TabBar.vue";
import { applySkin, SKINS } from "./themes";

const emit = defineEmits<{ close: [] }>();
const TABS = [["general", "General", "general"], ["input", "Input", "usb"], ["display", "Display", "display"], ["interface", "Interface", "settings"]] as const;
const tab = ref<(typeof TABS)[number][0]>("general");
const p = ref<Prefs | null>(null); // what is saved
const d = reactive({ workspace: "", theme: "", scale: "" }); // what the pages show
const loadError = ref("");
const error = ref("");
const saved = ref("");
const busy = ref(false);

const SKIN_OPTIONS: Option<string>[] = [...SKINS.map((k): Option<string> => [k.id, k.label]), ["system", "System (follows the desktop)", { sep: true }]];
const SCALES: Option<string>[] = [["100", "100%"], ["125", "125%"], ["150", "150%"], ["200", "200%"]];
const dirty = computed(() => !!p.value && (d.workspace.trim() !== p.value.workspace || d.theme !== p.value.theme || d.scale !== p.value.scale));

async function load() {
  loadError.value = "";
  try {
    p.value = await GetPrefs();
    Object.assign(d, { workspace: p.value.workspace, theme: p.value.theme, scale: p.value.scale });
  } catch (e) { loadError.value = String(e); }
}
async function apply(): Promise<boolean> {
  const was = p.value;
  if (!was || busy.value) return false;
  error.value = saved.value = "";
  busy.value = true;
  try {
    if (d.workspace.trim() !== was.workspace) await SetWorkspace(d.workspace.trim());
    if (d.theme !== was.theme) { await SetTheme(d.theme); applySkin(d.theme); }
    if (d.scale !== was.scale) await SetScale(d.scale);
    await load();
    saved.value = "Preferences applied.";
    return true;
  } catch (e) { error.value = String(e); return false; } finally { busy.value = false; }
}
async function ok() { if (!dirty.value || (await apply())) emit("close"); }
async function close() {
  if (busy.value) return;
  if (!dirty.value || (await askConfirm("Preferences", "Some changes are not applied yet. Discard them and close?", "Discard"))) emit("close");
}
// Browse only fills the field; Apply or OK saves it, as every other choice here.
async function browse() {
  const f = await PickFolder("Choose the Workspace folder").catch(() => "");
  if (f) d.workspace = f;
}
onMounted(load);
</script>

<template>
  <Modal title="Preferences" :width="640" :height="420" :busy="busy" @close="close">
    <div v-if="loadError" class="pf">
      <p class="note bad" role="alert">The preferences could not be read: {{ loadError }}</p>
      <div><button type="button" class="btn" @click="load">Retry</button></div>
    </div>
    <div v-else-if="p" class="pf">
      <TabBar v-model="tab" :tabs="TABS" size="small" panel="pf-page" aria-label="Preference pages" />
      <div id="pf-page" class="pf-page tab-page" role="tabpanel" :aria-labelledby="`pf-page-tab-${tab}`">
        <template v-if="tab === 'general'">
          <section class="grp"><h2 class="grp-h">Workspace</h2><div class="grp-b">
            <div class="row"><input v-model="d.workspace" class="in grow mono" aria-label="Workspace folder" aria-describedby="pf-ws-help" spellcheck="false" @keydown.enter="apply" />
              <button type="button" class="btn small" aria-label="Browse for the Workspace folder" @click="browse">Browse…</button></div>
            <p id="pf-ws-help" class="muted">New VMs get a folder here, named after the VM. Existing VMs stay where they are.</p>
          </div></section>
        </template>
        <template v-else-if="tab === 'input'">
          <section class="grp"><h2 class="grp-h">Host key</h2><div class="grp-b">
            <div class="row"><span>Capture and release the keyboard of a VM screen:</span> <kbd class="key">{{ p.hostKey }}</kbd></div>
            <div class="row"><span>Full screen of a VM window:</span> <kbd class="key">Ctrl+Alt+F</kbd></div>
            <p class="muted">The VM screen takes the keyboard while it has the focus; this combination gives it back. It cannot be changed yet.</p>
          </div></section>
        </template>
        <template v-else-if="tab === 'display'">
          <section class="grp"><h2 class="grp-h">Interface scale</h2><div class="grp-b">
            <div class="row"><span>Scale</span><Dropdown v-model="d.scale" :options="SCALES" :width="100" aria-label="Interface scale" /></div>
            <p class="muted">Every window redraws at this scale when you apply it.</p>
          </div></section>
        </template>
        <template v-else>
          <section class="grp"><h2 class="grp-h">Skin</h2><div class="grp-b">
            <div class="row"><span>Skin</span><Dropdown v-model="d.theme" :options="SKIN_OPTIONS" :width="230" aria-label="Skin" /></div>
            <p class="muted">KVM_GUI_THEME (or MKG_THEME) overrides this choice at start.</p>
          </div></section>
        </template>
        <p v-if="error" class="note bad" role="alert">{{ error }}</p>
        <p v-else-if="saved && !dirty" class="ok-text" role="status">{{ saved }}</p>
      </div>
    </div>
    <p v-else class="muted" style="padding: 12px" role="status">Loading…</p>
    <template #footer>
      <span v-if="dirty" class="muted grow">Changes not applied yet.</span>
      <button type="button" class="btn primary" :disabled="busy || !p" @click="ok">OK</button>
      <button type="button" class="btn" :disabled="busy || !dirty" @click="apply">Apply</button>
      <button type="button" class="btn" :disabled="busy" @click="close">Cancel</button>
    </template>
  </Modal>
</template>

<style scoped>
.pf { display: flex; flex-direction: column; flex: 1; min-height: 0; padding: 8px 10px 10px; }
.pf-page { flex: 1; min-height: 0; overflow-y: auto; padding: 12px; }
.key { padding: 1px 6px; border: 1px solid var(--border); background: var(--surface); font: inherit; border-radius: var(--ctl-radius); }
</style>
