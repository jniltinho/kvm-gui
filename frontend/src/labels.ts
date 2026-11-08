// Display names for the core's ids (device models, buses, guest types), shared by Details, Settings and the wizard.
import type { Config, Guest } from "./api";

export const GUESTS: readonly [Guest, string][] = [["macos", "macOS"], ["linux", "Linux"], ["windows", "Windows"], ["other", "Other"]];
export const guestLabel = (g: string) => GUESTS.find((x) => x[0] === g)?.[1] ?? g;

const NAMES: Record<string, string> = {
  "virtio-net-pci": "Paravirtualized (virtio-net)",
  vmxnet3: "VMware vmxnet3",
  "e1000-82545em": "Intel 82545EM (e1000)",
  e1000e: "Intel 82574L (e1000e)",
  e1000: "Intel 82540EM (e1000)",
  "vmware-svga": "VMware SVGA",
  "reims-vgpu": "Reims vGPU (Metal)",
  "virtio-vga": "Paravirtualized (virtio-vga)",
  vga: "Standard VGA",
  virtio: "virtio-blk",
  sata: "SATA (AHCI)",
  nvme: "NVMe",
  uefi: "UEFI",
  bios: "Legacy BIOS",
  nat: "NAT",
  bridged: "Bridged",
};
export const label = (id: string | undefined) => (id ? NAMES[id] ?? id : "");

/** The graphics adapter id the core stores ("" is VMware SVGA for macOS VMs). */
export const graphicsOf = (c: Config) => c.graphics || (isMac(c) ? "vmware-svga" : "vga");
export const isMac = (c: Config) => !c.guest || c.guest === "macos";

export function size(bytes: number): string {
  if (!bytes) return "—";
  const gib = bytes / 2 ** 30;
  if (gib >= 1) return `${gib.toFixed(gib >= 100 ? 0 : 1)} GiB`;
  return bytes >= 2 ** 20 ? `${(bytes / 2 ** 20).toFixed(0)} MiB` : `${Math.ceil(bytes / 1024)} KiB`;
}

export const base = (p: string) => p.split("/").pop() || p;
