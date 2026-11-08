<script setup lang="ts">
// A running VM's screen inside a window of the app: Reims vGPU's own window taken into this area
// when the app started the run (screen-embedding), else the in-app viewer (VNC over the screen
// server). Used by the Manager's Screen tab (host "manager") and by a VM window (host "vm").
// While covered (a menu or dialog over it) or unmounted (another tab, another VM), the taken window
// is hidden, not given back: it stays the app's until its window closes or the app exits.
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { EmbedScreen, EmbedVisible } from "./api";
import Screen from "./Screen.vue";

const props = defineProps<{ id: string; name: string; host: "manager" | "vm"; reims: boolean; covered?: boolean }>();
const area = ref<HTMLElement>();
const state = ref<"embedded" | "waiting" | "unavailable">(props.reims ? "waiting" : "unavailable");
const reason = ref("");
const viewer = ref<InstanceType<typeof Screen>>();
const embedding = computed(() => props.reims && state.value !== "unavailable");
defineExpose({ state, reason, viewer });

async function place() {
  const el = area.value;
  if (!el || !embedding.value || props.covered) return;
  // Asked even before the area has a size: where it cannot embed (Wayland), the viewer at once.
  const r = el.getBoundingClientRect();
  try {
    const s = await EmbedScreen(props.id, props.host, r.left, r.top, r.width, r.height, window.devicePixelRatio || 1);
    state.value = s.state;
    reason.value = s.reason;
  } catch (e) {
    state.value = "unavailable";
    reason.value = String(e);
  }
}

let observer: ResizeObserver | undefined;
watch(area, (el) => {
  observer?.disconnect();
  if (el) {
    observer = new ResizeObserver(() => void place());
    observer.observe(el);
    void place();
  }
});
watch(() => props.covered, (covered) => {
  if (state.value !== "embedded") return;
  if (covered) void EmbedVisible(props.id, false);
  else void place();
});
// Until it is placed, ask again: the lookup runs in the app and takes a moment.
const poll = setInterval(() => { if (embedding.value && state.value === "waiting") void place(); }, 500);
onBeforeUnmount(() => {
  clearInterval(poll);
  observer?.disconnect();
  if (state.value === "embedded") void EmbedVisible(props.id, false);
});
</script>

<template>
  <div v-if="embedding" ref="area" class="vms-embed" role="img" :aria-label="`Screen of ${name}`">
    <p v-if="state === 'waiting'">Taking Reims vGPU's window into the app…</p>
  </div>
  <Screen v-else ref="viewer" :vm="id" interactive :label="`Screen of ${name}`" />
</template>

<style scoped>
.vms-embed { flex: 1; min-width: 0; min-height: 0; display: flex; align-items: center; justify-content: center; background: #000; }
.vms-embed p { margin: 0; color: #aaa; }
</style>
