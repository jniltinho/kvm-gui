<script setup lang="ts">
// The Manager window (spec vm-manager-ui › Manager window): File / Machine / Help menus, the toolbar,
// the VM list, the welcome pane, and the Details / Snapshots / Logs tools of the selected VM.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Application } from "@wailsio/runtime";
import {
  DeleteVM, GetLog, GetPrefs, GetVM, Ready, SelectVM, Show, ShutDown, PowerOff, Start, Theme, Version, VMs,
  type VM, type VMInfo,
} from "./api";
import { applySkin } from "./themes";
import { askConfirm, showInfo, askState } from "./ask";
import AddVMDialog from "./AddVMDialog.vue";
import AskDialog from "./AskDialog.vue";
import Details from "./Details.vue";
import GuestIcon from "./GuestIcon.vue";
import MediaManager from "./MediaManager.vue";
import Modal from "./Modal.vue";
import NewVMWizard from "./NewVMWizard.vue";
import Preferences from "./Preferences.vue";
import TabBar from "./TabBar.vue";
import VMScreen from "./VMScreen.vue";
import VMSettings from "./VMSettings.vue";

const vms = ref<VM[]>([]);
const selectedId = ref("");
const info = ref<VMInfo | null>(null);
const version = ref("");
const error = ref("");
const status = ref("");
const loaded = ref(false);

const selected = computed(() => vms.value.find((v) => v.id === selectedId.value) ?? null);
const running = computed(() => vms.value.filter((v) => v.running).length);
// The selected VM's details, only once they are this VM's (a slower answer for another VM never shows).
const shownInfo = computed(() => (info.value?.id === selectedId.value ? info.value : null));

// Dialogs: one at a time. Settings and Remove keep the VM they were opened for, whatever the list does meanwhile.
const dialog = ref<"" | "new" | "add" | "settings" | "prefs" | "media" | "about" | "remove">("");
const settingsPage = ref("general");
const editing = ref<VMInfo | null>(null);
const removing = ref<VM | null>(null);
const purge = ref(false);

// Tools of the selected VM.
const TOOLS = [["details", "Details", "general"], ["screen", "Screen", "display"], ["snapshots", "Snapshots", "snapshot"], ["logs", "Logs", "log"]] as const;
type Tool = (typeof TOOLS)[number][0];
const tool = ref<Tool>("details");
const log = ref("");

async function refresh() {
  try {
    const list = await VMs();
    const changed = JSON.stringify(list) !== JSON.stringify(vms.value);
    vms.value = list;
    if (!list.some((v) => v.id === selectedId.value)) selectedId.value = list[0]?.id ?? "";
    if (changed) await loadInfo();
    error.value = "";
  } catch (e) {
    error.value = String(e);
  }
}

// Each load is numbered: only the newest answer, and only for the VM still selected, is kept.
let infoSeq = 0;
async function loadInfo() {
  const id = selectedId.value;
  const seq = ++infoSeq;
  if (!id) { info.value = null; return; }
  try {
    const v = await GetVM(id);
    if (seq === infoSeq && id === selectedId.value) info.value = v;
  } catch (e) { if (seq === infoSeq) error.value = String(e); }
  if (tool.value === "logs") await loadLog();
}
let logSeq = 0;
async function loadLog() {
  const id = selectedId.value;
  const seq = ++logSeq;
  if (!id) return;
  let text: string;
  try { text = await GetLog(id); } catch (e) { text = String(e); }
  if (seq === logSeq && id === selectedId.value) log.value = text;
}

async function select(id: string) {
  if (id === selectedId.value) return;
  selectedId.value = id;
  log.value = "";
  void SelectVM(id).catch(() => {});
  await loadInfo();
}
watch(tool, (t) => { if (t === "logs") void loadLog(); });

