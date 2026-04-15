import type { ReactNode } from "react";
import { cn } from "~/lib/utils";

interface ToolbarToggleProps {
  active: boolean;
  onChange?: (value: boolean) => void;
  activeIcon: ReactNode;
  inactiveIcon: ReactNode;
  activeLabel: string;
  inactiveLabel: string;
  activeColor?: string;
  inactiveColor?: string;
  disabled?: boolean;
}

const BASE =
  "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 shrink-0";

function stripBg(classes: string): string {
  return classes
    .split(" ")
    .filter((c) => !c.startsWith("bg-"))
    .join(" ");
}

export function ToolbarToggle({
  active,
  onChange,
  activeIcon,
  inactiveIcon,
  activeLabel,
  inactiveLabel,
  activeColor = "bg-primary/10 text-primary",
  inactiveColor = "bg-primary/10 text-primary",
  disabled,
}: ToolbarToggleProps) {
  const color = active ? activeColor : inactiveColor;
  const icon = active ? activeIcon : inactiveIcon;
  const label = active ? activeLabel : inactiveLabel;

  if (!onChange) {
    return (
      <span className={cn(BASE, stripBg(color))}>
        {icon}
        {label}
      </span>
    );
  }

  return (
    <button
      type="button"
      onClick={() => onChange(!active)}
      disabled={disabled}
      className={cn(
        BASE,
        "transition-colors cursor-pointer",
        color,
        disabled && "opacity-40 cursor-not-allowed",
      )}
    >
      {icon}
      {label}
    </button>
  );
}
