import { Plus, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Tag } from "~/lib/generated-types";
import { TAG_COLORS, getTagColor } from "~/lib/tag-colors";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

interface TagInputProps {
  assignedTagIds: string[];
  onAssign: (tagId: string) => void;
  onCreate: (name: string) => Promise<Tag>;
  onUnassign: (tagId: string) => void;
}

export function TagInput({ assignedTagIds, onAssign, onCreate, onUnassign }: TagInputProps) {
  const tags = useAppStore((s) => s.tags);
  const [input, setInput] = useState("");
  const [open, setOpen] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [creating, setCreating] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const assignedTags = useMemo(
    () => tags.filter((t) => assignedTagIds.includes(t.id)),
    [tags, assignedTagIds],
  );

  const suggestions = useMemo(() => {
    const query = input.toLowerCase().trim();
    return tags.filter((t) => {
      if (assignedTagIds.includes(t.id)) return false;
      if (!query) return true;
      return t.name.toLowerCase().includes(query);
    });
  }, [tags, assignedTagIds, input]);

  const exactMatch = useMemo(() => {
    const query = input.trim().toLowerCase();
    if (!query) return true;
    return tags.some((t) => t.name.toLowerCase() === query);
  }, [tags, input]);

  const showCreate = input.trim().length > 0 && !exactMatch;
  const totalItems = suggestions.length + (showCreate ? 1 : 0);

  const handleSelect = useCallback(
    async (index: number) => {
      const suggestion = suggestions[index];
      if (suggestion) {
        onAssign(suggestion.id);
        setInput("");
        setOpen(false);
        return;
      }
      if (showCreate && !creating) {
        setCreating(true);
        try {
          const tag = await onCreate(input.trim());
          onAssign(tag.id);
          setInput("");
          setOpen(false);
        } finally {
          setCreating(false);
        }
      }
    },
    [suggestions, showCreate, creating, input, onAssign, onCreate],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!open && e.key !== "Escape") {
        if (e.key === "ArrowDown" || e.key === "ArrowUp" || (e.key === "Enter" && input.trim())) {
          setOpen(true);
        }
      }

      if (!open) return;

      switch (e.key) {
        case "ArrowDown":
          e.preventDefault();
          setSelectedIndex((i) => (i + 1) % totalItems);
          break;
        case "ArrowUp":
          e.preventDefault();
          setSelectedIndex((i) => (i - 1 + totalItems) % totalItems);
          break;
        case "Enter":
          e.preventDefault();
          if (totalItems > 0) handleSelect(selectedIndex);
          break;
        case "Escape":
          e.preventDefault();
          setOpen(false);
          break;
      }
    },
    [open, totalItems, selectedIndex, handleSelect, input],
  );

  // Scroll selected item into view
  useEffect(() => {
    if (!open || !listRef.current) return;
    const item = listRef.current.children[selectedIndex] as HTMLElement | undefined;
    item?.scrollIntoView({ block: "nearest" });
  }, [open, selectedIndex]);

  return (
    <div className="px-3 py-2 border-b">
      <div className="text-xs font-semibold text-muted-foreground mb-1.5">Tags</div>

      {/* Assigned tag pills */}
      {assignedTags.length > 0 && (
        <div className="flex flex-wrap gap-1 mb-1.5">
          {assignedTags.map((tag) => {
            const color = getTagColor(tag.color);
            return (
              <span
                key={tag.id}
                className="inline-flex items-center gap-0.5 rounded-full pl-2 pr-1 py-0.5 text-[10px] font-medium leading-none"
                style={{ backgroundColor: color.bg, color: color.text }}
              >
                {tag.name}
                <button
                  type="button"
                  onClick={() => onUnassign(tag.id)}
                  className="rounded-full p-0.5 hover:bg-black/20 transition-colors cursor-pointer"
                >
                  <X className="size-2.5" />
                </button>
              </span>
            );
          })}
        </div>
      )}

      {/* Autocomplete input */}
      <div className="relative">
        <input
          ref={inputRef}
          type="text"
          value={input}
          onChange={(e) => {
            setInput(e.target.value);
            setSelectedIndex(0);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => {
            // Delay to allow click on suggestion
            setTimeout(() => setOpen(false), 150);
          }}
          onKeyDown={handleKeyDown}
          placeholder="Add tag..."
          className="w-full text-xs bg-input/50 border rounded px-2 py-1 outline-none focus:ring-1 focus:ring-ring placeholder:text-muted-foreground/60"
        />

        {/* Dropdown */}
        {open && totalItems > 0 && (
          <div
            ref={listRef}
            className="absolute left-0 right-0 top-full z-50 mt-1 max-h-40 overflow-y-auto rounded-md border bg-popover text-popover-foreground shadow-md"
          >
            {suggestions.map((tag, i) => {
              const color = getTagColor(tag.color);
              return (
                <button
                  key={tag.id}
                  type="button"
                  onMouseDown={(e) => {
                    e.preventDefault();
                    handleSelect(i);
                  }}
                  className={cn(
                    "flex items-center gap-2 w-full px-2 py-1.5 text-xs cursor-pointer",
                    i === selectedIndex ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
                  )}
                >
                  <span
                    className="inline-block size-2.5 rounded-full shrink-0"
                    style={{ backgroundColor: color.bg }}
                  />
                  <span className="truncate">{tag.name}</span>
                </button>
              );
            })}
            {showCreate && (
              <button
                type="button"
                onMouseDown={(e) => {
                  e.preventDefault();
                  handleSelect(suggestions.length);
                }}
                className={cn(
                  "flex items-center gap-2 w-full px-2 py-1.5 text-xs cursor-pointer",
                  suggestions.length === selectedIndex
                    ? "bg-accent text-accent-foreground"
                    : "hover:bg-accent/50",
                )}
              >
                <Plus className="size-3 shrink-0 text-muted-foreground" />
                <span>
                  Create &ldquo;<span className="font-medium">{input.trim()}</span>&rdquo;
                </span>
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** Pick a color for a new tag by cycling through the palette. */
export function nextTagColor(existingCount: number): string {
  return (TAG_COLORS[existingCount % TAG_COLORS.length] ?? TAG_COLORS[0]).id;
}
