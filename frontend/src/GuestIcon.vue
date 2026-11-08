<script setup lang="ts">
// Guest type icons (original artwork, BSD-3-Clause): a screen tile with a mark per guest type, drawn with
// the skin's role tokens so every skin covers them (design D21). Decorative: the list row names the type.
defineProps<{ guest: string; size?: number }>();
</script>

<template>
  <svg class="gi" :width="size ?? 32" :height="size ?? 32" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
    <rect class="gi-frame" x="1.5" y="3.5" width="29" height="21" rx="2" />
    <rect class="gi-screen" x="4" y="6" width="24" height="16" />
    <path class="gi-frame" d="M12 25h8v2h3v2H9v-2h3z" />
    <!-- macOS: the command-key loop -->
    <path v-if="guest === 'macos'" class="gi-mark" fill="none" stroke-width="1.6"
      d="M13 11v6m6-6v6m-6-6h6m-6 6h6M13 11a2 2 0 1 0-2 2h2m6-2a2 2 0 1 1 2 2h-2m-6 4a2 2 0 1 1-2-2h2m6 2a2 2 0 1 0 2-2h-2" />
    <!-- Linux: a penguin -->
    <g v-else-if="guest === 'linux'">
      <ellipse class="gi-ink" cx="16" cy="15.5" rx="4.2" ry="5.6" />
      <ellipse class="gi-screen" cx="16" cy="17" rx="2.6" ry="3.6" />
      <circle class="gi-screen" cx="14.7" cy="12" r="0.9" /><circle class="gi-screen" cx="17.3" cy="12" r="0.9" />
      <path class="gi-warm" d="M14.6 13.4h2.8L16 15z" />
      <path class="gi-warm" d="M11.5 21h3.5v-1.2h-2.5zM20.5 21H17v-1.2h2.5z" />
    </g>
    <!-- Windows: an application window with its title bar -->
    <g v-else-if="guest === 'windows'">
      <rect class="gi-mark-fill" x="8.5" y="8" width="15" height="12" rx="1" />
      <rect class="gi-screen" x="10" y="11" width="12" height="7.5" />
      <rect class="gi-warm" x="19" y="9" width="3" height="1.4" />
    </g>
    <!-- Other: an optical disc -->
    <g v-else>
      <circle class="gi-ink" cx="16" cy="14" r="6" />
      <circle class="gi-screen" cx="16" cy="14" r="1.8" />
      <path class="gi-warm" d="M16 8a6 6 0 0 1 6 6h-2.5a3.5 3.5 0 0 0-3.5-3.5z" />
    </g>
  </svg>
</template>

<style scoped>
.gi { flex: none; display: block; }
.gi-frame { fill: var(--text-muted); }
.gi-screen { fill: var(--surface); }
.gi-mark { stroke: var(--accent); }
.gi-mark-fill { fill: var(--accent); }
.gi-ink { fill: var(--text); }
.gi-warm { fill: var(--warning); }
</style>
