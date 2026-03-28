import { Pencil, Plus, Trash2 } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { toast } from "sonner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { Tag } from "~/lib/generated-types";
import { createTag, deleteTag, updateTag } from "~/lib/project-actions";
import { TAG_COLORS, getTagColor } from "~/lib/tag-colors";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

interface TagManagerDropdownProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}

export function TagManagerDropdown({ open, onOpenChange, children }: TagManagerDropdownProps) {
  const tags = useAppStore((s) => s.tags);
  const ws = useWebSocket();
  const [editingTag, setEditingTag] = useState<Tag | null>(null);
  const [newTagName, setNewTagName] = useState("");
  const [newTagColor, setNewTagColor] = useState(TAG_COLORS[0].id);
  const [showNewForm, setShowNewForm] = useState(false);

  const handleCreate = useCallback(async () => {
    const name = newTagName.trim();
    if (!name) return;
    try {
      const tag = await createTag(ws, name, newTagColor);
      useAppStore.getState().addTag(tag);
      setNewTagName("");
      setShowNewForm(false);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to create tag"));
    }
  }, [ws, newTagName, newTagColor]);

  const handleUpdate = useCallback(
    async (tag: Tag, name: string, color: string) => {
      try {
        const updated = await updateTag(ws, tag.id, name, color);
        useAppStore.getState().updateTag(updated);
        setEditingTag(null);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to update tag"));
      }
    },
    [ws],
  );

  const handleDelete = useCallback(
    async (id: string) => {
      try {
        await deleteTag(ws, id);
        useAppStore.getState().removeTag(id);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to delete tag"));
      }
    },
    [ws],
  );

  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent side="right" align="start" sideOffset={8} className="w-56">
        <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground">Tags</div>
        {tags.map((tag) => {
          const color = getTagColor(tag.color);
          if (editingTag?.id === tag.id) {
            return (
              <TagEditForm
                key={tag.id}
                initialName={tag.name}
                initialColor={tag.color}
                onSave={(name, colorId) => handleUpdate(tag, name, colorId)}
                onCancel={() => setEditingTag(null)}
              />
            );
          }
          return (
            <div
              key={tag.id}
              className="flex items-center gap-2 px-2 py-1.5 rounded-sm hover:bg-accent group"
            >
              <span
                className="inline-block size-2.5 rounded-full shrink-0"
                style={{ backgroundColor: color.bg }}
              />
              <span className="text-sm flex-1 truncate">{tag.name}</span>
              <button
                type="button"
                onClick={() => setEditingTag(tag)}
                className="opacity-0 group-hover:opacity-100 p-0.5 text-muted-foreground hover:text-foreground transition-opacity cursor-pointer"
              >
                <Pencil className="size-3" />
              </button>
              <button
                type="button"
                onClick={() => handleDelete(tag.id)}
                className="opacity-0 group-hover:opacity-100 p-0.5 text-muted-foreground hover:text-destructive transition-opacity cursor-pointer"
              >
                <Trash2 className="size-3" />
              </button>
            </div>
          );
        })}
        {tags.length > 0 && <DropdownMenuSeparator />}
        {showNewForm ? (
          <TagEditForm
            initialName={newTagName}
            initialColor={newTagColor}
            onSave={(name, color) => {
              setNewTagName(name);
              setNewTagColor(color);
              handleCreate();
            }}
            onCancel={() => setShowNewForm(false)}
            autoFocus
          />
        ) : (
          <button
            type="button"
            onClick={() => setShowNewForm(true)}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-sm text-muted-foreground hover:text-foreground hover:bg-accent rounded-sm transition-colors cursor-pointer"
          >
            <Plus className="size-3.5" />
            New tag
          </button>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function TagEditForm({
  initialName,
  initialColor,
  onSave,
  onCancel,
  autoFocus,
}: {
  initialName: string;
  initialColor: string;
  onSave: (name: string, color: string) => void;
  onCancel: () => void;
  autoFocus?: boolean;
}) {
  const [name, setName] = useState(initialName);
  const [color, setColor] = useState(initialColor);

  return (
    <div className="px-2 py-2 space-y-2">
      <input
        type="text"
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && name.trim()) onSave(name.trim(), color);
          if (e.key === "Escape") onCancel();
        }}
        placeholder="Tag name"
        className="w-full text-sm bg-input/50 border rounded px-2 py-1 outline-none focus:ring-1 focus:ring-ring"
        autoFocus={autoFocus}
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
          />
        ))}
      </div>
      <div className="flex gap-1.5 justify-end">
        <button
          type="button"
          onClick={onCancel}
          className="text-xs px-2 py-0.5 text-muted-foreground hover:text-foreground cursor-pointer"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={() => name.trim() && onSave(name.trim(), color)}
          disabled={!name.trim()}
          className="text-xs px-2 py-0.5 bg-primary text-primary-foreground rounded disabled:opacity-50 cursor-pointer"
        >
          Save
        </button>
      </div>
    </div>
  );
}