// VM list keyboard: ↑/↓/Home/End move the selection; Enter opens Settings.
const list = ref<HTMLUListElement>();
function listKey(e: KeyboardEvent) {
  const i = vms.value.findIndex((v) => v.id === selectedId.value);
  const n = vms.value.length;
  const next = { ArrowDown: Math.min(n - 1, i + 1), ArrowUp: Math.max(0, i - 1), Home: 0, End: n - 1 }[e.key];
  if (e.key === "Enter" && selected.value) { void openSettings("general"); return; }
  if (next === undefined || !n) return;
  e.preventDefault();
  void select(vms.value[next].id);
  nextTick(() => list.value?.querySelector<HTMLElement>(".vm.on")?.scrollIntoView({ block: "nearest" }));
}

// Actions.
// Settings edits the VM selected when it was asked for, with its details read again for it.
async function openSettings(page: string) {
  const id = selectedId.value;
  if (!id) return;
  await loadInfo();
  if (id !== selectedId.value || info.value?.id !== id) return;
  editing.value = info.value;
  settingsPage.value = page;
  dialog.value = "settings";
}
function addVM() {
  menu.value = "";
  dialog.value = "add";
}
async function added(id: string, notice: string) {
  dialog.value = "";
  await refresh();
  if (id) await select(id);
  status.value = notice || "VM added.";
}
function removeVM() {
  if (!selected.value) return;
  removing.value = selected.value;
  purge.value = false;
  dialog.value = "remove";
}
async function confirmRemove() {
  const vm = removing.value;
  dialog.value = "";
  removing.value = null;
  if (!vm) return;
  try {
    const removed = await DeleteVM(vm.id, purge.value);
    status.value = `Removed ${vm.name}` + (purge.value ? ` and ${removed.length - 1} of its files.` : " from the list.");
    await refresh();
  } catch (e) { void showInfo("Remove VM", String(e)); }
}
async function shutDown() {
  const vm = selected.value;
  if (!vm?.running) return;
  if (!(await askConfirm("Shut down", `Send the shutdown signal to ${vm.name}? The guest shuts down by itself, or asks on its screen.`, "Shut down"))) return;
  try { await ShutDown(vm.id); status.value = `Shutdown signal sent to ${vm.name}.`; } catch (e) { void showInfo("Shut down", String(e)); }
}
// Power off: for a guest that ignores the shutdown signal or hangs (VirtualBox's Power Off).
async function powerOff() {
  const vm = selected.value;
  if (!vm?.running) return;
  if (!(await askConfirm("Power off", `Power off ${vm.name} now, like pulling the plug? The guest loses everything it has not saved.`, "Power off"))) return;
  try { await PowerOff(vm.id); status.value = `${vm.name} was powered off.`; await refresh(); } catch (e) { void showInfo("Power off", String(e)); }
}

// Start runs the VM detached (core.StartDetached): it keeps running when this window closes.
const starting = ref("");
const canStart = computed(() => !!selected.value && !selected.value.running && !starting.value);
const reimsGPU = computed(() => selected.value?.graphics === "reims-vgpu");
const headlessNote = "Reims vGPU always opens its own window, so Headless is not available.";
async function start(headless: boolean) {
  const vm = selected.value;
  closeStart(false);
  menu.value = "";
  if (!vm || vm.running || starting.value || (headless && reimsGPU.value)) return;
  starting.value = vm.id;
  status.value = `Starting ${vm.name}…`;
  try {
    await Start(vm.id, headless);
    await refresh();
    if (!headless && selectedId.value === vm.id) tool.value = "screen";
    status.value = `${vm.name} is running. It keeps running when you close this window.`;
  } catch (e) {
    status.value = `${vm.name} did not start.`;
    void showInfo(`Start ${vm.name}`, String(e));
  } finally { starting.value = ""; }
}
// Show opens the running VM's window, or brings it to the front (Normal start opens it from Go).
async function show() {
  const vm = selected.value;
  menu.value = "";
  if (!vm?.running) return;
  try { await Show(vm.id); } catch (e) { void showInfo(`Show ${vm.name}`, String(e)); }
}

// Exit closes the Manager only: VMs keep running (vm-manager-ui › Close dialog).
const quit = () => void Application.Quit();
async function created(id: string, notice: string) {
  dialog.value = "";
  await refresh();
  await select(id);
  tool.value = "details";
  status.value = notice || "VM created.";
}
async function saved() {
  dialog.value = "";
  editing.value = null;
  await refresh();
  await loadInfo();
  status.value = "Settings saved.";
}

