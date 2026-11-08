<script setup lang="ts" generic="T">
// Themed combo box (from goXterm, MIT, same author): a bordered button with a ▼ and a flat popup list drawn with the
// skin's tokens. Replaces <select>, whose WebKitGTK popup is a native GTK menu that ignores the skin.
import { computed, nextTick, onBeforeUnmount, ref } from "vue";

/** [value, label, extra]: extra.font previews the label in that family; extra.sep draws a line above. */
export type Option<V = unknown> = readonly [V, string, { font?: string; sep?: boolean }?];

defineOptions({ inheritAttrs: false }); // two roots (button + teleported list): attrs go to the button
const props = defineProps<{ modelValue: T; options: readonly Option<T>[]; disabled?: boolean; width?: number }>();
const emit = defineEmits<{ "update:modelValue": [v: T] }>();

const open = ref(false);
const btn = ref<HTMLButtonElement>();
const pop = ref<HTMLDivElement>();
const pos = ref({ left: 0, top: 0, width: 0, maxHeight: 300 });

const index = computed(() => props.options.findIndex((o) => o[0] === props.modelValue));
const text = computed(() => props.options[index.value]?.[1] ?? String(props.modelValue ?? ""));

function outside(e: MouseEvent) {
  if (!btn.value?.contains(e.target as Node) && !pop.value?.contains(e.target as Node)) close();
}
// Escape while the list is open closes only the list, never the dialog around it.
function esc(e: KeyboardEvent) {
  if (e.key === "Escape") { e.stopPropagation(); e.preventDefault(); close(); }
}
function close() {
  open.value = false;
  document.removeEventListener("mousedown", outside, true);
  document.removeEventListener("keydown", esc, true);
}
async function toggle() {
  if (open.value || props.disabled) return close();
  const r = btn.value!.getBoundingClientRect();
  const below = window.innerHeight - r.bottom - 8;
  const up = below < 160 && r.top > below;
  const maxHeight = Math.min(300, up ? r.top - 8 : below); // egui combo_height
  pos.value = { left: r.left, top: up ? r.top - maxHeight - 1 : r.bottom + 1, width: r.width, maxHeight };
  open.value = true;
  document.addEventListener("mousedown", outside, true);
  document.addEventListener("keydown", esc, true);
  await nextTick();
  if (up && pop.value) pos.value.top = r.top - pop.value.offsetHeight - 1;
  pop.value?.querySelector<HTMLElement>(".dd-item.on")?.scrollIntoView({ block: "nearest" });
}
function pick(v: T) {
  emit("update:modelValue", v);
  close();
  btn.value?.focus();
}
// Arrow keys step through the options like a native select; Escape closes the list only.
function key(e: KeyboardEvent) {
  const step = e.key === "ArrowDown" ? 1 : e.key === "ArrowUp" ? -1 : 0;
  if (!step || props.disabled) return;
  e.preventDefault();
  const i = Math.min(props.options.length - 1, Math.max(0, index.value + step));
  if (i !== index.value) emit("update:modelValue", props.options[i][0]);
}
onBeforeUnmount(close);
</script>

<template>
  <button ref="btn" v-bind="$attrs" type="button" class="dd" :class="{ open }" aria-haspopup="listbox" :aria-expanded="open" :style="width ? { width: width + 'px' } : undefined" :disabled="disabled" @click="toggle" @keydown="key">
    <span class="dd-text">{{ text }}</span>
  </button>
  <Teleport to="body">
    <div v-if="open" ref="pop" class="dd-pop" role="listbox"
      :style="{ left: pos.left + 'px', top: pos.top + 'px', minWidth: pos.width + 'px', maxHeight: pos.maxHeight + 'px' }">
      <template v-for="(o, i) in options" :key="i">
        <div v-if="o[2]?.sep" class="dd-sep"></div>
        <button type="button" class="dd-item" :class="{ on: i === index }" role="option" :aria-selected="i === index"
          :style="o[2]?.font ? { fontFamily: `'${o[2].font}'` } : undefined" @click="pick(o[0])">{{ o[1] }}</button>
      </template>
    </div>
  </Teleport>
</template>

<style scoped>
.dd { position: relative; display: inline-flex; align-items: center; height: 24px; min-width: 60px; padding: 0 22px 0 6px; font-size: 12px; color: var(--text); background: var(--panel); border: 1px solid var(--border); cursor: pointer; text-align: left; }
.dd::after { content: ""; position: absolute; right: 7px; top: 50%; margin-top: -3px; border: 5px solid transparent; border-top: 6px solid var(--text); border-bottom: 0; }
.dd:hover:not(:disabled) { background: var(--hover); border-color: var(--outline); }
.dd.open { background: var(--pressed); }
.dd:focus-visible { outline: 1px solid var(--focus); }
.dd:disabled { color: var(--text-disabled); cursor: default; }
.dd:disabled::after { border-top-color: var(--text-disabled); }
.dd-text { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
</style>

<style>
/* Teleported to <body>, so unscoped. */
.dd-pop { position: fixed; z-index: 200; overflow-y: auto; padding: 2px; background: var(--panel); border: 1px solid var(--border); box-shadow: 0 4px 12px color-mix(in srgb, var(--border) 80%, transparent); font-size: 12px; }
.dd-item { display: block; width: 100%; height: 26px; padding: 0 6px; font-size: 12px; color: var(--text); background: transparent; border: none; text-align: left; white-space: nowrap; cursor: pointer; }
.dd-item:hover { background: var(--hover); }
.dd-item.on { background: var(--selection); color: var(--selection-text); }
.dd-sep { height: 1px; margin: 2px 0; background: var(--border-subtle); }
</style>
