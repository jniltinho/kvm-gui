// The VM screen's pure parts (Screen.vue): physical keys as Linux evdev codes, the guest
// coordinates of a point in the scaled canvas, the pointer button mask and the frame header.

/** KeyboardEvent.code → Linux evdev keycode (input-event-codes.h), every key of a US keyboard plus the ISO and JIS extras. */
export const EVDEV: Readonly<Record<string, number>> = {
  Escape: 1, Digit1: 2, Digit2: 3, Digit3: 4, Digit4: 5, Digit5: 6, Digit6: 7, Digit7: 8, Digit8: 9, Digit9: 10,
  Digit0: 11, Minus: 12, Equal: 13, Backspace: 14, Tab: 15,
  KeyQ: 16, KeyW: 17, KeyE: 18, KeyR: 19, KeyT: 20, KeyY: 21, KeyU: 22, KeyI: 23, KeyO: 24, KeyP: 25,
  BracketLeft: 26, BracketRight: 27, Enter: 28, ControlLeft: 29,
  KeyA: 30, KeyS: 31, KeyD: 32, KeyF: 33, KeyG: 34, KeyH: 35, KeyJ: 36, KeyK: 37, KeyL: 38,
  Semicolon: 39, Quote: 40, Backquote: 41, ShiftLeft: 42, Backslash: 43,
  KeyZ: 44, KeyX: 45, KeyC: 46, KeyV: 47, KeyB: 48, KeyN: 49, KeyM: 50,
  Comma: 51, Period: 52, Slash: 53, ShiftRight: 54, NumpadMultiply: 55, AltLeft: 56, Space: 57, CapsLock: 58,
  F1: 59, F2: 60, F3: 61, F4: 62, F5: 63, F6: 64, F7: 65, F8: 66, F9: 67, F10: 68,
  NumLock: 69, ScrollLock: 70, Numpad7: 71, Numpad8: 72, Numpad9: 73, NumpadSubtract: 74,
  Numpad4: 75, Numpad5: 76, Numpad6: 77, NumpadAdd: 78, Numpad1: 79, Numpad2: 80, Numpad3: 81,
  Numpad0: 82, NumpadDecimal: 83, IntlBackslash: 86, F11: 87, F12: 88, IntlRo: 89,
  Convert: 92, KanaMode: 93, NonConvert: 94, NumpadEnter: 96, ControlRight: 97, NumpadDivide: 98,
  PrintScreen: 99, AltRight: 100, Home: 102, ArrowUp: 103, PageUp: 104, ArrowLeft: 105, ArrowRight: 106,
  End: 107, ArrowDown: 108, PageDown: 109, Insert: 110, Delete: 111, NumpadEqual: 117, Pause: 119,
  NumpadComma: 121, IntlYen: 124, MetaLeft: 125, MetaRight: 126, ContextMenu: 127,
};

/** The host key: grabs and releases the keyboard; it never reaches the guest. */
export const HOST_KEY = "ControlRight";
export const HOST_KEY_LABEL = "Right Ctrl";

/** Where a fw×fh frame is drawn in a bw×bh box, scaled to fit with its aspect kept (as CSS object-fit: contain). */
export function fit(fw: number, fh: number, bw: number, bh: number) {
  const s = fw > 0 && fh > 0 ? Math.min(bw / fw, bh / fh) : 0;
  return { s, ox: (bw - fw * s) / 2, oy: (bh - fh * s) / 2 };
}

/** The guest pixel under (px, py) of the box, clamped to the frame so a drag past its edge stays on it. */
export function toGuest(fw: number, fh: number, bw: number, bh: number, px: number, py: number): [number, number] {
  const { s, ox, oy } = fit(fw, fh, bw, bh);
  if (!s) return [0, 0];
  const clamp = (v: number, n: number) => Math.min(n - 1, Math.max(0, Math.floor(v)));
  return [clamp((px - ox) / s, fw), clamp((py - oy) / s, fh)];
}

/** MouseEvent.buttons (1 left, 2 right, 4 middle) → the screen protocol's mask (1 left, 2 middle, 4 right). */
export const guestButtons = (b: number) => (b & 1) | (b & 4 ? 2 : 0) | (b & 2 ? 4 : 0);

/** A binary frame's 12-byte header: the changed rectangle and the full frame size (uint16, little endian). */
export function frameHeader(buf: ArrayBuffer) {
  const d = new DataView(buf);
  const [x, y, w, h, fw, fh] = [0, 2, 4, 6, 8, 10].map((o) => d.getUint16(o, true));
  return { x, y, w, h, fw, fh };
}
