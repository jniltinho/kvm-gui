import { createApp } from "vue";
import App from "./App.vue";
import "./style.css";
import { applySkin } from "./themes";

// No WebKit context menu (Back/Reload/Inspect): the app shows its own where it needs one.
window.addEventListener("contextmenu", (e) => e.preventDefault());

applySkin("system"); // until the saved theme loads (App.vue applies it)
createApp(App).mount("#app");
