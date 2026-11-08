import { describe, expect, it } from "vite-plus/test";
import { EVDEV, fit, frameHeader, guestButtons, toGuest } from "./viewer";

// The evdev codes the screen server turns into scancodes (internal/core KeyForX11: 1-88 but 84, and evdevToXT).
const SERVED = new Set([...Array.from({ length: 88 }, (_, i) => i + 1).filter((c) => c !== 84),
  85, 86, 89, 92, 93, 94, 96, 97, 98, 99, 100, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 117, 119, 121, 124, 125, 126, 127]);

describe("EVDEV", () => {
  it("names every key of a US keyboard with its input-event-codes.h code", () => {
    const want: Record<string, number> = {
      Escape: 1, Digit1: 2, Digit0: 11, Backspace: 14, Tab: 15, KeyQ: 16, KeyP: 25, Enter: 28, KeyA: 30, KeyL: 38,
      Quote: 40, Backquote: 41, Backslash: 43, KeyZ: 44, KeyM: 50, Slash: 53, Space: 57, CapsLock: 58,
      ShiftLeft: 42, ShiftRight: 54, ControlLeft: 29, ControlRight: 97, AltLeft: 56, AltRight: 100, MetaLeft: 125, MetaRight: 126,
      F1: 59, F10: 68, F11: 87, F12: 88, ArrowUp: 103, ArrowLeft: 105, ArrowRight: 106, ArrowDown: 108,
      Home: 102, End: 107, PageUp: 104, PageDown: 109, Insert: 110, Delete: 111, PrintScreen: 99, Pause: 119, ContextMenu: 127,
      NumLock: 69, Numpad0: 82, Numpad5: 76, Numpad9: 73, NumpadDecimal: 83, NumpadEnter: 96, NumpadDivide: 98,
      NumpadMultiply: 55, NumpadSubtract: 74, NumpadAdd: 78, IntlBackslash: 86, IntlRo: 89,
    };
    for (const [code, ev] of Object.entries(want)) expect([code, EVDEV[code]]).toEqual([code, ev]);
    for (const l of "ABCDEFGHIJKLMNOPQRSTUVWXYZ") expect(EVDEV["Key" + l]).toBeGreaterThan(0);
    for (let d = 0; d <= 9; d++) expect(EVDEV["Digit" + d]).toBeGreaterThan(0);
    for (let f = 1; f <= 12; f++) expect(EVDEV["F" + f]).toBeGreaterThan(0);
    for (let d = 0; d <= 9; d++) expect(EVDEV["Numpad" + d]).toBeGreaterThan(0);
  });
  it("gives each key its own code, one the server accepts", () => {
    const codes = Object.values(EVDEV);
    expect(new Set(codes).size).toBe(codes.length);
    for (const [code, ev] of Object.entries(EVDEV)) expect([code, SERVED.has(ev)]).toEqual([code, true]);
  });
});

describe("coordinates", () => {
  it("letterboxes a 16:10 frame in a wide box and maps through the scale", () => {
    expect(fit(1280, 800, 2000, 800)).toEqual({ s: 1, ox: 360, oy: 0 });
    expect(toGuest(1280, 800, 2000, 800, 360, 0)).toEqual([0, 0]);
    expect(toGuest(1280, 800, 640, 400, 320, 200)).toEqual([640, 400]); // half size
    expect(toGuest(1920, 1080, 960, 1000, 480, 500)).toEqual([960, 540]); // bars above and below
  });
  it("clamps points in the bars and past the edges to the frame", () => {
    expect(toGuest(1280, 800, 2000, 800, 10, 10)).toEqual([0, 10]);
    expect(toGuest(1280, 800, 640, 400, 9999, -5)).toEqual([1279, 0]);
    expect(toGuest(0, 0, 640, 400, 10, 10)).toEqual([0, 0]); // no frame yet
  });
  it("maps the DOM button bits to left 1, middle 2, right 4", () => {
    expect([guestButtons(1), guestButtons(2), guestButtons(4), guestButtons(7), guestButtons(0)]).toEqual([1, 4, 2, 7, 0]);
  });
  it("reads the frame header little endian", () => {
    const b = new ArrayBuffer(12);
    const d = new DataView(b);
    [1, 2, 3, 4, 1920, 1080].forEach((v, i) => d.setUint16(i * 2, v, true));
    expect(frameHeader(b)).toEqual({ x: 1, y: 2, w: 3, h: 4, fw: 1920, fh: 1080 });
  });
});
