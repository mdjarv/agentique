import { FileText, Terminal } from "lucide-react";
import { useCallback } from "react";
import { ScrollArea } from "~/components/ui/scroll-area";
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
    <div className="absolute bottom-full left-0 right-0 z-50 mb-1">
      <div className="rounded-lg border bg-popover text-popover-foreground shadow-md">
        <ScrollArea className="max-h-60">
          <div className="p-1">
            {items.map((item, i) => (
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
                {triggerType === "@" ? (
                  <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <Terminal className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                )}
                <span className="truncate">{item.label}</span>
                {item.source && (
                  <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                    {item.source}
                  </span>
                )}
              </button>
            ))}
          </div>
        </ScrollArea>
      </div>
    </div>
  );
}
