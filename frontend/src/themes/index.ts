// Skin registry (Settings › Display › Skin). Each skin is a CSS file in this folder keyed by
// data-theme on <html>; "system" is resolved here to light or dark and follows the OS live.
export type Skin = { id: string; label: string; dark: boolean };

export const SKINS: Skin[] = [
  { id: "light", label: "Light", dark: false },
  { id: "dark", label: "Dark", dark: true },
  { id: "xp", label: "Windows XP", dark: false },
  { id: "macos", label: "macOS", dark: false },
  { id: "macos-dark", label: "macOS Dark", dark: true },
  { id: "macos-retro", label: "Mac OS Retro", dark: false },
];

const os = matchMedia("(prefers-color-scheme: dark)");
let chosen = "system";

/** The skin actually drawn: "system" follows the OS, unknown ids fall back to it. */
export function effectiveSkin(id = chosen): Skin {
  return SKINS.find((s) => s.id === id) ?? SKINS[os.matches ? 1 : 0];
}

function paint() {
  const el = document.documentElement;
  el.dataset.theme = effectiveSkin().id;
  el.dataset.skin = chosen;
}

/** Apply the Skin setting ("light", "dark", "xp", "system"). */
export function applySkin(id: string | undefined) {
  chosen = id || "system";
  paint();
}

os.addEventListener("change", () => { if (chosen === "system") paint(); });
