import { Check, ChevronDown } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
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
  /** Optional group key. Options sharing a key render under a single header. */
  group?: string;
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
        {renderItems(options, value, onChange, hasDescriptions)}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function renderItems(
  options: ToolbarDropdownOption[],
  value: string,
  onChange: (v: string) => void,
  hasDescriptions: boolean,
) {
  const grouped = options.some((o) => o.group);
  if (!grouped) {
    return options.map((opt) => renderItem(opt, value, onChange, hasDescriptions));
  }

  const groups: { key: string; items: ToolbarDropdownOption[] }[] = [];
  for (const opt of options) {
    const key = opt.group ?? "";
    const last = groups[groups.length - 1];
    if (last && last.key === key) {
      last.items.push(opt);
    } else {
      groups.push({ key, items: [opt] });
    }
  }

  return groups.map((g, i) => (
    <Fragment key={g.key || `__g${i}`}>
      {i > 0 && <DropdownMenuSeparator />}
      {g.key && (
        <div className="px-2 pt-1.5 pb-1 text-[10px] uppercase tracking-wide text-muted-foreground/70 select-none">
          {g.key}
        </div>
      )}
      {g.items.map((opt) => renderItem(opt, value, onChange, hasDescriptions))}
    </Fragment>
  ));
}

function renderItem(
  opt: ToolbarDropdownOption,
  value: string,
  onChange: (v: string) => void,
  hasDescriptions: boolean,
) {
  return (
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
  );
}
