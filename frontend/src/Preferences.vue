<script setup lang="ts">
// File › Preferences (spec vm-manager-ui › Preferences): each choice applies at once and is kept for the next start.
import { onMounted, ref } from "vue";
import { GetPrefs, PickFolder, SetScale, SetTheme, SetWorkspace, type Prefs } from "./api";
import Dropdown, { type Option } from "./Dropdown.vue";
import Modal from "./Modal.vue";
import TabBar from "./TabBar.vue";
import { applySkin, SKINS } from "./themes";

const emit = defineEmits<{ close: [] }>();
const TABS = [["general", "General", "general"], ["input", "Input", "usb"], ["display", "Display", "display"], ["interface", "Interface", "settings"]] as const;
const tab = ref<(typeof TABS)[number][0]>("general");
const p = ref<Prefs | null>(null);
const workspace = ref("");
const error = ref("");
const saved = ref("");

const SKIN_OPTIONS: Option<string>[] = [...SKINS.map((k): Option<string> => [k.id, k.label]), ["system", "System (follows the desktop)", { sep: true }]];
const SCALES: Option<string>[] = [["100", "100%"], ["125", "125%"], ["150", "150%"], ["200", "200%"]];

async function run(fn: () => Promise<void>, done: string) {
  error.value = saved.value = "";
  try { await fn(); saved.value = done; } catch (e) { error.value = String(e); }
}
const setSkin = (id: string) => { p.value!.theme = id; applySkin(id); void run(() => SetTheme(id), "Skin saved."); };
const setScale = (s: string) => { p.value!.scale = s; void run(() => SetScale(s), "Interface scale saved."); };
const saveWorkspace = () => run(async () => { await SetWorkspace(workspace.value); p.value = await GetPrefs(); workspace.value = p.value.workspace; }, "Workspace saved.");
async function browse() {
  const d = await PickFolder("Choose the Workspace folder").catch(() => "");
  if (d) { workspace.value = d; await saveWorkspace(); }
}
onMounted(async () => {
  try { p.value = await GetPrefs(); workspace.value = p.value.workspace; } catch (e) { error.value = String(e); }
});
</script>

<template>
  <Modal title="Preferences" :width="640" :height="420" @close="emit('close')">
    <div v-if="p" class="pf">
      <TabBar v-model="tab" :tabs="TABS" size="small" aria-label="Preference pages" />
      <div class="pf-page tab-page" role="tabpanel" :aria-label="TABS.find((t) => t[0] === tab)?.[1]">
        <template v-if="tab === 'general'">
          <section class="grp"><div class="grp-h">Workspace</div><div class="grp-b">
            <div class="row"><input v-model="workspace" class="in grow mono" aria-label="Workspace folder" spellcheck="false" @keydown.enter="saveWorkspace" />
              <button type="button" class="btn small" @click="browse">Browse…</button>
              <button type="button" class="btn small" :disabled="workspace === p.workspace" @click="saveWorkspace">Apply</button></div>
            <p class="muted">New VMs get a folder here, named after the VM. Existing VMs stay where they are.</p>
          </div></section>
        </template>
        <template v-else-if="tab === 'input'">
          <section class="grp"><div class="grp-h">Host key</div><div class="grp-b">
            <div class="row"><span>Release the keyboard and pointer from a VM screen:</span> <kbd class="key">{{ p.hostKey }}</kbd></div>
            <p class="muted">The VM screen takes the keyboard while it has the focus; this combination gives it back. It cannot be changed yet.</p>
          </div></section>
        </template>
        <template v-else-if="tab === 'display'">
          <section class="grp"><div class="grp-h">Interface scale</div><div class="grp-b">
            <div class="row"><span>Scale</span><Dropdown :model-value="p.scale" :options="SCALES" :width="100" aria-label="Interface scale" @update:model-value="setScale" /></div>
            <p class="muted">Every window redraws at once at this scale.</p>
          </div></section>
        </template>
        <template v-else>
          <section class="grp"><div class="grp-h">Skin</div><div class="grp-b">
            <div class="row"><span>Skin</span><Dropdown :model-value="p.theme" :options="SKIN_OPTIONS" :width="230" aria-label="Skin" @update:model-value="setSkin" /></div>
            <p class="muted">KVM_GUI_THEME (or MKG_THEME) overrides this choice at start.</p>
          </div></section>
        </template>
        <p v-if="error" class="note bad" role="alert">{{ error }}</p>
        <p v-else-if="saved" class="ok-text" role="status">{{ saved }}</p>
      </div>
    </div>
    <template #footer><button type="button" class="btn primary" @click="emit('close')">Close</button></template>
  </Modal>
</template>

<style scoped>
.pf { display: flex; flex-direction: column; flex: 1; min-height: 0; padding: 8px 10px 10px; }
.pf-page { flex: 1; min-height: 0; overflow-y: auto; padding: 12px; }
.key { padding: 1px 6px; border: 1px solid var(--border); background: var(--surface); font: inherit; border-radius: var(--ctl-radius); }
</style>
