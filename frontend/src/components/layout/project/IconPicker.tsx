import { Search } from "lucide-react";
import { DynamicIcon, type IconName, iconNames } from "lucide-react/dynamic";
import { useCallback, useMemo, useRef, useState } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { cacheProjectIcon, getProjectIcon, PROJECT_ICONS } from "~/lib/project-icons";
import { cn } from "~/lib/utils";

const MAX_RESULTS = 36;

interface IconPickerProps {
  value: string;
  onChange: (iconId: string) => void;
}

export function IconPicker({ value, onChange }: IconPickerProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = useMemo(() => {
    const q = query.toLowerCase().trim();
    if (!q) return null; // null = show featured
    return iconNames.filter((n) => n.includes(q)).slice(0, MAX_RESULTS);
  }, [query]);

  const handleSelect = useCallback(
    async (id: string) => {
      onChange(id);
      setOpen(false);
      setQuery("");

      // Cache the icon so the rail can render it synchronously
      if (id && !getProjectIcon(id)) {
        try {
          const { dynamicIconImports } = await import("lucide-react/dynamic");
          const importFn = dynamicIconImports[id as IconName];
          if (importFn) {
            const mod = await importFn();
            if (mod?.default) {
              cacheProjectIcon(id, mod.default);
            }
          }
        } catch {
          // Rail falls back to initials
        }
      }
    },
    [onChange],
  );

  // Resolve current value for the trigger button
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
        className="w-[280px] p-3"
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          inputRef.current?.focus();
        }}
      >
        {/* Search input */}
        <div className="relative mb-3">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground-faint" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search icons..."
            className="w-full text-xs bg-input/50 border rounded pl-7 pr-2 py-1.5 outline-none focus:ring-1 focus:ring-ring placeholder:text-muted-foreground-faint"
          />
        </div>

        {/* Icon grid */}
        <div className="max-h-[240px] overflow-y-auto">
          {/* Clear option */}
          <button
            type="button"
            onClick={() => handleSelect("")}
            className={cn(
              "inline-flex size-8 rounded-md items-center justify-center transition-colors cursor-pointer",
              !value ? "bg-primary/10 text-foreground" : "hover:bg-muted/50 text-muted-foreground",
            )}
            title="None (use initials)"
          >
            <span className="text-[10px] font-medium">Aa</span>
          </button>

          {filtered === null ? (
            // Featured icons (no search query)
            PROJECT_ICONS.map((opt) => {
              const Icon = opt.icon;
              return (
                <button
                  key={opt.id}
                  type="button"
                  onClick={() => handleSelect(opt.id)}
                  className={cn(
                    "inline-flex size-8 rounded-md items-center justify-center transition-colors cursor-pointer",
                    value === opt.id
                      ? "bg-primary/10 text-foreground"
                      : "hover:bg-muted/50 text-muted-foreground",
                  )}
                  title={opt.id}
                >
                  <Icon className="size-4" />
                </button>
              );
            })
          ) : filtered.length === 0 ? (
            <p className="text-xs text-muted-foreground py-4 text-center">No icons found</p>
          ) : (
            // Search results with dynamic loading
            filtered.map((name) => {
              // Use static component if available, otherwise DynamicIcon
              const StaticIcon = getProjectIcon(name);
              return (
                <button
                  key={name}
                  type="button"
                  onClick={() => handleSelect(name)}
                  className={cn(
                    "inline-flex size-8 rounded-md items-center justify-center transition-colors cursor-pointer",
                    value === name
                      ? "bg-primary/10 text-foreground"
                      : "hover:bg-muted/50 text-muted-foreground",
                  )}
                  title={name}
                >
                  {StaticIcon ? (
                    <StaticIcon className="size-4" />
                  ) : (
                    <DynamicIcon name={name as IconName} className="size-4" />
                  )}
                </button>
              );
            })
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
