import { memo, useCallback } from "react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { ICON_MAP, SESSION_ICON_KEYS, getSessionIconComponent } from "~/lib/session-icons";
import { cn } from "~/lib/utils";

interface IconPickerProps {
  value: string | undefined;
  onChange: (icon: string | undefined) => void;
  children: React.ReactNode;
}

export const IconPicker = memo(function IconPicker({ value, onChange, children }: IconPickerProps) {
  const handleSelect = useCallback(
    (key: string) => {
      onChange(key === value ? undefined : key);
    },
    [value, onChange],
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent className="w-56 p-2" align="start">
        <p className="text-[10px] font-medium text-muted-foreground mb-1.5 px-1">Session icon</p>
        <div className="grid grid-cols-5 gap-1">
          {SESSION_ICON_KEYS.map((key) => {
            const def = ICON_MAP[key];
            if (!def) return null;
            const Icon = def.icon;
            const isActive = key === value;
            return (
              <button
                key={key}
                type="button"
                onClick={() => handleSelect(key)}
                title={def.label}
                className={cn(
                  "flex items-center justify-center h-8 w-full rounded-md cursor-pointer transition-colors",
                  "hover:bg-accent hover:text-accent-foreground",
                  isActive && "bg-accent text-accent-foreground ring-1 ring-ring",
                )}
              >
                <Icon className="h-4 w-4" />
              </button>
            );
          })}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
});

/** Small button showing the current session icon, opens picker on click. */
export function SessionIconButton({
  icon,
  onChange,
  className,
}: {
  icon: string | undefined;
  onChange: (icon: string | undefined) => void;
  className?: string;
}) {
  const Icon = getSessionIconComponent(icon);
  return (
    <IconPicker value={icon} onChange={onChange}>
      <button
        type="button"
        title="Change session icon"
        className={cn(
          "shrink-0 rounded-md p-1 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer",
          className,
        )}
      >
        <Icon className="h-4 w-4" />
      </button>
    </IconPicker>
  );
}