// Menus (goXterm's menubar): one open at a time; arrows move between menus and items.
const menu = ref("");
const MENUS = ["file", "machine", "help"] as const;
function toggleMenu(m: string) { menu.value = menu.value === m ? "" : m; if (menu.value) focusItem(0); }
function run(fn: () => void) { menu.value = ""; fn(); }
function focusItem(i: number) {
  nextTick(() => {
    const items = [...document.querySelectorAll<HTMLButtonElement>(".menu-d .menu-i:not(:disabled)")];
    items[(i + items.length) % items.length]?.focus();
  });
}
function menuKey(e: KeyboardEvent) {
  if (!menu.value) {
    if (e.key === "ArrowDown") { e.preventDefault(); toggleMenu((e.target as HTMLElement).dataset.menu ?? "file"); }
    return;
  }
  const items = [...document.querySelectorAll<HTMLButtonElement>(".menu-d .menu-i:not(:disabled)")];
  const i = items.indexOf(document.activeElement as HTMLButtonElement);
  const m = MENUS.indexOf(menu.value as (typeof MENUS)[number]);
  if (e.key === "Escape") { e.preventDefault(); const t = menu.value; menu.value = ""; document.querySelector<HTMLElement>(`[data-menu="${t}"]`)?.focus(); }
  else if (e.key === "ArrowDown") { e.preventDefault(); focusItem(i + 1); }
  else if (e.key === "ArrowUp") { e.preventDefault(); focusItem(i - 1); }
  else if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
    e.preventDefault();
    menu.value = MENUS[(m + (e.key === "ArrowRight" ? 1 : MENUS.length - 1)) % MENUS.length];
    focusItem(0);
  }
}

// Start options: drawn outside the toolbar (which scrolls sideways, and would clip it), under its ▼.
const startOpen = ref(false);
const startArrow = ref<HTMLButtonElement>();
const startPop = ref<HTMLDivElement>();
const startPos = ref({ left: 0, top: 0 });
function toggleStart() {
  if (startOpen.value) return closeStart(true);
  const group = startArrow.value!.parentElement!.getBoundingClientRect();
  startPos.value = { left: group.left, top: group.bottom + 1 };
  startOpen.value = true;
  nextTick(() => (startPop.value?.querySelector<HTMLElement>(".menu-i:not(:disabled)") ?? startPop.value)?.focus());
}
function closeStart(focus: boolean) {
  if (!startOpen.value) return;
  startOpen.value = false;
  if (focus) startArrow.value?.focus();
}
function startKey(e: KeyboardEvent) {
  const items = [...(startPop.value?.querySelectorAll<HTMLElement>(".menu-i:not(:disabled)") ?? [])];
  const i = items.indexOf(document.activeElement as HTMLElement);
  if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); closeStart(true); }
  else if (e.key === "Tab") closeStart(false);
  else if (items.length && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    e.preventDefault();
    items[(i + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length].focus();
  }
}
function closeMenus(e: MouseEvent) {
  const t = e.target as HTMLElement;
  if (!t.closest(".menubar")) menu.value = "";
  if (!t.closest(".start, .start-d")) closeStart(false);
}

function shortcut(e: KeyboardEvent) {
  if (e.defaultPrevented || dialog.value || !e.ctrlKey || e.altKey) return; // a guest's keys are its own
  const k = e.key.toLowerCase();
  if (k === "n") { e.preventDefault(); dialog.value = "new"; }
  else if (k === "s" && selected.value) { e.preventDefault(); void openSettings("general"); }
}

let timer: ReturnType<typeof setInterval> | undefined;
onMounted(async () => {
  window.addEventListener("click", closeMenus);
  window.addEventListener("keydown", shortcut);
  try {
    applySkin(await Theme());
    version.value = await Version();
    await refresh();
    loaded.value = true;
    await Ready();
  } catch (e) {
    error.value = String(e);
  }
  void GetPrefs().catch(() => {});
  timer = setInterval(refresh, 2000); // states follow without a refresh (vm-manager-ui › State in the list)
});
onBeforeUnmount(() => {
  clearInterval(timer);
  window.removeEventListener("click", closeMenus);
  window.removeEventListener("keydown", shortcut);
});
</script>

