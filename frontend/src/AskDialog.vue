<script setup lang="ts">
// Renders the pending ask.ts request (confirmations and messages) in the app's dialog frame.
import { ref, watch } from "vue";
import { askState } from "./ask";
import Modal from "./Modal.vue";

const value = ref("");
watch(askState, (r) => { value.value = r?.input ?? ""; });

function answer(ok: boolean) {
  const r = askState.value;
  if (!r) return;
  askState.value = null;
  if (r.input !== undefined) r.resolve(ok && value.value.trim() ? value.value.trim() : null);
  else r.resolve(ok);
}
</script>

<template>
  <Modal v-if="askState" :key="askState.title + askState.message" :title="askState.title" :width="440" @close="answer(false)">
    <div class="ask-body">
      <p class="ask-msg">{{ askState.message }}</p>
      <input v-if="askState.input !== undefined" v-model="value" class="in" :aria-label="askState.title" autofocus spellcheck="false" @keydown.enter="answer(true)" />
    </div>
    <template #footer>
      <button type="button" class="btn primary" :autofocus="askState.input === undefined" @click="answer(true)">{{ askState.ok }}</button>
      <button v-if="askState.cancel" type="button" class="btn" @click="answer(false)">{{ askState.cancel }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.ask-body { padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; }
.ask-msg { margin: 0; white-space: pre-wrap; line-height: 1.45; }
</style>
