// The window's API: github.com/jniltinho/kvm-gui/internal/gui.App, bound by Wails v3.
import { Call } from "@wailsio/runtime";

const svc = "github.com/jniltinho/kvm-gui/internal/gui.App.";
// A failed call rejects with the Go error's text alone.
const call = <T>(method: string, ...args: unknown[]) =>
  (Call.ByName(svc + method, ...args) as Promise<T>).catch((e: unknown) => {
    throw e instanceof Error ? e.message : e;
  });

export interface VM {
  id: string;
  name: string;
  version: string;
  state: string;
  graphics: string;
  memoryMiB: number;
  cpus: number;
}

export const Version = () => call<string>("Version");
export const Theme = () => call<string>("Theme");
export const VMs = () => call<VM[]>("VMs");
export const Ready = () => call<void>("Ready");
