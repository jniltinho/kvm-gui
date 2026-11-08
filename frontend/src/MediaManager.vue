<script setup lang="ts">
// File › Media manager: the hard disk and optical images the VMs use, each with its size, format, location and
// the VMs using it. Read-only for now, and it says so: creating, copying, growing, converting and removing
// images are not built yet.
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
const shown = computed(() => media.value.filter((m) => m.kind === tab.value));

async function load() {
  error.value = "";
  try { media.value = await GetMedia(); } catch (e) { error.value = String(e); }
}
onMounted(load);
</script>

<template>
  <Modal title="Media manager" :width="900" :height="560" @close="emit('close')">
    <div class="mm">
      <div class="row mm-bar">
        <p class="note mm-ro grow">Read-only for now: this list shows the images your VMs use. Creating, copying, growing, converting and removing images are not available yet.</p>
        <button type="button" class="btn small" @click="load">Refresh</button>
      </div>
      <div class="mm-tabs">
        <TabBar v-model="tab" :tabs="TABS" size="small" panel="mm-page" aria-label="Media types" />
        <div id="mm-page" class="tab-page mm-page" role="tabpanel" :aria-labelledby="`mm-page-tab-${tab}`">
          <p v-if="error" class="note bad" role="alert">{{ error }}</p>
          <table class="tbl" :aria-label="tab === 'disk' ? 'Hard disks' : 'Optical discs'">
            <thead><tr><th>Name</th><th>Location</th><th>Virtual size</th><th>Used on host</th><th>Format</th><th>Attached to</th></tr></thead>
            <tbody>
              <tr v-for="m in shown" :key="m.path">
                <td><img :src="`./icons/${m.kind === 'disk' ? 'storage' : 'optical'}.svg`" alt="" /> {{ base(m.path) }}<span v-if="m.missing" class="bad"> (missing)</span></td>
                <td class="mono path" :title="m.path">{{ m.path }}</td>
                <td>{{ size(m.sizeBytes) }}</td><td>{{ size(m.usedBytes) }}</td><td>{{ m.format }}</td><td class="vms">{{ m.vms.join(", ") }}</td>
              </tr>
              <tr v-if="!shown.length"><td colspan="6" class="muted">No {{ tab === "disk" ? "hard disk" : "optical disc" }} image is used by a VM.</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
    <template #footer><button type="button" class="btn primary" @click="emit('close')">Close</button></template>
  </Modal>
</template>

<style scoped>
.mm { display: flex; flex-direction: column; flex: 1; min-height: 0; padding: 8px 10px 10px; gap: 6px; }
.mm-bar { align-items: flex-start; flex-wrap: nowrap; }
.mm-ro { margin: 0; color: var(--text); }
.mm-tabs { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.mm-page { flex: 1; min-height: 0; overflow: auto; }
.tbl { width: 100%; border-collapse: collapse; }
.tbl th { position: sticky; top: 0; text-align: left; font-weight: 600; padding: 4px 8px; background: var(--panel); border-bottom: 1px solid var(--border); }
.tbl td { padding: 4px 8px; border-bottom: 1px solid var(--border-subtle); white-space: nowrap; vertical-align: top; }
.tbl td.path { white-space: normal; min-width: 220px; font-size: 11px; }
.tbl td.vms { white-space: normal; }
.tbl td img { display: inline-block; width: 16px; height: 16px; vertical-align: -3px; }
</style>
