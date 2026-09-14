import { DynamicIcon, type IconName } from "lucide-react/dynamic";
import { useCallback, useState } from "react";
import { IconGrid } from "~/components/icons/IconGrid";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { iconGroupsFor } from "~/lib/icon-catalog";
import { getProjectIcon } from "~/lib/project-icons";
import { cn } from "~/lib/utils";

const PROJECT_GROUPS = iconGroupsFor("project");

interface IconPickerProps {
  value: string;
  onChange: (iconId: string) => void;
}

export function IconPicker({ value, onChange }: IconPickerProps) {
  const [open, setOpen] = useState(false);

  const handleSelect = useCallback(
    (id: string) => {
      onChange(id);
      setOpen(false);
    },
    [onChange],
  );

  const CurrentIcon = value ? getProjectIcon(value) : undefined;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "size-9 rounded-lg border-2 transition-all flex items-center justify-center cursor-pointer",
            value
              ? "border-primary bg-primary/10"
              : "border-muted-foreground/20 hover:border-muted-foreground/40",
          )}
          title={value || "Choose icon"}
        >
          {CurrentIcon ? (
            <CurrentIcon className="size-4 text-foreground" />
          ) : value ? (
            <DynamicIcon name={value as IconName} className="size-4 text-foreground" />
          ) : (
            <span className="text-[10px] font-medium text-muted-foreground">Aa</span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent
        collisionPadding={8}
        className="flex h-[400px] max-h-[min(400px,calc(100vh-32px))] w-[312px] max-w-[calc(100vw-16px)] flex-col p-3"
      >
        <IconGrid
          groups={PROJECT_GROUPS}
          value={value}
          onSelect={handleSelect}
          clearTitle="None (use initials)"
          autoFocus
        />
      </PopoverContent>
    </Popover>
  );
}
