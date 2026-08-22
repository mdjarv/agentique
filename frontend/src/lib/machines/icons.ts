/**
 * Machine icons (multi-machine). Same mechanism as project icons — an id
 * string, a featured set, a lucide fallback — with a machine-flavoured
 * shortlist, because "which of my boxes is this" is answered by a laptop or a
 * cloud, not by a paintbrush.
 *
 * Resolution falls through to the project registry, so any lucide id a user
 * has picked elsewhere still renders here.
 */
import {
  Box,
  Building2,
  Cloud,
  Container,
  Cpu,
  Database,
  Globe,
  HardDrive,
  Home,
  Laptop,
  type LucideIcon,
  Monitor,
  Rocket,
  Server,
  Smartphone,
  Terminal,
} from "lucide-react";
import { getProjectIcon } from "~/lib/project-icons";

export interface MachineIconDef {
  id: string;
  icon: LucideIcon;
  /** Spoken name — the picker's tooltip and its accessible label. */
  label: string;
}

export const MACHINE_ICONS: MachineIconDef[] = [
  { id: "laptop", icon: Laptop, label: "Laptop" },
  { id: "monitor", icon: Monitor, label: "Desktop" },
  { id: "server", icon: Server, label: "Server" },
  { id: "cloud", icon: Cloud, label: "Cloud" },
  { id: "container", icon: Container, label: "Container" },
  { id: "cpu", icon: Cpu, label: "Board" },
  { id: "hard-drive", icon: HardDrive, label: "Storage" },
  { id: "smartphone", icon: Smartphone, label: "Phone" },
  { id: "database", icon: Database, label: "Database" },
  { id: "terminal", icon: Terminal, label: "Shell" },
  { id: "globe", icon: Globe, label: "Remote" },
  { id: "home", icon: Home, label: "Home" },
  { id: "building-2", icon: Building2, label: "Office" },
  { id: "rocket", icon: Rocket, label: "Production" },
  { id: "box", icon: Box, label: "Box" },
];

const featured = new Map(MACHINE_ICONS.map((i) => [i.id, i.icon]));

/** The machine's icon component, or undefined when unset/unknown. */
export function getMachineIcon(iconId: string): LucideIcon | undefined {
  if (!iconId) return undefined;
  return featured.get(iconId) ?? getProjectIcon(iconId);
}

/** The glyph a machine falls back to when it has no icon of its own. */
export const DEFAULT_MACHINE_ICON: LucideIcon = Server;
