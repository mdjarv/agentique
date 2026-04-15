import {
  Bot,
  Brain,
  Bug,
  Code,
  Database,
  FileSearch,
  FlaskConical,
  Globe,
  HardDrive,
  Layout,
  type LucideIcon,
  Palette,
  PenTool,
  Search,
  Server,
  Settings,
  Shield,
  Terminal,
  TestTube2,
  Wrench,
  Zap,
} from "lucide-react";

export interface SessionIconDef {
  icon: LucideIcon;
  label: string;
}

const ICON_MAP: Record<string, SessionIconDef> = {
  bot: { icon: Bot, label: "Bot" },
  brain: { icon: Brain, label: "Brain" },
  bug: { icon: Bug, label: "Bug" },
  code: { icon: Code, label: "Code" },
  database: { icon: Database, label: "Database" },
  "file-search": { icon: FileSearch, label: "File search" },
  flask: { icon: FlaskConical, label: "Flask" },
  globe: { icon: Globe, label: "Globe" },
  "hard-drive": { icon: HardDrive, label: "Hard drive" },
  layout: { icon: Layout, label: "Layout" },
  palette: { icon: Palette, label: "Palette" },
  pen: { icon: PenTool, label: "Pen" },
  search: { icon: Search, label: "Search" },
  server: { icon: Server, label: "Server" },
  settings: { icon: Settings, label: "Settings" },
  shield: { icon: Shield, label: "Shield" },
  terminal: { icon: Terminal, label: "Terminal" },
  "test-tube": { icon: TestTube2, label: "Test tube" },
  wrench: { icon: Wrench, label: "Wrench" },
  zap: { icon: Zap, label: "Zap" },
};

const DEFAULT: SessionIconDef = ICON_MAP.bot ?? { icon: Bot, label: "Bot" };

export function getSessionIcon(key: string | undefined): SessionIconDef {
  if (!key) return DEFAULT;
  return ICON_MAP[key] ?? DEFAULT;
}

export function getSessionIconComponent(key: string | undefined): LucideIcon {
  return getSessionIcon(key).icon;
}

/** All available icon keys, sorted by label. */
export const SESSION_ICON_KEYS = Object.keys(ICON_MAP).sort((a, b) =>
  (ICON_MAP[a]?.label ?? "").localeCompare(ICON_MAP[b]?.label ?? ""),
);

export { ICON_MAP };
