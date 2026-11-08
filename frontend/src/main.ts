import { createApp } from "vue";
import App from "./App.vue";
import VMWindow from "./VMWindow.vue";
import "./style.css";
import { applySkin } from "./themes";
import { startAudit } from "./a11y";

// No WebKit context menu (Back/Reload/Inspect): the app shows its own where it needs one.
window.addEventListener("contextmenu", (e) => e.preventDefault());

applySkin("system"); // until the saved theme loads (App.vue applies it)
// One page, two windows: the Manager, and a VM window per VM at #/vm/<id> (opened from Go).
const vm = /^#\/vm\/(.+)$/.exec(location.hash);
(vm ? createApp(VMWindow, { id: decodeURIComponent(vm[1]) }) : createApp(App)).mount("#app");
void startAudit();
