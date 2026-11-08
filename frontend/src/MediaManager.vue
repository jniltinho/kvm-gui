<script setup lang="ts">
// File › Media manager: the hard disk and optical images the VMs use, with size, format, location and
// the VMs using each. Read-only for now: create, copy, grow, convert and remove arrive with task 7.7.
import { computed, onMounted, ref } from "vue";
import { GetMedia, type Medium } from "./api";
import Modal from "./Modal.vue";
import TabBar from "./TabBar.vue";
import { base, size } from "./labels";

const emit = defineEmits<{ close: [] }>();
const TABS = [["disk", "Hard disks", "storage"], ["optical", "Optical discs", "optical"]] as const;
const tab = ref<"disk" | "optical">("disk");
const media = ref<Medium[]>([]);
const error = ref("");
const sel = ref("");
const shown = computed(() => media.value.filter((m) => m.kind === tab.value));
const current = computed(() => shown.value.find((m) => m.path === sel.value));

onMounted(async () => {
  try { media.value = await GetMedia(); } catch (e) { error.value = String(e); }
});
</script>

<template>
  <Modal title="Media manager" :width="900" :height="560" @close="emit('close')">
    <div class="mm">
      <div class="row mm-bar" role="toolbar" aria-label="Media actions">
        <!-- TODO(task 7.7): the core's create, copy, grow-only, raw to qcow2 and remove/delete operations. -->
        <button type="button" class="btn small" disabled title="Arrives with task 7.7">Create…</button>
        <button type="button" class="btn small" disabled title="Arrives with task 7.7">Copy…</button>
        <button type="button" class="btn small" disabled title="Arrives with task 7.7">Grow…</button>
        <button type="button" class="btn small" disabled title="Arrives with task 7.7">Convert to qcow2</button>
        <button type="button" class="btn small" disabled title="Arrives with task 7.7">Remove…</button>
        <button type="button" class="btn small" @click="GetMedia().then((m) => (media = m))">Refresh</button>
      </div>
      <TabBar v-model="tab" :tabs="TABS" size="small" aria-label="Media types" />
      <div class="tab-page mm-page">
        <p v-if="error" class="note bad" role="alert">{{ error }}</p>
        <table class="tbl" :aria-label="tab === 'disk' ? 'Hard disks' : 'Optical discs'">
          <thead><tr><th>Name</th><th>Virtual size</th><th>Used on host</th><th>Format</th><th>Attached to</th></tr></thead>
          <tbody>
            <tr v-for="m in shown" :key="m.path" :class="{ on: m.path === sel }" tabindex="0" :aria-selected="m.path === sel"
              @click="sel = m.path" @keydown.enter="sel = m.path">
              <td><img :src="`./icons/${m.kind === 'disk' ? 'storage' : 'optical'}.svg`" alt="" /> {{ base(m.path) }}<span v-if="m.missing" class="bad"> (missing)</span></td>
              <td>{{ size(m.sizeBytes) }}</td><td>{{ size(m.usedBytes) }}</td><td>{{ m.format }}</td><td>{{ m.vms.join(", ") }}</td>
            </tr>
            <tr v-if="!shown.length"><td colspan="5" class="muted">No {{ tab === "disk" ? "hard disk" : "optical disc" }} image is used by a VM.</td></tr>
          </tbody>
        </table>
      </div>
      <dl v-if="current" class="mm-info" aria-label="Selected image">
        <dt>Location</dt><dd class="mono">{{ current.path }}</dd>
        <dt>Attached to</dt><dd>{{ current.vms.join(", ") }}</dd>
      </dl>
    </div>
    <template #footer><button type="button" class="btn primary" @click="emit('close')">Close</button></template>
  </Modal>
</template>

<style scoped>
.mm { display: flex; flex-direction: column; flex: 1; min-height: 0; padding: 8px 10px 10px; gap: 6px; }
.mm-bar { padding-bottom: 2px; }
.mm-page { flex: 1; min-height: 0; overflow: auto; }
.tbl { width: 100%; border-collapse: collapse; }
.tbl th { position: sticky; top: 0; text-align: left; font-weight: 600; padding: 4px 8px; background: var(--panel); border-bottom: 1px solid var(--border); }
.tbl td { padding: 4px 8px; border-bottom: 1px solid var(--border-subtle); white-space: nowrap; }
.tbl td img { display: inline-block; width: 16px; height: 16px; vertical-align: -3px; }
.tbl tr[tabindex] { cursor: pointer; }
.tbl tr[tabindex]:hover:not(.on) td { background: var(--hover); }
.tbl tr.on td { background: var(--selection); color: var(--selection-text); }
.mm-info { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 3px 14px; margin: 0; padding: 8px; border: 1px solid var(--border-subtle); background: var(--surface); }
.mm-info dt { color: var(--text-muted); }
.mm-info dd { margin: 0; }
</style>
