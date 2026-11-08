<script setup lang="ts">
// A running VM's screen (design D5): frames from the screen server drawn into a canvas scaled to fit with
// its aspect kept, one acknowledged at a time. Interactive, it sends the keyboard as evdev codes, the
// absolute pointer, buttons and wheel; the host key (Right Ctrl) grabs and releases the keyboard,
// Ctrl+Alt+F toggles full screen, and losing focus releases every key and button it pressed. It
// reconnects when the run changes and says "Not running" meanwhile.
import { onBeforeUnmount, onMounted, ref } from "vue";
import { Window } from "@wailsio/runtime";
import { GetScreen } from "./api";
import { EVDEV, HOST_KEY, frameHeader, guestButtons, toGuest } from "./viewer";

const props = withDefaults(defineProps<{
  vm: string;
  interactive?: boolean;
  /** Least time between frames in ms (the Preview); 0 draws at the display's rate. */
  interval?: number;
  label?: string;
}>(), { interval: 0, label: "VM screen" });

const canvas = ref<HTMLCanvasElement>();
const connected = ref(false);
const grabbed = ref(false);
const size = ref<[number, number]>([0, 0]);
const message = ref("Connecting…");
defineExpose({ connected, grabbed, size });

let ws: WebSocket | null = null;
let retry: ReturnType<typeof setTimeout> | undefined;
let gone = false;
const pressed = new Set<number>();
let buttons = 0;
let at: [number, number] = [0, 0];
let wheelAcc = 0;

function down(why = "Not running") {
  connected.value = false;
  grabbed.value = false;
  pressed.clear();
  buttons = 0;
  message.value = why;
}

async function connect() {
  clearTimeout(retry);
  if (gone) return;
  let info;
  try {
    info = await GetScreen(props.vm);
  } catch {
    down();
    retry = setTimeout(connect, 2000);
    return;
  }
  if (gone) return;
  const s = new WebSocket(info.url);
  s.binaryType = "arraybuffer";
  ws = s;
  s.onopen = () => s.send(JSON.stringify({ token: info.token, vm: props.vm, generation: info.generation }));
  s.onmessage = (e) => { if (e.data instanceof ArrayBuffer) draw(s, e.data); };
  // The run ended or restarted: ask again, which binds to the new run's generation.
  s.onclose = () => {
    if (ws !== s) return;
    ws = null;
    down();
    retry = setTimeout(connect, 1000);
  };
}

function draw(s: WebSocket, buf: ArrayBuffer) {
  const c = canvas.value;
  if (!c || buf.byteLength < 12) return;
  const f = frameHeader(buf);
  if (c.width !== f.fw || c.height !== f.fh) {
    c.width = f.fw;
    c.height = f.fh;
    size.value = [f.fw, f.fh];
  }
  if (f.w && f.h && buf.byteLength >= 12 + f.w * f.h * 4) {
    c.getContext("2d")!.putImageData(new ImageData(new Uint8ClampedArray(buf, 12, f.w * f.h * 4), f.w, f.h), f.x, f.y);
  }
  connected.value = true;
  const ack = () => { if (ws === s && s.readyState === WebSocket.OPEN) s.send('{"t":"ack"}'); };
  if (props.interval) setTimeout(ack, props.interval);
  else requestAnimationFrame(ack);
}

function send(m: object) {
  if (ws?.readyState === WebSocket.OPEN && connected.value) ws.send(JSON.stringify(m));
}
function keyUp(code: number) {
  if (pressed.delete(code)) send({ t: "key", down: false, code });
}
function release() {
  for (const c of pressed) keyUp(c); // a Set iterates safely while keyUp deletes
  if (buttons) send({ t: "pointer", x: at[0], y: at[1], b: (buttons = 0) });
}
function grab(on: boolean) {
  if (on) canvas.value?.focus();
  else release();
  grabbed.value = on && connected.value;
}

function onKeyDown(e: KeyboardEvent) {
  if (e.code === HOST_KEY) {
    e.preventDefault();
    if (!e.repeat) grab(!grabbed.value);
    return;
  }
  if (e.ctrlKey && e.altKey && e.code === "KeyF") {
    // Full screen: F never reaches the guest, and the Ctrl and Alt it already got are let go.
    e.preventDefault();
    if (!e.repeat) {
      for (const c of [EVDEV.ControlLeft, EVDEV.ControlRight, EVDEV.AltLeft, EVDEV.AltRight]) keyUp(c);
      void Window.ToggleFullscreen();
    }
    return;
  }
  const code = EVDEV[e.code];
  if (!grabbed.value || !code) return;
  e.preventDefault();
  pressed.add(code);
  send({ t: "key", down: true, code });
}
function onKeyUp(e: KeyboardEvent) {
  const code = EVDEV[e.code];
  if (e.code === HOST_KEY || (code && pressed.has(code))) e.preventDefault();
  if (code && e.code !== HOST_KEY) keyUp(code);
}
function onBlur() {
  release();
  grabbed.value = false;
}

function onPointer(e: PointerEvent) {
  const c = canvas.value;
  if (!c || !connected.value) return;
  const r = c.getBoundingClientRect();
  at = toGuest(c.width, c.height, r.width, r.height, e.clientX - r.left, e.clientY - r.top);
  buttons = guestButtons(e.buttons);
  send({ t: "pointer", x: at[0], y: at[1], b: buttons });
}
function onPointerDown(e: PointerEvent) {
  grab(true);
  canvas.value?.setPointerCapture(e.pointerId);
  onPointer(e);
}
function onWheel(e: WheelEvent) {
  e.preventDefault();
  wheelAcc += e.deltaMode ? Math.sign(e.deltaY) : e.deltaY / 50; // in pixels a wheel notch is about 50
  while (Math.abs(wheelAcc) >= 1) {
    const dy = Math.sign(wheelAcc);
    send({ t: "wheel", dy });
    wheelAcc -= dy;
  }
}

onMounted(() => {
  void connect();
  if (!props.interactive) return;
  window.addEventListener("keydown", onKeyDown);
  window.addEventListener("keyup", onKeyUp);
  window.addEventListener("blur", onBlur);
  canvas.value?.addEventListener("wheel", onWheel, { passive: false });
});
onBeforeUnmount(() => {
  gone = true;
  clearTimeout(retry);
  release();
  ws?.close();
  window.removeEventListener("keydown", onKeyDown);
  window.removeEventListener("keyup", onKeyUp);
  window.removeEventListener("blur", onBlur);
});
</script>

<template>
  <div class="scr">
    <canvas v-show="connected" ref="canvas" role="img" :aria-label="label" :tabindex="interactive ? 0 : -1"
      v-on="interactive ? { pointerdown: onPointerDown, pointermove: onPointer, pointerup: onPointer, blur: onBlur } : {}"></canvas>
    <p v-if="!connected" class="scr-msg" role="status">{{ message }}</p>
  </div>
</template>

<style scoped>
/* A monitor: black around the picture in every skin. */
.scr { position: relative; width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; background: #000; overflow: hidden; }
.scr canvas { display: block; width: 100%; height: 100%; object-fit: contain; outline: none; touch-action: none; }
.scr-msg { margin: 0; padding: 8px; color: #d0d0d0; font-size: 12px; text-align: center; }
</style>
