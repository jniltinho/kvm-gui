// The window's API: github.com/jniltinho/kvm-gui/internal/gui.App, bound by Wails v3.
import { Call } from "@wailsio/runtime";

const svc = "github.com/jniltinho/kvm-gui/internal/gui.App.";
// A failed call rejects with the Go error's text alone.
const call = <T>(method: string, ...args: unknown[]) =>
  (Call.ByName(svc + method, ...args) as Promise<T>).catch((e: unknown) => {
    throw e instanceof Error ? e.message : e;
  });

export type Guest = "macos" | "linux" | "windows" | "other";

export interface VM {
  id: string;
  name: string;
  guest: Guest;
  os: string;
  version: string;
  state: string;
  running: boolean;
  screen: boolean; // the run exports its screen to the app
  graphics: string;
  memoryMiB: number;
  cpus: number;
}

export interface NIC { enabled: boolean; mode: string; bridge?: string; model: string; mac: string }
export interface Disk { path: string; format: string; bus: string }
export interface SharedFolder { path: string; tag: string; read_only?: boolean }
export interface USBDevice { vendor: string; product: string }

/** internal/core.Config as JSON (snake_case keys, as the core stores it). */
export interface Config {
  name?: string;
  /** "reims-ctl": a macOS VM reims-ctl keeps; only CPUs, memory, resolution and SSH port change. */
  backend?: string;
  workspace: string;
  version: string;
  ram_mib: number;
  cores: number;
  vcpus: number;
  ssh_port: number;
  gpu?: string;
  display?: string;
  networks: NIC[] | null;
  root_patch?: boolean;
  graphics?: string;
  resolution?: string;
  guest?: string;
  profile?: string;
  firmware?: string;
  secure_boot?: boolean;
  tpm?: boolean;
  disks?: Disk[];
  optical?: string[];
  boot_order?: string;
  serial?: string;
  shared_folders?: SharedFolder[];
  usb_devices?: USBDevice[];
  description?: string;
}

export interface DiskInfo { path: string; format: string; bus: string; sizeBytes: number; usedBytes: number; missing: boolean }

export interface VMInfo extends VM {
  config: Config;
  disks: DiskInfo[];
  recovery: string;
  hasVars: boolean;
  sshWarn: string;
}

export interface Profile {
  id: string; name: string; guest: Guest; firmware: string; secureBoot: boolean; tpm: boolean;
  diskBus: string; nic: string; graphics: string; ramMiB: number; minRamMiB: number; diskGiB: number;
}

export interface Catalog {
  versions: { id: string; name: string }[];
  profiles: Profile[];
  nicModels: string[];
  guestNicModels: string[];
  resolutions: string[];
  bridges: string[];
  hostMemMiB: number;
  hostThreads: number;
  recRamMiB: number;
  recVcpus: number;
  minMacRamMiB: number;
  macDiskGiB: number;
  workspace: string;
  reimsMissing: string;
}

export interface Result { id?: string; notice?: string; field?: string; error?: string }

export interface NewVM {
  name: string; folder: string; guest: Guest; version: string; profile: string; iso: string; graphics: string;
  ramMiB: number; vcpus: number; firmware: string; secureBoot: boolean; tpm: boolean;
  disk: "new" | "existing" | "none"; diskGiB: number; diskPath: string;
  backend: "" | "reims-ctl"; offline: boolean;
}

/** A reims-ctl VM not in the list yet (core.ReimsCtlCandidate). */
export interface ReimsCandidate { name: string; dir: string; state: string; config_dir: string }

export interface Prefs { workspace: string; theme: string; scale: string; hostKey: string }
export interface Medium extends DiskInfo { kind: "disk" | "optical"; vms: string[] }

export const Version = () => call<string>("Version");
export const Theme = () => call<string>("Theme");
export const VMs = () => call<VM[]>("VMs");
export const Ready = () => call<void>("Ready");
export const GetVM = (id: string) => call<VMInfo>("VM", id);
export const GetCatalog = () => call<Catalog>("Catalog");
export const DetectISO = (path: string) => call<{ label: string; profile: string; guest: Guest | ""; name: string }>("DetectISO", path);
export const CreateVM = (n: NewVM) => call<Result>("CreateVM", n);
export const SaveVM = (id: string, c: Config, resetVars: boolean) => call<Result>("SaveVM", id, c, resetVars);
export const AddVM = (path: string) => call<Result>("AddVM", path);
export const DeleteVM =(id: string, purge: boolean) => call<string[]>("DeleteVM", id, purge);
export const SelectVM = (id: string) => call<void>("SelectVM", id);
export const ShutDown = (id: string) => call<void>("ShutDown", id);
/** Starts the VM detached: it keeps running when the app closes. */
export const Start = (id: string, headless: boolean) => call<void>("Start", id, headless);
export const ReimsCtlCandidates = () => call<ReimsCandidate[] | null>("ReimsCtlCandidates");
export const AddReimsCtl = (name: string, configDir: string) => call<Result>("AddReimsCtl", name, configDir);
export const GetLog = (id: string) => call<string>("Log", id);
export const GetMedia = () => call<Medium[]>("Media");
export const GetPrefs = () => call<Prefs>("Preferences");
export const SetTheme = (id: string) => call<void>("SetTheme", id);
export const SetScale = (p: string) => call<void>("SetScale", p);
export const SetWorkspace = (p: string) => call<void>("SetWorkspace", p);
/** Native file chooser; "" when cancelled. kind: "iso", "disk" or "". */
export const PickFile = (title: string, kind = "") => call<string>("PickFile", title, kind);
export const PickFolder = (title: string) => call<string>("PickFolder", title);
/** Opens the VM's window, or brings it to the front. */
export const Show = (id: string) => call<void>("Show", id);
/** Where the page connects for a running VM's screen (internal/gui/screen.go); rejects when it is not running. */
export const GetScreen = (id: string) => call<{ url: string; token: string; generation: string }>("Screen", id);

// Reims vGPU's own window placed over the page rectangle (CSS pixels): "embedded", "waiting", or
// "unavailable" with the reason (the page uses the viewer then).
export interface EmbedState { state: "embedded" | "waiting" | "elsewhere" | "unavailable"; reason: string }
export const EmbedScreen = (id: string, host: "manager" | "vm", take: boolean, x: number, y: number, w: number, h: number, dpr: number) =>
  call<EmbedState>("EmbedScreen", id, host, take, x, y, w, h, dpr);
export const EmbedVisible = (id: string, host: "manager" | "vm", visible: boolean) => call<void>("EmbedVisible", id, host, visible);