<template>
  <div class="mgr" :inert="!!dialog || undefined">
    <nav class="menubar" role="menubar" aria-label="Main menu" @keydown="menuKey">
      <div class="menu-w">
        <button type="button" class="menu-t" role="menuitem" data-menu="file" aria-haspopup="menu" :aria-expanded="menu === 'file'" :class="{ on: menu === 'file' }" @click.stop="toggleMenu('file')">File</button>
        <div v-if="menu === 'file'" class="menu menu-d" role="menu" aria-label="File">
          <button type="button" class="menu-i" role="menuitem" @click="run(() => dialog = 'prefs')"><img src="/icons/settings.svg" alt="" /> Preferences…</button>
          <button type="button" class="menu-i" role="menuitem" @click="run(() => dialog = 'media')"><img src="/icons/storage.svg" alt="" /> Media manager…</button>
          <div class="menu-sep" role="separator"></div>
          <button type="button" class="menu-i" role="menuitem" @click="run(quit)"><span class="ico"></span> Exit</button>
        </div>
      </div>
      <div class="menu-w">
        <button type="button" class="menu-t" role="menuitem" data-menu="machine" aria-haspopup="menu" :aria-expanded="menu === 'machine'" :class="{ on: menu === 'machine' }" @click.stop="toggleMenu('machine')">Machine</button>
        <div v-if="menu === 'machine'" class="menu menu-d" role="menu" aria-label="Machine">
          <button type="button" class="menu-i" role="menuitem" @click="run(() => dialog = 'new')"><img src="/icons/new.svg" alt="" /> New…<kbd>Ctrl+N</kbd></button>
          <button type="button" class="menu-i" role="menuitem" @click="run(addVM)"><img src="/icons/import.svg" alt="" /> Add…</button>
          <button type="button" class="menu-i" role="menuitem" :disabled="!selected" @click="run(() => openSettings('general'))"><img src="/icons/settings.svg" alt="" /> Settings…<kbd>Ctrl+S</kbd></button>
          <button type="button" class="menu-i" role="menuitem" :disabled="!selected || selected.running" @click="run(removeVM)"><img src="/icons/remove.svg" alt="" /> Remove…</button>
          <div class="menu-sep" role="separator"></div>
          <button type="button" class="menu-i" role="menuitem" :disabled="!canStart" @click="start(false)"><img src="/icons/start.svg" alt="" /> Start (Normal)</button>
          <button type="button" class="menu-i" role="menuitem" :disabled="!canStart || reimsGPU" :title="reimsGPU ? headlessNote : undefined" @click="start(true)"><img src="/icons/start.svg" alt="" /> Start (Headless)</button>
          <button type="button" class="menu-i" role="menuitem" :disabled="!selected?.running" @click="show"><img src="/icons/window.svg" alt="" /> Show</button>
          <button type="button" class="menu-i" role="menuitem" :disabled="!selected?.running" @click="run(shutDown)"><img src="/icons/shutdown.svg" alt="" /> Shut down</button>
          <button type="button" class="menu-i" role="menuitem" :disabled="!selected?.running" @click="run(powerOff)"><img src="/icons/forcestop.svg" alt="" /> Power off…</button>
          <div class="menu-sep" role="separator"></div>
          <button type="button" class="menu-i" role="menuitem" :disabled="!selected" @click="run(() => tool = 'logs')"><img src="/icons/log.svg" alt="" /> Show log</button>
          <button type="button" class="menu-i" role="menuitem" @click="run(refresh)"><span class="ico"></span> Refresh</button>
        </div>
      </div>
      <div class="menu-w">
        <button type="button" class="menu-t" role="menuitem" data-menu="help" aria-haspopup="menu" :aria-expanded="menu === 'help'" :class="{ on: menu === 'help' }" @click.stop="toggleMenu('help')">Help</button>
        <div v-if="menu === 'help'" class="menu menu-d" role="menu" aria-label="Help">
          <button type="button" class="menu-i" role="menuitem" @click="run(() => dialog = 'about')"><img src="/icons/help.svg" alt="" /> About kvm-gui</button>
        </div>
      </div>
    </nav>

    <div class="toolbar" role="toolbar" aria-label="Machine tools">
      <button type="button" class="tb" aria-label="New" title="Create a new VM (Ctrl+N)" @click="dialog = 'new'"><img src="/icons/new.svg" alt="" /><span>New</span></button>
      <button type="button" class="tb" aria-label="Add" title="Add an existing VM: a settings file or a reims-ctl VM" @click="addVM"><img src="/icons/import.svg" alt="" /><span>Add</span></button>
      <span class="tb-sep" role="separator"></span>
      <button type="button" class="tb" aria-label="Settings" title="Settings of the selected VM (Ctrl+S)" :disabled="!selected" @click="openSettings('general')"><img src="/icons/settings.svg" alt="" /><span>Settings</span></button>
      <button type="button" class="tb" aria-label="Remove" title="Remove the selected VM from the list, optionally with its files" :disabled="!selected || selected.running" @click="removeVM"><img src="/icons/remove.svg" alt="" /><span>Remove…</span></button>
      <span class="tb-sep" role="separator"></span>
      <div class="start">
        <button type="button" class="tb" aria-label="Start" title="Start the selected VM; it keeps running when you close this window" :disabled="!canStart" @click="start(false)"><img src="/icons/start.svg" alt="" /><span>{{ starting ? "Starting…" : "Start" }}</span></button>
        <button ref="startArrow" type="button" class="tb tb-arrow" aria-label="Start options" aria-haspopup="menu" :aria-expanded="startOpen" :disabled="!selected" @click.stop="toggleStart"></button>
      </div>
      <button type="button" class="tb" aria-label="Show" title="Open the window of the running VM" :disabled="!selected?.running" @click="show"><img src="/icons/window.svg" alt="" /><span>Show</span></button>
      <button type="button" class="tb" aria-label="Shut down" title="Send the shutdown signal (ACPI)" :disabled="!selected?.running" @click="shutDown"><img src="/icons/shutdown.svg" alt="" /><span>Shut down</span></button>
      <button type="button" class="tb" aria-label="Power off" title="Stop the VM at once, like pulling the plug: for a guest that ignores the shutdown signal" :disabled="!selected?.running" @click="powerOff"><img src="/icons/forcestop.svg" alt="" /><span>Power off</span></button>
    </div>

    <p v-if="error" role="alert" class="note bad err">{{ error }}</p>

    <div v-if="loaded && !vms.length" class="welcome" role="region" aria-label="Welcome">
      <img src="/icons/window.svg" alt="" class="welcome-ico" />
      <h1>Welcome to kvm-gui</h1>
      <p class="muted">Create a virtual machine for macOS, Linux, Windows or another system, or add one you already have from its settings file or from reims-ctl.</p>
      <div class="row">
        <button type="button" class="btn primary" @click="dialog = 'new'"><img src="/icons/new.svg" alt="" /> New…</button>
        <button type="button" class="btn" @click="addVM"><img src="/icons/import.svg" alt="" /> Add…</button>
      </div>
    </div>

    <div v-else class="body">
      <ul ref="list" class="vmlist" role="listbox" aria-label="Virtual machines" tabindex="0"
        :aria-activedescendant="selectedId ? 'vm-' + selectedId : undefined" @keydown="listKey">
        <li v-for="vm in vms" :id="'vm-' + vm.id" :key="vm.id" role="option" class="vm" :class="{ on: vm.id === selectedId }"
          :aria-selected="vm.id === selectedId" :aria-label="`${vm.name}, ${vm.os}, ${vm.state}`"
          @click="select(vm.id)" @dblclick="openSettings('general')">
          <GuestIcon :guest="vm.guest" :size="34" />
          <div class="vm-t">
            <div class="vm-name"><b>{{ vm.name }}</b> <span class="vm-os">({{ vm.os }})</span></div>
            <div class="vm-state"><img :src="`./icons/${vm.running ? 'state-running' : 'state-off'}.svg`" alt="" /> {{ starting === vm.id ? "Starting…" : vm.state }}</div>
            <div class="vm-sum">{{ (vm.memoryMiB / 1024).toFixed(vm.memoryMiB % 1024 ? 1 : 0) }} GiB · {{ vm.cpus }} CPUs · {{ vm.graphics === 'reims-vgpu' ? 'Reims vGPU' : vm.guest === 'macos' ? 'VMware SVGA' : vm.graphics === 'virtio-vga' ? 'virtio-vga' : 'VGA' }}</div>
          </div>
        </li>
      </ul>

      <section class="tools" aria-label="Selected VM">
        <TabBar v-model="tool" :tabs="TOOLS" size="small" panel="tool-page" class="tools-tabs" aria-label="Tools" />
        <div id="tool-page" class="tool-page tab-page" role="tabpanel" :aria-labelledby="`tool-page-tab-${tool}`">
          <template v-if="tool === 'details'">
            <Details v-if="shownInfo" :vm="shownInfo" @open="openSettings" />
            <p v-else-if="selected" class="muted pad" role="status">Loading {{ selected.name }}…</p>
          </template>

          <div v-else-if="tool === 'screen'" class="screen-tab">
            <VMScreen v-if="selected?.running && (reimsGPU || selected.screen)" :key="selected.id"
              :id="selected.id" :name="selected.name" host="manager" :reims="reimsGPU" :covered="!!dialog || !!menu || startOpen || !!askState" />
            <p v-else-if="selected?.running" class="muted pad">The screen of {{ selected.name }} is in QEMU's own window (Settings › Display › Screen).</p>
            <p v-else-if="selected" class="muted pad">{{ selected.name }} is powered off: Start shows its screen here.</p>
          </div>

          <div v-else-if="tool === 'snapshots'" class="snap">
            <div class="row snap-bar" role="toolbar" aria-label="Snapshot actions">
              <button type="button" class="btn small" disabled><img src="/icons/snapshot.svg" alt="" /> Take…</button>
              <button type="button" class="btn small" disabled>Restore</button>
              <button type="button" class="btn small" disabled>Delete</button>
              <button type="button" class="btn small" disabled>Properties</button>
            </div>
            <ul class="tree" role="tree" aria-label="Snapshots">
              <li role="treeitem" aria-selected="true" class="tree-i on" tabindex="0"><img src="/icons/state-running.svg" alt="" /> Current state</li>
            </ul>
            <p class="muted pad">{{ shownInfo?.config.backend === "reims-ctl"
              ? "VMs that reims-ctl keeps cannot have snapshots in the app yet."
              : "Snapshots are not available yet. Copy the VM folder while the VM is powered off to keep a version of it." }}</p>
          </div>

          <div v-else-if="tool === 'logs'" class="logs">
            <div class="row snap-bar"><button type="button" class="btn small" @click="loadLog">Refresh</button>
              <span class="muted">The QEMU output of the current or last run.</span></div>
            <pre class="log mono" tabindex="0" aria-label="Log">{{ log || "No run log for this VM yet." }}</pre>
          </div>
        </div>
      </section>
    </div>

    <footer class="status" role="status" aria-live="polite">
      <span v-if="selected"><img :src="`./icons/${selected.running ? 'state-running' : 'state-off'}.svg`" alt="" /> {{ selected.name }}: {{ selected.state }}</span>
      <span class="grow muted">{{ status }}</span>
      <span class="muted">{{ vms.length }} VMs, {{ running }} running</span>
      <span class="muted">kvm-gui {{ version }}</span>
    </footer>
  </div>

  <div v-if="startOpen" ref="startPop" class="menu menu-d start-d" role="menu" aria-label="Start options" tabindex="-1"
    :style="{ left: startPos.left + 'px', top: startPos.top + 'px' }" @keydown="startKey">
    <button type="button" class="menu-i" role="menuitem" :disabled="!canStart" @click="start(false)"><img src="/icons/window.svg" alt="" /> Normal start</button>
    <button type="button" class="menu-i" role="menuitem" :disabled="!canStart || reimsGPU" @click="start(true)"><span class="ico"></span> Headless start</button>
    <p class="menu-note">{{ reimsGPU ? headlessNote : "Headless runs the VM without a window." }} The VM keeps running when you close this window.</p>
  </div>

  <NewVMWizard v-if="dialog === 'new'" :names="vms.map((v) => v.name)" @close="dialog = ''" @created="created" />
  <AddVMDialog v-if="dialog === 'add'" @close="dialog = ''" @added="added" />
  <VMSettings v-if="dialog === 'settings' && editing" :vm="editing" :page="settingsPage" @close="dialog = ''; editing = null" @saved="saved" />
  <Preferences v-if="dialog === 'prefs'" @close="dialog = ''" />
  <MediaManager v-if="dialog === 'media'" @close="dialog = ''" />
  <Modal v-if="dialog === 'remove' && removing" title="Remove VM" :width="460" @close="dialog = ''; removing = null">
    <div class="pad col">
      <p>Remove <b>{{ removing.name }}</b> from the list?</p>
      <label class="chk"><input v-model="purge" type="checkbox" /> Also delete the files the app created for it (disk, install media, boot files)</label>
      <p class="muted">Other files in its folder are never deleted. Deleted files cannot be recovered.</p>
    </div>
    <template #footer>
      <button type="button" class="btn" :class="purge ? 'danger' : 'primary'" @click="confirmRemove">{{ purge ? "Delete files and remove" : "Remove" }}</button>
      <button type="button" class="btn" autofocus @click="dialog = ''; removing = null">Cancel</button>
    </template>
  </Modal>
  <Modal v-if="dialog === 'about'" title="About kvm-gui" :width="420" @close="dialog = ''">
    <div class="pad col">
      <p><b>kvm-gui</b> {{ version }}</p>
      <p class="muted">Create and run macOS, Linux, Windows and other VMs on Linux KVM. BSD-3-Clause; the licenses of the parts it ships are printed by <span class="mono">kvm-gui licenses</span>.</p>
    </div>
    <template #footer><button type="button" class="btn primary" @click="dialog = ''">OK</button></template>
  </Modal>
  <AskDialog />
