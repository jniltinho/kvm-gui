<script setup lang="ts">
// The Manager window (spec vm-manager-ui › Manager window): File / Machine / Help menus, the toolbar,
// the VM list, the welcome pane, and the Details / Snapshots / Logs tools of the selected VM.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Application } from "@wailsio/runtime";
import {
  AddVM, DeleteVM, GetLog, GetPrefs, GetVM, PickFile, Ready, SelectVM, ShutDown, Theme, Version, VMs,
  type VM, type VMInfo,
} from "./api";
import { applySkin } from "./themes";
import { askConfirm, showInfo } from "./ask";
import AskDialog from "./AskDialog.vue";
import Details from "./Details.vue";
import GuestIcon from "./GuestIcon.vue";
import MediaManager from "./MediaManager.vue";
import Modal from "./Modal.vue";
import NewVMWizard from "./NewVMWizard.vue";
import Preferences from "./Preferences.vue";
import TabBar from "./TabBar.vue";
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

// Dialogs: one at a time.
const dialog = ref<"" | "new" | "settings" | "prefs" | "media" | "about" | "remove">("");
const settingsPage = ref("general");
const purge = ref(false);

// Tools of the selected VM.
const TOOLS = [["details", "Details", "general"], ["snapshots", "Snapshots", "snapshot"], ["logs", "Logs", "log"]] as const;
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

async function loadInfo() {
  if (!selectedId.value) { info.value = null; return; }
  try { info.value = await GetVM(selectedId.value); } catch (e) { error.value = String(e); }
  if (tool.value === "logs") await loadLog();
}
async function loadLog() {
  if (!selectedId.value) return;
  try { log.value = await GetLog(selectedId.value); } catch (e) { log.value = String(e); }
}

async function select(id: string) {
  if (id === selectedId.value) return;
  selectedId.value = id;
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
  if (e.key === "Enter" && selected.value) { openSettings("general"); return; }
  if (next === undefined || !n) return;
  e.preventDefault();
  void select(vms.value[next].id);
  nextTick(() => list.value?.querySelector<HTMLElement>(".vm.on")?.scrollIntoView({ block: "nearest" }));
}

// Actions.
function openSettings(page: string) {
  if (!selected.value) return;
  settingsPage.value = page;
  dialog.value = "settings";
}
async function addVM() {
  menu.value = "";
  try {
    const path = await PickFile("Add a VM from its settings file (vm.json or an export)", "json");
    if (!path) return;
    const r = await AddVM(path);
    if (r.error) return void showInfo("Add VM", r.error);
    await refresh();
    if (r.id) await select(r.id);
    status.value = r.notice || "VM added.";
  } catch (e) { void showInfo("Add VM", String(e)); }
}
function removeVM() {
  if (!selected.value) return;
  purge.value = false;
  dialog.value = "remove";
}
async function confirmRemove() {
  const vm = selected.value;
  dialog.value = "";
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
  await loadInfo();
  await refresh();
  status.value = "Settings saved.";
}

// Start: TODO(task 4.3, 6.3): detached `vm start --gui-run` and the VM window are built in the core;
// until then Start stays disabled and says so (core.VM.Start runs QEMU inside this process).
const startNote = "Starting from the window arrives with detached runs (task 4.3) and the VM window (task 6.3). Use `kvm-gui vm start` meanwhile.";
const headlessNote = computed(() => selected.value?.graphics === "reims-vgpu"
  ? "Reims vGPU always opens its own window, so Headless is not available." : startNote);

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
const startOpen = ref(false);
function closeMenus(e: MouseEvent) {
  const t = e.target as HTMLElement;
  if (!t.closest(".menubar")) menu.value = "";
  if (!t.closest(".start")) startOpen.value = false;
}

