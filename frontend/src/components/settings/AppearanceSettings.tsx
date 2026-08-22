/** Settings › Appearance — device-local look. Persisted in the UI store. */
import { Monitor, Moon, Sun } from "lucide-react";
import { SettingsRow, SettingsSection } from "~/components/settings/SettingsLayout";
import { useTheme } from "~/hooks/useTheme";
import { cn } from "~/lib/utils";
import type { Theme } from "~/stores/ui-store";

const THEMES: { id: Theme; label: string; icon: typeof Sun }[] = [
  { id: "light", label: "Light", icon: Sun },
  { id: "dark", label: "Dark", icon: Moon },
  { id: "system", label: "System", icon: Monitor },
];

export function AppearanceSettings() {
  const { theme, setTheme } = useTheme();

  return (
    <div className="flex flex-col gap-7">
      <SettingsSection title="Theme">
        <SettingsRow
          label="Colour scheme"
          description="System follows your OS setting."
          control={
            <div className="flex gap-1 rounded-lg border border-border/60 p-0.5">
              {THEMES.map(({ id, label, icon: Icon }) => (
                <button
                  key={id}
                  type="button"
                  onClick={() => setTheme(id)}
                  aria-pressed={theme === id}
                  className={cn(
                    "flex cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[12px] transition-colors",
                    theme === id
                      ? "bg-secondary font-medium text-foreground-bright"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  <Icon className="size-3.5" />
                  {label}
                </button>
              ))}
            </div>
          }
        />
      </SettingsSection>
    </div>
  );
}
