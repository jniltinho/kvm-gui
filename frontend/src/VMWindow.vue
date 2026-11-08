<script setup lang="ts">
// A VM window (vm-manager-ui › VM window; route #/vm/<id>, opened by the Manager's Start or Show): the VM's
// screen while it runs, a placeholder with Start while it is stopped, and a status bar with the host key.
// Closing it leaves the VM running.
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Window } from "@wailsio/runtime";
import { GetVM, Start, Theme, VMs, type VM, type VMInfo } from "./api";
import { applySkin } from "./themes";
import GuestIcon from "./GuestIcon.vue";
import VMScreen from "./VMScreen.vue";
import { HOST_KEY_LABEL } from "./viewer";

const props = defineProps<{ id: string }>();
const vm = ref<VM | null>(null);
const info = ref<VMInfo | null>(null);
const error = ref("");
const starting = ref(false);
const screen = ref<InstanceType<typeof VMScreen>>();
const viewer = computed(() => screen.value?.viewer);

const reims = computed(() => vm.value?.graphics === "reims-vgpu");
// The screen is in this window unless QEMU draws it in its own (Settings › Display › Screen).
const inApp = computed(() => reims.value || !!vm.value?.screen); // the run's, whatever the setting says now
async function refresh() {
  try {
    const v = (await VMs()).find((x) => x.id === props.id) ?? null;
    if (v && (!info.value || v.running !== vm.value?.running)) info.value = await GetVM(props.id);
    vm.value = v;
    error.value = v ? "" : "This VM is no longer in the list.";
  } catch (e) {
    error.value = String(e);
  }
}
watch(() => [vm.value?.name, vm.value?.state], ([name, state]) => {
  if (name) void Window.SetTitle(`${name} [${state}] — kvm-gui`);
});

async function start(headless: boolean) {
  starting.value = true;
  try {
    await Start(props.id, headless);
    await refresh();
    if (headless) void Window.Close(); // Headless: no VM window
  } catch (e) {
    error.value = String(e);
  } finally {
    starting.value = false;
  }
}

let timer: ReturnType<typeof setInterval> | undefined;
onMounted(async () => {
  try { applySkin(await Theme()); } catch { /* the system skin stays */ }
  await refresh();
  timer = setInterval(refresh, 2000);
});
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <div class="vmw">
    <main class="vmw-screen" :aria-label="vm ? `${vm.name} screen` : 'Screen'">
      <VMScreen v-if="vm?.running && inApp" ref="screen" :id="id" :name="vm.name" host="vm" :reims="reims" />
      <div v-else-if="vm?.running" class="ph">
        <GuestIcon :guest="vm.guest" :size="56" />
        <p>The screen of <b>{{ vm.name }}</b> is in QEMU's own window (Settings › Display › Screen).</p>
      </div>
      <div v-else-if="vm" class="ph">
        <GuestIcon :guest="vm.guest" :size="64" />
        <h1>{{ vm.name }}</h1>
        <p class="muted">{{ vm.os }} · {{ vm.state }}</p>
        <div class="row">
          <button type="button" class="btn primary" :disabled="starting" @click="start(false)"><img src="/icons/start.svg" alt="" /> {{ starting ? "Starting…" : "Start" }}</button>
          <button type="button" class="btn" :disabled="starting || reims" :title="reims ? 'Reims vGPU always opens its own window, so Headless is not available.' : undefined" @click="start(true)">Headless start</button>
        </div>
        <p class="muted">Once started, the VM keeps running when you close this window.</p>
      </div>
      <p v-else class="ph" role="status">{{ error || "Loading…" }}</p>
    </main>
    <footer class="status" role="status" aria-live="polite">
      <span v-if="vm"><img :src="`./icons/${vm.running ? 'state-running' : 'state-off'}.svg`" alt="" /> {{ vm.state }}</span>
      <span v-if="viewer?.connected" class="muted">{{ viewer.size[0] }}×{{ viewer.size[1] }}</span>
      <span class="grow" :class="error ? 'bad' : 'muted'">{{ error || (screen?.state === 'embedded' ? "Reims vGPU's display: move the pointer over it to type." : vm?.running && reims && screen?.reason ? `Reims vGPU shows this screen in its own window (${screen.reason}).` : "") }}</span>
      <template v-if="viewer?.connected">
        <span>{{ viewer.grabbed ? "Keyboard captured" : "Click the screen to capture the keyboard" }}</span>
        <span class="muted">Host key: {{ HOST_KEY_LABEL }} · Full screen: Ctrl+Alt+F</span>
      </template>
    </footer>
  </div>
</template>

<style scoped>
.vmw { height: 100%; display: flex; flex-direction: column; }
.vmw-screen { flex: 1; min-height: 0; display: flex; background: #000; }
.ph { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; margin: 0; padding: 24px; text-align: center; background: var(--surface); color: var(--text); }
.ph h1 { margin: 0; font-size: 20px; font-weight: 600; }
.ph p { margin: 0; max-width: 520px; }
.status { flex: none; display: flex; align-items: center; gap: 16px; height: 24px; padding: 0 10px; font-size: 12px; background: var(--toolbar); border-top: 1px solid var(--border-subtle); white-space: nowrap; overflow: hidden; }
.status > span { display: inline-flex; align-items: center; gap: 4px; }
.status img { width: 10px; height: 10px; }
.status .grow { overflow: hidden; text-overflow: ellipsis; }
</style>
