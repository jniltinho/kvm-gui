// Accessibility audit (task 7.8), on only with KVM_GUI_A11Y_AUDIT=1: after each change of the page it lists
// the interactive controls that have no accessible name, so a check under Xvfb (no AT-SPI bus there) can read
// them from the app's log. A rough name computation: aria-label(ledby), label, title, alt, text.
import { Call } from "@wailsio/runtime";

const svc = "github.com/jniltinho/kvm-gui/internal/gui.App.";
const CONTROLS = "button, input, select, textarea, a[href], [role=tab], [role=option], [role=menuitem], [role=treeitem], [role=listbox], [role=tree], [tabindex='0']";

function accessibleName(el: HTMLElement): string {
  const by = el.getAttribute("aria-labelledby");
  const text = (s: string | null | undefined) => (s ?? "").replace(/\s+/g, " ").trim();
  return text(el.getAttribute("aria-label"))
    || (by ? text(by.split(" ").map((id) => document.getElementById(id)?.textContent).join(" ")) : "")
    || text(el.id ? document.querySelector(`label[for="${el.id}"]`)?.textContent : "")
    || text(el.closest("label")?.textContent)
    || (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement ? "" : text(el.textContent))
    || text(el.querySelector("img[alt]")?.getAttribute("alt"))
    || text(el.getAttribute("title"));
}

function describe(el: HTMLElement) {
  return `${el.tagName.toLowerCase()}${el.id ? "#" + el.id : ""}${el.className && typeof el.className === "string" ? "." + el.className.split(" ")[0] : ""}`;
}

let last = "";
function audit() {
  const dialogs = [...document.querySelectorAll<HTMLElement>("[role=dialog]")];
  const root = dialogs[dialogs.length - 1] ?? document.body;
  const screen = root.getAttribute("aria-label") ?? "Manager";
  const all = [...root.querySelectorAll<HTMLElement>(CONTROLS)].filter((e) => e.offsetParent !== null || e.getClientRects().length);
  const unnamed = all.filter((e) => !accessibleName(e)).map(describe);
  const key = screen + unnamed.join(",") + all.length;
  if (key === last) return;
  last = key;
  void Call.ByName(svc + "A11yReport", screen, all.length, unnamed);
}

export async function startAudit() {
  if (!(await Call.ByName(svc + "A11yAudit").catch(() => false))) return;
  let t: ReturnType<typeof setTimeout> | undefined;
  new MutationObserver(() => { clearTimeout(t); t = setTimeout(audit, 400); })
    .observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["class", "aria-selected"] });
  audit();
}
