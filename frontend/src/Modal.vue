<script setup lang="ts">
// The one dialog frame of the app (goXterm's Settings frame): title bar with a close button, a body that
// scrolls, an optional footer. Escape closes it (not while a Dropdown list is open), Tab stays inside it,
// and the first control gets the focus.
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";

const props = defineProps<{ title: string; width?: number; height?: number }>();
const emit = defineEmits<{ close: [] }>();
const box = ref<HTMLDivElement>();
const before = document.activeElement as HTMLElement | null;

const focusables = () =>
  [...(box.value?.querySelectorAll<HTMLElement>("button, input, select, textarea, [tabindex]:not([tabindex='-1'])") ?? [])]
    .filter((e) => !(e as HTMLButtonElement).disabled && e.offsetParent !== null);

function key(e: KeyboardEvent) {
  if (e.key === "Escape" && !document.querySelector(".dd-pop")) {
    e.stopPropagation();
    emit("close");
  } else if (e.key === "Tab") {
    const f = focusables();
    if (!f.length) return;
    const i = f.indexOf(document.activeElement as HTMLElement);
    if (e.shiftKey && i <= 0) { e.preventDefault(); f[f.length - 1].focus(); }
    else if (!e.shiftKey && i === f.length - 1) { e.preventDefault(); f[0].focus(); }
  }
}
onMounted(async () => {
  await nextTick();
  (box.value?.querySelector<HTMLElement>("[autofocus]") ?? focusables().find((e) => !e.classList.contains("dlg-x")))?.focus();
});
onBeforeUnmount(() => before?.focus?.());
</script>

<template>
  <div class="dlg-backdrop" @keydown="key">
    <div ref="box" class="dialog dlg" role="dialog" aria-modal="true" :aria-label="props.title"
      :style="{ width: (props.width ?? 640) + 'px', height: props.height ? props.height + 'px' : undefined }">
      <div class="dlg-title"><span>{{ props.title }}</span>
        <button type="button" class="dlg-x" aria-label="Close" title="Close" @click="emit('close')"></button></div>
      <div class="dlg-body"><slot /></div>
      <div v-if="$slots.footer" class="dlg-foot"><slot name="footer" /></div>
    </div>
  </div>
</template>

<style>
.dlg-backdrop { position: fixed; inset: 0; z-index: 60; display: flex; align-items: center; justify-content: center; }
.dlg { display: flex; flex-direction: column; max-width: calc(100% - 24px); max-height: calc(100% - 24px); background: var(--panel); border: 1px solid var(--border); color: var(--text); font-size: 12px; box-shadow: 0 6px 24px color-mix(in srgb, var(--border) 80%, transparent); }
.dlg-title { height: 28px; flex: none; display: flex; align-items: center; justify-content: space-between; padding: 0 4px 0 10px; border-bottom: 1px solid var(--border-subtle); background: var(--title-bg); color: var(--title-fg); font-weight: var(--title-weight); }
.dlg-x { position: relative; width: 22px; height: 22px; background: transparent; border: none; cursor: pointer; }
.dlg-x:hover { background: var(--hover); }
.dlg-x::before, .dlg-x::after { content: ""; position: absolute; left: 5px; right: 5px; top: 10px; height: 1.5px; background: var(--title-fg); transform: rotate(45deg); }
.dlg-x::after { transform: rotate(-45deg); }
.dlg-body { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
.dlg-foot { flex: none; display: flex; justify-content: flex-end; align-items: center; gap: 8px; padding: 8px 10px; border-top: 1px solid var(--border-subtle); }
</style>
