<script setup lang="ts">
// The Manager window. This is the scaffold (task 2.5): the VM list from the Go core; the
// VirtualBox-parity screens come in group 7 of migrate-gui-to-go-wails.
import { onMounted, ref } from "vue";
import { Ready, Theme, Version, VMs, type VM } from "./api";
import { applySkin } from "./themes";

const vms = ref<VM[]>([]);
const version = ref("");
const error = ref("");

onMounted(async () => {
  try {
    applySkin(await Theme());
    version.value = await Version();
    vms.value = await VMs();
    await Ready();
  } catch (e) {
    error.value = String(e);
  }
});
</script>

<template>
  <main class="flex h-full flex-col" aria-label="kvm-gui Manager">
    <header class="border-b px-3 py-2" style="background: var(--toolbar); border-color: var(--border-subtle)">
      <h1 class="text-base font-bold">kvm-gui</h1>
      <span class="text-xs" style="color: var(--text-muted)">{{ version }}</span>
    </header>
    <p v-if="error" role="alert" class="p-3" style="color: var(--danger)">{{ error }}</p>
    <ul class="flex-1 overflow-auto" role="listbox" aria-label="Virtual machines">
      <li v-for="vm in vms" :key="vm.id" role="option" :aria-label="vm.name" class="border-b px-3 py-2"
          style="border-color: var(--border-subtle)">
        <div class="font-bold">{{ vm.name }}</div>
        <div class="text-xs" style="color: var(--text-muted)">{{ vm.state }} · {{ vm.memoryMiB }} MB · {{ vm.cpus }} CPUs · {{ vm.graphics }}</div>
      </li>
      <li v-if="!vms.length && !error" class="p-3" style="color: var(--text-muted)">No virtual machines yet.</li>
    </ul>
  </main>
</template>
