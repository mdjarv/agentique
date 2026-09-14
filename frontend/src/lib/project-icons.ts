import {
  Anchor,
  Apple,
  Atom,
  Banana,
  BarChart2,
  Beer,
  Bike,
  Bird,
  Bone,
  BookOpen,
  Bot,
  Brain,
  Briefcase,
  Bug,
  Bus,
  CakeSlice,
  Calendar,
  Camera,
  Car,
  Carrot,
  Castle,
  Cat,
  Cherry,
  Citrus,
  Clapperboard,
  Cloud,
  CloudSun,
  Code,
  Coffee,
  Compass,
  Container,
  Cookie,
  Cpu,
  Croissant,
  Crown,
  Database,
  Diamond,
  Dice5,
  Dna,
  Dog,
  Droplet,
  Dumbbell,
  Egg,
  Feather,
  FileCode,
  Film,
  Fish,
  Flame,
  FlaskConical,
  Flower,
  Flower2,
  Footprints,
  Gamepad2,
  Gem,
  Ghost,
  GitBranch,
  Globe,
  GraduationCap,
  Hammer,
  Headphones,
  Heart,
  Hop,
  IceCreamCone,
  Key,
  Landmark,
  Layers,
  LayoutDashboard,
  Leaf,
  Lightbulb,
  type LucideIcon,
  Mail,
  Map as MapIcon,
  Medal,
  MessageSquare,
  Mic,
  Microscope,
  Moon,
  Mountain,
  MountainSnow,
  Music,
  Orbit,
  Package,
  Paintbrush,
  Palette,
  Panda,
  PawPrint,
  PenTool,
  Pizza,
  Plane,
  Popcorn,
  Puzzle,
  Rabbit,
  Radio,
  Rat,
  Receipt,
  Rocket,
  Sailboat,
  Salad,
  Sandwich,
  Satellite,
  Server,
  Settings,
  Shell,
  Shield,
  Ship,
  ShoppingBag,
  ShoppingCart,
  Shrimp,
  Skull,
  Smartphone,
  Snail,
  Snowflake,
  Soup,
  Sparkles,
  Sprout,
  Squirrel,
  Star,
  Sun,
  Swords,
  Target,
  Telescope,
  Tent,
  Terminal,
  TrainFront,
  TreePalm,
  TreePine,
  Trees,
  Trophy,
  Turtle,
  Tv,
  Users,
  Utensils,
  Volleyball,
  Wallet,
  WandSparkles,
  Waves,
  Wine,
  Worm,
  Wrench,
  Zap,
} from "lucide-react";
import { dynamicIconImports } from "lucide-react/dynamic";

export interface ProjectIconDef {
  id: string;
  icon: LucideIcon;
}

export interface ProjectIconGroup {
  label: string;
  icons: ProjectIconDef[];
}

/**
 * Featured icons shown in the picker's default (no search) view, by category.
 * Every lucide icon is still reachable by search; these are the ones worth a
 * glance. Imported statically so the sidebar renders them without a fetch.
 */
