import { useState } from "react";
import { TAG_COLORS } from "~/lib/tag-colors";
import { cn } from "~/lib/utils";

interface TagEditFormProps {
  initialName: string;
  initialColor: string;
  onSave: (name: string, color: string) => void;
  onCancel: () => void;
  autoFocus?: boolean;
  disabled?: boolean;
  compact?: boolean;
}

export function TagEditForm({
  initialName,
  initialColor,
  onSave,
  onCancel,
  autoFocus,
  disabled,
  compact,
}: TagEditFormProps) {
  const [name, setName] = useState(initialName);
  const [color, setColor] = useState(initialColor);

  return (
    <div className={cn("px-2 py-2", compact ? "space-y-1" : "space-y-2")}>
      <input
        type="text"
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && name.trim() && !disabled) onSave(name.trim(), color);
          if (e.key === "Escape") onCancel();
        }}
        placeholder="Tag name"
        className="w-full text-sm bg-input/50 border rounded px-2 py-1 outline-none focus:ring-1 focus:ring-ring"
        autoFocus={autoFocus}
        disabled={disabled}
      />
      <div className="flex gap-1">
        {TAG_COLORS.map((c) => (
          <button
            key={c.id}
            type="button"
            onClick={() => setColor(c.id)}
            className="size-5 rounded-full transition-all cursor-pointer"
            style={{
              backgroundColor: c.bg,
              outline: color === c.id ? `2px solid ${c.bg}` : "none",
              outlineOffset: "2px",
            }}
            title={c.label}
            disabled={disabled}
          />
        ))}
      </div>
      <div className="flex gap-1.5 justify-end">
        <button
          type="button"
          onClick={onCancel}
          className="text-xs px-2 py-0.5 text-muted-foreground hover:text-foreground cursor-pointer"
          disabled={disabled}
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={() => name.trim() && onSave(name.trim(), color)}
          disabled={disabled || !name.trim()}
          className="text-xs px-2 py-0.5 bg-primary text-primary-foreground rounded disabled:opacity-50 cursor-pointer"
        >
          Save
        </button>
      </div>
    </div>
  );
}
