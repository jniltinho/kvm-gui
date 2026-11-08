// In-app replacements for window.prompt/confirm/alert (from goXterm, MIT, same author). The native ones show
// a foreign "JavaScript - wails://" box on WebKitGTK and may not be shown at all by WKWebView (macOS),
// so every component asks through these; AskDialog.vue (mounted once in App.vue) renders the request.
import { shallowRef } from "vue";

export interface AskRequest {
  title: string;
  message: string;
  input?: string; // present → a text field with this initial value
  ok: string;
  cancel?: string; // absent → an information box with one button
  resolve: (v: string | boolean | null) => void;
}

export const askState = shallowRef<AskRequest | null>(null);

function ask(r: Omit<AskRequest, "resolve">): Promise<string | boolean | null> {
  return new Promise((resolve) => {
    askState.value?.resolve(null); // a newer question replaces an unanswered one
    askState.value = { ...r, resolve };
  });
}

/** Text input; null when cancelled. */
export const askText = (title: string, message: string, initial = "") =>
  ask({ title, message, input: initial, ok: "OK", cancel: "Cancel" }) as Promise<string | null>;

/** Yes/no question; false when cancelled. */
export const askConfirm = async (title: string, message: string, ok = "OK") =>
  (await ask({ title, message, ok, cancel: "Cancel" })) === true;

/** Information box with an OK button. */
export const showInfo = async (title: string, message: string) => {
  await ask({ title, message, ok: "OK" });
};
