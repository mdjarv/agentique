import {
  BarChart2,
  Bot,
  Briefcase,
  Bug,
  Calendar,
  Camera,
  Cloud,
  Code,
  Container,
  Cpu,
  Database,
  FileCode,
  FlaskConical,
  Gamepad2,
  GitBranch,
  Globe,
  Headphones,
  Heart,
  Layers,
  LayoutDashboard,
  type LucideIcon,
  Mail,
  MessageSquare,
  Music,
  Package,
  Paintbrush,
  Rocket,
  Server,
  Settings,
  Shield,
  ShoppingCart,
  Smartphone,
  Star,
  Terminal,
  Users,
  Wrench,
  Zap,
} from "lucide-react";

export interface ProjectIconDef {
  id: string;
  icon: LucideIcon;
}

/** Featured icons shown in the default (no search) view of the picker. */
export const PROJECT_ICONS: ProjectIconDef[] = [
  // Dev
  { id: "code", icon: Code },
  { id: "terminal", icon: Terminal },
  { id: "cpu", icon: Cpu },
  { id: "server", icon: Server },
  { id: "database", icon: Database },
  { id: "git-branch", icon: GitBranch },
  { id: "bug", icon: Bug },
  { id: "file-code", icon: FileCode },
  { id: "settings", icon: Settings },
  { id: "cloud", icon: Cloud },
  { id: "container", icon: Container },
  // General
  { id: "globe", icon: Globe },
  { id: "rocket", icon: Rocket },
  { id: "zap", icon: Zap },
  { id: "package", icon: Package },
  { id: "layers", icon: Layers },
  { id: "shield", icon: Shield },
  { id: "wrench", icon: Wrench },
  { id: "bot", icon: Bot },
  { id: "flask-conical", icon: FlaskConical },
  // Business
  { id: "layout-dashboard", icon: LayoutDashboard },
  { id: "briefcase", icon: Briefcase },
  { id: "bar-chart-2", icon: BarChart2 },
  { id: "calendar", icon: Calendar },
  { id: "users", icon: Users },
  { id: "mail", icon: Mail },
  { id: "message-square", icon: MessageSquare },
  // Media / creative
  { id: "smartphone", icon: Smartphone },
  { id: "shopping-cart", icon: ShoppingCart },
  { id: "camera", icon: Camera },
  { id: "headphones", icon: Headphones },
  { id: "paintbrush", icon: Paintbrush },
  { id: "gamepad-2", icon: Gamepad2 },
  { id: "music", icon: Music },
  // Symbols
  { id: "heart", icon: Heart },
  { id: "star", icon: Star },
];

const staticMap = new Map(PROJECT_ICONS.map((i) => [i.id, i.icon]));
const dynamicCache = new Map<string, LucideIcon>();

/** Resolve a project icon ID to its Lucide component, or undefined if not set / unknown. */
export function getProjectIcon(iconId: string): LucideIcon | undefined {
  if (!iconId) return undefined;
  return staticMap.get(iconId) ?? dynamicCache.get(iconId);
}

/** Cache a dynamically loaded icon so the rail can render it synchronously. */
export function cacheProjectIcon(id: string, component: LucideIcon): void {
  if (id && !staticMap.has(id)) {
    dynamicCache.set(id, component);
  }
}

/**
 * Preload an icon by ID using dynamic imports. Fire-and-forget.
 * After loading, the icon is cached and available via getProjectIcon().
 */
export async function preloadProjectIcon(iconId: string): Promise<void> {
  if (!iconId || staticMap.has(iconId) || dynamicCache.has(iconId)) return;
  try {
    const { dynamicIconImports } = await import("lucide-react/dynamic");
    const importFn = dynamicIconImports[iconId as keyof typeof dynamicIconImports];
    if (!importFn) return;
    const mod = await importFn();
    // The module default export is the React component
    if (mod?.default) {
      dynamicCache.set(iconId, mod.default as LucideIcon);
    }
  } catch {
    // Silently ignore — rail falls back to initials
  }
}
