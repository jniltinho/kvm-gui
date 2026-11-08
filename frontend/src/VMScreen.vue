<script setup lang="ts">
// A running VM's screen inside a window of the app: Reims vGPU's own window taken into this area
// when the app started the run (screen-embedding), else the in-app viewer (VNC over the screen
// server). Used by the Manager's Screen tab (host "manager") and by a VM window (host "vm"); one of
// them holds the window at a time: the one that shows it last takes it, the other says where it is.
// While covered (a menu or dialog over it) or unmounted (another tab, another VM), the taken window
// is hidden, not given back: it stays the app's until its window closes or the app exits.
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { EmbedScreen, EmbedVisible } from "./api";
import Screen from "./Screen.vue";

const props = defineProps<{ id: string; name: string; host: "manager" | "vm"; reims: boolean; covered?: boolean }>();
const area = ref<HTMLElement>();
const state = ref<"embedded" | "waiting" | "elsewhere" | "unavailable">(props.reims ? "waiting" : "unavailable");
const reason = ref("");
const viewer = ref<InstanceType<typeof Screen>>();
const embedding = computed(() => props.reims && state.value !== "unavailable");
defineExpose({ state, reason, viewer });

// take: this area shows the screen now (mounted, uncovered, "Show here"), so it takes the window
// from the other host; a re-placement (resize, the timer) never does.
let take = true;
let asking = false; // one question at a time: two at once raced for the run in the app
async function place(force = false) {
  const el = area.value;
  if (force) take = true;
  if (!el || !embedding.value || props.covered || asking) return;
  asking = true;
  // Asked even before the area has a size: where it cannot embed (Wayland), the viewer at once.
  const r = el.getBoundingClientRect();
  try {
    const s = await EmbedScreen(props.id, props.host, take, r.left, r.top, r.width, r.height, window.devicePixelRatio || 1);
    if (s.state !== "waiting") take = false;
    state.value = s.state;
    reason.value = s.reason;
  } catch (e) {
    state.value = "unavailable";
    reason.value = String(e);
  } finally {
    asking = false;
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
  if (covered) void EmbedVisible(props.id, props.host, false);
  else void place(true);
});
// Until it is placed, ask again (the lookup runs in the app); once placed, keep asking: a scale
// change moves nothing on the page, and the other host may have taken the window.
const poll = setInterval(() => {
  if (state.value === "waiting" || state.value === "embedded") void place();
}, 500);
onBeforeUnmount(() => {
  clearInterval(poll);
  observer?.disconnect();
  if (state.value === "embedded") void EmbedVisible(props.id, props.host, false);
});
</script>

<template>
  <div v-if="embedding" ref="area" class="vms-embed" :role="state === 'embedded' ? 'img' : undefined" :aria-label="state === 'embedded' ? `Screen of ${name}` : undefined">
    <p v-if="state === 'waiting'">Taking Reims vGPU's window into the app…</p>
    <p v-else-if="state === 'elsewhere'">
      The screen of {{ name }} is in its {{ host === "manager" ? "own window" : "Manager's Screen tab" }}.
      <button type="button" class="btn" @click="place(true)">Show here</button>
    </p>
  </div>
  <Screen v-else ref="viewer" :vm="id" interactive :label="`Screen of ${name}`" />
</template>

<style scoped>
.vms-embed { flex: 1; min-width: 0; min-height: 0; display: flex; align-items: center; justify-content: center; background: #000; }
.vms-embed p { margin: 0; color: #aaa; }
</style>
