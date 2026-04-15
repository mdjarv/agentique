import { Search, X } from "lucide-react";
import { useCallback, useRef } from "react";
import { cn } from "~/lib/utils";

interface StreamSearchBarProps {
  value: string;
  onChange: (value: string) => void;
}

export function StreamSearchBar({ value, onChange }: StreamSearchBarProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  const handleClear = useCallback(() => {
    onChange("");
    inputRef.current?.focus();
  }, [onChange]);

  return (
    <div className="px-2 py-1.5">
      <div
        className={cn(
          "flex items-center gap-1.5 rounded-md border border-sidebar-border/50 bg-sidebar-accent/30 px-2 py-1",
          "focus-within:border-sidebar-border focus-within:bg-sidebar-accent/50 transition-colors",
        )}
      >
        <Search className="size-3.5 shrink-0 text-muted-foreground-faint" />
        <input
          ref={inputRef}
          type="text"
          placeholder="Filter sessions..."
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="flex-1 bg-transparent text-sm text-sidebar-foreground placeholder:text-muted-foreground-faint outline-none"
        />
        {value && (
          <button
            type="button"
            onClick={handleClear}
            className="shrink-0 text-muted-foreground-faint hover:text-muted-foreground cursor-pointer"
          >
            <X className="size-3" />
          </button>
        )}
      </div>
    </div>
  );
}