</template>

<style scoped>
.mgr { height: 100%; display: flex; flex-direction: column; min-width: 0; }
.menubar { flex: none; display: flex; height: 24px; background: var(--toolbar); user-select: none; }
.menu-w { position: relative; }
.menu-t { height: 24px; padding: 0 9px; font: inherit; font-size: 12px; color: var(--text); background: transparent; border: none; cursor: pointer; }
.menu-t:hover, .menu-t.on { background: var(--hover); }
.menu-d { position: absolute; top: 100%; left: 0; z-index: 70; min-width: 220px; display: flex; flex-direction: column; padding: 2px; background: var(--panel); border: 1px solid var(--border); box-shadow: 0 4px 12px color-mix(in srgb, var(--border) 80%, transparent); }
.menu-i { display: flex; align-items: center; gap: 8px; height: 26px; padding: 0 8px; font: inherit; font-size: 12px; color: var(--text); background: transparent; border: none; text-align: left; white-space: nowrap; cursor: pointer; }
.menu-i:hover:not(:disabled), .menu-i:focus-visible { background: var(--selection); color: var(--selection-text); outline: none; }
.menu-i:disabled { color: var(--text-disabled); cursor: default; }
.menu-i:disabled img { opacity: 0.45; }
.menu-i img, .ico { width: 16px; height: 16px; flex: none; }
.menu-i kbd { margin-left: auto; padding-left: 24px; font: inherit; color: inherit; opacity: 0.7; }
.menu-sep { height: 1px; margin: 2px 0; background: var(--border-subtle); }
.menu-note { max-width: 260px; margin: 2px 8px 4px; font-size: 11px; color: var(--text-muted); white-space: normal; }

