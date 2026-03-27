import {
  SiCss,
  SiGnubash,
  SiGo,
  SiHtml5,
  SiJavascript,
  SiJson,
  SiKotlin,
  SiLua,
  SiMarkdown,
  SiPhp,
  SiPython,
  SiReact,
  SiRuby,
  SiRust,
  SiSass,
  SiSqlite,
  SiSvelte,
  SiSwift,
  SiToml,
  SiTypescript,
  SiVuedotjs,
  SiYaml,
  SiZig,
} from "@icons-pack/react-simple-icons";
import type { LucideIcon } from "lucide-react";
import {
  Database,
  File,
  FileCog,
  FileImage,
  FileSpreadsheet,
  FileText,
  Terminal,
} from "lucide-react";
import type { ComponentType } from "react";
import { useCallback } from "react";
import type { AutocompleteItem } from "~/hooks/useAutocomplete";
import { cn } from "~/lib/utils";

interface AutocompletePopupProps {
  items: AutocompleteItem[];
  selectedIndex: number;
  triggerType: "@" | "/";
  onSelect: (item: AutocompleteItem) => void;
}

export function AutocompletePopup({
  items,
  selectedIndex,
  triggerType,
  onSelect,
}: AutocompletePopupProps) {
  const scrollRef = useCallback((el: HTMLButtonElement | null) => {
    el?.scrollIntoView({ block: "nearest" });
  }, []);

  return (
    <div className="absolute bottom-full left-0 z-50 mb-1 min-w-64 max-w-lg">
      <div className="relative overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-md">
        <div className="max-h-60 overflow-y-auto p-1">
          {items.map((item, i) => {
            const {
              icon: Icon,
              color,
              isSi,
            } = triggerType === "@" ? fileIconStyle(item.value) : COMMAND_STYLE;
            return (
              <button
                key={`${item.category}-${item.value}`}
                ref={i === selectedIndex ? scrollRef : undefined}
                type="button"
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm",
                  i === selectedIndex ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
                )}
                onMouseDown={(e) => {
                  e.preventDefault();
                  onSelect(item);
                }}
              >
                {isSi ? (
                  <Icon size={14} className="shrink-0" color={color} />
                ) : (
                  <Icon className="h-3.5 w-3.5 shrink-0" style={{ color }} />
                )}
                <span className={item.category === "command" ? "shrink-0" : "truncate"}>
                  {item.label}
                </span>
                {item.description && (
                  <span className="truncate text-xs text-muted-foreground">{item.description}</span>
                )}
                {item.source && <SourceBadge source={item.source} />}
              </button>
            );
          })}
        </div>
        {items.length > 7 && (
          <div className="pointer-events-none absolute inset-x-0 top-0 h-6 bg-gradient-to-b from-popover to-transparent" />
        )}
      </div>
    </div>
  );
}

// --- Source badges ---

const SOURCE_STYLES: Record<string, string> = {
  project: "border-primary/40 text-primary",
  user: "border-agent/40 text-agent",
};

function SourceBadge({ source }: { source: string }) {
  return (
    <span
      className={cn(
        "ml-auto shrink-0 rounded-full border px-1.5 py-0.5 text-[10px] leading-none",
        SOURCE_STYLES[source] ?? "border-border/50 text-muted-foreground",
      )}
    >
      {source}
    </span>
  );
}

// --- File icons with Tokyo Night colors ---

const TN = {
  blue: "var(--primary)",
  cyan: "var(--info)",
  green: "var(--success)",
  yellow: "var(--warning)",
  orange: "var(--orange)",
  red: "var(--destructive)",
  magenta: "var(--agent)",
  muted: "var(--muted-foreground)",
} as const;

// biome-ignore lint/suspicious/noExplicitAny: union of lucide + simple-icons component types
type AnyIcon = ComponentType<any>;

interface IconStyle {
  icon: AnyIcon;
  color: string;
  isSi?: boolean;
}

function si(icon: AnyIcon, color: string): IconStyle {
  return { icon, color, isSi: true };
}

function lucide(icon: LucideIcon, color: string): IconStyle {
  return { icon, color };
}

const COMMAND_STYLE: IconStyle = { icon: Terminal, color: TN.green };

const EXT_STYLES: Record<string, IconStyle> = {
  // TypeScript
  ts: si(SiTypescript, TN.blue),
  tsx: si(SiReact, TN.cyan),
  // JavaScript
  js: si(SiJavascript, TN.yellow),
  jsx: si(SiReact, TN.cyan),
  // Go
  go: si(SiGo, TN.cyan),
  // Python
  py: si(SiPython, TN.green),
  // Rust
  rs: si(SiRust, TN.orange),
  // Ruby
  rb: si(SiRuby, TN.red),
  // Lua
  lua: si(SiLua, TN.blue),
  // Swift
  swift: si(SiSwift, TN.orange),
  // Kotlin
  kt: si(SiKotlin, TN.magenta),
  kts: si(SiKotlin, TN.magenta),
  // PHP
  php: si(SiPhp, TN.magenta),
  // Zig
  zig: si(SiZig, TN.orange),
  // Frontend frameworks
  svelte: si(SiSvelte, TN.red),
  vue: si(SiVuedotjs, TN.green),
  // Shell
  sh: si(SiGnubash, TN.green),
  bash: si(SiGnubash, TN.green),
  fish: lucide(Terminal, TN.green),
  zsh: si(SiGnubash, TN.green),
  // Data / config
  json: si(SiJson, TN.yellow),
  yaml: si(SiYaml, TN.orange),
  yml: si(SiYaml, TN.orange),
  toml: si(SiToml, TN.orange),
  // Web
  html: si(SiHtml5, TN.red),
  css: si(SiCss, TN.blue),
  scss: si(SiSass, TN.magenta),
  sass: si(SiSass, TN.magenta),
  // Docs
  md: si(SiMarkdown, TN.muted),
  txt: lucide(FileText, TN.muted),
  // Database
  sql: si(SiSqlite, TN.cyan),
  csv: lucide(FileSpreadsheet, TN.green),
  // Images
  png: lucide(FileImage, TN.magenta),
  jpg: lucide(FileImage, TN.magenta),
  jpeg: lucide(FileImage, TN.magenta),
  gif: lucide(FileImage, TN.magenta),
  svg: lucide(FileImage, TN.orange),
  webp: lucide(FileImage, TN.magenta),
  ico: lucide(FileImage, TN.magenta),
  // Config
  cfg: lucide(FileCog, TN.muted),
  ini: lucide(FileCog, TN.muted),
  env: lucide(FileCog, TN.muted),
  // C / C++
  c: lucide(Database, TN.blue),
  h: lucide(Database, TN.blue),
  cpp: lucide(Database, TN.cyan),
  hpp: lucide(Database, TN.cyan),
};

const DEFAULT_STYLE: IconStyle = lucide(File, TN.muted);

function fileIconStyle(path: string): IconStyle {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  return EXT_STYLES[ext] ?? DEFAULT_STYLE;
}