function shortcut(e: KeyboardEvent) {
  if (dialog.value || !e.ctrlKey || e.altKey) return;
  const k = e.key.toLowerCase();
  if (k === "n") { e.preventDefault(); dialog.value = "new"; }
  else if (k === "s" && selected.value) { e.preventDefault(); openSettings("general"); }
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
          <button type="button" class="menu-i" role="menuitem" disabled :title="startNote"><img src="/icons/start.svg" alt="" /> Start (Normal)</button>
          <button type="button" class="menu-i" role="menuitem" disabled :title="headlessNote"><img src="/icons/start.svg" alt="" /> Start (Headless)</button>
          <button type="button" class="menu-i" role="menuitem" disabled :title="startNote"><img src="/icons/window.svg" alt="" /> Show</button>
          <button type="button" class="menu-i" role="menuitem" :disabled="!selected?.running" @click="run(shutDown)"><img src="/icons/shutdown.svg" alt="" /> Shut down</button>
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
      <button type="button" class="tb" aria-label="Add" title="Add an existing VM from its settings file" @click="addVM"><img src="/icons/import.svg" alt="" /><span>Add</span></button>
      <span class="tb-sep" role="separator"></span>
      <button type="button" class="tb" aria-label="Settings" title="Settings of the selected VM (Ctrl+S)" :disabled="!selected" @click="openSettings('general')"><img src="/icons/settings.svg" alt="" /><span>Settings</span></button>
      <button type="button" class="tb" aria-label="Discard" title="Remove the selected VM, optionally with its files" :disabled="!selected || selected.running" @click="removeVM"><img src="/icons/remove.svg" alt="" /><span>Discard</span></button>
      <span class="tb-sep" role="separator"></span>
      <div class="start">
        <button type="button" class="tb" aria-label="Start" :title="startNote" disabled><img src="/icons/start.svg" alt="" /><span>Start</span></button>
        <button type="button" class="tb tb-arrow" aria-label="Start options" aria-haspopup="menu" :aria-expanded="startOpen" :disabled="!selected" @click.stop="startOpen = !startOpen"></button>
        <div v-if="startOpen" class="menu menu-d start-d" role="menu" aria-label="Start options">
          <button type="button" class="menu-i" role="menuitem" disabled :title="startNote"><img src="/icons/window.svg" alt="" /> Normal start</button>
          <button type="button" class="menu-i" role="menuitem" disabled :title="headlessNote"><span class="ico"></span> Headless start</button>
          <p class="menu-note">{{ headlessNote }}</p>
        </div>
      </div>
      <button type="button" class="tb" aria-label="Show" :title="startNote" disabled><img src="/icons/window.svg" alt="" /><span>Show</span></button>
      <button type="button" class="tb" aria-label="Shut down" title="Send the shutdown signal (ACPI)" :disabled="!selected?.running" @click="shutDown"><img src="/icons/shutdown.svg" alt="" /><span>Shut down</span></button>
    </div>

    <p v-if="error" role="alert" class="note bad err">{{ error }}</p>

    <div v-if="loaded && !vms.length" class="welcome" role="region" aria-label="Welcome">
      <img src="/icons/window.svg" alt="" class="welcome-ico" />
      <h1>Welcome to kvm-gui</h1>
      <p class="muted">Create a virtual machine for macOS, Linux, Windows or another system, or add one you already have from its settings file.</p>
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
            <div class="vm-state"><img :src="`./icons/${vm.running ? 'state-running' : 'state-off'}.svg`" alt="" /> {{ vm.state }}</div>
            <div class="vm-sum">{{ (vm.memoryMiB / 1024).toFixed(vm.memoryMiB % 1024 ? 1 : 0) }} GiB · {{ vm.cpus }} CPUs · {{ vm.graphics === 'reims-vgpu' ? 'Reims vGPU' : vm.guest === 'macos' ? 'VMware SVGA' : vm.graphics === 'virtio-vga' ? 'virtio-vga' : 'VGA' }}</div>
          </div>
        </li>
      </ul>

      <section class="tools" aria-label="Selected VM">
        <TabBar v-model="tool" :tabs="TOOLS" size="small" class="tools-tabs" aria-label="Tools" />
        <div class="tool-page tab-page">
          <Details v-if="tool === 'details' && info" :vm="info" @open="openSettings" />

          <div v-else-if="tool === 'snapshots'" class="snap">
            <div class="row snap-bar" role="toolbar" aria-label="Snapshot actions">
              <!-- TODO(tasks 5.1, 5.5): take, restore and delete land with the core's snapshot journal. -->
              <button type="button" class="btn small" disabled title="Snapshots arrive with task 5.1"><img src="/icons/snapshot.svg" alt="" /> Take…</button>
              <button type="button" class="btn small" disabled title="Snapshots arrive with task 5.1">Restore</button>
              <button type="button" class="btn small" disabled title="Snapshots arrive with task 5.1">Delete</button>
              <button type="button" class="btn small" disabled title="Snapshots arrive with task 5.1">Properties</button>
            </div>
            <ul class="tree" role="tree" aria-label="Snapshots">
              <li role="treeitem" aria-selected="true" class="tree-i on" tabindex="0"><img src="/icons/state-running.svg" alt="" /> Current state</li>
            </ul>
            <p class="muted pad">Snapshots of powered-off VMs arrive with the core's snapshot support (task 5.1).</p>
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

  <NewVMWizard v-if="dialog === 'new'" :names="vms.map((v) => v.name)" @close="dialog = ''" @created="created" />
  <VMSettings v-if="dialog === 'settings' && info" :vm="info" :page="settingsPage" @close="dialog = ''" @saved="saved" />
  <Preferences v-if="dialog === 'prefs'" @close="dialog = ''" />
  <MediaManager v-if="dialog === 'media'" @close="dialog = ''" />
  <Modal v-if="dialog === 'remove' && selected" title="Remove VM" :width="460" @close="dialog = ''">
    <div class="pad col">
      <p>Remove <b>{{ selected.name }}</b> from the list?</p>
      <label class="chk"><input v-model="purge" type="checkbox" /> Also delete the files the app created for it (disk, install media, boot files)</label>
      <p class="muted">Other files in its folder are never deleted. Deleted files cannot be recovered.</p>
    </div>
    <template #footer>
      <button type="button" class="btn primary" :class="{ danger: purge }" @click="confirmRemove">{{ purge ? "Delete files and remove" : "Remove" }}</button>
      <button type="button" class="btn" autofocus @click="dialog = ''">Cancel</button>
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
.tb-sep { width: 1px; margin: 6px 4px; background: var(--border-subtle); }
.start { position: relative; display: flex; }
.tb-arrow { min-width: 16px; padding: 0 3px; position: relative; }
.tb-arrow::after { content: ""; border: 4px solid transparent; border-top: 5px solid currentColor; margin-top: 6px; }
.start-d { top: 100%; left: 0; }

.err { margin: 6px; }
.body { flex: 1; min-height: 0; display: flex; }
.vmlist { flex: none; width: clamp(240px, 27%, 380px); margin: 0; padding: 0; list-style: none; overflow-y: auto; background: var(--surface); border-right: 1px solid var(--border-subtle); outline: none; }
.vmlist:focus-visible { outline: 2px solid var(--focus); outline-offset: -2px; }
.vm { display: flex; align-items: center; gap: 10px; padding: 7px 10px; border-bottom: 1px solid var(--border-subtle); cursor: pointer; }
.vm:hover:not(.on) { background: var(--hover); }
.vm.on { background: var(--selection); color: var(--selection-text); }
.vm.on .vm-os, .vm.on .vm-sum { color: inherit; opacity: 0.85; }
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
.danger { color: var(--danger); border-color: var(--danger); }
</style>
