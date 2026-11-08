<script setup lang="ts" generic="T extends string">
// The one tab pattern of the app's dialogs (Settings, Preferences, Manager tools): icon tabs joined to the
// page below, 2 px apart, no accent line on the active tab (bold label on the page colour instead),
// keyboard access as a WAI-ARIA tablist (←/→/Home/End move and select), a visible focus ring.
// The page under it uses the `.tab-page` class (border + joining edge).
import { ref } from "vue";

export type TabItem<K extends string = string> = readonly [K, string, string]; // [id, label, icon]

const props = defineProps<{ tabs: readonly TabItem<T>[]; modelValue: T; size?: "large" | "small" }>();
const emit = defineEmits<{ "update:modelValue": [id: T] }>();
const bar = ref<HTMLDivElement>();
const icon = (n: string) => `./icons/${n}.svg`;

function key(e: KeyboardEvent) {
  const i = props.tabs.findIndex((t) => t[0] === props.modelValue);
  const n = props.tabs.length;
  const next = { ArrowRight: (i + 1) % n, ArrowLeft: (i - 1 + n) % n, Home: 0, End: n - 1 }[e.key];
  if (next === undefined) return;
  e.preventDefault();
  emit("update:modelValue", props.tabs[next][0]);
  bar.value?.querySelectorAll<HTMLButtonElement>("[role=tab]")[next]?.focus();
}
</script>

<template>
  <div ref="bar" class="tabbar" :class="size ?? 'large'" role="tablist" @keydown="key">
    <button
      v-for="[id, label, ic] in tabs"
      :key="id"
      type="button"
      role="tab"
      class="tabbar-tab"
      :class="{ on: id === modelValue }"
      :aria-selected="id === modelValue"
      :tabindex="id === modelValue ? 0 : -1"
      @click="emit('update:modelValue', id)"
    >
      <img :src="icon(ic)" alt="" /><span :data-label="label">{{ label }}</span>
    </button>
  </div>
</template>

<style>
/* Unscoped: the page class below is used by the host dialogs. */
.tabbar { display: flex; flex: none; align-items: flex-end; gap: 2px; position: relative; z-index: 1; padding: 0 2px; }
.tabbar-tab {
  display: flex; align-items: center; gap: 6px; padding: 0 9px; font-size: 12px; color: var(--text-muted);
  background: var(--tab-inactive); border: 1px solid var(--border-subtle); border-bottom-color: var(--border);
  border-radius: var(--ctl-radius) var(--ctl-radius) 0 0; cursor: pointer; white-space: nowrap;
  transition: background-color 0.12s ease, color 0.12s ease;
}
.tabbar.large .tabbar-tab { height: 34px; }
.tabbar.large .tabbar-tab img { width: 22px; height: 22px; }
.tabbar.small .tabbar-tab { height: 28px; }
.tabbar.small .tabbar-tab img { width: 16px; height: 16px; }
.tabbar-tab:hover:not(.on) { color: var(--text); background: var(--hover); }
.tabbar-tab.on {
  color: var(--text); font-weight: 600; background: var(--panel);
  border-color: var(--border); border-bottom-color: var(--panel);
}
.tabbar.large .tabbar-tab.on { height: 36px; }
.tabbar.small .tabbar-tab.on { height: 30px; }
/* the bold active label reserves its width on every tab, so selecting a tab never shifts the row */
.tabbar-tab span { display: inline-flex; flex-direction: column; }
.tabbar-tab span::after { content: attr(data-label); font-weight: 600; height: 0; visibility: hidden; overflow: hidden; }
.tabbar-tab:focus-visible { outline: 2px solid var(--focus); outline-offset: -3px; }
.tab-page { margin-top: -1px; background: var(--panel); border: 1px solid var(--border); }
@media (prefers-reduced-motion: reduce) { .tabbar-tab { transition: none; } }
</style>
