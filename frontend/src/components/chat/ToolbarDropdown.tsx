import { Check, ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { cn } from "~/lib/utils";

export interface ToolbarDropdownOption {
  value: string;
  label: string;
  icon?: ReactNode;
  color?: string;
  bgColor?: string;
  description?: string;
}

interface ToolbarDropdownProps {
  value: string;
  onChange?: (value: string) => void;
  options: ToolbarDropdownOption[];
  icon?: ReactNode;
  triggerColor?: string;
  triggerBgColor?: string;
  readOnlyColor?: string;
}

const BASE =
  "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 shrink-0";

export function ToolbarDropdown({
  value,
  onChange,
  options,
  icon,
  triggerColor = "text-muted-foreground",
  triggerBgColor,
  readOnlyColor,
}: ToolbarDropdownProps) {
  const selected = options.find((o) => o.value === value);
  const label = selected?.label ?? value;
  const hasDescriptions = options.some((o) => o.description);

  if (!onChange) {
    return (
      <span className={cn(BASE, readOnlyColor ?? triggerColor)}>
        {icon}
        {label}
      </span>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(
          BASE,
          "transition-colors cursor-pointer",
          "hover:text-foreground hover:bg-muted/80 focus-visible:outline-none",
          triggerColor,
          triggerBgColor,
        )}
      >
        {icon}
        {label}
        <ChevronDown className="h-3 w-3" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className={hasDescriptions ? "min-w-[16rem]" : undefined}>
        {options.map((opt) => (
          <DropdownMenuItem
            key={opt.value || "__default"}
            onClick={() => onChange(opt.value)}
            className={cn("text-xs gap-2", hasDescriptions && "items-start")}
          >
            <Check
              className={cn(
                "h-3 w-3",
                hasDescriptions && "mt-0.5",
                opt.value === value ? "opacity-100" : "opacity-0",
              )}
            />
            {hasDescriptions ? (
              <div className="flex flex-col gap-0.5">
                <span className={cn("flex items-center gap-1", opt.color)}>
                  {opt.icon}
                  {opt.label}
                </span>
                <span className="text-[10px] text-muted-foreground">{opt.description}</span>
              </div>
            ) : (
              <span className={opt.color}>{opt.label}</span>
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