export const PROJECT_ICON_GROUPS: ProjectIconGroup[] = [
  {
    label: "Dev",
    icons: [
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
      { id: "bot", icon: Bot },
    ],
  },
  {
    label: "General",
    icons: [
      { id: "globe", icon: Globe },
      { id: "rocket", icon: Rocket },
      { id: "zap", icon: Zap },
      { id: "package", icon: Package },
      { id: "layers", icon: Layers },
      { id: "shield", icon: Shield },
      { id: "wrench", icon: Wrench },
      { id: "hammer", icon: Hammer },
      { id: "key", icon: Key },
      { id: "lightbulb", icon: Lightbulb },
      { id: "puzzle", icon: Puzzle },
      { id: "target", icon: Target },
    ],
  },
  {
    label: "Work",
    icons: [
      { id: "layout-dashboard", icon: LayoutDashboard },
      { id: "briefcase", icon: Briefcase },
      { id: "bar-chart-2", icon: BarChart2 },
      { id: "calendar", icon: Calendar },
      { id: "users", icon: Users },
      { id: "mail", icon: Mail },
      { id: "message-square", icon: MessageSquare },
      { id: "wallet", icon: Wallet },
      { id: "receipt", icon: Receipt },
      { id: "shopping-cart", icon: ShoppingCart },
      { id: "shopping-bag", icon: ShoppingBag },
      { id: "landmark", icon: Landmark },
    ],
  },
  {
    label: "Media",
    icons: [
      { id: "smartphone", icon: Smartphone },
      { id: "camera", icon: Camera },
      { id: "headphones", icon: Headphones },
      { id: "music", icon: Music },
      { id: "mic", icon: Mic },
      { id: "radio", icon: Radio },
      { id: "tv", icon: Tv },
      { id: "film", icon: Film },
      { id: "clapperboard", icon: Clapperboard },
      { id: "paintbrush", icon: Paintbrush },
      { id: "palette", icon: Palette },
      { id: "pen-tool", icon: PenTool },
    ],
  },
  {
    label: "Animals",
    icons: [
      { id: "cat", icon: Cat },
      { id: "dog", icon: Dog },
      { id: "bird", icon: Bird },
      { id: "fish", icon: Fish },
      { id: "rabbit", icon: Rabbit },
      { id: "squirrel", icon: Squirrel },
      { id: "turtle", icon: Turtle },
      { id: "snail", icon: Snail },
      { id: "rat", icon: Rat },
      { id: "worm", icon: Worm },
      { id: "shrimp", icon: Shrimp },
      { id: "panda", icon: Panda },
      { id: "paw-print", icon: PawPrint },
      { id: "feather", icon: Feather },
      { id: "egg", icon: Egg },
      { id: "bone", icon: Bone },
    ],
  },
  {
    label: "Nature",
    icons: [
      { id: "tree-pine", icon: TreePine },
      { id: "trees", icon: Trees },
      { id: "tree-palm", icon: TreePalm },
      { id: "leaf", icon: Leaf },
      { id: "flower", icon: Flower },
      { id: "flower-2", icon: Flower2 },
      { id: "sprout", icon: Sprout },
      { id: "mountain", icon: Mountain },
      { id: "mountain-snow", icon: MountainSnow },
      { id: "sun", icon: Sun },
      { id: "moon", icon: Moon },
      { id: "cloud-sun", icon: CloudSun },
      { id: "snowflake", icon: Snowflake },
      { id: "flame", icon: Flame },
      { id: "droplet", icon: Droplet },
      { id: "waves", icon: Waves },
      { id: "shell", icon: Shell },
    ],
  },
  {
    label: "Food",
    icons: [
      { id: "coffee", icon: Coffee },
      { id: "pizza", icon: Pizza },
      { id: "apple", icon: Apple },
      { id: "cherry", icon: Cherry },
      { id: "carrot", icon: Carrot },
      { id: "banana", icon: Banana },
      { id: "citrus", icon: Citrus },
      { id: "salad", icon: Salad },
      { id: "sandwich", icon: Sandwich },
      { id: "soup", icon: Soup },
      { id: "croissant", icon: Croissant },
      { id: "cookie", icon: Cookie },
      { id: "cake-slice", icon: CakeSlice },
      { id: "ice-cream-cone", icon: IceCreamCone },
      { id: "popcorn", icon: Popcorn },
      { id: "beer", icon: Beer },
      { id: "wine", icon: Wine },
      { id: "hop", icon: Hop },
      { id: "utensils", icon: Utensils },
    ],
  },
  {
    label: "Travel",
    icons: [
      { id: "plane", icon: Plane },
      { id: "car", icon: Car },
      { id: "bike", icon: Bike },
      { id: "bus", icon: Bus },
      { id: "train-front", icon: TrainFront },
      { id: "ship", icon: Ship },
      { id: "sailboat", icon: Sailboat },
      { id: "anchor", icon: Anchor },
      { id: "map", icon: MapIcon },
      { id: "compass", icon: Compass },
      { id: "tent", icon: Tent },
      { id: "castle", icon: Castle },
      { id: "footprints", icon: Footprints },
    ],
  },
  {
    label: "Sport & play",
    icons: [
      { id: "trophy", icon: Trophy },
      { id: "medal", icon: Medal },
      { id: "dumbbell", icon: Dumbbell },
      { id: "volleyball", icon: Volleyball },
      { id: "gamepad-2", icon: Gamepad2 },
      { id: "dice-5", icon: Dice5 },
      { id: "crown", icon: Crown },
      { id: "gem", icon: Gem },
      { id: "sparkles", icon: Sparkles },
      { id: "ghost", icon: Ghost },
      { id: "skull", icon: Skull },
      { id: "swords", icon: Swords },
      { id: "wand-sparkles", icon: WandSparkles },
    ],
  },
  {
    label: "Science",
    icons: [
      { id: "atom", icon: Atom },
      { id: "brain", icon: Brain },
      { id: "dna", icon: Dna },
      { id: "flask-conical", icon: FlaskConical },
      { id: "microscope", icon: Microscope },
      { id: "telescope", icon: Telescope },
      { id: "orbit", icon: Orbit },
      { id: "satellite", icon: Satellite },
      { id: "graduation-cap", icon: GraduationCap },
      { id: "book-open", icon: BookOpen },
    ],
  },
  {
    label: "Symbols",
    icons: [
      { id: "heart", icon: Heart },
      { id: "star", icon: Star },
      { id: "diamond", icon: Diamond },
    ],
  },
];

export const PROJECT_ICONS: ProjectIconDef[] = PROJECT_ICON_GROUPS.flatMap((g) => g.icons);

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
