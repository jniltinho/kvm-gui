<script setup lang="ts" generic="T">
// Themed combo box (from goXterm, MIT, same author): a bordered box with a ▼ and a flat popup list drawn with the
// skin's tokens. Replaces <select>, whose WebKitGTK popup is a native GTK menu that ignores the skin.
// WAI-ARIA select-only combobox: the box is the combobox (its aria-label names it, its text is the value), the focus
// stays on it while the list is open and aria-activedescendant points at the active option.
// Closed: ↑/↓/Home/End/letters change the value as a native select does; Enter, Space or Alt+↓ open the list.
// Open: ↑/↓/Home/End/letters move, Enter or Space choose, Escape closes the list only (never the dialog), Tab closes.
import { computed, nextTick, onBeforeUnmount, ref, useId } from "vue";

/** [value, label, extra]: extra.font previews the label in that family; extra.sep draws a line above. */
export type Option<V = unknown> = readonly [V, string, { font?: string; sep?: boolean }?];

defineOptions({ inheritAttrs: false }); // two roots (box + teleported list): attrs go to the box
const props = defineProps<{ modelValue: T; options: readonly Option<T>[]; disabled?: boolean; width?: number }>();
const emit = defineEmits<{ "update:modelValue": [v: T] }>();

const id = useId();
const open = ref(false);
const active = ref(-1);
const box = ref<HTMLDivElement>();
const pop = ref<HTMLDivElement>();
const pos = ref({ left: 0, top: 0, width: 0, maxHeight: 300 });

const index = computed(() => props.options.findIndex((o) => o[0] === props.modelValue));
const text = computed(() => props.options[index.value]?.[1] ?? String(props.modelValue ?? ""));
const optId = (i: number) => `${id}-o${i}`;

function outside(e: MouseEvent) {
  if (!box.value?.contains(e.target as Node) && !pop.value?.contains(e.target as Node)) close();
}
function close() {
  open.value = false;
  document.removeEventListener("mousedown", outside, true);
}
async function show() {
  if (props.disabled) return;
  const r = box.value!.getBoundingClientRect();
  const below = window.innerHeight - r.bottom - 8;
  const up = below < 160 && r.top > below;
  const maxHeight = Math.min(300, up ? r.top - 8 : below); // egui combo_height
  pos.value = { left: r.left, top: up ? r.top - maxHeight - 1 : r.bottom + 1, width: r.width, maxHeight };
  active.value = Math.max(0, index.value);
  open.value = true;
  document.addEventListener("mousedown", outside, true);
  await nextTick();
  if (up && pop.value) pos.value.top = r.top - pop.value.offsetHeight - 1;
  reveal();
}
function reveal() {
  nextTick(() => document.getElementById(optId(active.value))?.scrollIntoView({ block: "nearest" }));
}
function pick(i: number) {
  if (i >= 0 && i !== index.value) emit("update:modelValue", props.options[i][0]);
  close();
  box.value?.focus();
}
// The next option whose label starts with the typed letter, after `from` and wrapping.
function typed(k: string, from: number) {
  if (k.length !== 1 || !/\S/.test(k)) return undefined;
  const n = props.options.length;
  for (let s = 1; s <= n; s++) {
    const i = (from + s) % n;
    if (props.options[i][1].toLowerCase().startsWith(k.toLowerCase())) return i;
  }
  return undefined;
}
function key(e: KeyboardEvent) {
  if (props.disabled || e.ctrlKey || e.metaKey) return;
  const n = props.options.length;
  const cur = open.value ? active.value : index.value;
  if (!open.value && (e.key === "Enter" || e.key === " " || (e.altKey && e.key === "ArrowDown"))) {
    e.preventDefault();
    void show();
    return;
  }
  if (open.value && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); pick(active.value); return; }
  if (open.value && e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(); return; }
  if (open.value && e.key === "Tab") { close(); return; }
  const next = ({ ArrowDown: Math.min(n - 1, cur + 1), ArrowUp: Math.max(0, cur - 1), Home: 0, End: n - 1 } as Record<string, number>)[e.key]
    ?? typed(e.key, cur);
  if (next === undefined || !n) return;
  e.preventDefault();
  if (open.value) { active.value = next; reveal(); }
  else if (next !== index.value) emit("update:modelValue", props.options[next][0]);
}
onBeforeUnmount(close);
</script>

<template>
  <div ref="box" v-bind="$attrs" class="dd" :class="{ open, off: disabled }" role="combobox" aria-haspopup="listbox"
    :aria-expanded="open" :aria-controls="id" :aria-activedescendant="open && active >= 0 ? optId(active) : undefined"
    :aria-disabled="disabled || undefined" :tabindex="disabled ? -1 : 0" :style="width ? { width: width + 'px' } : undefined"
    @click="open ? close() : show()" @keydown="key">
    <span class="dd-text">{{ text }}</span>
  </div>
  <Teleport to="body">
    <div v-show="open" :id="id" ref="pop" class="dd-pop" role="listbox" :aria-label="($attrs['aria-label'] as string) || undefined"
      :style="{ left: pos.left + 'px', top: pos.top + 'px', minWidth: pos.width + 'px', maxHeight: pos.maxHeight + 'px' }">
      <template v-if="open">
        <template v-for="(o, i) in options" :key="i">
          <div v-if="o[2]?.sep" class="dd-sep" role="separator"></div>
          <div :id="optId(i)" class="dd-item" :class="{ on: i === index, act: i === active }" role="option" :aria-selected="i === index"
            :style="o[2]?.font ? { fontFamily: `'${o[2].font}'` } : undefined" @mousedown.prevent @mousemove="active = i" @click="pick(i)">{{ o[1] }}</div>
        </template>
      </template>
    </div>
  </Teleport>
</template>

<style scoped>
.dd { position: relative; display: inline-flex; align-items: center; height: 24px; min-width: 60px; padding: 0 22px 0 6px; font-size: 12px; color: var(--text); background: var(--panel); border: 1px solid var(--ctl-border); border-radius: var(--ctl-radius); cursor: pointer; text-align: left; user-select: none; box-sizing: border-box; }
.dd::after { content: ""; position: absolute; right: 7px; top: 50%; margin-top: -3px; border: 5px solid transparent; border-top: 6px solid var(--text); border-bottom: 0; }
.dd:hover:not(.off) { background: var(--hover); border-color: var(--outline); }
.dd.open { background: var(--pressed); }
.dd:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
.dd.off { color: var(--text-disabled); border-color: var(--border); cursor: default; }
.dd.off::after { border-top-color: var(--text-disabled); }
.dd-text { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
</style>

<style>
/* Teleported to <body>, so unscoped. */
.dd-pop { position: fixed; z-index: 200; overflow-y: auto; padding: 2px; background: var(--panel); border: 1px solid var(--border); box-shadow: 0 4px 12px color-mix(in srgb, var(--border) 80%, transparent); font-size: 12px; }
.dd-item { display: flex; align-items: center; height: 26px; padding: 0 6px; font-size: 12px; color: var(--text); white-space: nowrap; cursor: pointer; }
.dd-item.act { background: var(--hover); outline: 1px solid var(--focus); outline-offset: -1px; }
.dd-item.on { background: var(--selection); color: var(--selection-text); }
.dd-sep { height: 1px; margin: 2px 0; background: var(--border-subtle); }
</style>
