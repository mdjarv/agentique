import { FolderOpen } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { IconSlot } from "./IconSlot";
import { SidebarRow } from "./SidebarRow";
import { LEVEL } from "./types";

export function InlineRename({
  initialValue,
  onConfirm,
  onCancel,
}: {
  initialValue: string;
  onConfirm: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(initialValue);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.select();
  }, []);

  const handleSubmit = () => {
    const trimmed = value.trim();
    if (trimmed && trimmed !== initialValue) {
      onConfirm(trimmed);
    } else {
      onCancel();
    }
  };

  return (
    <SidebarRow as="div" indent={LEVEL.folder} className="gap-1.5">
      <IconSlot>
        <FolderOpen className="size-3.5 text-muted-foreground" />
      </IconSlot>
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={handleSubmit}
        onKeyDown={(e) => {
          if (e.key === "Enter") handleSubmit();
          if (e.key === "Escape") onCancel();
        }}
        className="flex-1 text-[11px] font-bold uppercase tracking-wider bg-sidebar-accent rounded px-1.5 py-0.5 outline-none ring-1 ring-primary/50 text-foreground"
        autoFocus
      />
    </SidebarRow>
  );
}