.toolbar { flex: none; display: flex; align-items: stretch; gap: 2px; padding: 3px 6px; background: var(--toolbar); border-bottom: 1px solid var(--border-subtle); overflow-x: auto; }
.tb { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px; min-width: 58px; padding: 3px 6px; font: inherit; font-size: 12px; color: var(--text); background: transparent; border: 1px solid transparent; cursor: pointer; white-space: nowrap; border-radius: var(--ctl-radius); }
.tb img { width: 28px; height: 28px; }
.tb:hover:not(:disabled) { background: var(--hover); border-color: var(--border-subtle); }
.tb:active:not(:disabled) { background: var(--pressed); }
.tb:disabled { color: var(--text-disabled); cursor: default; }
.tb:disabled img { opacity: 0.4; filter: grayscale(1); }
.tb:focus-visible { outline-offset: -2px; }
.tb-sep { width: 1px; margin: 6px 4px; background: var(--border-subtle); }
.start { display: flex; }
.tb-arrow { min-width: 16px; padding: 0 3px; position: relative; }
.tb-arrow::after { content: ""; border: 4px solid transparent; border-top: 5px solid currentColor; margin-top: 6px; }
.start-d { position: fixed; outline: none; }

.err { margin: 6px; }
.body { flex: 1; min-height: 0; display: flex; }
.vmlist { flex: none; width: clamp(240px, 27%, 380px); margin: 0; padding: 0; list-style: none; overflow-y: auto; background: var(--surface); border-right: 1px solid var(--border-subtle); outline: none; }
.vmlist:focus-visible { outline: 2px solid var(--focus); outline-offset: -2px; }
.vm { display: flex; align-items: center; gap: 10px; padding: 7px 10px; border-bottom: 1px solid var(--border-subtle); cursor: pointer; }
.vm:hover:not(.on) { background: var(--hover); }
.vm.on { background: var(--selection); color: var(--selection-text); }
/* full selection colour: the secondary lines keep 4.5:1 on the selection */
.vm.on .vm-os, .vm.on .vm-sum { color: inherit; }
.vm-t { min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.vm-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.vm-os { color: var(--text-muted); }
.vm-state { display: flex; align-items: center; gap: 4px; font-size: 12px; }
.vm-state img { width: 12px; height: 12px; }
.vm-sum { font-size: 11px; color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tools { flex: 1; min-width: 0; display: flex; flex-direction: column; padding: 6px 6px 0; background: var(--panel); }
.tool-page { flex: 1; min-height: 0; overflow-y: auto; }
.snap, .logs { display: flex; flex-direction: column; height: 100%; }
.snap-bar { padding: 6px 8px; border-bottom: 1px solid var(--border-subtle); }
.tree { list-style: none; margin: 0; padding: 6px; }
.tree-i { display: flex; align-items: center; gap: 6px; padding: 3px 6px; }
.tree-i.on { background: var(--selection); color: var(--selection-text); }
.tree-i img { width: 14px; height: 14px; }
.pad { padding: 10px 12px; }
.col { display: flex; flex-direction: column; gap: 8px; }
.col p { margin: 0; }
.log { flex: 1; min-height: 0; margin: 0; padding: 8px; overflow: auto; background: var(--surface); white-space: pre-wrap; font-size: 12px; }
.welcome { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; padding: 24px; text-align: center; }
.welcome h1 { margin: 0; font-size: 20px; font-weight: 600; }
.welcome p { max-width: 520px; }
.welcome-ico { width: 72px; height: 72px; }
.status { flex: none; display: flex; align-items: center; gap: 16px; height: 24px; padding: 0 10px; font-size: 12px; background: var(--toolbar); border-top: 1px solid var(--border-subtle); white-space: nowrap; overflow: hidden; }
.status > span { display: inline-flex; align-items: center; gap: 4px; }
.status img { width: 10px; height: 10px; }
.status .grow { overflow: hidden; text-overflow: ellipsis; }
.screen-tab { height: 100%; display: flex; background: #000; }
</style>
