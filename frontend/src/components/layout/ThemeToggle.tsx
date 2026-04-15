import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme } from "~/hooks/useTheme";
import type { Theme } from "~/stores/ui-store";

const CYCLE: Record<Theme, Theme> = {
  dark: "light",
  light: "system",
  system: "dark",
};

const ICONS: Record<Theme, typeof Sun> = {
  dark: Moon,
  light: Sun,
  system: Monitor,
};

const LABELS: Record<Theme, string> = {
  dark: "Dark",
  light: "Light",
  system: "System",
};

export function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const Icon = ICONS[theme];

  return (
    <button
      type="button"
      onClick={() => setTheme(CYCLE[theme])}
      className="cursor-pointer rounded p-1 text-muted-foreground-faint hover:bg-muted hover:text-foreground transition-colors"
      title={`Theme: ${LABELS[theme]}`}
    >
      <Icon className="size-3.5" />
    </button>
  );
}
